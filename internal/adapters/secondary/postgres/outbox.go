package postgres

import (
	"context"
	"database/sql"

	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// OutboxStore implementa ports.OutboxStore sobre email_outbox.
type OutboxStore struct {
	db *sql.DB
}

// NewOutboxStore crea el adaptador sobre el pool dado.
func NewOutboxStore(db *sql.DB) *OutboxStore {
	return &OutboxStore{db: db}
}

var _ ports.OutboxStore = (*OutboxStore)(nil)

// WithTx ejecuta fn en una transacción; los bloqueos de ClaimDueEmails se
// liberan al confirmar.
func (o *OutboxStore) WithTx(ctx context.Context, fn func(q ports.OutboxQueries) error) error {
	return runInTx(ctx, o.db, func(q *sqlc.Queries) error { return fn(q) })
}
