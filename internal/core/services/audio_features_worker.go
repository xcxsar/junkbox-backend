package services

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites MinMax globales del catálogo histórico (los mismos que
// db/scripts/ingest_songs.sql): loudness en [-58.1, 6.172] dB y tempo en
// [0, 250] BPM.
const (
	loudnessMin = -58.1
	loudnessMax = 6.172
	tempoMax    = 250.0
)

// AudioFeaturesWorkerConfig ajusta el worker. Los valores en cero usan los
// defaults de NewAudioFeaturesWorker.
type AudioFeaturesWorkerConfig struct {
	QueueSize      int           // capacidad del canal (default 5000)
	BatchSize      int           // canciones por petición al proveedor (default 50)
	FlushInterval  time.Duration // envía un lote incompleto tras este tiempo (default 2s)
	RescanInterval time.Duration // relee los pendientes de la BD (default 1m)
	LeaseDuration  time.Duration // reserva de los pendientes frente a otras réplicas (default 5m)
	MaxAttempts    int32         // intentos sin resultado antes de rendirse (default 5)
	RetryBase      time.Duration // backoff tras un intento sin resultado: base·2^intentos (default 30s)
	ProviderPause  time.Duration // pausa ante un error del proveedor sin Retry-After (default 1m)
}

// handledTTL es cuánto se recuerda una canción ya procesada para ignorarla
// si vuelve a llegar por la cola (p. ej. una búsqueda repetida). Sus
// reintentos los programa la BD y los recoge el rescan.
const handledTTL = time.Hour

// AudioFeaturesWorker calcula en segundo plano el embedding de las canciones
// importadas desde Spotify: pide sus audio features a FreqBlog por lotes,
// imputa los nulos con la mediana del catálogo y guarda el vector(10).
type AudioFeaturesWorker struct {
	provider ports.AudioFeaturesProvider
	store    ports.EmbeddingStore
	cfg      AudioFeaturesWorkerConfig
	queue    chan domain.EmbeddingJob

	// Estado propio de la goroutine de Run.
	buffer      map[string]domain.EmbeddingJob // por track_id (deduplica)
	order       []string                       // orden de llegada de buffer
	handled     map[string]time.Time           // track_id → cuándo se procesó
	medians     *sqlc.GetCatalogFeatureMediansRow
	pausedUntil time.Time

	dropped sync.Once // avisa una sola vez de que la cola se llenó
}

// NewAudioFeaturesWorker crea el worker. Run debe lanzarse en una goroutine.
func NewAudioFeaturesWorker(provider ports.AudioFeaturesProvider, store ports.EmbeddingStore, cfg AudioFeaturesWorkerConfig) *AudioFeaturesWorker {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 5000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	if cfg.RescanInterval <= 0 {
		cfg.RescanInterval = time.Minute
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 5 * time.Minute
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 30 * time.Second
	}
	if cfg.ProviderPause <= 0 {
		cfg.ProviderPause = time.Minute
	}
	return &AudioFeaturesWorker{
		provider: provider,
		store:    store,
		cfg:      cfg,
		queue:    make(chan domain.EmbeddingJob, cfg.QueueSize),
		buffer:   map[string]domain.EmbeddingJob{},
		handled:  map[string]time.Time{},
	}
}

// Enqueue encola canciones sin bloquear. Si la cola está llena se
// descartan: siguen con pending_embedding en la BD y el rescan las recupera.
func (w *AudioFeaturesWorker) Enqueue(jobs ...domain.EmbeddingJob) {
	for _, j := range jobs {
		if j.TrackID == "" || j.Attempts >= w.cfg.MaxAttempts {
			continue
		}
		select {
		case w.queue <- j:
		default:
			w.dropped.Do(func() {
				log.Printf("worker de embeddings: cola llena; el rescan recuperará las canciones descartadas")
			})
		}
	}
}

// Run procesa la cola hasta que ctx se cancela. Al cancelarse, envía lo que
// queda en el búfer (con un plazo de 10 s) antes de volver.
func (w *AudioFeaturesWorker) Run(ctx context.Context) {
	w.loadMedians(ctx)
	w.rescan(ctx)
	w.flushAll(ctx)

	flush := time.NewTicker(w.cfg.FlushInterval)
	defer flush.Stop()
	rescan := time.NewTicker(w.cfg.RescanInterval)
	defer rescan.Stop()

	for {
		select {
		case <-ctx.Done():
			w.drainQueue()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			w.flushAll(shutdownCtx)
			cancel()
			return
		case j := <-w.queue:
			w.addFromQueue(j)
			if len(w.order) >= w.cfg.BatchSize {
				w.flushAll(ctx)
			}
		case <-flush.C:
			w.flushAll(ctx)
		case <-rescan.C:
			w.rescan(ctx)
			w.flushAll(ctx)
		}
	}
}

func (w *AudioFeaturesWorker) add(j domain.EmbeddingJob) {
	if _, ok := w.buffer[j.TrackID]; !ok {
		w.order = append(w.order, j.TrackID)
	}
	w.buffer[j.TrackID] = j
}

// addFromQueue ignora las canciones ya procesadas: si el rescan la tomó
// primero, consultarla de nuevo gastaría cuota de FreqBlog.
func (w *AudioFeaturesWorker) addFromQueue(j domain.EmbeddingJob) {
	if _, done := w.handled[j.TrackID]; !done {
		w.add(j)
	}
}

func (w *AudioFeaturesWorker) drainQueue() {
	for {
		select {
		case j := <-w.queue:
			w.addFromQueue(j)
		default:
			return
		}
	}
}

func (w *AudioFeaturesWorker) loadMedians(ctx context.Context) bool {
	if w.medians != nil {
		return true
	}
	m, err := w.store.GetCatalogFeatureMedians(ctx)
	if err != nil {
		log.Printf("worker de embeddings: calcular medianas del catálogo: %v", err)
		return false
	}
	w.medians = &m
	return true
}

// rescan reserva y agrega al búfer las canciones pendientes cuyo reintento
// ya venció (incluye las que quedaron de una ejecución anterior). La reserva
// dura LeaseDuration para que otras réplicas no consulten las mismas: no se
// reserva más mientras el proveedor está en pausa o el búfer ya tiene
// trabajo, porque el lease vencería antes de enviarlas.
func (w *AudioFeaturesWorker) rescan(ctx context.Context) {
	for id, at := range w.handled {
		if time.Since(at) > handledTTL {
			delete(w.handled, id)
		}
	}

	limit := w.cfg.BatchSize * 10
	if time.Now().Before(w.pausedUntil) || len(w.order) >= limit {
		return
	}
	rows, err := w.store.ClaimPendingEmbeddingTracks(ctx, sqlc.ClaimPendingEmbeddingTracksParams{
		LeaseSeconds: w.cfg.LeaseDuration.Seconds(),
		MaxAttempts:  w.cfg.MaxAttempts,
		MaxResults:   int32(limit - len(w.order)),
	})
	if err != nil {
		log.Printf("worker de embeddings: reservar pendientes: %v", err)
		return
	}
	for _, r := range rows {
		if r.TrackID.String == "" {
			continue
		}
		w.add(domain.EmbeddingJob{TrackID: r.TrackID.String, ISRC: r.Isrc.String, Attempts: r.EmbeddingAttempts})
	}
}

// flushAll envía el búfer en lotes de BatchSize. Se detiene si el proveedor
// pide una pausa o si aún no hay medianas; lo no enviado queda en el búfer.
func (w *AudioFeaturesWorker) flushAll(ctx context.Context) {
	for len(w.order) > 0 {
		if time.Now().Before(w.pausedUntil) || !w.loadMedians(ctx) {
			return
		}
		n := min(w.cfg.BatchSize, len(w.order))
		batch := make([]domain.EmbeddingJob, n)
		for i, id := range w.order[:n] {
			batch[i] = w.buffer[id]
		}
		if !w.processBatch(ctx, batch) {
			return
		}
		for _, id := range w.order[:n] {
			delete(w.buffer, id)
		}
		w.order = w.order[n:]
	}
}

// processBatch consulta al proveedor y persiste el resultado de cada
// canción. Devuelve false si el proveedor falló (el lote se reintenta tras
// la pausa, sin consumir intentos).
func (w *AudioFeaturesWorker) processBatch(ctx context.Context, batch []domain.EmbeddingJob) bool {
	ids := make([]string, 0, len(batch))
	seen := map[string]bool{}
	for _, j := range batch {
		if id := j.LookupID(); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	features, err := w.provider.GetAudioFeatures(ctx, ids)
	if err != nil {
		pause := w.cfg.ProviderPause
		var ra ports.RetryAfterError
		if errors.As(err, &ra) && ra.RetryAfter() > 0 {
			pause = ra.RetryAfter()
		}
		w.pausedUntil = time.Now().Add(pause)
		log.Printf("worker de embeddings: proveedor de audio features falló (pausa de %s): %v", pause, err)
		return false
	}

	now := time.Now()
	for _, j := range batch {
		w.handled[j.TrackID] = now
		f := features[j.LookupID()]
		if f == nil {
			w.markFailed(ctx, j)
			continue
		}
		vec := BuildEmbedding(f, *w.medians)
		_, err := w.store.UpdateSongAudioFeaturesAndEmbedding(ctx, sqlc.UpdateSongAudioFeaturesAndEmbeddingParams{
			Danceability:     toNullFloat64(f.Danceability),
			Energy:           toNullFloat64(f.Energy),
			Key:              toNullInt32(f.Key),
			Loudness:         toNullFloat64(f.Loudness),
			Mode:             toNullInt32(f.Mode),
			Speechiness:      toNullFloat64(f.Speechiness),
			Acousticness:     toNullFloat64(f.Acousticness),
			Instrumentalness: toNullFloat64(f.Instrumentalness),
			Liveness:         toNullFloat64(f.Liveness),
			Valence:          toNullFloat64(f.Valence),
			Tempo:            toNullFloat64(f.Tempo),
			TimeSignature:    toNullInt32(f.TimeSignature),
			Embedding:        FormatVector(vec),
			TrackID:          sql.NullString{String: j.TrackID, Valid: true},
		})
		if err != nil {
			// El rescan volverá a tomarla: sigue con pending_embedding.
			log.Printf("worker de embeddings: guardar embedding de %s: %v", j.TrackID, err)
		}
	}
	return true
}

// markFailed registra un intento sin resultado y programa el siguiente con
// backoff exponencial.
func (w *AudioFeaturesWorker) markFailed(ctx context.Context, j domain.EmbeddingJob) {
	delay := w.cfg.RetryBase << min(j.Attempts, 10)
	err := w.store.MarkEmbeddingAttemptFailed(ctx, sqlc.MarkEmbeddingAttemptFailedParams{
		RetryInSeconds: delay.Seconds(),
		TrackID:        sql.NullString{String: j.TrackID, Valid: true},
		Attempts:       j.Attempts,
	})
	if err != nil {
		log.Printf("worker de embeddings: registrar intento fallido de %s: %v", j.TrackID, err)
		return
	}
	if j.Attempts+1 >= w.cfg.MaxAttempts {
		log.Printf("worker de embeddings: %s sin audio features tras %d intentos; se deja de intentar", j.TrackID, w.cfg.MaxAttempts)
	}
}

// BuildEmbedding arma el vector de 10 dimensiones
// [danceability, energy, loudness_norm, speechiness, acousticness,
// instrumentalness, liveness, valence, tempo_norm, mode] con la misma
// normalización MinMax que la ingesta histórica. Los atributos nulos se
// imputan con la mediana del catálogo y cada componente se acota a [0, 1].
func BuildEmbedding(f *domain.AudioFeatures, m sqlc.GetCatalogFeatureMediansRow) []float32 {
	mode := m.Mode
	if f.Mode != nil {
		mode = float64(*f.Mode)
	}
	values := []float64{
		orMedian(f.Danceability, m.Danceability),
		orMedian(f.Energy, m.Energy),
		(orMedian(f.Loudness, m.Loudness) - loudnessMin) / (loudnessMax - loudnessMin),
		orMedian(f.Speechiness, m.Speechiness),
		orMedian(f.Acousticness, m.Acousticness),
		orMedian(f.Instrumentalness, m.Instrumentalness),
		orMedian(f.Liveness, m.Liveness),
		orMedian(f.Valence, m.Valence),
		orMedian(f.Tempo, m.Tempo) / tempoMax,
		mode,
	}
	vec := make([]float32, len(values))
	for i, v := range values {
		vec[i] = float32(min(max(v, 0), 1))
	}
	return vec
}

// FormatVector serializa un vector en el formato de texto de pgvector
// ("[a,b,...]").
func FormatVector(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func orMedian(v *float64, median float64) float64 {
	if v == nil {
		return median
	}
	return *v
}

func toNullFloat64(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func toNullInt32(v *int32) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *v, Valid: true}
}
