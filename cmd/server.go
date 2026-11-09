package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xcxsar/junkbox-backend/config"
	httpapi "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http"
	chimw "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/freqblog"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/logmailer"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/postgres"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/security"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/smtp"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/spotify"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Inicia la API REST",
	Run: func(cmd *cobra.Command, args []string) {
		runServer(cmd)
	},
}

func init() {
	serverCmd.Flags().String("addr", "", "dirección de escucha HTTP (default: config HTTP_ADDR o :8080)")
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command) {
	// 1. Configuración (validación dura de JWT_SECRET en config.Load).
	cfg, err := config.Load(configFile)
	if err != nil {
		fatalf("config: %v", err)
	}
	if addr, _ := cmd.Flags().GetString("addr"); addr != "" {
		cfg.HTTPAddr = addr
	}

	// 2. Pool de conexiones y queries SQLC.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.Options{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		fatalf("db: %v", err)
	}
	defer db.Close()
	queries := sqlc.New(db)

	// 3. Adaptadores de seguridad y caso de uso de auth.
	jwtManager := security.NewJWTManager(cfg.JWTSecret, cfg.AccessTTL)
	hasher := security.NewHasher(security.DefaultBcryptCost)
	revocationChecker := chimw.NewRevocationChecker(queries)
	authSvc := services.NewAuthService(queries, queries, queries, hasher, jwtManager, cfg.RefreshTTL)

	// 3b. Correos de cuenta (recuperación de contraseña y verificación): se
	// encolan en email_outbox y un worker los envía con reintentos.
	mailer, err := newMailer(cfg)
	if err != nil {
		fatalf("correo: %v", err)
	}
	accountSvc := services.NewAccountEmailService(queries, queries, postgres.NewAccountTx(db), hasher, services.AccountEmailConfig{
		FrontendURL: cfg.FrontendURL,
		ResetTTL:    cfg.PasswordResetTTL,
		VerifyTTL:   cfg.EmailVerificationTTL,
	})
	authSvc.SetVerificationSender(accountSvc)
	outboxWorker := services.NewEmailOutboxWorker(postgres.NewOutboxStore(db), mailer, services.EmailOutboxWorkerConfig{})
	outboxDone := make(chan struct{})
	go func() {
		defer close(outboxDone)
		outboxWorker.Run(ctx)
	}()

	// 4. Catálogo musical: Spotify como respaldo de la búsqueda y worker de
	// embeddings con FreqBlog. Ambos son opcionales.
	var provider ports.MusicProvider
	if cfg.SpotifyEnabled() {
		provider = spotify.New(spotify.Config{ClientID: cfg.SpotifyClientID, ClientSecret: cfg.SpotifyClientSecret})
	} else {
		log.Println("aviso: sin SPOTIFY_CLIENT_ID/SPOTIFY_CLIENT_SECRET; la búsqueda será solo local")
	}

	var queue ports.EmbeddingQueue
	workerDone := make(chan struct{})
	if cfg.FreqBlogAPIKey != "" {
		worker := services.NewAudioFeaturesWorker(freqblog.New(freqblog.Config{APIKey: cfg.FreqBlogAPIKey}), queries, services.AudioFeaturesWorkerConfig{})
		queue = worker
		go func() {
			defer close(workerDone)
			worker.Run(ctx)
		}()
	} else {
		close(workerDone)
		log.Println("aviso: sin FREQBLOG_API_KEY; las canciones importadas quedarán sin embedding")
	}
	musicSvc := services.NewMusicService(queries, postgres.NewCatalogImporter(db), provider, queue, cfg.SearchFallbackThreshold)
	ratingSvc := services.NewRatingService(queries, musicSvc)
	reviewSvc := services.NewReviewService(queries, musicSvc)

	// 5. Purga periódica de la denylist, los tokens de correo y el outbox.
	go purgeRevokedTokens(ctx, queries)

	// 6. Router y servidor HTTP con shutdown elegante. En Kubernetes el
	// hostname es el nombre del pod: identifica la réplica que respondió.
	instance, _ := os.Hostname()
	handler := httpapi.NewRouter(httpapi.RouterOptions{
		Auth:           httpapi.NewAuthHandler(authSvc, jwtManager),
		Music:          httpapi.NewMusicHandler(musicSvc),
		Account:        httpapi.NewAccountHandler(accountSvc),
		Ratings:        httpapi.NewRatingHandler(ratingSvc),
		Reviews:        httpapi.NewReviewHandler(reviewSvc),
		JWT:            jwtManager,
		Revocation:     revocationChecker,
		AllowedOrigins: cfg.CORSAllowedOrigins,
		Instance:       instance,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("servidor HTTP escuchando en %s (instancia %s)", cfg.HTTPAddr, instance)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("señal recibida, cerrando servidor…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fatalf("shutdown: %v", err)
	}
	// El worker termina de guardar su último lote antes de cerrar el pool.
	<-workerDone
	<-outboxDone
	log.Println("servidor detenido")
}

// purgeRevokedTokens borra periódicamente los access tokens revocados que
// ya expiraron (no aportan nada una vez pasado su exp), los tokens de correo
// vencidos y los correos ya resueltos del outbox. Se detiene con ctx.
func purgeRevokedTokens(ctx context.Context, q *sqlc.Queries) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge(ctx, "tokens revocados", q.PurgeExpiredRevokedTokens)
			purge(ctx, "tokens de correo expirados", q.PurgeExpiredEmailTokens)
			purge(ctx, "outbox de correo", q.PurgeOldOutbox)
		}
	}
}

func purge(ctx context.Context, what string, fn func(context.Context) (int64, error)) {
	n, err := fn(ctx)
	if err != nil {
		log.Printf("purga de %s: %v", what, err)
		return
	}
	if n > 0 {
		log.Printf("purga de %s: %d filas eliminadas", what, n)
	}
}

// newMailer construye el driver de correo elegido en MAIL_DRIVER.
func newMailer(cfg *config.Config) (ports.Mailer, error) {
	if cfg.MailDriver != config.MailDriverSMTP {
		log.Println("aviso: MAIL_DRIVER=log; los correos se escriben en la consola y no se envían")
		return logmailer.Mailer{}, nil
	}
	return smtp.New(smtp.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.MailFrom,
		TLS:      cfg.SMTPTLS,
	})
}
