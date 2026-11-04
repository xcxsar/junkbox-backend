package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// runInTx ejecuta fn con queries ligadas a una transacción: confirma si fn
// termina sin error y revierte en caso contrario. El error de fn se devuelve
// sin envolver para que el llamador pueda compararlo con errors.Is.
func runInTx(ctx context.Context, db *sql.DB, fn func(q *sqlc.Queries) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: iniciar transacción: %w", err)
	}
	if err := fn(sqlc.New(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: confirmar transacción: %w", err)
	}
	return nil
}
