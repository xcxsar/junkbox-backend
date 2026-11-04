package services

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

type storedEmailToken struct {
	ports.IssuedEmailToken
	used bool
}

// fakeAccountTx simula user_email_tokens y el outbox en memoria.
type fakeAccountTx struct {
	users  *fakeUserStore
	tokens map[string]*storedEmailToken // por hash
	outbox []domain.Email
	resets int // llamadas a ResetPassword que cambiaron la contraseña
}

func newFakeAccountTx(users *fakeUserStore) *fakeAccountTx {
	return &fakeAccountTx{users: users, tokens: map[string]*storedEmailToken{}}
}

func (f *fakeAccountTx) IssueEmailToken(_ context.Context, tok ports.IssuedEmailToken) error {
	for _, t := range f.tokens {
		if t.UserID == tok.UserID && t.Purpose == tok.Purpose {
			t.used = true
		}
	}
	f.tokens[tok.TokenHash] = &storedEmailToken{IssuedEmailToken: tok}
	f.outbox = append(f.outbox, tok.Email)
	return nil
}

func (f *fakeAccountTx) consume(hash string, purpose domain.EmailTokenPurpose) (uuid.UUID, error) {
	t, ok := f.tokens[hash]
	if !ok || t.used || t.Purpose != purpose || !t.ExpiresAt.After(time.Now()) {
		return uuid.Nil, domain.ErrInvalidEmailToken
	}
	t.used = true
	return t.UserID, nil
}

func (f *fakeAccountTx) ResetPassword(_ context.Context, hash, newHash string, notify func(sqlc.User) (domain.Email, error)) error {
	userID, err := f.consume(hash, domain.PurposePasswordReset)
	if err != nil {
		return err
	}
	u := f.users.users[userID]
	u.HashedPassword = newHash
	u.EmailVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	f.users.users[userID] = u
	email, err := notify(u)
	if err != nil {
		return err
	}
	f.resets++
	f.outbox = append(f.outbox, email)
	return nil
}

func (f *fakeAccountTx) VerifyEmail(_ context.Context, hash string) error {
	userID, err := f.consume(hash, domain.PurposeEmailVerification)
	if err != nil {
		return err
	}
	u := f.users.users[userID]
	u.EmailVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	f.users.users[userID] = u
	return nil
}

// CountRecentEmailTokens cuenta los emitidos por usuario y propósito (en
// los tests todos son "recientes").
func (f *fakeAccountTx) CountRecentEmailTokens(_ context.Context, arg sqlc.CountRecentEmailTokensParams) (int64, error) {
	var n int64
	for _, t := range f.tokens {
		if t.UserID == arg.UserID && string(t.Purpose) == string(arg.Purpose) {
			n++
		}
	}
	return n, nil
}

// --- helpers ---

type accountEnv struct {
	svc   *AccountEmailService
	users *fakeUserStore
	tx    *fakeAccountTx
}

func newAccountEnv() *accountEnv {
	users := newFakeUserStore(seedUser)
	tx := newFakeAccountTx(users)
	svc := NewAccountEmailService(users, tx, tx, fakeHasher{}, AccountEmailConfig{FrontendURL: "http://localhost:5173/"})
	return &accountEnv{svc: svc, users: users, tx: tx}
}

// tokenFromLink extrae el token en claro del enlace de un correo.
func tokenFromLink(t *testing.T, email domain.Email, path string) string {
	t.Helper()
	prefix := "http://localhost:5173" + path + "#token="
	i := strings.Index(email.Text, prefix)
	if i < 0 {
		t.Fatalf("el correo no contiene un enlace %s…:\n%s", prefix, email.Text)
	}
	rest := email.Text[i+len(prefix):]
	if j := strings.IndexAny(rest, " \n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func (e *accountEnv) forgot(t *testing.T) string {
	t.Helper()
	if err := e.svc.ForgotPassword(context.Background(), seedUser.Email); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	return tokenFromLink(t, e.tx.outbox[len(e.tx.outbox)-1], resetPasswordPath)
}

// --- forgot ---

func TestForgotPasswordEnviaEnlace(t *testing.T) {
	env := newAccountEnv()
	if err := env.svc.ForgotPassword(context.Background(), "  CESAR@Junkbox.local "); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if len(env.tx.outbox) != 1 {
		t.Fatalf("esperaba 1 correo encolado, obtuve %d", len(env.tx.outbox))
	}
	email := env.tx.outbox[0]
	if email.To != seedUser.Email || !strings.Contains(email.Subject, "Restablece") {
		t.Errorf("correo inesperado: %+v", email)
	}
	token := tokenFromLink(t, email, resetPasswordPath)
	if !strings.Contains(email.HTML, token) {
		t.Error("la versión HTML debe incluir el mismo enlace")
	}
	stored, ok := env.tx.tokens[hashToken(token)]
	if !ok {
		t.Fatal("el token debe guardarse hasheado")
	}
	if _, plain := env.tx.tokens[token]; plain {
		t.Error("el token no debe guardarse en claro")
	}
	if d := time.Until(stored.ExpiresAt); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("TTL por defecto debe ser 30m, obtuve %v", d)
	}
	if !strings.Contains(email.Text, "30 minutos") {
		t.Error("el correo debe indicar la vigencia")
	}
}

func TestForgotPasswordEmailInexistente(t *testing.T) {
	env := newAccountEnv()
	if err := env.svc.ForgotPassword(context.Background(), "nadie@junkbox.local"); err != nil {
		t.Fatalf("no debe fallar para no revelar qué correos existen: %v", err)
	}
	if len(env.tx.outbox) != 0 {
		t.Error("no debe encolarse ningún correo")
	}
}

func TestForgotPasswordLimitePorHora(t *testing.T) {
	env := newAccountEnv()
	for i := 0; i < maxEmailTokensPerHour+2; i++ {
		if err := env.svc.ForgotPassword(context.Background(), seedUser.Email); err != nil {
			t.Fatalf("ForgotPassword #%d: %v", i+1, err)
		}
	}
	if len(env.tx.outbox) != maxEmailTokensPerHour {
		t.Errorf("esperaba %d correos (límite), obtuve %d", maxEmailTokensPerHour, len(env.tx.outbox))
	}
}

// --- reset ---

func TestResetPasswordOK(t *testing.T) {
	env := newAccountEnv()
	token := env.forgot(t)
	if err := env.svc.ResetPassword(context.Background(), token, "NuevaClave9$"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	u := env.users.users[seedUser.ID]
	if u.HashedPassword != "hash:NuevaClave9$" {
		t.Errorf("la contraseña debe actualizarse hasheada, obtuve %q", u.HashedPassword)
	}
	notice := env.tx.outbox[len(env.tx.outbox)-1]
	if notice.To != seedUser.Email || !strings.Contains(notice.Subject, "se cambió") {
		t.Errorf("esperaba el aviso de contraseña cambiada, obtuve %+v", notice)
	}
	if strings.Contains(notice.Text, "#token=") {
		t.Error("el aviso no debe llevar enlaces con token")
	}
}

func TestResetPasswordTokenUsadoOInvalido(t *testing.T) {
	env := newAccountEnv()
	token := env.forgot(t)
	if err := env.svc.ResetPassword(context.Background(), token, "NuevaClave9$"); err != nil {
		t.Fatalf("primer uso: %v", err)
	}
	for name, tok := range map[string]string{"reutilizado": token, "desconocido": "no-existe"} {
		t.Run(name, func(t *testing.T) {
			err := env.svc.ResetPassword(context.Background(), tok, "OtraClave9$")
			if !errors.Is(err, domain.ErrInvalidEmailToken) {
				t.Fatalf("esperaba ErrInvalidEmailToken, obtuve %v", err)
			}
		})
	}
}

func TestResetPasswordTokenExpirado(t *testing.T) {
	env := newAccountEnv()
	token := env.forgot(t)
	env.tx.tokens[hashToken(token)].ExpiresAt = time.Now().Add(-time.Second)
	if err := env.svc.ResetPassword(context.Background(), token, "NuevaClave9$"); !errors.Is(err, domain.ErrInvalidEmailToken) {
		t.Fatalf("esperaba ErrInvalidEmailToken, obtuve %v", err)
	}
}

func TestResetPasswordSoloInvalidaElAnterior(t *testing.T) {
	env := newAccountEnv()
	first := env.forgot(t)
	second := env.forgot(t)
	if err := env.svc.ResetPassword(context.Background(), first, "NuevaClave9$"); !errors.Is(err, domain.ErrInvalidEmailToken) {
		t.Fatalf("un enlace nuevo debe invalidar el anterior, obtuve %v", err)
	}
	if err := env.svc.ResetPassword(context.Background(), second, "NuevaClave9$"); err != nil {
		t.Fatalf("el enlace más reciente debe funcionar: %v", err)
	}
}

func TestResetPasswordInvalidaNoConsumeToken(t *testing.T) {
	env := newAccountEnv()
	token := env.forgot(t)
	err := env.svc.ResetPassword(context.Background(), token, "corta")
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("esperaba ValidationError, obtuve %v", err)
	}
	if env.tx.tokens[hashToken(token)].used {
		t.Error("una contraseña inválida no debe gastar el token")
	}
	if err := env.svc.ResetPassword(context.Background(), token, "NuevaClave9$"); err != nil {
		t.Fatalf("el token debe seguir sirviendo: %v", err)
	}
}

func TestTokenDeVerificacionNoSirveParaReset(t *testing.T) {
	env := newAccountEnv()
	if err := env.svc.SendVerification(context.Background(), seedUser); err != nil {
		t.Fatalf("SendVerification: %v", err)
	}
	token := tokenFromLink(t, env.tx.outbox[0], verifyEmailPath)
	if err := env.svc.ResetPassword(context.Background(), token, "NuevaClave9$"); !errors.Is(err, domain.ErrInvalidEmailToken) {
		t.Fatalf("esperaba ErrInvalidEmailToken, obtuve %v", err)
	}
}

// --- verificación ---

func TestVerifyEmail(t *testing.T) {
	env := newAccountEnv()
	if err := env.svc.SendVerification(context.Background(), seedUser); err != nil {
		t.Fatalf("SendVerification: %v", err)
	}
	email := env.tx.outbox[0]
	if !strings.Contains(email.Subject, "Confirma") || !strings.Contains(email.Text, "24 horas") {
		t.Errorf("correo de verificación inesperado: %+v", email)
	}
	token := tokenFromLink(t, email, verifyEmailPath)
	if err := env.svc.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if !env.users.users[seedUser.ID].EmailVerifiedAt.Valid {
		t.Error("el email debe quedar verificado")
	}
	if err := env.svc.VerifyEmail(context.Background(), token); !errors.Is(err, domain.ErrInvalidEmailToken) {
		t.Errorf("el token es de un solo uso, obtuve %v", err)
	}
}

func TestResendVerification(t *testing.T) {
	env := newAccountEnv()
	claims := &domain.AccessClaims{UserID: seedUser.ID}
	if err := env.svc.ResendVerification(context.Background(), claims); err != nil {
		t.Fatalf("ResendVerification: %v", err)
	}
	if len(env.tx.outbox) != 1 {
		t.Fatalf("esperaba 1 correo, obtuve %d", len(env.tx.outbox))
	}

	u := env.users.users[seedUser.ID]
	u.EmailVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	env.users.users[seedUser.ID] = u
	if err := env.svc.ResendVerification(context.Background(), claims); err != nil {
		t.Fatalf("ResendVerification verificado: %v", err)
	}
	if len(env.tx.outbox) != 1 {
		t.Error("no debe reenviarse si ya está verificado")
	}
}

func TestResendVerificationUsuarioBorrado(t *testing.T) {
	env := newAccountEnv()
	err := env.svc.ResendVerification(context.Background(), &domain.AccessClaims{UserID: uuid.New()})
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("esperaba ErrInvalidCredentials, obtuve %v", err)
	}
}

// --- integración con Register ---

type recordingVerificationSender struct {
	sent []sqlc.User
	err  error
}

func (r *recordingVerificationSender) SendVerification(_ context.Context, u sqlc.User) error {
	r.sent = append(r.sent, u)
	return r.err
}

func TestRegisterEnviaVerificacion(t *testing.T) {
	env := newEnv()
	sender := &recordingVerificationSender{}
	env.svc.SetVerificationSender(sender)
	_, profile, err := env.svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].Email != "melomano@junkbox.local" {
		t.Fatalf("esperaba una verificación para el usuario nuevo, obtuve %+v", sender.sent)
	}
	if profile.EmailVerified {
		t.Error("un usuario recién registrado no está verificado")
	}
}

func TestRegisterNoFallaSiLaVerificacionFalla(t *testing.T) {
	env := newEnv()
	env.svc.SetVerificationSender(&recordingVerificationSender{err: errors.New("outbox caído")})
	if _, _, err := env.svc.Register(context.Background(), validRegister()); err != nil {
		t.Fatalf("el registro no debe fallar por el correo: %v", err)
	}
}

func TestHumanDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute: "30 minutos",
		time.Minute:      "1 minuto",
		time.Hour:        "1 hora",
		24 * time.Hour:   "24 horas",
		90 * time.Minute: "90 minutos",
	} {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, esperaba %q", d, got, want)
		}
	}
}
