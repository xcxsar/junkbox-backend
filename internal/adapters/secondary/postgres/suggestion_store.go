package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// SuggestionRefresher implementa ports.SuggestionRefresher.
type SuggestionRefresher struct {
	db *sql.DB
}

// NewSuggestionRefresher crea el adaptador sobre el pool dado.
func NewSuggestionRefresher(db *sql.DB) *SuggestionRefresher {
	return &SuggestionRefresher{db: db}
}

var _ ports.SuggestionRefresher = (*SuggestionRefresher)(nil)

// RefreshSuggestions reconstruye user_taste_profiles y user_suggestions en
// una sola transacción: el endpoint nunca ve las tablas a medio llenar.
func (r *SuggestionRefresher) RefreshSuggestions(ctx context.Context, minScore float64, maxPerUser int32) (int64, int64, error) {
	var profiles, suggestions int64
	err := runInTx(ctx, r.db, func(q *sqlc.Queries) error {
		if err := q.DeleteTasteProfiles(ctx); err != nil {
			return fmt.Errorf("postgres: borrar perfiles acústicos: %w", err)
		}
		n, err := q.InsertTasteProfiles(ctx, minScore)
		if err != nil {
			return fmt.Errorf("postgres: calcular perfiles acústicos: %w", err)
		}
		profiles = n
		if err := q.DeleteSuggestions(ctx); err != nil {
			return fmt.Errorf("postgres: borrar sugerencias: %w", err)
		}
		n, err = q.InsertSuggestions(ctx, sqlc.InsertSuggestionsParams{MinScore: minScore, MaxPerUser: maxPerUser})
		if err != nil {
			return fmt.Errorf("postgres: calcular sugerencias: %w", err)
		}
		suggestions = n
		return nil
	})
	return profiles, suggestions, err
}
