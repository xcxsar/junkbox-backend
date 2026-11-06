package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// TargetResolver resuelve el objetivo de una calificación o reseña (tipo e
// id local o de Spotify) a su id local. Lo implementa MusicService.
type TargetResolver interface {
	ResolveTarget(ctx context.Context, targetType, id string) (domain.Target, error)
}

// RatingStore son las queries de calificaciones.
type RatingStore interface {
	UpsertSongRating(ctx context.Context, arg sqlc.UpsertSongRatingParams) (sqlc.UpsertSongRatingRow, error)
	UpsertAlbumRating(ctx context.Context, arg sqlc.UpsertAlbumRatingParams) (sqlc.UpsertAlbumRatingRow, error)
	UpsertArtistRating(ctx context.Context, arg sqlc.UpsertArtistRatingParams) (sqlc.UpsertArtistRatingRow, error)
	GetUserRating(ctx context.Context, arg sqlc.GetUserRatingParams) (sqlc.Rating, error)
	GetRatingByID(ctx context.Context, id uuid.UUID) (sqlc.Rating, error)
	DeleteRating(ctx context.Context, id uuid.UUID) (int64, error)
}

// ReviewStore son las queries de reseñas.
type ReviewStore interface {
	CreateReview(ctx context.Context, arg sqlc.CreateReviewParams) (uuid.UUID, error)
	GetReviewByID(ctx context.Context, id uuid.UUID) (sqlc.GetReviewByIDRow, error)
	ListReviewsByTarget(ctx context.Context, arg sqlc.ListReviewsByTargetParams) ([]sqlc.ListReviewsByTargetRow, error)
	CountReviewsByTarget(ctx context.Context, arg sqlc.CountReviewsByTargetParams) (int64, error)
	UpdateReview(ctx context.Context, arg sqlc.UpdateReviewParams) (int64, error)
	DeleteReview(ctx context.Context, id uuid.UUID) (int64, error)
}
