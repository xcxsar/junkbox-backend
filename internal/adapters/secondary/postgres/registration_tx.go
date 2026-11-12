package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// RegistrationTx implementa ports.RegistrationTx: el alta del usuario y sus
// consentimientos corren en la misma transacción.
type RegistrationTx struct {
	db *sql.DB
}

// NewRegistrationTx crea el adaptador sobre el pool dado.
func NewRegistrationTx(db *sql.DB) *RegistrationTx {
	return &RegistrationTx{db: db}
}

var _ ports.RegistrationTx = (*RegistrationTx)(nil)

// CreateUser inserta el usuario y una fila de user_consents por documento
// aceptado. El error de CreateUser se envuelve con %w para conservar su
// SQLSTATE (23505 = email o username duplicado).
func (r *RegistrationTx) CreateUser(ctx context.Context, arg sqlc.CreateUserParams, termsVersion string) (sqlc.CreateUserRow, error) {
	var created sqlc.CreateUserRow
	err := runInTx(ctx, r.db, func(q *sqlc.Queries) error {
		var err error
		created, err = q.CreateUser(ctx, arg)
		if err != nil {
			return fmt.Errorf("postgres: crear usuario: %w", err)
		}
		for _, doc := range domain.RegistrationConsents {
			if err := q.InsertUserConsent(ctx, sqlc.InsertUserConsentParams{
				UserID:   created.ID,
				Document: sqlc.ConsentDocument(doc),
				Version:  termsVersion,
			}); err != nil {
				return fmt.Errorf("postgres: guardar consentimiento %s: %w", doc, err)
			}
		}
		return nil
	})
	return created, err
}
