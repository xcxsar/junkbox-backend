package ports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Mailer entrega un correo al proveedor (SMTP, log…).
type Mailer interface {
	Send(ctx context.Context, email domain.Email) error
}

// EmailTokenStore son las lecturas sobre user_email_tokens fuera de una
// transacción.
type EmailTokenStore interface {
	CountRecentEmailTokens(ctx context.Context, arg sqlc.CountRecentEmailTokensParams) (int64, error)
}

// IssuedEmailToken es un token nuevo, ya hasheado, junto con el correo que
// lo entrega.
type IssuedEmailToken struct {
	UserID    uuid.UUID
	Purpose   domain.EmailTokenPurpose
	TokenHash string
	ExpiresAt time.Time
	Email     domain.Email
}

// AccountTx agrupa las operaciones de cuenta que deben ser atómicas. Cada
// método corre en su propia transacción y encola los correos en el outbox
// dentro de ella, así que no queda un token sin correo ni un correo sin
// token.
type AccountTx interface {
	// IssueEmailToken invalida los tokens pendientes del mismo propósito,
	// guarda el nuevo y encola su correo.
	IssueEmailToken(ctx context.Context, tok IssuedEmailToken) error
	// ResetPassword consume el token de recuperación, cambia la contraseña,
	// marca el email como verificado, cierra todas las sesiones y encola el
	// aviso que construye notify (si notify falla, se revierte todo).
	// Devuelve domain.ErrInvalidEmailToken si el token no es válido.
	ResetPassword(ctx context.Context, tokenHash, newHash string, notify func(sqlc.User) (domain.Email, error)) error
	// VerifyEmail consume el token de verificación y marca el email como
	// verificado. Devuelve domain.ErrInvalidEmailToken si no es válido.
	VerifyEmail(ctx context.Context, tokenHash string) error
}

// OutboxStore son las operaciones del worker sobre email_outbox. WithTx
// ejecuta fn en una transacción para que el bloqueo de ClaimDueEmails
// (FOR UPDATE SKIP LOCKED) dure hasta marcar el resultado.
type OutboxStore interface {
	WithTx(ctx context.Context, fn func(q OutboxQueries) error) error
}

// OutboxQueries son las queries del outbox disponibles dentro de WithTx.
type OutboxQueries interface {
	ClaimDueEmails(ctx context.Context, limit int32) ([]sqlc.ClaimDueEmailsRow, error)
	MarkEmailSent(ctx context.Context, id uuid.UUID) error
	MarkEmailRetry(ctx context.Context, arg sqlc.MarkEmailRetryParams) error
	MarkEmailFailed(ctx context.Context, arg sqlc.MarkEmailFailedParams) error
}
