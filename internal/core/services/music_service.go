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
)

// spotifyIDPattern: los IDs de Spotify son base62 de 22 caracteres.
var spotifyIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

// MusicService implementa la búsqueda cache-through (catálogo local con
// respaldo en Spotify) y el detalle de canciones, artistas y álbumes.
type MusicService struct {
	catalog   ports.CatalogStore
	importer  ports.CatalogImporter
	provider  ports.MusicProvider  // nil: búsqueda solo local
	queue     ports.EmbeddingQueue // nil: no se encolan embeddings
	threshold float64
}

// NewMusicService crea el servicio. provider y queue pueden ser nil (sin
// credenciales de Spotify o de FreqBlog). threshold <= 0 usa el default.
func NewMusicService(catalog ports.CatalogStore, importer ports.CatalogImporter, provider ports.MusicProvider, queue ports.EmbeddingQueue, threshold float64) *MusicService {
	if threshold <= 0 {
		threshold = DefaultFallbackThreshold
	}
	return &MusicService{catalog: catalog, importer: importer, provider: provider, queue: queue, threshold: threshold}
}

// localResults es el resultado de una búsqueda local de cualquier tipo.
type localResults struct {
	results any
	count   int
	best    float64 // puntaje del mejor resultado (0 si no hay)
}

// Search busca primero en el catálogo local y consulta Spotify si no hay
// resultados o si el mejor puntaje no alcanza el umbral. Lo que devuelve
// Spotify se persiste y las canciones nuevas se encolan para embedding.
// limit = 0 usa DefaultSearchLimit.
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
	result := &domain.SearchResult{Source: domain.SourceLocalCache, Query: query, Type: searchType, Count: local.count, Results: local.results}

	if (local.count > 0 && local.best >= s.threshold) || s.provider == nil {
		return result, nil
	}

	remote, err := s.searchRemote(ctx, query, searchType, limit)
	if err != nil {
		if local.count > 0 {
			log.Printf("búsqueda en Spotify falló, se responden resultados locales: %v", err)
			return result, nil
		}
		log.Printf("búsqueda en Spotify falló: %v", err)
		return nil, domain.ErrMusicProviderUnavailable
	}
	if remote.count == 0 && local.count > 0 {
		return result, nil
	}
	return &domain.SearchResult{Source: domain.SourceSpotifyFallback, Query: query, Type: searchType, Count: remote.count, Results: remote.results}, nil
}

func (s *MusicService) searchLocal(ctx context.Context, query, searchType string, limit int) (localResults, error) {
	switch searchType {
	case domain.SearchTypeArtist:
		rows, err := s.catalog.SearchArtistsTrigram(ctx, sqlc.SearchArtistsTrigramParams{Query: query, MaxResults: int32(limit)})
		if err != nil {
			return localResults{}, fmt.Errorf("music: buscar artistas: %w", err)
		}
		out := make([]domain.Artist, len(rows))
		for i, r := range rows {
			out[i] = artistFromRow(sqlc.GetArtistRow{ID: r.ID, SpotifyID: r.SpotifyID, Name: r.Name, Genres: r.Genres, Popularity: r.Popularity, ImageUrl: r.ImageUrl, CreatedAt: r.CreatedAt})
		}
		return localResults{results: out, count: len(out), best: bestScore(len(rows), func(i int) float64 { return rows[i].Score })}, nil
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
		for i, r := range rows {
			out[i] = domain.Track{
				ID:               r.ID,
				SpotifyID:        r.TrackID.String,
				TrackName:        r.TrackName.String,
				ArtistName:       r.ArtistName.String,
				AlbumTitle:       stringPtr(r.AlbumTitle),
				Year:             int32Ptr(r.Year),
				Genre:            stringPtr(r.Genre),
				Popularity:       int32Ptr(r.Popularity),
				DurationMs:       int32Ptr(r.DurationMs),
				HasEmbedding:     r.HasEmbedding,
				PendingEmbedding: r.PendingEmbedding,
			}
		}
		return localResults{results: out, count: len(out), best: bestScore(len(rows), func(i int) float64 { return rows[i].Score })}, nil
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
		var jobs []domain.EmbeddingJob
		for i, song := range saved {
			out[i] = song.Track
			if song.PendingEmbedding {
				job := domain.EmbeddingJob{TrackID: song.SpotifyID, Attempts: song.EmbeddingAttempts}
				if song.ISRC != nil {
					job.ISRC = *song.ISRC
				}
				jobs = append(jobs, job)
			}
		}
		if s.queue != nil && len(jobs) > 0 {
			s.queue.Enqueue(jobs...)
		}
		return localResults{results: out, count: len(out)}, nil
	}
}

// GetTrack devuelve el detalle de una canción por id local (entero) o por
// su ID de Spotify.
func (s *MusicService) GetTrack(ctx context.Context, id string) (*domain.TrackDetail, error) {
	var arg sqlc.GetTrackParams
	if n, err := strconv.ParseInt(id, 10, 32); err == nil && n > 0 {
		arg.ID = int32(n)
	} else if spotifyIDPattern.MatchString(id) {
		arg.SpotifyID = id
	} else {
		return nil, domain.NewValidationError("id debe ser un entero positivo o un ID de Spotify")
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
