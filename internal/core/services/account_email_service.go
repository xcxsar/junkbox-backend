package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// maxEmailTokensPerHour limita cuántos correos de recuperación o de
// verificación se emiten por usuario y hora; por encima se ignoran en
// silencio.
const maxEmailTokensPerHour = 3

// Rutas del frontend a las que apuntan los enlaces. El token va en el
// fragmento (#) para que no llegue a logs de servidor ni al Referer.
const (
	resetPasswordPath = "/restablecer-contrasena"
	verifyEmailPath   = "/verificar-correo"
)

// AccountEmailConfig ajusta el servicio. Los TTL en cero usan los defaults.
type AccountEmailConfig struct {
	FrontendURL string        // origen del frontend, p. ej. http://localhost:5173
	ResetTTL    time.Duration // vigencia del enlace de recuperación (default 30m)
	VerifyTTL   time.Duration // vigencia del enlace de verificación (default 24h)
}

// AccountEmailService implementa la recuperación de contraseña y la
// verificación de correo. Los correos se encolan en el outbox dentro de la
// misma transacción que crea o consume el token (ports.AccountTx).
type AccountEmailService struct {
	users  ports.UserStore
	tokens ports.EmailTokenStore
	tx     ports.AccountTx
	hasher ports.PasswordHasher
	cfg    AccountEmailConfig
}

// NewAccountEmailService crea el servicio.
func NewAccountEmailService(users ports.UserStore, tokens ports.EmailTokenStore, tx ports.AccountTx, hasher ports.PasswordHasher, cfg AccountEmailConfig) *AccountEmailService {
	if cfg.ResetTTL <= 0 {
		cfg.ResetTTL = 30 * time.Minute
	}
	if cfg.VerifyTTL <= 0 {
		cfg.VerifyTTL = 24 * time.Hour
	}
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	return &AccountEmailService{users: users, tokens: tokens, tx: tx, hasher: hasher, cfg: cfg}
}

// ForgotPassword envía un enlace de recuperación si el email pertenece a un
// usuario. Devuelve nil también si no existe o si se superó el límite, para
// no revelar qué correos están registrados; el handler responde lo mismo
// incluso ante un error interno.
func (s *AccountEmailService) ForgotPassword(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.users.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cuenta: buscar usuario: %w", err)
	}
	return s.issue(ctx, user, domain.PurposePasswordReset)
}

// ResetPassword cambia la contraseña con un token de recuperación y cierra
// todas las sesiones del usuario. Una contraseña inválida devuelve un
// ValidationError sin consumir el token.
func (s *AccountEmailService) ResetPassword(ctx context.Context, token, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash([]byte(password))
	if err != nil {
		return fmt.Errorf("cuenta: hashear contraseña: %w", err)
	}
	return s.tx.ResetPassword(ctx, hashToken(token), hash, func(user sqlc.User) (domain.Email, error) {
		return passwordChangedEmail.render(user, "", 0)
	})
}

// VerifyEmail marca el correo como verificado con un token de verificación.
func (s *AccountEmailService) VerifyEmail(ctx context.Context, token string) error {
	return s.tx.VerifyEmail(ctx, hashToken(token))
}

// ResendVerification emite un nuevo enlace de verificación para el usuario
// autenticado. No hace nada si ya está verificado o si superó el límite.
func (s *AccountEmailService) ResendVerification(ctx context.Context, claims *domain.AccessClaims) error {
	user, err := s.users.GetUserByID(ctx, claims.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("cuenta: buscar usuario: %w", err)
	}
	if user.EmailVerifiedAt.Valid {
		return nil
	}
	return s.issue(ctx, user, domain.PurposeEmailVerification)
}

// SendVerification envía el enlace de verificación a un usuario recién
// registrado (implementa VerificationSender).
func (s *AccountEmailService) SendVerification(ctx context.Context, user sqlc.User) error {
	return s.issue(ctx, user, domain.PurposeEmailVerification)
}

// issue aplica el límite por hora, genera el token y lo guarda hasheado
// junto con su correo.
func (s *AccountEmailService) issue(ctx context.Context, user sqlc.User, purpose domain.EmailTokenPurpose) error {
	recent, err := s.tokens.CountRecentEmailTokens(ctx, sqlc.CountRecentEmailTokensParams{
		UserID:  user.ID,
		Purpose: sqlc.EmailTokenPurpose(purpose),
	})
	if err != nil {
		return fmt.Errorf("cuenta: contar tokens recientes: %w", err)
	}
	if recent >= maxEmailTokensPerHour {
		return nil
	}

	token, err := newRefreshToken()
	if err != nil {
		return err
	}
	tmpl, path, ttl := passwordResetEmail, resetPasswordPath, s.cfg.ResetTTL
	if purpose == domain.PurposeEmailVerification {
		tmpl, path, ttl = emailVerificationEmail, verifyEmailPath, s.cfg.VerifyTTL
	}
	email, err := tmpl.render(user, s.cfg.FrontendURL+path+"#token="+token, ttl)
	if err != nil {
		return err
	}
	return s.tx.IssueEmailToken(ctx, ports.IssuedEmailToken{
		UserID:    user.ID,
		Purpose:   purpose,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().Add(ttl),
		Email:     email,
	})
}
