package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites del feed de Inicio.
const (
	// DefaultHomeLimit coincide con la plantilla de 8 canciones que usaba
	// el frontend.
	DefaultHomeLimit = 8
	MaxHomeLimit     = 50
)

// RecommendationService arma el feed de Inicio con el motor de
// recomendaciones de ../recommendation-engine (recommend_songs): canciones
// acústicamente parecidas a la última que el usuario calificó o reseñó,
// del mismo género si se conoce. Sin semilla, responde las más populares.
type RecommendationService struct {
	store  ports.RecommendationStore
	enrich SearchEnrichment
}

// NewRecommendationService crea el servicio. enrich es la misma
// configuración de imágenes que usa la búsqueda.
func NewRecommendationService(store ports.RecommendationStore, enrich SearchEnrichment) *RecommendationService {
	return &RecommendationService{store: store, enrich: enrich.withDefaults()}
}

// Home devuelve el feed de Inicio del usuario. limit = 0 usa
// DefaultHomeLimit.
func (s *RecommendationService) Home(ctx context.Context, userID uuid.UUID, limit int) (*domain.HomeFeed, error) {
	if limit == 0 {
		limit = DefaultHomeLimit
	}
	if limit < 1 || limit > MaxHomeLimit {
		return nil, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxHomeLimit))
	}
	section, err := s.madeForYou(ctx, userID, int32(limit))
	if err != nil {
		return nil, err
	}
	return &domain.HomeFeed{Sections: []domain.HomeSection{section}}, nil
}

// madeForYou arma "Hecho para tus oídos". Si el filtro de género deja la
// lista vacía se repite sin género, y si aun así no hay nada, se usan las
// populares.
func (s *RecommendationService) madeForYou(ctx context.Context, userID uuid.UUID, limit int32) (domain.HomeSection, error) {
	section := domain.HomeSection{ID: domain.HomeSectionMadeForYou, Title: domain.HomeSectionMadeForYouTitle}

	seed, err := s.store.GetRecommendationSeed(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return section, fmt.Errorf("recomendaciones: obtener semilla: %w", err)
	default:
		arg := sqlc.RecommendSimilarTracksParams{
			Embedding:      seed.Embedding,
			SeedID:         seed.ID,
			Genre:          seed.Genre,
			SeedTrackName:  seed.TrackName.String,
			SeedArtistName: seed.ArtistName.String,
			UserID:         userID,
			MaxResults:     limit,
		}
		rows, err := s.store.RecommendSimilarTracks(ctx, arg)
		if err == nil && len(rows) == 0 && arg.Genre.Valid {
			arg.Genre = sql.NullString{}
			rows, err = s.store.RecommendSimilarTracks(ctx, arg)
		}
		if err != nil {
			return section, fmt.Errorf("recomendaciones: canciones similares: %w", err)
		}
		if len(rows) > 0 {
			popularShape := make([]sqlc.ListPopularTracksRow, len(rows))
			for i, r := range rows {
				popularShape[i] = sqlc.ListPopularTracksRow(r)
			}
			section.Strategy = domain.StrategySimilarToLast
			section.Seed = &domain.RecommendationSeed{
				ID:         seed.ID,
				SpotifyID:  seed.TrackID.String,
				TrackName:  seed.TrackName.String,
				ArtistName: seed.ArtistName.String,
				Genre:      stringPtr(seed.Genre),
			}
			section.Items = s.items(ctx, popularShape)
			return section, nil
		}
	}

	rows, err := s.store.ListPopularTracks(ctx, sqlc.ListPopularTracksParams{UserID: userID, MaxResults: limit})
	if err != nil {
		return section, fmt.Errorf("recomendaciones: canciones populares: %w", err)
	}
	section.Strategy = domain.StrategyPopular
	section.Items = s.items(ctx, rows)
	return section, nil
}

// items convierte las filas en elementos del feed y completa con Spotify
// las imágenes que faltan, igual que la búsqueda. RecommendSimilarTracks
// devuelve las mismas columnas, convertidas a esta forma.
func (s *RecommendationService) items(ctx context.Context, rows []sqlc.ListPopularTracksRow) []domain.HomeItem {
	tracks := make([]domain.Track, len(rows))
	artists := make([]string, len(rows))
	var jobs []domain.EnrichmentJob
	for i, r := range rows {
		tracks[i] = domain.Track{
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
			Ratings:          ratingStats(r.RatingsCount, r.AvgRating),
		}
		artists[i] = r.ArtistSpotifyID.String
		jobs = append(jobs, trackEnrichmentState{
			SpotifyID:            r.TrackID.String,
			AlbumID:              r.AlbumID,
			LinkAttempts:         r.LinkAttempts,
			HasArtistImage:       r.ArtistImageUrl.Valid,
			ArtistSpotifyID:      r.ArtistSpotifyID.String,
			ArtistEnriched:       r.ArtistEnriched,
			ArtistEnrichAttempts: r.ArtistEnrichAttempts,
		}.jobs()...)
	}
	if got, ok := resolveEnrichment(ctx, s.enrich, jobs); ok {
		applyTrackEnrichment(tracks, artists, got)
	}

	items := make([]domain.HomeItem, len(tracks))
	for i, t := range tracks {
		items[i] = domain.HomeItem{Type: domain.HomeItemTrack, Track: t}
	}
	return items
}
