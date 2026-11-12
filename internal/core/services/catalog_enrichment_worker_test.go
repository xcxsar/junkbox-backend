package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes del enriquecimiento (compartidos con music_service_test.go) ---

// fakeMetadata simula GET /v1/artists/{id} y GET /v1/tracks/{id}. Los IDs
// que no están en los mapas devuelven domain.ErrNotFound.
type fakeMetadata struct {
	mu          sync.Mutex
	artists     map[string]domain.ImportedArtist
	tracks      map[string]domain.ImportedTrack
	err         error
	delay       time.Duration
	artistCalls map[string]int
	trackCalls  map[string]int
}

func newFakeMetadata() *fakeMetadata {
	return &fakeMetadata{
		artists:     map[string]domain.ImportedArtist{},
		tracks:      map[string]domain.ImportedTrack{},
		artistCalls: map[string]int{},
		trackCalls:  map[string]int{},
	}
}

func (f *fakeMetadata) wait(ctx context.Context) error {
	if f.delay == 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(f.delay):
		return nil
	}
}

func (f *fakeMetadata) GetArtist(ctx context.Context, id string) (domain.ImportedArtist, error) {
	f.mu.Lock()
	f.artistCalls[id]++
	err := f.err
	a, ok := f.artists[id]
	f.mu.Unlock()
	if err := f.wait(ctx); err != nil {
		return domain.ImportedArtist{}, err
	}
	if err != nil {
		return domain.ImportedArtist{}, err
	}
	if !ok {
		return domain.ImportedArtist{}, fmt.Errorf("artista %s: %w", id, domain.ErrNotFound)
	}
	return a, nil
}

func (f *fakeMetadata) GetTrack(ctx context.Context, id string) (domain.ImportedTrack, error) {
	f.mu.Lock()
	f.trackCalls[id]++
	err := f.err
	t, ok := f.tracks[id]
	f.mu.Unlock()
	if err := f.wait(ctx); err != nil {
		return domain.ImportedTrack{}, err
	}
	if err != nil {
		return domain.ImportedTrack{}, err
	}
	if !ok {
		return domain.ImportedTrack{}, fmt.Errorf("canción %s: %w", id, domain.ErrNotFound)
	}
	return t, nil
}

func (f *fakeMetadata) calls() (artists, tracks int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.artistCalls {
		artists += n
	}
	for _, n := range f.trackCalls {
		tracks += n
	}
	return artists, tracks
}

// fakeEnrichStore registra las escrituras. Los claims devuelven solo los IDs
// que no están en notClaimable.
type fakeEnrichStore struct {
	mu           sync.Mutex
	enriched     map[string]sql.NullString // spotify_id → imagen guardada
	artistFails  []string
	songFails    []sqlc.MarkSongLinkFailedParams
	notClaimable map[string]bool
}

func newFakeEnrichStore() *fakeEnrichStore {
	return &fakeEnrichStore{enriched: map[string]sql.NullString{}, notClaimable: map[string]bool{}}
}

func (f *fakeEnrichStore) UpdateArtistEnrichment(_ context.Context, arg sqlc.UpdateArtistEnrichmentParams) (sql.NullString, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enriched[arg.SpotifyID.String] = arg.ImageUrl
	return arg.ImageUrl, nil
}

func (f *fakeEnrichStore) MarkArtistEnrichmentFailed(_ context.Context, arg sqlc.MarkArtistEnrichmentFailedParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artistFails = append(f.artistFails, arg.SpotifyID.String)
	return nil
}

func (f *fakeEnrichStore) MarkSongLinkFailed(_ context.Context, arg sqlc.MarkSongLinkFailedParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.songFails = append(f.songFails, arg)
	return nil
}

func (f *fakeEnrichStore) claim(ids []string) []sql.NullString {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sql.NullString
	for _, id := range ids {
		if !f.notClaimable[id] {
			out = append(out, sql.NullString{String: id, Valid: true})
		}
	}
	return out
}

func (f *fakeEnrichStore) ClaimArtistsForEnrichment(_ context.Context, arg sqlc.ClaimArtistsForEnrichmentParams) ([]sql.NullString, error) {
	return f.claim(arg.SpotifyIds), nil
}

func (f *fakeEnrichStore) ClaimSongsForLinking(_ context.Context, arg sqlc.ClaimSongsForLinkingParams) ([]sql.NullString, error) {
	return f.claim(arg.TrackIds), nil
}

type fakeEnrichQueue struct {
	mu   sync.Mutex
	jobs []domain.EnrichmentJob
}

func (f *fakeEnrichQueue) Enqueue(jobs ...domain.EnrichmentJob) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, jobs...)
}

// IDs de prueba (base62 de 22 caracteres).
const (
	artistA = "74KM79TiuVKeVCqs8QtB0B"
	artistB = "4q3ewBCX7sLwd24euuV69X"
	artistC = "06HL4z0CvFAxyc27GXpf02"
	trackA  = "1TIHAkrLbRALyt4O4Oh28i"
)

func strp(s string) *string { return &s }

type workerFixture struct {
	metadata *fakeMetadata
	store    *fakeEnrichStore
	importer *fakeImporter
	worker   *CatalogEnrichmentWorker
}

func newWorkerFixture() *workerFixture {
	f := &workerFixture{metadata: newFakeMetadata(), store: newFakeEnrichStore(), importer: &fakeImporter{}}
	enricher := NewCatalogEnricher(f.metadata, f.store, f.importer, nil)
	f.worker = NewCatalogEnrichmentWorker(enricher, CatalogEnrichmentWorkerConfig{RequestInterval: time.Millisecond})
	return f
}

// --- worker de respaldo ---

func TestEnrichmentWorkerSoloConsultaLoReservado(t *testing.T) {
	f := newWorkerFixture()
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("a.jpg")}
	f.metadata.artists[artistB] = domain.ImportedArtist{SpotifyID: artistB, ImageURL: strp("b.jpg")}
	f.store.notClaimable[artistB] = true // ya enriquecido u otra réplica lo tiene

	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistA})
	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistB})
	f.worker.flush(context.Background())

	if f.metadata.artistCalls[artistA] != 1 || f.metadata.artistCalls[artistB] != 0 {
		t.Fatalf("llamadas inesperadas: %+v", f.metadata.artistCalls)
	}
	if got := f.store.enriched[artistA]; got.String != "a.jpg" {
		t.Fatalf("imagen guardada = %+v", got)
	}
	if len(f.worker.order) != 0 || len(f.worker.claimed) != 0 {
		t.Fatalf("debía vaciar el búfer: order=%v claimed=%v", f.worker.order, f.worker.claimed)
	}
}

func TestEnrichmentWorkerEnlazaCancionYEncadenaArtista(t *testing.T) {
	f := newWorkerFixture()
	f.metadata.tracks[trackA] = domain.ImportedTrack{
		SpotifyID: trackA, Name: "Grain",
		Artist: domain.ImportedArtist{SpotifyID: artistA, Name: "Dan Telfer"},
		Album:  domain.ImportedAlbum{Title: "Grain", ImageURL: strp("cover.jpg")},
	}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("a.jpg")}

	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: trackA})
	f.worker.flush(context.Background())

	if len(f.importer.imported) != 1 || f.importer.imported[0].SpotifyID != trackA {
		t.Fatalf("debía importar la canción: %+v", f.importer.imported)
	}
	if f.metadata.artistCalls[artistA] != 1 || f.store.enriched[artistA].String != "a.jpg" {
		t.Fatalf("debía encadenar el artista: calls=%+v enriched=%+v", f.metadata.artistCalls, f.store.enriched)
	}
}

func TestEnrichmentWorkerPausaAnteRateLimit(t *testing.T) {
	f := newWorkerFixture()
	f.metadata.err = rateLimitErr{wait: time.Minute}

	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistA})
	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistB})
	f.worker.flush(context.Background())

	if a, _ := f.metadata.calls(); a != 1 {
		t.Fatalf("debía detenerse tras el primer 429 (calls=%d)", a)
	}
	if time.Until(f.worker.pausedUntil) < 50*time.Second {
		t.Fatalf("debía pausar según Retry-After: %v", f.worker.pausedUntil)
	}
	if len(f.worker.claimed) != 2 {
		t.Fatalf("los reservados deben quedar para después: %v", f.worker.claimed)
	}
	if len(f.store.artistFails) != 0 {
		t.Fatalf("un 429 no debe consumir intentos: %v", f.store.artistFails)
	}
}

func TestEnrichmentWorkerCancionInexistenteSeRinde(t *testing.T) {
	f := newWorkerFixture()

	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: trackA})
	f.worker.flush(context.Background())

	if len(f.store.songFails) != 1 || !f.store.songFails[0].GiveUp {
		t.Fatalf("debía agotar los intentos: %+v", f.store.songFails)
	}
	if !f.worker.pausedUntil.IsZero() {
		t.Fatalf("un 404 no debe pausar el worker")
	}
}

func TestEnrichmentWorkerErrorDelProveedorRegistraIntento(t *testing.T) {
	f := newWorkerFixture()
	f.metadata.err = errors.New("spotify: HTTP 503")

	f.worker.add(domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistA})
	f.worker.flush(context.Background())

	if len(f.store.artistFails) != 1 || f.store.artistFails[0] != artistA {
		t.Fatalf("debía registrar el intento fallido: %v", f.store.artistFails)
	}
	if f.worker.pausedUntil.IsZero() || len(f.worker.claimed) != 0 {
		t.Fatalf("debía pausar y descartar el job: paused=%v claimed=%v", f.worker.pausedUntil, f.worker.claimed)
	}
}

func TestEnrichmentWorkerIgnoraJobsYaProcesados(t *testing.T) {
	f := newWorkerFixture()
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("a.jpg")}
	job := domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistA}

	f.worker.addFromQueue(job)
	f.worker.flush(context.Background())
	f.worker.addFromQueue(job)
	f.worker.flush(context.Background())

	if f.metadata.artistCalls[artistA] != 1 {
		t.Fatalf("calls = %d, se esperaba 1", f.metadata.artistCalls[artistA])
	}
}
