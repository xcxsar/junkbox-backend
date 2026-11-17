package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// RecommendationStore implementa ports.RecommendationStore.
type RecommendationStore struct {
	db *sql.DB
	q  *sqlc.Queries
}

// NewRecommendationStore crea el adaptador sobre el pool dado.
func NewRecommendationStore(db *sql.DB) *RecommendationStore {
	return &RecommendationStore{db: db, q: sqlc.New(db)}
}

var _ ports.RecommendationStore = (*RecommendationStore)(nil)

// GetRecommendationSeed devuelve la última canción que el usuario calificó
// o reseñó (sql.ErrNoRows si no hay ninguna con embedding).
func (s *RecommendationStore) GetRecommendationSeed(ctx context.Context, userID uuid.UUID) (sqlc.GetRecommendationSeedRow, error) {
	return s.q.GetRecommendationSeed(ctx, userID)
}

// RecommendSimilarTracks corre la búsqueda vectorial en una transacción de
// solo lectura con ivfflat.iterative_scan: sin él, el índice IVFFlat
// (probes = 1) revisa una sola lista y, con filtro de género, devuelve
// menos canciones que el LIMIT. SET LOCAL no sale de la transacción, así
// que la conexión vuelve al pool sin el ajuste.
func (s *RecommendationStore) RecommendSimilarTracks(ctx context.Context, arg sqlc.RecommendSimilarTracksParams) ([]sqlc.RecommendSimilarTracksRow, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("postgres: iniciar transacción: %w", err)
	}
	// Solo lectura: no hay nada que confirmar.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SET LOCAL ivfflat.iterative_scan = relaxed_order"); err != nil {
		return nil, fmt.Errorf("postgres: activar iterative_scan: %w", err)
	}
	return sqlc.New(tx).RecommendSimilarTracks(ctx, arg)
}

// ListPopularTracks devuelve las canciones más populares que el usuario
// aún no calificó ni reseñó.
func (s *RecommendationStore) ListPopularTracks(ctx context.Context, arg sqlc.ListPopularTracksParams) ([]sqlc.ListPopularTracksRow, error) {
	return s.q.ListPopularTracks(ctx, arg)
}
