package services

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Escala de calificación: de 0.5 a 5.0 estrellas en pasos de 0.5. La misma
// regla está en el CHECK ratings_score_range.
const (
	MinRatingScore = 0.5
	MaxRatingScore = 5.0
)

const invalidScoreMsg = "la calificación debe estar entre 0.5 y 5.0 en incrementos de 0.5"

// RatingService implementa las calificaciones de canciones, álbumes y
// artistas. Las estadísticas del objetivo (ratings_count, avg_rating) las
// mantiene un trigger de PostgreSQL.
type RatingService struct {
	ratings ports.RatingStore
	targets ports.TargetResolver
}

// NewRatingService crea el servicio.
func NewRatingService(ratings ports.RatingStore, targets ports.TargetResolver) *RatingService {
	return &RatingService{ratings: ratings, targets: targets}
}

// Rate crea o reemplaza el voto del usuario sobre el objetivo. created es
// true si el voto es nuevo. score nil significa que el cliente no lo envió.
func (s *RatingService) Rate(ctx context.Context, userID uuid.UUID, targetType, targetID string, score *float64) (*domain.Rating, bool, error) {
	if score == nil {
		return nil, false, domain.NewValidationError("score es requerido")
	}
	if !validScore(*score) {
		return nil, false, domain.NewValidationError(invalidScoreMsg)
	}
	target, err := s.targets.ResolveTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, false, err
	}

	// Las tres filas de upsert tienen las mismas columnas: se convierten a
	// la de canciones.
	var row sqlc.UpsertSongRatingRow
	switch target.Type {
	case domain.TargetSong:
		row, err = s.ratings.UpsertSongRating(ctx, sqlc.UpsertSongRatingParams{UserID: userID, SongID: target.SongID, Score: *score})
	case domain.TargetAlbum:
		var r sqlc.UpsertAlbumRatingRow
		r, err = s.ratings.UpsertAlbumRating(ctx, sqlc.UpsertAlbumRatingParams{UserID: userID, AlbumID: target.AlbumID, Score: *score})
		row = sqlc.UpsertSongRatingRow(r)
	default:
		var r sqlc.UpsertArtistRatingRow
		r, err = s.ratings.UpsertArtistRating(ctx, sqlc.UpsertArtistRatingParams{UserID: userID, ArtistID: target.ArtistID, Score: *score})
		row = sqlc.UpsertSongRatingRow(r)
	}
	if err != nil {
		return nil, false, fmt.Errorf("ratings: calificar %s: %w", target.Type, err)
	}
	out := ratingFromRow(sqlc.Rating{
		ID: row.ID, UserID: row.UserID, SongID: row.SongID, AlbumID: row.AlbumID, ArtistID: row.ArtistID,
		Score: row.Score, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	})
	return &out, row.Inserted, nil
}

// GetMine devuelve el voto del usuario sobre el objetivo, o ErrNotFound si
// no lo calificó.
func (s *RatingService) GetMine(ctx context.Context, userID uuid.UUID, targetType, targetID string) (*domain.Rating, error) {
	target, err := s.targets.ResolveTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}
	songID, albumID, artistID := targetArgs(target)
	r, err := s.ratings.GetUserRating(ctx, sqlc.GetUserRatingParams{UserID: userID, SongID: songID, AlbumID: albumID, ArtistID: artistID})
	if err != nil {
		return nil, notFoundOr(err, "ratings: obtener calificación")
	}
	out := ratingFromRow(r)
	return &out, nil
}

// Delete borra un voto del usuario. ErrNotFound si no existe y
// ErrForbidden si es de otro usuario.
func (s *RatingService) Delete(ctx context.Context, userID, ratingID uuid.UUID) error {
	r, err := s.ratings.GetRatingByID(ctx, ratingID)
	if err != nil {
		return notFoundOr(err, "ratings: obtener calificación")
	}
	if r.UserID != userID {
		return domain.ErrForbidden
	}
	n, err := s.ratings.DeleteRating(ctx, ratingID)
	if err != nil {
		return fmt.Errorf("ratings: eliminar calificación: %w", err)
	}
	if n == 0 {
		// Otra petición lo borró entre la lectura y el DELETE.
		return domain.ErrNotFound
	}
	return nil
}

// validScore comprueba el rango y el paso de 0.5 (score*2 entero).
func validScore(score float64) bool {
	return score >= MinRatingScore && score <= MaxRatingScore && score*2 == math.Trunc(score*2)
}

func ratingFromRow(r sqlc.Rating) domain.Rating {
	return domain.Rating{
		ID:        r.ID,
		UserID:    r.UserID,
		Target:    targetRefFromColumns(r.SongID, r.AlbumID, r.ArtistID),
		Score:     r.Score,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// targetArgs reparte el objetivo en las tres columnas nulables del arco
// exclusivo; solo una queda con valor.
func targetArgs(t domain.Target) (sql.NullInt32, *uuid.UUID, *uuid.UUID) {
	switch t.Type {
	case domain.TargetSong:
		return sql.NullInt32{Int32: t.SongID, Valid: true}, nil, nil
	case domain.TargetAlbum:
		return sql.NullInt32{}, &t.AlbumID, nil
	default:
		return sql.NullInt32{}, nil, &t.ArtistID
	}
}

// targetRefFromColumns es la inversa de targetArgs para una fila leída.
func targetRefFromColumns(songID sql.NullInt32, albumID, artistID *uuid.UUID) domain.TargetRef {
	switch {
	case songID.Valid:
		return domain.TargetRef{Type: domain.TargetSong, ID: strconv.Itoa(int(songID.Int32))}
	case albumID != nil:
		return domain.TargetRef{Type: domain.TargetAlbum, ID: albumID.String()}
	case artistID != nil:
		return domain.TargetRef{Type: domain.TargetArtist, ID: artistID.String()}
	}
	return domain.TargetRef{}
}
