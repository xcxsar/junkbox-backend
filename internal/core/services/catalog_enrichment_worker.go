package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// CatalogEnrichmentWorkerConfig ajusta el worker. Los valores en cero usan
// los defaults de NewCatalogEnrichmentWorker.
type CatalogEnrichmentWorkerConfig struct {
	QueueSize       int           // capacidad del canal (default 5000)
	BatchSize       int           // IDs reservados por vez (default 20)
	FlushInterval   time.Duration // procesa lo acumulado tras este tiempo (default 2s)
	RequestInterval time.Duration // pausa entre llamadas a Spotify (default 250ms)
	LeaseDuration   time.Duration // reserva frente a otras réplicas (default 5m)
	ProviderPause   time.Duration // pausa ante un error del proveedor sin Retry-After (default 1m)
}

// CatalogEnrichmentWorker completa en segundo plano lo que la búsqueda no
// alcanzó a enriquecer dentro de su presupuesto. Trabaja bajo demanda (no
// relee la BD): si un job se pierde, la próxima búsqueda que muestre la
// entidad lo vuelve a intentar. Como Spotify no tiene endpoints por lotes en
// Development Mode, hace una llamada por ID y en serie, con RequestInterval
// entre llamadas para no competir con las búsquedas por la cuota.
type CatalogEnrichmentWorker struct {
	enricher *CatalogEnricher
	cfg      CatalogEnrichmentWorkerConfig
	queue    chan domain.EnrichmentJob

	// Estado propio de la goroutine de Run.
	buffer      map[domain.EnrichmentJob]bool      // deduplica lo que espera reserva
	order       []domain.EnrichmentJob             // orden de llegada de buffer
	claimed     []domain.EnrichmentJob             // reservados, pendientes de consultar
	handled     map[domain.EnrichmentJob]time.Time // job → cuándo se procesó
	pausedUntil time.Time

	dropped sync.Once // avisa una sola vez de que la cola se llenó
}

// NewCatalogEnrichmentWorker crea el worker. Run debe lanzarse en una
// goroutine.
func NewCatalogEnrichmentWorker(enricher *CatalogEnricher, cfg CatalogEnrichmentWorkerConfig) *CatalogEnrichmentWorker {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 5000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	if cfg.RequestInterval <= 0 {
		cfg.RequestInterval = 250 * time.Millisecond
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 5 * time.Minute
	}
	if cfg.ProviderPause <= 0 {
		cfg.ProviderPause = time.Minute
	}
	return &CatalogEnrichmentWorker{
		enricher: enricher,
		cfg:      cfg,
		queue:    make(chan domain.EnrichmentJob, cfg.QueueSize),
		buffer:   map[domain.EnrichmentJob]bool{},
		handled:  map[domain.EnrichmentJob]time.Time{},
	}
}

// Enqueue encola jobs sin bloquear. Si la cola está llena se descartan: la
// próxima búsqueda que los necesite los vuelve a intentar.
func (w *CatalogEnrichmentWorker) Enqueue(jobs ...domain.EnrichmentJob) {
	for _, j := range jobs {
		if j.SpotifyID == "" {
			continue
		}
		select {
		case w.queue <- j:
		default:
			w.dropped.Do(func() {
				log.Printf("worker de enriquecimiento: cola llena; se descartan jobs")
			})
		}
	}
}

// Run procesa la cola hasta que ctx se cancela. Lo pendiente al cancelarse
// se descarta: sus reservas vencen y otra búsqueda lo volverá a pedir.
func (w *CatalogEnrichmentWorker) Run(ctx context.Context) {
	flush := time.NewTicker(w.cfg.FlushInterval)
	defer flush.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case j := <-w.queue:
			w.addFromQueue(j)
			if len(w.order) >= w.cfg.BatchSize {
				w.flush(ctx)
			}
		case <-flush.C:
			w.forgetHandled()
			w.flush(ctx)
		}
	}
}

func (w *CatalogEnrichmentWorker) add(j domain.EnrichmentJob) {
	if !w.buffer[j] {
		w.buffer[j] = true
		w.order = append(w.order, j)
	}
}

// addFromQueue ignora los jobs ya procesados: una búsqueda repetida no
// debe gastar cuota de Spotify.
func (w *CatalogEnrichmentWorker) addFromQueue(j domain.EnrichmentJob) {
	if _, done := w.handled[j]; !done {
		w.add(j)
	}
}

func (w *CatalogEnrichmentWorker) forgetHandled() {
	for j, at := range w.handled {
		if time.Since(at) > handledTTL {
			delete(w.handled, j)
		}
	}
}

// flush procesa los jobs reservados y reserva más hasta vaciar el búfer.
// Se detiene si el proveedor pide una pausa; lo reservado queda para el
// siguiente flush.
func (w *CatalogEnrichmentWorker) flush(ctx context.Context) {
	for ctx.Err() == nil && !time.Now().Before(w.pausedUntil) {
		if len(w.claimed) == 0 {
			if len(w.order) == 0 {
				return
			}
			w.claimNext(ctx)
			continue
		}
		if !w.process(ctx, w.claimed[0]) {
			return
		}
		w.claimed = w.claimed[1:]
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.cfg.RequestInterval):
		}
	}
}

// claimNext reserva hasta BatchSize jobs del búfer. Los que no se pueden
// reservar (ya enriquecidos, sin intentos o reservados por otra réplica) se
// descartan. Las canciones van primero porque pueden encadenar a su
// artista.
func (w *CatalogEnrichmentWorker) claimNext(ctx context.Context) {
	n := min(w.cfg.BatchSize, len(w.order))
	var artistIDs, trackIDs []string
	for _, j := range w.order[:n] {
		delete(w.buffer, j)
		if j.Kind == domain.EnrichTrack {
			trackIDs = append(trackIDs, j.SpotifyID)
		} else {
			artistIDs = append(artistIDs, j.SpotifyID)
		}
	}
	w.order = w.order[n:]

	store := w.enricher.store
	if len(trackIDs) > 0 {
		ids, err := store.ClaimSongsForLinking(ctx, sqlc.ClaimSongsForLinkingParams{
			LeaseSeconds: w.cfg.LeaseDuration.Seconds(),
			TrackIds:     trackIDs,
			MaxAttempts:  MaxEnrichAttempts,
		})
		if err != nil {
			log.Printf("worker de enriquecimiento: reservar canciones: %v", err)
		}
		for _, id := range ids {
			w.claimed = append(w.claimed, domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: id.String})
		}
	}
	if len(artistIDs) > 0 {
		ids, err := store.ClaimArtistsForEnrichment(ctx, sqlc.ClaimArtistsForEnrichmentParams{
			LeaseSeconds: w.cfg.LeaseDuration.Seconds(),
			SpotifyIds:   artistIDs,
			MaxAttempts:  MaxEnrichAttempts,
		})
		if err != nil {
			log.Printf("worker de enriquecimiento: reservar artistas: %v", err)
		}
		for _, id := range ids {
			w.claimed = append(w.claimed, domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: id.String})
		}
	}
}

// process consulta y guarda un job. Devuelve false si hay que dejarlo para
// después (Spotify pidió esperar o ctx se canceló).
func (w *CatalogEnrichmentWorker) process(ctx context.Context, j domain.EnrichmentJob) bool {
	var err error
	if j.Kind == domain.EnrichTrack {
		var song domain.ImportedSong
		song, err = w.enricher.LinkTrack(ctx, j.SpotifyID)
		if err == nil && song.ArtistImageURL == nil && artistEnrichable(song.ArtistSpotifyID, song.ArtistEnriched, song.ArtistEnrichAttempts) {
			w.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: song.ArtistSpotifyID})
		}
	} else {
		_, err = w.enricher.EnrichArtist(ctx, j.SpotifyID)
	}

	switch {
	case err == nil, errors.Is(err, domain.ErrNotFound):
	case ctx.Err() != nil:
		return false
	default:
		pause := w.cfg.ProviderPause
		var ra ports.RetryAfterError
		if errors.As(err, &ra) && ra.RetryAfter() > 0 {
			pause = ra.RetryAfter()
		}
		w.pausedUntil = time.Now().Add(pause)
		log.Printf("worker de enriquecimiento: %s %s falló (pausa de %s): %v", j.Kind, j.SpotifyID, pause, err)
		if rateLimited(err) {
			return false // se reintenta tras la pausa, sin consumir intento
		}
		// El enriquecedor ya registró el intento fallido con backoff.
	}
	w.handled[j] = time.Now()
	return true
}
