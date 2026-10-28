package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xcxsar/junkbox-backend/config"
	httpapi "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http"
	chimw "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/postgres"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/security"
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

	// 4. Purga periódica de la denylist de access tokens expirados.
	go purgeRevokedTokens(ctx, queries)

	// 5. Router y servidor HTTP con shutdown elegante.
	handler := httpapi.NewRouter(httpapi.RouterOptions{
		Auth:       httpapi.NewAuthHandler(authSvc),
		JWT:        jwtManager,
		Revocation: revocationChecker,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("servidor HTTP escuchando en %s", cfg.HTTPAddr)
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
	log.Println("servidor detenido")
}

// purgeRevokedTokens borra periódicamente los access tokens revocados que
// ya expiraron (no aportan nada una vez pasado su exp). Se detiene con ctx.
func purgeRevokedTokens(ctx context.Context, q *sqlc.Queries) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := q.PurgeExpiredRevokedTokens(ctx)
			if err != nil {
				log.Printf("purga de tokens revocados: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("purga de tokens revocados: %d filas eliminadas", n)
			}
		}
	}
}
