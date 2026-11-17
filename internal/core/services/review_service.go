package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites de las reseñas. Las longitudes se cuentan en caracteres (runas);
// los de content repiten el CHECK reviews_content_length.
const (
	MinReviewContentLen = 5
	MaxReviewContentLen = 5000
	MaxReviewTitleLen   = 200
	DefaultReviewLimit  = 20
	MaxReviewLimit      = 50
)

// ReviewService implementa las reseñas de canciones, álbumes y artistas.
type ReviewService struct {
	reviews   ports.ReviewStore
	targets   ports.TargetResolver
	summaries ports.TargetSummarizer // nil: las reseñas salen sin target.summary
}

// NewReviewService crea el servicio. summaries puede ser nil.
func NewReviewService(reviews ports.ReviewStore, targets ports.TargetResolver, summaries ports.TargetSummarizer) *ReviewService {
	return &ReviewService{reviews: reviews, targets: targets, summaries: summaries}
}

// ReviewInput son los campos editables de una reseña. Title vacío o nil
// deja la reseña sin título.
type ReviewInput struct {
	Title   *string
	Content string
}

// Create publica la reseña del usuario sobre el objetivo.
// ErrReviewAlreadyExists si ya reseñó ese objetivo.
func (s *ReviewService) Create(ctx context.Context, userID uuid.UUID, targetType, targetID string, in ReviewInput) (*domain.Review, error) {
	title, content, err := normalizeReview(in)
	if err != nil {
		return nil, err
	}
	target, err := s.targets.ResolveTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}
	songID, albumID, artistID := targetArgs(target)
	id, err := s.reviews.CreateReview(ctx, sqlc.CreateReviewParams{
		UserID:   userID,
		SongID:   songID,
		AlbumID:  albumID,
		ArtistID: artistID,
		Title:    title,
		Content:  content,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrReviewAlreadyExists
		}
		return nil, fmt.Errorf("reviews: crear reseña: %w", err)
	}
	return s.Get(ctx, userID, id)
}

// Get devuelve una reseña con su autor, el voto del autor, el resumen de su
// objetivo y los campos sociales vistos por viewerID.
func (s *ReviewService) Get(ctx context.Context, viewerID, id uuid.UUID) (*domain.Review, error) {
	r, err := s.reviews.GetReviewByID(ctx, sqlc.GetReviewByIDParams{ViewerID: viewerID, ID: id})
	if err != nil {
		return nil, notFoundOr(err, "reviews: obtener reseña")
	}
	out := reviewFromRow(sqlc.ListReviewsByTargetRow(r))
	summary, err := s.summary(ctx, targetFromColumns(r.SongID, r.AlbumID, r.ArtistID))
	if err != nil {
		return nil, err
	}
	out.Target.Summary = summary
	return &out, nil
}

// List devuelve las reseñas del objetivo, las más recientes primero.
// limit = 0 usa DefaultReviewLimit y page = 0 la primera página.
func (s *ReviewService) List(ctx context.Context, viewerID uuid.UUID, targetType, targetID string, limit, page int) (*domain.ReviewPage, error) {
	if limit == 0 {
		limit = DefaultReviewLimit
	}
	if limit < 1 || limit > MaxReviewLimit {
		return nil, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxReviewLimit))
	}
	if page == 0 {
		page = 1
	}
	// El tope evita desbordar el OFFSET (int32) con páginas absurdas.
	if page < 1 || page > math.MaxInt32/limit {
		return nil, domain.NewValidationError("page debe ser un entero positivo")
	}
	target, err := s.targets.ResolveTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}

	songID, albumID, artistID := targetArgs(target)
	total, err := s.reviews.CountReviewsByTarget(ctx, sqlc.CountReviewsByTargetParams{SongID: songID, AlbumID: albumID, ArtistID: artistID})
	if err != nil {
		return nil, fmt.Errorf("reviews: contar reseñas: %w", err)
	}
	rows, err := s.reviews.ListReviewsByTarget(ctx, sqlc.ListReviewsByTargetParams{
		ViewerID:   viewerID,
		SongID:     songID,
		AlbumID:    albumID,
		ArtistID:   artistID,
		MaxResults: int32(limit),
		Skip:       int32((page - 1) * limit),
	})
	if err != nil {
		return nil, fmt.Errorf("reviews: listar reseñas: %w", err)
	}

	// Todas las reseñas de la página son del mismo objetivo: el resumen se
	// resuelve una vez y se repite en cada una.
	summary, err := s.summary(ctx, target)
	if err != nil {
		return nil, err
	}
	reviews := make([]domain.Review, len(rows))
	for i, r := range rows {
		reviews[i] = reviewFromRow(r)
		reviews[i].Target.Summary = summary
	}
	ref := target.Ref()
	ref.Summary = summary
	return &domain.ReviewPage{
		Target:  ref,
		Page:    page,
		Limit:   limit,
		Count:   len(reviews),
		Total:   total,
		Reviews: reviews,
	}, nil
}

// Update reemplaza título y contenido de una reseña del usuario.
// ErrNotFound si no existe y ErrForbidden si es de otro usuario.
func (s *ReviewService) Update(ctx context.Context, userID, id uuid.UUID, in ReviewInput) (*domain.Review, error) {
	title, content, err := normalizeReview(in)
	if err != nil {
		return nil, err
	}
	if err := s.checkOwner(ctx, userID, id); err != nil {
		return nil, err
	}
	n, err := s.reviews.UpdateReview(ctx, sqlc.UpdateReviewParams{ID: id, Title: title, Content: content})
	if err != nil {
		return nil, fmt.Errorf("reviews: editar reseña: %w", err)
	}
	if n == 0 {
		// Otra petición la borró entre la lectura y el UPDATE.
		return nil, domain.ErrNotFound
	}
	return s.Get(ctx, userID, id)
}

// Delete borra una reseña del usuario. ErrNotFound si no existe y
// ErrForbidden si es de otro usuario.
func (s *ReviewService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.checkOwner(ctx, userID, id); err != nil {
		return err
	}
	n, err := s.reviews.DeleteReview(ctx, id)
	if err != nil {
		return fmt.Errorf("reviews: eliminar reseña: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// summary devuelve el resumen del objetivo, o nil si no hay summarizer o el
// objetivo ya no existe.
func (s *ReviewService) summary(ctx context.Context, target domain.Target) (any, error) {
	return targetSummary(ctx, s.summaries, target)
}

func targetSummary(ctx context.Context, summaries ports.TargetSummarizer, target domain.Target) (any, error) {
	if summaries == nil {
		return nil, nil
	}
	summary, err := summaries.SummarizeTarget(ctx, target)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("reviews: resumir objetivo: %w", err)
	}
	return summary, nil
}

// checkOwner distingue "no existe" (404) de "no es tuya" (403) antes de
// modificar una reseña.
func (s *ReviewService) checkOwner(ctx context.Context, userID, id uuid.UUID) error {
	owner, err := s.reviews.GetReviewOwner(ctx, id)
	if err != nil {
		return notFoundOr(err, "reviews: obtener reseña")
	}
	if owner != userID {
		return domain.ErrForbidden
	}
	return nil
}

// normalizeReview recorta espacios y valida longitudes. Un título vacío se
// guarda como NULL.
func normalizeReview(in ReviewInput) (sql.NullString, string, error) {
	content := strings.TrimSpace(in.Content)
	if n := utf8.RuneCountInString(content); n < MinReviewContentLen || n > MaxReviewContentLen {
		return sql.NullString{}, "", domain.NewValidationError(fmt.Sprintf("content debe tener entre %d y %d caracteres", MinReviewContentLen, MaxReviewContentLen))
	}
	var title sql.NullString
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if utf8.RuneCountInString(t) > MaxReviewTitleLen {
			return sql.NullString{}, "", domain.NewValidationError(fmt.Sprintf("title debe tener como máximo %d caracteres", MaxReviewTitleLen))
		}
		title = sql.NullString{String: t, Valid: t != ""}
	}
	return title, content, nil
}

// reviewFromRow convierte una fila de reseña; GetReviewByIDRow y las filas de
// los feeds tienen las mismas columnas y se convierten a
// ListReviewsByTargetRow.
func reviewFromRow(r sqlc.ListReviewsByTargetRow) domain.Review {
	return domain.Review{
		ID:              r.ID,
		Target:          targetRefFromColumns(r.SongID, r.AlbumID, r.ArtistID),
		Title:           stringPtr(r.Title),
		Content:         r.Content,
		RatingScore:     r.RatingScore,
		LikesCount:      r.LikesCount,
		CommentsCount:   r.CommentsCount,
		LikedByMe:       r.LikedByMe,
		FollowingAuthor: r.FollowingAuthor,
		Author: domain.ReviewAuthor{
			ID:          r.UserID,
			Username:    r.AuthorUsername,
			DisplayName: stringPtr(r.AuthorDisplayName),
			AvatarURL:   stringPtr(r.AuthorAvatarUrl),
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
