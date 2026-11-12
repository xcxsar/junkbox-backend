package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

const (
	// MaxEnrichAttempts es cuántos errores del proveedor se toleran al
	// enriquecer un artista o enlazar una canción antes de rendirse.
	MaxEnrichAttempts = 5
	// enrichRetryBase es la base del backoff tras un error: base·2^intentos.
	enrichRetryBase = 30 * time.Second
	// persistTimeout acota las escrituras posteriores a una respuesta de
	// Spotify. Corren aunque el presupuesto de la búsqueda haya vencido,
	// para no perder una llamada ya hecha.
	persistTimeout = 5 * time.Second
)

// errEnrichStopped: Spotify pidió esperar y no se lanzan más llamadas en
// esta búsqueda.
var errEnrichStopped = errors.New("music: enriquecimiento detenido por límite de Spotify")

// CatalogEnricher completa con Spotify lo que el catálogo no tiene: la
// imagen de un artista (GET /v1/artists/{id}) y el álbum de una canción del
// CSV (GET /v1/tracks/{id}). Lo usan la búsqueda (síncrono, con
// presupuesto) y el worker de respaldo. Es seguro para uso concurrente.
type CatalogEnricher struct {
	provider   ports.MusicMetadataProvider
	store      ports.EnrichmentStore
	importer   ports.CatalogImporter
	embeddings ports.EmbeddingQueue // nil: no se encolan embeddings
}

// NewCatalogEnricher crea el enriquecedor. embeddings puede ser nil.
func NewCatalogEnricher(provider ports.MusicMetadataProvider, store ports.EnrichmentStore, importer ports.CatalogImporter, embeddings ports.EmbeddingQueue) *CatalogEnricher {
	return &CatalogEnricher{provider: provider, store: store, importer: importer, embeddings: embeddings}
}

// EnrichArtist pide el artista a Spotify y guarda su imagen, géneros y
// popularidad. Devuelve la imagen resultante (nil si Spotify no tiene). Un
// artista que ya no existe en Spotify se marca como consultado, sin error.
func (e *CatalogEnricher) EnrichArtist(ctx context.Context, spotifyID string) (*string, error) {
	a, err := e.provider.GetArtist(ctx, spotifyID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		e.markArtistFailed(ctx, spotifyID, err)
		return nil, err
	}

	wctx, cancel := persistContext(ctx)
	defer cancel()
	genres := a.Genres
	if genres == nil {
		genres = []string{}
	}
	img, err := e.store.UpdateArtistEnrichment(wctx, sqlc.UpdateArtistEnrichmentParams{
		Genres:     genres,
		Popularity: toNullInt32(a.Popularity),
		ImageUrl:   toNullString(a.ImageURL),
		SpotifyID:  nullString(spotifyID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return a.ImageURL, nil
	}
	if err != nil {
		return nil, fmt.Errorf("music: guardar artista enriquecido %s: %w", spotifyID, err)
	}
	return stringPtr(img), nil
}

// LinkTrack pide la canción a Spotify y la importa: enlaza la fila del
// catálogo con su álbum (y portada) y con su artista, sin pisar los datos
// históricos. Si no existe en Spotify devuelve domain.ErrNotFound y no se
// vuelve a intentar.
func (e *CatalogEnricher) LinkTrack(ctx context.Context, trackID string) (domain.ImportedSong, error) {
	t, err := e.provider.GetTrack(ctx, trackID)
	if err != nil {
		e.markSongFailed(ctx, trackID, err)
		return domain.ImportedSong{}, err
	}
	// Spotify puede responder con otra versión de la canción (relinking):
	// se guarda bajo el ID pedido para enlazar la fila existente.
	t.SpotifyID = trackID

	wctx, cancel := persistContext(ctx)
	defer cancel()
	songs, err := e.importer.ImportTracks(wctx, []domain.ImportedTrack{t})
	if err != nil {
		return domain.ImportedSong{}, fmt.Errorf("music: enlazar canción %s: %w", trackID, err)
	}
	if len(songs) != 1 {
		return domain.ImportedSong{}, fmt.Errorf("music: enlazar canción %s: el importador devolvió %d canciones", trackID, len(songs))
	}
	song := songs[0]
	if song.PendingEmbedding && e.embeddings != nil {
		e.embeddings.Enqueue(embeddingJob(song))
	}
	return song, nil
}

// enrichment es lo que resolvió Resolve.
type enrichment struct {
	artistImages map[string]*string             // spotify_id del artista → imagen (nil: Spotify no tiene)
	songs        map[string]domain.ImportedSong // track_id → canción ya enlazada
}

// Resolve ejecuta los jobs en paralelo (como máximo concurrency a la vez)
// hasta que terminan o vence budget. Al enlazar una canción pide también la
// imagen de su artista si falta. Devuelve lo resuelto y los jobs que no
// terminaron (por tiempo, por el límite de Spotify o por un error), para el
// worker de respaldo.
func (e *CatalogEnricher) Resolve(ctx context.Context, jobs []domain.EnrichmentJob, budget time.Duration, concurrency int) (enrichment, []domain.EnrichmentJob) {
	res := enrichment{artistImages: map[string]*string{}, songs: map[string]domain.ImportedSong{}}
	if len(jobs) == 0 {
		return res, nil
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	type outcome struct {
		job   domain.EnrichmentJob
		image *string
		song  domain.ImportedSong
		err   error
	}
	// Cada job lanzado envía exactamente un outcome y una canción lanza a lo
	// sumo un artista: con 2·len(jobs) ninguna goroutine se bloquea aunque
	// ya no se lea el canal.
	results := make(chan outcome, 2*len(jobs))
	sem := make(chan struct{}, concurrency)
	var stopped atomic.Bool

	var order []domain.EnrichmentJob
	launched := map[domain.EnrichmentJob]bool{}
	finished := map[domain.EnrichmentJob]bool{}
	running := 0
	launch := func(j domain.EnrichmentJob) {
		if launched[j] {
			return
		}
		launched[j] = true
		order = append(order, j)
		if stopped.Load() {
			return // queda para el worker
		}
		running++
		go func() {
			o := outcome{job: j}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				o.err = ctx.Err()
				results <- o
				return
			}
			switch {
			case stopped.Load():
				o.err = errEnrichStopped
			case j.Kind == domain.EnrichTrack:
				o.song, o.err = e.LinkTrack(ctx, j.SpotifyID)
			default:
				o.image, o.err = e.EnrichArtist(ctx, j.SpotifyID)
			}
			// Antes de liberar el semáforo, para que ningún job en espera
			// alcance a llamar a Spotify.
			if rateLimited(o.err) {
				stopped.Store(true)
			}
			results <- o
		}()
	}

	for _, j := range jobs {
		launch(j)
	}
wait:
	for running > 0 {
		select {
		case o := <-results:
			running--
			if o.err != nil {
				switch {
				case errors.Is(o.err, domain.ErrNotFound):
					finished[o.job] = true // no existe en Spotify: nada que reintentar
				case rateLimited(o.err):
					// stopped ya se marcó en la goroutine; queda para el worker.
				case ctx.Err() == nil && !errors.Is(o.err, errEnrichStopped):
					log.Printf("búsqueda: enriquecer %s %s: %v", o.job.Kind, o.job.SpotifyID, o.err)
				}
				continue
			}
			finished[o.job] = true
			if o.job.Kind == domain.EnrichTrack {
				res.songs[o.job.SpotifyID] = o.song
				if o.song.ArtistImageURL == nil && artistEnrichable(o.song.ArtistSpotifyID, o.song.ArtistEnriched, o.song.ArtistEnrichAttempts) {
					launch(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: o.song.ArtistSpotifyID})
				}
			} else {
				res.artistImages[o.job.SpotifyID] = o.image
			}
		case <-ctx.Done():
			break wait
		}
	}

	var left []domain.EnrichmentJob
	for _, j := range order {
		if !finished[j] {
			left = append(left, j)
		}
	}
	return res, left
}

// markArtistFailed registra un error del proveedor con backoff. No cuenta
// los cortes por presupuesto ni los límites de Spotify, que no son culpa
// del artista.
func (e *CatalogEnricher) markArtistFailed(ctx context.Context, spotifyID string, err error) {
	if !providerFailure(ctx, err) {
		return
	}
	wctx, cancel := persistContext(ctx)
	defer cancel()
	if err := e.store.MarkArtistEnrichmentFailed(wctx, sqlc.MarkArtistEnrichmentFailedParams{
		RetryBaseSeconds: enrichRetryBase.Seconds(),
		SpotifyID:        nullString(spotifyID),
	}); err != nil {
		log.Printf("music: registrar intento fallido del artista %s: %v", spotifyID, err)
	}
}

// markSongFailed registra un error del proveedor con backoff. Si la canción
// no existe en Spotify, agota los intentos.
func (e *CatalogEnricher) markSongFailed(ctx context.Context, trackID string, err error) {
	giveUp := errors.Is(err, domain.ErrNotFound)
	if !giveUp && !providerFailure(ctx, err) {
		return
	}
	wctx, cancel := persistContext(ctx)
	defer cancel()
	if err := e.store.MarkSongLinkFailed(wctx, sqlc.MarkSongLinkFailedParams{
		GiveUp:           giveUp,
		MaxAttempts:      MaxEnrichAttempts,
		RetryBaseSeconds: enrichRetryBase.Seconds(),
		TrackID:          nullString(trackID),
	}); err != nil {
		log.Printf("music: registrar intento fallido de la canción %s: %v", trackID, err)
	}
}

// artistEnrichable indica si vale la pena pedir a Spotify la imagen de un
// artista que no la tiene.
func artistEnrichable(spotifyID string, enriched bool, attempts int32) bool {
	return spotifyIDPattern.MatchString(spotifyID) && !enriched && attempts < MaxEnrichAttempts
}

// providerFailure distingue un error del proveedor (red, 5xx) de un corte
// por contexto o de un límite de Spotify.
func providerFailure(ctx context.Context, err error) bool {
	return ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !rateLimited(err)
}

// rateLimited indica si el proveedor pidió esperar (HTTP 429 con Retry-After).
func rateLimited(err error) bool {
	var ra ports.RetryAfterError
	return errors.As(err, &ra) && ra.RetryAfter() > 0
}

// persistContext deriva un contexto para escribir en la BD que no se
// cancela con ctx, acotado por persistTimeout.
func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

func embeddingJob(song domain.ImportedSong) domain.EmbeddingJob {
	job := domain.EmbeddingJob{TrackID: song.SpotifyID, Attempts: song.EmbeddingAttempts}
	if song.ISRC != nil {
		job.ISRC = *song.ISRC
	}
	return job
}

func toNullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
