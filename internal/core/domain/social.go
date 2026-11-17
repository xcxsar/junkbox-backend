package domain

import (
	"time"

	"github.com/google/uuid"
)

// Pestañas del feed de reseñas (GET /feed).
const (
	// FeedTabPopular ("Para ti"): reseñas de otros usuarios por día local,
	// del más reciente al más antiguo, y por popularidad dentro del día.
	FeedTabPopular = "popular"
	// FeedTabFollowing ("Siguiendo"): reseñas de los usuarios seguidos, de
	// la más nueva a la más antigua.
	FeedTabFollowing = "following"
)

// FeedPage es la respuesta de GET /feed. Timezone solo se informa en
// "popular". NextCursor es nil en la última página.
type FeedPage struct {
	Tab        string   `json:"tab"`
	Timezone   string   `json:"timezone,omitempty"`
	Limit      int      `json:"limit"`
	NextCursor *string  `json:"next_cursor"`
	Reviews    []Review `json:"reviews"`
}

// PublicProfile es el perfil público de GET /users/{id}. FollowedByMe es si
// quien pide sigue a este usuario.
type PublicProfile struct {
	ID             uuid.UUID `json:"id"`
	Username       string    `json:"username"`
	DisplayName    *string   `json:"display_name"`
	AvatarURL      *string   `json:"avatar_url"`
	CreatedAt      time.Time `json:"created_at"`
	FollowersCount int64     `json:"followers_count"`
	FollowingCount int64     `json:"following_count"`
	ReviewsCount   int64     `json:"reviews_count"`
	FollowedByMe   bool      `json:"followed_by_me"`
	IsMe           bool      `json:"is_me"`
}

// FollowEntry es un usuario en una lista de seguidores o seguidos.
type FollowEntry struct {
	ReviewAuthor
	FollowedAt   time.Time `json:"followed_at"`
	FollowedByMe bool      `json:"followed_by_me"`
}

// FollowPage es la respuesta de GET /users/{id}/followers y /following.
type FollowPage struct {
	UserID     uuid.UUID     `json:"user_id"`
	Limit      int           `json:"limit"`
	NextCursor *string       `json:"next_cursor"`
	Users      []FollowEntry `json:"users"`
}

// LikeResult es la respuesta de POST/DELETE /reviews/{id}/like.
type LikeResult struct {
	ReviewID   uuid.UUID `json:"review_id"`
	Liked      bool      `json:"liked"`
	LikesCount int32     `json:"likes_count"`
}

// Comment es un comentario de una reseña. ParentID es nil en un raíz. Un
// raíz borrado que aún tiene respuestas sale con Deleted y Content nil.
// RepliesCount solo aplica a los raíces.
type Comment struct {
	ID           uuid.UUID    `json:"id"`
	ReviewID     uuid.UUID    `json:"review_id"`
	ParentID     *uuid.UUID   `json:"parent_id"`
	Author       ReviewAuthor `json:"author"`
	Content      *string      `json:"content"`
	Deleted      bool         `json:"deleted"`
	RepliesCount int64        `json:"replies_count"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// CommentPage es la respuesta de GET /reviews/{id}/comments (ParentID nil)
// y de GET /comments/{id}/replies.
type CommentPage struct {
	ReviewID   uuid.UUID  `json:"review_id"`
	ParentID   *uuid.UUID `json:"parent_id"`
	Limit      int        `json:"limit"`
	NextCursor *string    `json:"next_cursor"`
	Comments   []Comment  `json:"comments"`
}
