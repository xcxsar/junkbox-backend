package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// RecommendationStore son las queries del feed de Inicio. Si el usuario no
// tiene semilla, GetRecommendationSeed devuelve sql.ErrNoRows.
type RecommendationStore interface {
	GetRecommendationSeed(ctx context.Context, userID uuid.UUID) (sqlc.GetRecommendationSeedRow, error)
	RecommendSimilarTracks(ctx context.Context, arg sqlc.RecommendSimilarTracksParams) ([]sqlc.RecommendSimilarTracksRow, error)
	ListPopularTracks(ctx context.Context, arg sqlc.ListPopularTracksParams) ([]sqlc.ListPopularTracksRow, error)
}
