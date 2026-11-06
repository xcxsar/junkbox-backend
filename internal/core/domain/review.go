package domain

import (
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Errores de calificaciones y reseñas. Los handlers los traducen a HTTP.
var (
	// ErrForbidden: la calificación o reseña pertenece a otro usuario.
	ErrForbidden = errors.New("social: el recurso pertenece a otro usuario")
	// ErrReviewAlreadyExists: el usuario ya reseñó ese objetivo; se edita
	// con PUT /reviews/{id}.
	ErrReviewAlreadyExists = errors.New("social: ya existe una reseña del usuario para este objetivo")
)

// Tipos de objetivo que se pueden calificar y reseñar.
const (
	TargetSong   = "song"
	TargetAlbum  = "album"
	TargetArtist = "artist"
)

// Target es un objetivo del catálogo ya resuelto a su id local. Según Type,
// solo uno de SongID, AlbumID o ArtistID tiene valor.
type Target struct {
	Type     string
	SongID   int32
	AlbumID  uuid.UUID
	ArtistID uuid.UUID
}

// Ref devuelve la representación pública del objetivo.
func (t Target) Ref() TargetRef {
	switch t.Type {
	case TargetSong:
		return TargetRef{Type: t.Type, ID: strconv.Itoa(int(t.SongID))}
	case TargetAlbum:
		return TargetRef{Type: t.Type, ID: t.AlbumID.String()}
	default:
		return TargetRef{Type: t.Type, ID: t.ArtistID.String()}
	}
}

// TargetRef identifica un objetivo en las respuestas: id entero (canción) o
// UUID (álbum o artista), siempre el local.
type TargetRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// RatingStats son las estadísticas de calificación de un objetivo del
// catálogo. Average es nil mientras no haya votos.
type RatingStats struct {
	Count   int32    `json:"count"`
	Average *float64 `json:"average"`
}

// Rating es el voto de un usuario sobre un objetivo (0.5 a 5.0).
type Rating struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Target    TargetRef `json:"target"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReviewAuthor es el perfil público del autor de una reseña.
type ReviewAuthor struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName *string   `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Review es una reseña. RatingScore es el voto actual del autor sobre el
// mismo objetivo (nil si no calificó). LikesCount está reservado y por
// ahora siempre es 0.
type Review struct {
	ID          uuid.UUID    `json:"id"`
	Target      TargetRef    `json:"target"`
	Title       *string      `json:"title"`
	Content     string       `json:"content"`
	RatingScore *float64     `json:"rating_score"`
	LikesCount  int32        `json:"likes_count"`
	Author      ReviewAuthor `json:"author"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// ReviewPage es la respuesta de GET /reviews. Count es el número de reseñas
// de esta página y Total el de todas las del objetivo.
type ReviewPage struct {
	Target  TargetRef `json:"target"`
	Page    int       `json:"page"`
	Limit   int       `json:"limit"`
	Count   int       `json:"count"`
	Total   int64     `json:"total"`
	Reviews []Review  `json:"reviews"`
}
