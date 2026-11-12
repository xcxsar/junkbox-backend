package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites de la búsqueda.
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 50
	maxQueryLen        = 200
	// DefaultFallbackThreshold es el puntaje (strict_word_similarity) mínimo
	// del mejor resultado local para no consultar Spotify. Con 0.8, a una
	// consulta de tres palabras no le puede faltar ninguna.
	DefaultFallbackThreshold = 0.8
	// DefaultEnrichBudget es el tiempo máximo que la búsqueda espera a
	// Spotify para completar imágenes que faltan.
	DefaultEnrichBudget = 2 * time.Second
	// DefaultEnrichConcurrency es cuántas llamadas de enriquecimiento corren
	// a la vez por búsqueda.
	DefaultEnrichConcurrency = 8
)

// spotifyIDPattern: los IDs de Spotify son base62 de 22 caracteres.
var spotifyIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

// SearchEnrichment configura cómo la búsqueda completa con Spotify las
// imágenes que faltan antes de responder. Con Enricher nil no se completa
// nada. Fallback recibe lo que no terminó dentro de Budget (puede ser nil).
// Los valores en cero usan los defaults.
type SearchEnrichment struct {
	Enricher    *CatalogEnricher
	Fallback    ports.EnrichmentQueue
	Budget      time.Duration
	Concurrency int
}

// MusicService implementa la búsqueda cache-through (catálogo local con
// respaldo en Spotify) y el detalle de canciones, artistas y álbumes.
type MusicService struct {
	catalog   ports.CatalogStore
	importer  ports.CatalogImporter
	provider  ports.MusicProvider  // nil: búsqueda solo local
	queue     ports.EmbeddingQueue // nil: no se encolan embeddings
	threshold float64
	enrich    SearchEnrichment
}

// NewMusicService crea el servicio. provider y queue pueden ser nil (sin
// credenciales de Spotify o de FreqBlog). threshold <= 0 usa el default.
func NewMusicService(catalog ports.CatalogStore, importer ports.CatalogImporter, provider ports.MusicProvider, queue ports.EmbeddingQueue, threshold float64, enrich SearchEnrichment) *MusicService {
	if threshold <= 0 {
		threshold = DefaultFallbackThreshold
	}
	if enrich.Budget <= 0 {
		enrich.Budget = DefaultEnrichBudget
	}
	if enrich.Concurrency <= 0 {
		enrich.Concurrency = DefaultEnrichConcurrency
	}
	return &MusicService{catalog: catalog, importer: importer, provider: provider, queue: queue, threshold: threshold, enrich: enrich}
}

// localResults es el resultado de una búsqueda local de cualquier tipo.
// jobs es lo que falta pedir a Spotify para completar las imágenes y
// trackArtists el spotify_id del artista de cada canción ("" si no se
// conoce); ninguno de los dos se expone en la respuesta.
type localResults struct {
	results      any
	count        int
	best         float64 // puntaje del mejor resultado (0 si no hay)
	jobs         []domain.EnrichmentJob
	trackArtists []string
}

// Search busca primero en el catálogo local y consulta Spotify si no hay
// resultados o si el mejor puntaje no alcanza el umbral. Lo que devuelve
// Spotify se persiste y las canciones nuevas se encolan para embedding.
// Antes de responder completa con Spotify las imágenes que faltan (ver
// enrichResults). limit = 0 usa DefaultSearchLimit.
func (s *MusicService) Search(ctx context.Context, query, searchType string, limit int) (*domain.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, domain.NewValidationError("q es requerido")
	}
	if utf8.RuneCountInString(query) > maxQueryLen {
		return nil, domain.NewValidationError(fmt.Sprintf("q no puede exceder %d caracteres", maxQueryLen))
	}
	if searchType == "" {
		searchType = domain.SearchTypeTrack
	}
	if searchType != domain.SearchTypeTrack && searchType != domain.SearchTypeArtist && searchType != domain.SearchTypeAlbum {
		return nil, domain.NewValidationError("type debe ser track, artist o album")
	}
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	if limit < 1 || limit > MaxSearchLimit {
		return nil, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxSearchLimit))
	}

	local, err := s.searchLocal(ctx, query, searchType, limit)
	if err != nil {
		return nil, err
	}
	final, source := local, domain.SourceLocalCache

	if (local.count == 0 || local.best < s.threshold) && s.provider != nil {
		remote, err := s.searchRemote(ctx, query, searchType, limit)
		switch {
		case err != nil && local.count > 0:
			log.Printf("búsqueda en Spotify falló, se responden resultados locales: %v", err)
		case err != nil:
			log.Printf("búsqueda en Spotify falló: %v", err)
			return nil, domain.ErrMusicProviderUnavailable
		case remote.count == 0 && local.count > 0:
		default:
			final, source = remote, domain.SourceSpotifyFallback
		}
	}

	s.enrichResults(ctx, &final)
	return &domain.SearchResult{Source: source, Query: query, Type: searchType, Count: final.count, Results: final.results}, nil
}

// enrichResults pide a Spotify, en paralelo y dentro del presupuesto, las
// imágenes de artista y los álbumes (portadas) que faltan en los
// resultados, y los aplica. Lo que no termina a tiempo sale en null y pasa
// al worker de respaldo. Nunca hace fallar la búsqueda.
func (s *MusicService) enrichResults(ctx context.Context, r *localResults) {
	if s.enrich.Enricher == nil || len(r.jobs) == 0 {
		return
	}
	got, left := s.enrich.Enricher.Resolve(ctx, r.jobs, s.enrich.Budget, s.enrich.Concurrency)
	if len(left) > 0 && s.enrich.Fallback != nil {
		s.enrich.Fallback.Enqueue(left...)
	}

	switch results := r.results.(type) {
	case []domain.Track:
		for i := range results {
			t := &results[i]
			artistID := r.trackArtists[i]
			if song, ok := got.songs[t.SpotifyID]; ok {
				if t.AlbumTitle == nil {
					t.AlbumTitle = song.AlbumTitle
				}
				t.AlbumImageURL = song.AlbumImageURL
				if song.ArtistImageURL != nil {
					t.ArtistImageURL = song.ArtistImageURL
				}
				artistID = song.ArtistSpotifyID
			}
			if t.ArtistImageURL == nil {
				t.ArtistImageURL = got.artistImages[artistID]
			}
		}
	case []domain.Artist:
		for i := range results {
			a := &results[i]
			if a.ImageURL == nil && a.SpotifyID != nil {
				a.ImageURL = got.artistImages[*a.SpotifyID]
			}
		}
	}
}

func (s *MusicService) searchLocal(ctx context.Context, query, searchType string, limit int) (localResults, error) {
	switch searchType {
	case domain.SearchTypeArtist:
		rows, err := s.catalog.SearchArtistsTrigram(ctx, sqlc.SearchArtistsTrigramParams{Query: query, MaxResults: int32(limit)})
		if err != nil {
			return localResults{}, fmt.Errorf("music: buscar artistas: %w", err)
		}
		out := make([]domain.Artist, len(rows))
		var jobs []domain.EnrichmentJob
		for i, r := range rows {
			out[i] = artistFromRow(sqlc.GetArtistRow{ID: r.ID, SpotifyID: r.SpotifyID, Name: r.Name, Genres: r.Genres, Popularity: r.Popularity, ImageUrl: r.ImageUrl, CreatedAt: r.CreatedAt})
			if !r.ImageUrl.Valid && artistEnrichable(r.SpotifyID.String, r.Enriched, r.EnrichAttempts) {
				jobs = append(jobs, domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: r.SpotifyID.String})
			}
		}
		return localResults{results: out, count: len(out), best: bestScore(len(rows), func(i int) float64 { return rows[i].Score }), jobs: jobs}, nil
	case domain.SearchTypeAlbum:
		rows, err := s.catalog.SearchAlbumsTrigram(ctx, sqlc.SearchAlbumsTrigramParams{Query: query, MaxResults: int32(limit)})
		if err != nil {
			return localResults{}, fmt.Errorf("music: buscar álbumes: %w", err)
		}
		out := make([]domain.Album, len(rows))
		for i, r := range rows {
			out[i] = albumFromRow(sqlc.GetAlbumRow{ID: r.ID, SpotifyID: r.SpotifyID, Title: r.Title, ArtistID: r.ArtistID, ArtistName: r.ArtistName, ReleaseYear: r.ReleaseYear, AlbumType: r.AlbumType, ImageUrl: r.ImageUrl, TotalTracks: r.TotalTracks, CreatedAt: r.CreatedAt})
		}
		return localResults{results: out, count: len(out), best: bestScore(len(rows), func(i int) float64 { return rows[i].Score })}, nil
	default:
		rows, err := s.catalog.SearchTracksTrigram(ctx, sqlc.SearchTracksTrigramParams{Query: query, MaxResults: int32(limit)})
		if err != nil {
			return localResults{}, fmt.Errorf("music: buscar canciones: %w", err)
		}
		out := make([]domain.Track, len(rows))
		artists := make([]string, len(rows))
		var jobs []domain.EnrichmentJob
		for i, r := range rows {
			out[i] = domain.Track{
				ID:               r.ID,
				SpotifyID:        r.TrackID.String,
				TrackName:        r.TrackName.String,
				ArtistName:       r.ArtistName.String,
				AlbumTitle:       stringPtr(r.AlbumTitle),
				AlbumImageURL:    stringPtr(r.AlbumImageUrl),
				ArtistImageURL:   stringPtr(r.ArtistImageUrl),
				Year:             int32Ptr(r.Year),
				Genre:            stringPtr(r.Genre),
				Popularity:       int32Ptr(r.Popularity),
				DurationMs:       int32Ptr(r.DurationMs),
				HasEmbedding:     r.HasEmbedding,
				PendingEmbedding: r.PendingEmbedding,
			}
			artists[i] = r.ArtistSpotifyID.String
			// Las canciones del CSV no tienen álbum: enlazarlas trae la
			// portada y reclama a su artista.
			if r.AlbumID == nil && spotifyIDPattern.MatchString(r.TrackID.String) && r.LinkAttempts < MaxEnrichAttempts {
				jobs = append(jobs, domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: r.TrackID.String})
			}
			if !r.ArtistImageUrl.Valid && artistEnrichable(r.ArtistSpotifyID.String, r.ArtistEnriched, r.ArtistEnrichAttempts) {
				jobs = append(jobs, domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: r.ArtistSpotifyID.String})
			}
		}
		return localResults{results: out, count: len(out), best: bestScore(len(rows), func(i int) float64 { return rows[i].Score }), jobs: jobs, trackArtists: artists}, nil
	}
}

func (s *MusicService) searchRemote(ctx context.Context, query, searchType string, limit int) (localResults, error) {
	switch searchType {
	case domain.SearchTypeArtist:
		found, err := s.provider.SearchArtists(ctx, query, limit)
		if err != nil {
			return localResults{}, err
		}
		saved, err := s.importer.ImportArtists(ctx, found)
		if err != nil {
			return localResults{}, err
		}
		return localResults{results: saved, count: len(saved)}, nil
	case domain.SearchTypeAlbum:
		found, err := s.provider.SearchAlbums(ctx, query, limit)
		if err != nil {
			return localResults{}, err
		}
		saved, err := s.importer.ImportAlbums(ctx, found)
		if err != nil {
			return localResults{}, err
		}
		return localResults{results: saved, count: len(saved)}, nil
	default:
		found, err := s.provider.SearchTracks(ctx, query, limit)
		if err != nil {
			return localResults{}, err
		}
		saved, err := s.importer.ImportTracks(ctx, found)
		if err != nil {
			return localResults{}, err
		}
		out := make([]domain.Track, len(saved))
		artists := make([]string, len(saved))
		var jobs []domain.EmbeddingJob
		var enrich []domain.EnrichmentJob
		for i, song := range saved {
			out[i] = song.Track
			artists[i] = song.ArtistSpotifyID
			if song.PendingEmbedding {
				jobs = append(jobs, embeddingJob(song))
			}
			// El artista de una canción llega simplificado, sin imagen.
			if song.ArtistImageURL == nil && artistEnrichable(song.ArtistSpotifyID, song.ArtistEnriched, song.ArtistEnrichAttempts) {
				enrich = append(enrich, domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: song.ArtistSpotifyID})
			}
		}
		if s.queue != nil && len(jobs) > 0 {
			s.queue.Enqueue(jobs...)
		}
		return localResults{results: out, count: len(out), jobs: enrich, trackArtists: artists}, nil
	}
}

// GetTrack devuelve el detalle de una canción por id local (entero) o por
// su ID de Spotify.
func (s *MusicService) GetTrack(ctx context.Context, id string) (*domain.TrackDetail, error) {
	arg, err := parseTrackID(id)
	if err != nil {
		return nil, err
	}
	r, err := s.catalog.GetTrack(ctx, arg)
	if err != nil {
		return nil, notFoundOr(err, "music: obtener canción")
	}
	detail := &domain.TrackDetail{
		Track: domain.Track{
			ID:               r.ID,
			SpotifyID:        r.TrackID.String,
			TrackName:        r.TrackName.String,
			ArtistName:       r.ArtistName.String,
			AlbumTitle:       stringPtr(r.AlbumTitle),
			AlbumImageURL:    stringPtr(r.AlbumImageUrl),
			ArtistImageURL:   stringPtr(r.ArtistImageUrl),
			Year:             int32Ptr(r.Year),
			Genre:            stringPtr(r.Genre),
			Popularity:       int32Ptr(r.Popularity),
			DurationMs:       int32Ptr(r.DurationMs),
			HasEmbedding:     r.HasEmbedding,
			PendingEmbedding: r.PendingEmbedding,
		},
		ArtistID: r.ArtistID,
		AlbumID:  r.AlbumID,
		ISRC:     stringPtr(r.Isrc),
		Ratings:  domain.RatingStats{Count: r.RatingsCount, Average: r.AvgRating},
	}
	features := domain.AudioFeatures{
		Danceability:     float64Ptr(r.Danceability),
		Energy:           float64Ptr(r.Energy),
		Key:              int32Ptr(r.Key),
		Loudness:         float64Ptr(r.Loudness),
		Mode:             int32Ptr(r.Mode),
		Speechiness:      float64Ptr(r.Speechiness),
		Acousticness:     float64Ptr(r.Acousticness),
		Instrumentalness: float64Ptr(r.Instrumentalness),
		Liveness:         float64Ptr(r.Liveness),
		Valence:          float64Ptr(r.Valence),
		Tempo:            float64Ptr(r.Tempo),
		TimeSignature:    int32Ptr(r.TimeSignature),
	}
	if features != (domain.AudioFeatures{}) {
		detail.AudioFeatures = &features
	}
	return detail, nil
}

// GetArtist devuelve un artista por UUID local o por ID de Spotify.
func (s *MusicService) GetArtist(ctx context.Context, id string) (*domain.Artist, error) {
	localID, spotifyID, err := parseEntityID(id)
	if err != nil {
		return nil, err
	}
	r, err := s.catalog.GetArtist(ctx, sqlc.GetArtistParams{ID: localID, SpotifyID: spotifyID})
	if err != nil {
		return nil, notFoundOr(err, "music: obtener artista")
	}
	artist := artistFromRow(r)
	artist.Ratings = &domain.RatingStats{Count: r.RatingsCount, Average: r.AvgRating}
	return &artist, nil
}

// GetAlbum devuelve un álbum, con sus pistas, por UUID local o por ID de
// Spotify.
func (s *MusicService) GetAlbum(ctx context.Context, id string) (*domain.Album, error) {
	localID, spotifyID, err := parseEntityID(id)
	if err != nil {
		return nil, err
	}
	r, err := s.catalog.GetAlbum(ctx, sqlc.GetAlbumParams{ID: localID, SpotifyID: spotifyID})
	if err != nil {
		return nil, notFoundOr(err, "music: obtener álbum")
	}
	album := albumFromRow(r)
	album.Ratings = &domain.RatingStats{Count: r.RatingsCount, Average: r.AvgRating}

	rows, err := s.catalog.ListTracksByAlbum(ctx, &album.ID)
	if err != nil {
		return nil, fmt.Errorf("music: listar pistas del álbum: %w", err)
	}
	album.Tracks = make([]domain.Track, len(rows))
	for i, t := range rows {
		album.Tracks[i] = domain.Track{
			ID:               t.ID,
			SpotifyID:        t.TrackID.String,
			TrackName:        t.TrackName.String,
			ArtistName:       t.ArtistName.String,
			AlbumTitle:       &album.Title,
			AlbumImageURL:    album.ImageURL,
			ArtistImageURL:   stringPtr(t.ArtistImageUrl),
			Year:             int32Ptr(t.Year),
			Genre:            stringPtr(t.Genre),
			Popularity:       int32Ptr(t.Popularity),
			DurationMs:       int32Ptr(t.DurationMs),
			HasEmbedding:     t.HasEmbedding,
			PendingEmbedding: t.PendingEmbedding,
		}
	}
	return &album, nil
}

// ResolveTarget resuelve el objetivo de una calificación o reseña a su id
// local. id admite los mismos formatos que el detalle del catálogo (id
// local o ID de Spotify). Solo busca en el catálogo local: un ID de Spotify
// que nunca se importó devuelve ErrNotFound.
func (s *MusicService) ResolveTarget(ctx context.Context, targetType, id string) (domain.Target, error) {
	switch targetType {
	case domain.TargetSong:
		arg, err := parseTrackID(id)
		if err != nil {
			return domain.Target{}, err
		}
		r, err := s.catalog.GetTrack(ctx, arg)
		if err != nil {
			return domain.Target{}, notFoundOr(err, "music: resolver canción")
		}
		return domain.Target{Type: targetType, SongID: r.ID}, nil
	case domain.TargetAlbum:
		localID, spotifyID, err := parseEntityID(id)
		if err != nil {
			return domain.Target{}, err
		}
		r, err := s.catalog.GetAlbum(ctx, sqlc.GetAlbumParams{ID: localID, SpotifyID: spotifyID})
		if err != nil {
			return domain.Target{}, notFoundOr(err, "music: resolver álbum")
		}
		return domain.Target{Type: targetType, AlbumID: r.ID}, nil
	case domain.TargetArtist:
		localID, spotifyID, err := parseEntityID(id)
		if err != nil {
			return domain.Target{}, err
		}
		r, err := s.catalog.GetArtist(ctx, sqlc.GetArtistParams{ID: localID, SpotifyID: spotifyID})
		if err != nil {
			return domain.Target{}, notFoundOr(err, "music: resolver artista")
		}
		return domain.Target{Type: targetType, ArtistID: r.ID}, nil
	default:
		return domain.Target{}, domain.NewValidationError("target_type debe ser song, album o artist")
	}
}

// parseTrackID interpreta el id de una canción: entero local o ID de
// Spotify.
func parseTrackID(id string) (sqlc.GetTrackParams, error) {
	if n, err := strconv.ParseInt(id, 10, 32); err == nil && n > 0 {
		return sqlc.GetTrackParams{ID: int32(n)}, nil
	}
	if spotifyIDPattern.MatchString(id) {
		return sqlc.GetTrackParams{SpotifyID: id}, nil
	}
	return sqlc.GetTrackParams{}, domain.NewValidationError("id debe ser un entero positivo o un ID de Spotify")
}

// parseEntityID interpreta el id de un artista o álbum: UUID local o ID de
// Spotify. Exactamente uno de los dos queda con valor.
func parseEntityID(id string) (uuid.UUID, string, error) {
	if u, err := uuid.Parse(id); err == nil {
		return u, "", nil
	}
	if spotifyIDPattern.MatchString(id) {
		return uuid.Nil, id, nil
	}
	return uuid.Nil, "", domain.NewValidationError("id debe ser un UUID o un ID de Spotify")
}

func notFoundOr(err error, op string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

func bestScore(n int, score func(int) float64) float64 {
	best := 0.0
	for i := 0; i < n; i++ {
		best = max(best, score(i))
	}
	return best
}

func artistFromRow(r sqlc.GetArtistRow) domain.Artist {
	genres := r.Genres
	if genres == nil {
		genres = []string{}
	}
	return domain.Artist{
		ID:         r.ID,
		SpotifyID:  stringPtr(r.SpotifyID),
		Name:       r.Name,
		Genres:     genres,
		Popularity: int32Ptr(r.Popularity),
		ImageURL:   stringPtr(r.ImageUrl),
		CreatedAt:  r.CreatedAt,
	}
}

func albumFromRow(r sqlc.GetAlbumRow) domain.Album {
	return domain.Album{
		ID:          r.ID,
		SpotifyID:   stringPtr(r.SpotifyID),
		Title:       r.Title,
		ArtistID:    r.ArtistID,
		ArtistName:  r.ArtistName,
		ReleaseYear: int32Ptr(r.ReleaseYear),
		AlbumType:   r.AlbumType,
		ImageURL:    stringPtr(r.ImageUrl),
		TotalTracks: r.TotalTracks,
		CreatedAt:   r.CreatedAt,
	}
}

// --- conversión de tipos nulos de database/sql a punteros (JSON null); stringPtr está en auth_service.go ---

func int32Ptr(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func float64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}
