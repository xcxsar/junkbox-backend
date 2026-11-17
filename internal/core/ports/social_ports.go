package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// FeedStore son las queries de los feeds de reseñas.
type FeedStore interface {
	ListPopularFeed(ctx context.Context, arg sqlc.ListPopularFeedParams) ([]sqlc.ListPopularFeedRow, error)
	ListFollowingFeed(ctx context.Context, arg sqlc.ListFollowingFeedParams) ([]sqlc.ListFollowingFeedRow, error)
}

// SocialStore son las queries de seguidores y Me gusta.
type SocialStore interface {
	GetPublicProfile(ctx context.Context, arg sqlc.GetPublicProfileParams) (sqlc.GetPublicProfileRow, error)
	FollowUser(ctx context.Context, arg sqlc.FollowUserParams) error
	UnfollowUser(ctx context.Context, arg sqlc.UnfollowUserParams) error
	ListFollowers(ctx context.Context, arg sqlc.ListFollowersParams) ([]sqlc.ListFollowersRow, error)
	ListFollowing(ctx context.Context, arg sqlc.ListFollowingParams) ([]sqlc.ListFollowingRow, error)
	LikeReview(ctx context.Context, arg sqlc.LikeReviewParams) error
	UnlikeReview(ctx context.Context, arg sqlc.UnlikeReviewParams) error
	GetReviewLikesCount(ctx context.Context, id uuid.UUID) (int32, error)
}

// CommentStore son las queries de lectura y escritura de comentarios.
type CommentStore interface {
	ReviewExists(ctx context.Context, id uuid.UUID) (bool, error)
	CreateComment(ctx context.Context, arg sqlc.CreateCommentParams) (uuid.UUID, error)
	GetComment(ctx context.Context, id uuid.UUID) (sqlc.GetCommentRow, error)
	ListRootComments(ctx context.Context, arg sqlc.ListRootCommentsParams) ([]sqlc.ListRootCommentsRow, error)
	ListReplies(ctx context.Context, arg sqlc.ListRepliesParams) ([]sqlc.ListRepliesRow, error)
	UpdateComment(ctx context.Context, arg sqlc.UpdateCommentParams) (int64, error)
}

// CommentRemover borra un comentario en una transacción: un raíz con
// respuestas queda como eliminado (soft = true) y el resto se borra; si era
// la última respuesta de un raíz ya eliminado, también borra el raíz. Si el
// comentario no existe devuelve domain.ErrNotFound.
type CommentRemover interface {
	RemoveComment(ctx context.Context, id uuid.UUID) (soft bool, err error)
}

// SuggestionStore son las lecturas de las sugerencias de usuarios.
type SuggestionStore interface {
	ListSuggestionsForUser(ctx context.Context, arg sqlc.ListSuggestionsForUserParams) ([]sqlc.ListSuggestionsForUserRow, error)
	ListPopularUsers(ctx context.Context, arg sqlc.ListPopularUsersParams) ([]sqlc.ListPopularUsersRow, error)
	ListArtistsByIDs(ctx context.Context, ids []uuid.UUID) ([]sqlc.ListArtistsByIDsRow, error)
}

// SuggestionRefresher recalcula en una transacción los perfiles acústicos y
// las sugerencias de todos los usuarios. Devuelve cuántos perfiles y
// sugerencias quedaron.
type SuggestionRefresher interface {
	RefreshSuggestions(ctx context.Context, minScore float64, maxPerUser int32) (profiles, suggestions int64, err error)
}
