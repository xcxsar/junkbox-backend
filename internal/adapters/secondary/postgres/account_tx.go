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

// AccountTx implementa ports.AccountTx: cada operación de cuenta corre en
// una transacción y encola sus correos en email_outbox dentro de ella.
type AccountTx struct {
	db *sql.DB
}

// NewAccountTx crea el adaptador sobre el pool dado.
func NewAccountTx(db *sql.DB) *AccountTx {
	return &AccountTx{db: db}
}

var _ ports.AccountTx = (*AccountTx)(nil)

// IssueEmailToken invalida los tokens pendientes del mismo propósito, guarda
// el nuevo y encola su correo.
func (a *AccountTx) IssueEmailToken(ctx context.Context, tok ports.IssuedEmailToken) error {
	purpose := sqlc.EmailTokenPurpose(tok.Purpose)
	return runInTx(ctx, a.db, func(q *sqlc.Queries) error {
		if err := q.InvalidateEmailTokens(ctx, sqlc.InvalidateEmailTokensParams{UserID: tok.UserID, Purpose: purpose}); err != nil {
			return fmt.Errorf("postgres: invalidar tokens de correo: %w", err)
		}
		if err := q.InsertEmailToken(ctx, sqlc.InsertEmailTokenParams{
			UserID:    tok.UserID,
			Purpose:   purpose,
			TokenHash: tok.TokenHash,
			ExpiresAt: tok.ExpiresAt,
		}); err != nil {
			return fmt.Errorf("postgres: guardar token de correo: %w", err)
		}
		return enqueue(ctx, q, tok.Email)
	})
}

// ResetPassword consume el token, cambia la contraseña, marca el email como
// verificado (recibir el correo lo demuestra), cierra todas las sesiones y
// encola el aviso.
func (a *AccountTx) ResetPassword(ctx context.Context, tokenHash, newHash string, notify func(sqlc.User) (domain.Email, error)) error {
	return runInTx(ctx, a.db, func(q *sqlc.Queries) error {
		userID, err := consume(ctx, q, tokenHash, domain.PurposePasswordReset)
		if err != nil {
			return err
		}
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{ID: userID, HashedPassword: newHash}); err != nil {
			return fmt.Errorf("postgres: actualizar contraseña: %w", err)
		}
		if err := q.MarkEmailVerified(ctx, userID); err != nil {
			return fmt.Errorf("postgres: marcar email verificado: %w", err)
		}
		if err := q.RevokeAllRefreshTokensByUser(ctx, userID); err != nil {
			return fmt.Errorf("postgres: revocar refresh tokens: %w", err)
		}
		if err := q.RevokeAllUserAccessTokens(ctx, userID); err != nil {
			return fmt.Errorf("postgres: corte global de access tokens: %w", err)
		}
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("postgres: leer usuario: %w", err)
		}
		email, err := notify(user)
		if err != nil {
			return err
		}
		return enqueue(ctx, q, email)
	})
}

// VerifyEmail consume el token de verificación y marca el email.
func (a *AccountTx) VerifyEmail(ctx context.Context, tokenHash string) error {
	return runInTx(ctx, a.db, func(q *sqlc.Queries) error {
		userID, err := consume(ctx, q, tokenHash, domain.PurposeEmailVerification)
		if err != nil {
			return err
		}
		if err := q.MarkEmailVerified(ctx, userID); err != nil {
			return fmt.Errorf("postgres: marcar email verificado: %w", err)
		}
		return nil
	})
}

// consume marca el token como usado y devuelve su usuario, o
// domain.ErrInvalidEmailToken si no existe, ya se usó o expiró.
func consume(ctx context.Context, q *sqlc.Queries, tokenHash string, purpose domain.EmailTokenPurpose) (uuid.UUID, error) {
	id, err := q.ConsumeEmailToken(ctx, sqlc.ConsumeEmailTokenParams{
		TokenHash: tokenHash,
		Purpose:   sqlc.EmailTokenPurpose(purpose),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return id, domain.ErrInvalidEmailToken
	}
	if err != nil {
		return id, fmt.Errorf("postgres: consumir token de correo: %w", err)
	}
	return id, nil
}

func enqueue(ctx context.Context, q *sqlc.Queries, email domain.Email) error {
	if err := q.EnqueueEmail(ctx, sqlc.EnqueueEmailParams{
		ToAddress: email.To,
		Subject:   email.Subject,
		TextBody:  email.Text,
		HtmlBody:  email.HTML,
	}); err != nil {
		return fmt.Errorf("postgres: encolar correo: %w", err)
	}
	return nil
}
