package services

import (
	"context"
	"database/sql"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

type fakeEmbeddingStore struct {
	mu       sync.Mutex
	pending  []sqlc.ClaimPendingEmbeddingTracksRow
	claims   []sqlc.ClaimPendingEmbeddingTracksParams
	updates  []sqlc.UpdateSongAudioFeaturesAndEmbeddingParams
	failures []sqlc.MarkEmbeddingAttemptFailedParams
	medians  sqlc.GetCatalogFeatureMediansRow
}

func (f *fakeEmbeddingStore) ClaimPendingEmbeddingTracks(_ context.Context, arg sqlc.ClaimPendingEmbeddingTracksParams) ([]sqlc.ClaimPendingEmbeddingTracksRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims = append(f.claims, arg)
	rows := f.pending
	f.pending = nil
	return rows, nil
}

func (f *fakeEmbeddingStore) UpdateSongAudioFeaturesAndEmbedding(_ context.Context, arg sqlc.UpdateSongAudioFeaturesAndEmbeddingParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, arg)
	return 1, nil
}

func (f *fakeEmbeddingStore) MarkEmbeddingAttemptFailed(_ context.Context, arg sqlc.MarkEmbeddingAttemptFailedParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, arg)
	return nil
}

func (f *fakeEmbeddingStore) GetCatalogFeatureMedians(_ context.Context) (sqlc.GetCatalogFeatureMediansRow, error) {
	return f.medians, nil
}

func (f *fakeEmbeddingStore) counts() (updates, failures int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updates), len(f.failures)
}

type rateLimitErr struct{ wait time.Duration }

func (e rateLimitErr) Error() string             { return "429" }
func (e rateLimitErr) RetryAfter() time.Duration { return e.wait }

// fakeFeaturesProvider devuelve features para los IDs de known y registra
// cada llamada. Si errs no está vacío, la siguiente llamada falla con su
// primer error.
type fakeFeaturesProvider struct {
	mu    sync.Mutex
	known map[string]*domain.AudioFeatures
	errs  []error
	calls [][]string
}

func (f *fakeFeaturesProvider) GetAudioFeatures(_ context.Context, ids []string) (map[string]*domain.AudioFeatures, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), ids...))
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	out := map[string]*domain.AudioFeatures{}
	for _, id := range ids {
		if feat, ok := f.known[id]; ok {
			out[id] = feat
		}
	}
	return out, nil
}

func (f *fakeFeaturesProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func ptr[T any](v T) *T { return &v }

func sampleFeatures() *domain.AudioFeatures {
	return &domain.AudioFeatures{
		Danceability: ptr(0.7), Energy: ptr(0.8), Loudness: ptr(-5.0), Acousticness: ptr(0.1),
		Instrumentalness: ptr(0.0), Valence: ptr(0.6), Tempo: ptr(125.0), Mode: ptr(int32(1)),
	}
}

// startWorker lanza Run y devuelve una función que lo detiene y espera.
func startWorker(t *testing.T, w *AudioFeaturesWorker) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- vector ---

func TestBuildEmbeddingFormulaEImputacion(t *testing.T) {
	medians := sqlc.GetCatalogFeatureMediansRow{Speechiness: 0.05, Liveness: 0.13, Mode: 1}
	vec := BuildEmbedding(sampleFeatures(), medians)

	want := []float64{
		0.7, 0.8,
		(-5.0 + 58.1) / (6.172 + 58.1), // loudness_norm
		0.05,                           // speechiness imputada
		0.1, 0.0,
		0.13, // liveness imputada
		0.6,
		125.0 / 250.0, // tempo_norm
		1,
	}
	if len(vec) != 10 {
		t.Fatalf("len = %d, se esperaban 10 dimensiones", len(vec))
	}
	for i := range want {
		if math.Abs(float64(vec[i])-want[i]) > 1e-6 {
			t.Fatalf("dim %d = %v, se esperaba %v", i, vec[i], want[i])
		}
	}
}

func TestBuildEmbeddingAcotaACeroUno(t *testing.T) {
	f := &domain.AudioFeatures{Loudness: ptr(10.0), Tempo: ptr(300.0), Danceability: ptr(-0.2)}
	vec := BuildEmbedding(f, sqlc.GetCatalogFeatureMediansRow{})
	if vec[0] != 0 || vec[2] != 1 || vec[8] != 1 {
		t.Fatalf("vector sin acotar: %v", vec)
	}
}

func TestFormatVector(t *testing.T) {
	if got := FormatVector([]float32{0.5, 0, 1, 0.25}); got != "[0.5,0,1,0.25]" {
		t.Fatalf("FormatVector = %q", got)
	}
}

// --- ciclo del worker ---

func TestWorkerEnviaLotePorTamano(t *testing.T) {
	store := &fakeEmbeddingStore{}
	provider := &fakeFeaturesProvider{known: map[string]*domain.AudioFeatures{"ISRC00000001": sampleFeatures(), "track2": sampleFeatures()}}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{BatchSize: 2, FlushInterval: time.Hour, RescanInterval: time.Hour})
	startWorker(t, w)

	w.Enqueue(domain.EmbeddingJob{TrackID: "track1", ISRC: "ISRC00000001"}, domain.EmbeddingJob{TrackID: "track2"})
	waitFor(t, "2 embeddings guardados", func() bool { u, _ := store.counts(); return u == 2 })

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 1 || len(provider.calls[0]) != 2 || provider.calls[0][0] != "ISRC00000001" {
		t.Fatalf("llamadas inesperadas (debía consultar por ISRC primero): %v", provider.calls)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.updates[0].TrackID.String != "track1" || store.updates[0].Speechiness.Valid {
		t.Fatalf("update inesperado: %+v", store.updates[0])
	}
}

func TestWorkerEnviaLotePorTiempo(t *testing.T) {
	store := &fakeEmbeddingStore{}
	provider := &fakeFeaturesProvider{known: map[string]*domain.AudioFeatures{"track1": sampleFeatures()}}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{BatchSize: 50, FlushInterval: 20 * time.Millisecond, RescanInterval: time.Hour})
	startWorker(t, w)

	w.Enqueue(domain.EmbeddingJob{TrackID: "track1"})
	waitFor(t, "embedding guardado", func() bool { u, _ := store.counts(); return u == 1 })
}

func TestWorkerSinResultadoRegistraIntentoConBackoff(t *testing.T) {
	store := &fakeEmbeddingStore{}
	provider := &fakeFeaturesProvider{}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{FlushInterval: 10 * time.Millisecond, RescanInterval: time.Hour, RetryBase: 30 * time.Second})
	startWorker(t, w)

	w.Enqueue(domain.EmbeddingJob{TrackID: "track1", Attempts: 2})
	waitFor(t, "intento fallido", func() bool { _, f := store.counts(); return f == 1 })

	store.mu.Lock()
	defer store.mu.Unlock()
	if got := store.failures[0].RetryInSeconds; got != 120 { // 30s · 2^2
		t.Fatalf("RetryInSeconds = %v, se esperaba 120", got)
	}
}

func TestWorkerErrorDelProveedorNoConsumeIntentos(t *testing.T) {
	store := &fakeEmbeddingStore{}
	provider := &fakeFeaturesProvider{
		known: map[string]*domain.AudioFeatures{"track1": sampleFeatures()},
		errs:  []error{rateLimitErr{wait: 50 * time.Millisecond}},
	}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{FlushInterval: 10 * time.Millisecond, RescanInterval: time.Hour})
	startWorker(t, w)

	w.Enqueue(domain.EmbeddingJob{TrackID: "track1"})
	waitFor(t, "embedding guardado tras la pausa", func() bool { u, _ := store.counts(); return u == 1 })

	if _, failures := store.counts(); failures != 0 {
		t.Fatalf("un 429 no debe consumir intentos (failures=%d)", failures)
	}
	if provider.callCount() != 2 {
		t.Fatalf("calls = %d, se esperaban 2 (429 y reintento)", provider.callCount())
	}
}

func TestWorkerReanudaPendientesAlArrancar(t *testing.T) {
	store := &fakeEmbeddingStore{pending: []sqlc.ClaimPendingEmbeddingTracksRow{
		{TrackID: nullStr("track1"), Isrc: nullStr("ISRC00000001")},
	}}
	provider := &fakeFeaturesProvider{known: map[string]*domain.AudioFeatures{"ISRC00000001": sampleFeatures()}}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{FlushInterval: time.Hour, RescanInterval: time.Hour})
	startWorker(t, w)

	waitFor(t, "pendiente procesado al arrancar", func() bool { u, _ := store.counts(); return u == 1 })
}

func TestWorkerShutdownEnviaElBufer(t *testing.T) {
	store := &fakeEmbeddingStore{}
	provider := &fakeFeaturesProvider{known: map[string]*domain.AudioFeatures{"track1": sampleFeatures()}}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{FlushInterval: time.Hour, RescanInterval: time.Hour})
	stop := startWorker(t, w)

	w.Enqueue(domain.EmbeddingJob{TrackID: "track1"})
	stop()

	if u, _ := store.counts(); u != 1 {
		t.Fatalf("el shutdown debía guardar el búfer pendiente (updates=%d)", u)
	}
}

func TestEnqueueDescartaAgotadosYColaLlena(t *testing.T) {
	w := NewAudioFeaturesWorker(&fakeFeaturesProvider{}, &fakeEmbeddingStore{}, AudioFeaturesWorkerConfig{QueueSize: 1, MaxAttempts: 5})

	w.Enqueue(domain.EmbeddingJob{TrackID: "agotado", Attempts: 5})
	if len(w.queue) != 0 {
		t.Fatalf("no debía encolar un track con los intentos agotados")
	}
	w.Enqueue(domain.EmbeddingJob{TrackID: "a"}, domain.EmbeddingJob{TrackID: "b"}) // no bloquea
	if len(w.queue) != 1 {
		t.Fatalf("len(queue) = %d, se esperaba 1", len(w.queue))
	}
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func TestWorkerNoReconsultaLoQueYaProcesoElRescan(t *testing.T) {
	store := &fakeEmbeddingStore{pending: []sqlc.ClaimPendingEmbeddingTracksRow{
		{TrackID: nullStr("track1")},
		{TrackID: nullStr("track2")},
	}}
	provider := &fakeFeaturesProvider{known: map[string]*domain.AudioFeatures{"track1": sampleFeatures()}}
	w := NewAudioFeaturesWorker(provider, store, AudioFeaturesWorkerConfig{FlushInterval: 10 * time.Millisecond, RescanInterval: time.Hour})
	startWorker(t, w)
	waitFor(t, "rescan inicial procesado", func() bool { u, f := store.counts(); return u == 1 && f == 1 })

	// La misma búsqueda vuelve a encolar ambas canciones.
	w.Enqueue(domain.EmbeddingJob{TrackID: "track1"}, domain.EmbeddingJob{TrackID: "track2"})
	time.Sleep(50 * time.Millisecond)

	if n := provider.callCount(); n != 1 {
		t.Fatalf("calls = %d, no debía volver a consultar canciones ya procesadas", n)
	}
	if u, f := store.counts(); u != 1 || f != 1 {
		t.Fatalf("updates=%d failures=%d, se esperaba 1 y 1", u, f)
	}
}

func TestRescanReservaConLeaseYNoReservaEnPausa(t *testing.T) {
	store := &fakeEmbeddingStore{}
	w := NewAudioFeaturesWorker(&fakeFeaturesProvider{}, store, AudioFeaturesWorkerConfig{BatchSize: 5, LeaseDuration: 2 * time.Minute})

	w.rescan(context.Background())
	if len(store.claims) != 1 {
		t.Fatalf("claims = %d, se esperaba 1", len(store.claims))
	}
	if c := store.claims[0]; c.LeaseSeconds != 120 || c.MaxResults != 50 {
		t.Fatalf("claim = %+v, se esperaba lease 120s y 50 resultados", c)
	}

	// En pausa, una reserva vencería antes de poder enviarse.
	w.pausedUntil = time.Now().Add(time.Minute)
	w.rescan(context.Background())
	if len(store.claims) != 1 {
		t.Fatalf("no debía reservar pendientes con el proveedor en pausa (claims=%d)", len(store.claims))
	}
}
