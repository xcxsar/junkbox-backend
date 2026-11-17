package postgres

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

// CommentRemover implementa ports.CommentRemover.
type CommentRemover struct {
	db *sql.DB
}

// NewCommentRemover crea el adaptador sobre el pool dado.
func NewCommentRemover(db *sql.DB) *CommentRemover {
	return &CommentRemover{db: db}
}

var _ ports.CommentRemover = (*CommentRemover)(nil)

// RemoveComment bloquea el comentario y decide en la misma transacción: así
// una respuesta que llega a la vez espera (su FK toma FOR KEY SHARE sobre
// el raíz) y no queda colgando de un raíz borrado.
func (c *CommentRemover) RemoveComment(ctx context.Context, id uuid.UUID) (bool, error) {
	soft := false
	err := runInTx(ctx, c.db, func(q *sqlc.Queries) error {
		row, err := q.LockComment(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("postgres: bloquear comentario: %w", err)
		}
		if row.ParentID == nil {
			replies, err := q.CountReplies(ctx, &id)
			if err != nil {
				return fmt.Errorf("postgres: contar respuestas: %w", err)
			}
			if replies > 0 {
				soft = true
				if _, err := q.SoftDeleteComment(ctx, id); err != nil {
					return fmt.Errorf("postgres: marcar comentario eliminado: %w", err)
				}
				return nil
			}
		}
		if _, err := q.DeleteComment(ctx, id); err != nil {
			return fmt.Errorf("postgres: borrar comentario: %w", err)
		}
		// Si era la última respuesta de un raíz ya eliminado, el raíz sobra.
		if row.ParentID != nil {
			if err := q.DeleteEmptyDeletedRoot(ctx, *row.ParentID); err != nil {
				return fmt.Errorf("postgres: borrar raíz vacío: %w", err)
			}
		}
		return nil
	})
	return soft, err
}
