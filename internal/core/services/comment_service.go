package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// MaxCommentLen es la longitud máxima de un comentario en caracteres
// (repite el CHECK review_comments_content).
const MaxCommentLen = 1000

// CommentService implementa los comentarios de reseñas: hilos de un nivel
// (raíz → respuestas).
type CommentService struct {
	store   ports.CommentStore
	remover ports.CommentRemover
}

// NewCommentService crea el servicio.
func NewCommentService(store ports.CommentStore, remover ports.CommentRemover) *CommentService {
	return &CommentService{store: store, remover: remover}
}

// Create publica un comentario en la reseña. Con parentID es una
// respuesta: si el padre es a su vez una respuesta, se cuelga de su raíz.
func (s *CommentService) Create(ctx context.Context, userID, reviewID uuid.UUID, parentID *uuid.UUID, content string) (*domain.Comment, error) {
	content, err := normalizeComment(content)
	if err != nil {
		return nil, err
	}
	exists, err := s.store.ReviewExists(ctx, reviewID)
	if err != nil {
		return nil, fmt.Errorf("comentarios: comprobar reseña: %w", err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	if parentID != nil {
		parent, err := s.store.GetComment(ctx, *parentID)
		if err != nil {
			return nil, notFoundOr(err, "comentarios: obtener comentario padre")
		}
		if parent.ReviewID != reviewID {
			return nil, domain.NewValidationError("parent_id no pertenece a esta reseña")
		}
		if parent.ParentID != nil {
			parentID = parent.ParentID
		} else if parent.DeletedAt.Valid {
			return nil, domain.NewValidationError("no se puede responder a un comentario eliminado")
		}
	}

	id, err := s.store.CreateComment(ctx, sqlc.CreateCommentParams{
		ReviewID: reviewID,
		UserID:   userID,
		ParentID: parentID,
		Content:  sql.NullString{String: content, Valid: true},
	})
	if isForeignKeyViolation(err) {
		// La reseña o el raíz se borraron mientras tanto.
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("comentarios: crear: %w", err)
	}
	return s.get(ctx, id)
}

// List devuelve los comentarios raíz de la reseña, del más antiguo al más
// nuevo.
func (s *CommentService) List(ctx context.Context, reviewID uuid.UUID, rawCursor string, limit int) (*domain.CommentPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	scope := "comments:" + reviewID.String()
	cur, err := decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	exists, err := s.store.ReviewExists(ctx, reviewID)
	if err != nil {
		return nil, fmt.Errorf("comentarios: comprobar reseña: %w", err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	cursorID, cursorAt := cur.args()
	rows, err := s.store.ListRootComments(ctx, sqlc.ListRootCommentsParams{
		ReviewID: reviewID, CursorID: cursorID, CursorCreatedAt: cursorAt, MaxResults: int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("comentarios: listar: %w", err)
	}
	page := make([]sqlc.GetCommentRow, len(rows))
	for i, r := range rows {
		page[i] = sqlc.GetCommentRow(r)
	}
	return commentPage(page, reviewID, nil, scope, limit), nil
}

// Replies devuelve las respuestas de un comentario raíz, de la más antigua
// a la más nueva.
func (s *CommentService) Replies(ctx context.Context, commentID uuid.UUID, rawCursor string, limit int) (*domain.CommentPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	scope := "replies:" + commentID.String()
	cur, err := decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	root, err := s.store.GetComment(ctx, commentID)
	if err != nil {
		return nil, notFoundOr(err, "comentarios: obtener comentario")
	}
	if root.ParentID != nil {
		return nil, domain.NewValidationError("las respuestas no tienen respuestas: usa el comentario raíz")
	}
	cursorID, cursorAt := cur.args()
	rows, err := s.store.ListReplies(ctx, sqlc.ListRepliesParams{
		ParentID: &commentID, CursorID: cursorID, CursorCreatedAt: cursorAt, MaxResults: int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("comentarios: listar respuestas: %w", err)
	}
	page := make([]sqlc.GetCommentRow, len(rows))
	for i, r := range rows {
		page[i] = sqlc.GetCommentRow(r)
	}
	return commentPage(page, root.ReviewID, &commentID, scope, limit), nil
}

// Update reemplaza el contenido de un comentario del usuario. ErrNotFound
// si no existe o está eliminado y ErrForbidden si es de otro usuario.
func (s *CommentService) Update(ctx context.Context, userID, id uuid.UUID, content string) (*domain.Comment, error) {
	content, err := normalizeComment(content)
	if err != nil {
		return nil, err
	}
	c, err := s.store.GetComment(ctx, id)
	if err != nil {
		return nil, notFoundOr(err, "comentarios: obtener comentario")
	}
	if c.DeletedAt.Valid {
		return nil, domain.ErrNotFound
	}
	if c.UserID != userID {
		return nil, domain.ErrForbidden
	}
	n, err := s.store.UpdateComment(ctx, sqlc.UpdateCommentParams{ID: id, Content: sql.NullString{String: content, Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("comentarios: editar: %w", err)
	}
	if n == 0 {
		return nil, domain.ErrNotFound
	}
	return s.get(ctx, id)
}

// Delete borra un comentario del usuario. Un raíz con respuestas queda como
// "comentario eliminado" para conservar la conversación.
func (s *CommentService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	c, err := s.store.GetComment(ctx, id)
	if err != nil {
		return notFoundOr(err, "comentarios: obtener comentario")
	}
	if c.DeletedAt.Valid {
		return domain.ErrNotFound
	}
	if c.UserID != userID {
		return domain.ErrForbidden
	}
	if _, err := s.remover.RemoveComment(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *CommentService) get(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	r, err := s.store.GetComment(ctx, id)
	if err != nil {
		return nil, notFoundOr(err, "comentarios: obtener comentario")
	}
	c := commentFromRow(r)
	return &c, nil
}

func normalizeComment(content string) (string, error) {
	content = strings.TrimSpace(content)
	if n := utf8.RuneCountInString(content); n < 1 || n > MaxCommentLen {
		return "", domain.NewValidationError(fmt.Sprintf("content debe tener entre 1 y %d caracteres", MaxCommentLen))
	}
	return content, nil
}

func commentPage(rows []sqlc.GetCommentRow, reviewID uuid.UUID, parentID *uuid.UUID, scope string, limit int) *domain.CommentPage {
	page := &domain.CommentPage{ReviewID: reviewID, ParentID: parentID, Limit: limit, Comments: []domain.Comment{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = keysetCursor{Scope: scope, CreatedAt: last.CreatedAt, ID: last.ID}.encode()
	}
	for _, r := range rows {
		page.Comments = append(page.Comments, commentFromRow(r))
	}
	return page
}

func commentFromRow(r sqlc.GetCommentRow) domain.Comment {
	return domain.Comment{
		ID:       r.ID,
		ReviewID: r.ReviewID,
		ParentID: r.ParentID,
		Author: domain.ReviewAuthor{
			ID:          r.UserID,
			Username:    r.AuthorUsername,
			DisplayName: stringPtr(r.AuthorDisplayName),
			AvatarURL:   stringPtr(r.AuthorAvatarUrl),
		},
		Content:      stringPtr(r.Content),
		Deleted:      r.DeletedAt.Valid,
		RepliesCount: r.RepliesCount,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}
