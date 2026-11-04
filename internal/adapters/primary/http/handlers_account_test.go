package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// accountFakes implementa los puertos que usa AccountEmailService con el
// mínimo de comportamiento que necesitan los handlers.
type accountFakes struct {
	user      sqlc.User
	lookupErr error // error forzado en GetUserByEmail
	issued    int
}

func (f *accountFakes) CreateUser(context.Context, sqlc.CreateUserParams) (sqlc.CreateUserRow, error) {
	return sqlc.CreateUserRow{}, errors.New("no usado")
}
func (f *accountFakes) GetUserByEmail(_ context.Context, email string) (sqlc.User, error) {
	if f.lookupErr != nil {
		return sqlc.User{}, f.lookupErr
	}
	if email == f.user.Email {
		return f.user, nil
	}
	return sqlc.User{}, sql.ErrNoRows
}
func (f *accountFakes) GetUserByUsername(context.Context, string) (sqlc.User, error) {
	return sqlc.User{}, sql.ErrNoRows
}
func (f *accountFakes) GetUserByID(_ context.Context, id uuid.UUID) (sqlc.User, error) {
	if id == f.user.ID {
		return f.user, nil
	}
	return sqlc.User{}, sql.ErrNoRows
}
func (f *accountFakes) ExistsUserByEmailOrUsername(context.Context, sqlc.ExistsUserByEmailOrUsernameParams) (bool, error) {
	return false, nil
}
func (f *accountFakes) CountRecentEmailTokens(context.Context, sqlc.CountRecentEmailTokensParams) (int64, error) {
	return 0, nil
}
func (f *accountFakes) IssueEmailToken(context.Context, ports.IssuedEmailToken) error {
	f.issued++
	return nil
}
func (f *accountFakes) ResetPassword(_ context.Context, tokenHash, _ string, _ func(sqlc.User) (domain.Email, error)) error {
	return domain.ErrInvalidEmailToken
}
func (f *accountFakes) VerifyEmail(context.Context, string) error {
	return domain.ErrInvalidEmailToken
}

type plainHasher struct{}

func (plainHasher) Hash(p []byte) (string, error) { return string(p), nil }
func (plainHasher) Compare(string, []byte) error  { return nil }

// staticJWT acepta solo el token "valid" y lo asocia al usuario dado.
type staticJWT struct{ userID uuid.UUID }

func (s staticJWT) ParseAccess(token string) (*domain.AccessClaims, error) {
	if token != "valid" {
		return nil, errors.New("token inválido")
	}
	return &domain.AccessClaims{UserID: s.userID, JTI: uuid.NewString()}, nil
}

func newAccountRouter(f *accountFakes) http.Handler {
	svc := services.NewAccountEmailService(f, f, f, plainHasher{}, services.AccountEmailConfig{FrontendURL: frontendOrigin})
	return NewRouter(RouterOptions{
		Auth:    &AuthHandler{},
		Account: NewAccountHandler(svc),
		JWT:     staticJWT{userID: f.user.ID},
	})
}

func post(h http.Handler, path, body, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var accountUser = sqlc.User{ID: uuid.New(), Username: "cesar", Email: "cesar@junkbox.local"}

func TestForgotPasswordRespuestaUniforme(t *testing.T) {
	cases := map[string]*accountFakes{
		"existe":        {user: accountUser},
		"no existe":     {user: sqlc.User{Email: "otro@x.mx"}},
		"error interno": {user: accountUser, lookupErr: errors.New("bd caída")},
	}
	var bodies []string
	for name, f := range cases {
		rec := post(newAccountRouter(f), "/api/v1/auth/password/forgot", `{"email":"cesar@junkbox.local"}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("las respuestas deben ser idénticas:\n%s\n%s", bodies[0], b)
		}
	}
	if cases["existe"].issued != 1 || cases["no existe"].issued != 0 {
		t.Errorf("solo debe emitirse token si el usuario existe: %d / %d", cases["existe"].issued, cases["no existe"].issued)
	}
}

func TestForgotPasswordSinEmail(t *testing.T) {
	rec := post(newAccountRouter(&accountFakes{user: accountUser}), "/api/v1/auth/password/forgot", `{}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, esperaba 400", rec.Code)
	}
}

func TestResetPasswordCodigos(t *testing.T) {
	h := newAccountRouter(&accountFakes{user: accountUser})
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"contraseña inválida": {`{"token":"t","password":"corta"}`, http.StatusBadRequest},
		"faltan campos":       {`{"token":"t"}`, http.StatusBadRequest},
		"token inválido":      {`{"token":"t","password":"NuevaClave9$"}`, http.StatusUnauthorized},
	} {
		if rec := post(h, "/api/v1/auth/password/reset", tc.body, ""); rec.Code != tc.want {
			t.Errorf("%s: status %d, esperaba %d (%s)", name, rec.Code, tc.want, rec.Body)
		}
	}
}

func TestVerifyEmailTokenInvalido(t *testing.T) {
	rec := post(newAccountRouter(&accountFakes{user: accountUser}), "/api/v1/auth/email/verify", `{"token":"t"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, esperaba 401", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "token inválido o expirado" {
		t.Errorf("mensaje inesperado: %v", body)
	}
}

func TestResendVerificationRequiereBearer(t *testing.T) {
	f := &accountFakes{user: accountUser}
	h := newAccountRouter(f)
	if rec := post(h, "/api/v1/auth/email/verify/resend", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sin Bearer: status %d, esperaba 401", rec.Code)
	}
	if rec := post(h, "/api/v1/auth/email/verify/resend", "", "valid"); rec.Code != http.StatusOK {
		t.Fatalf("con Bearer: status %d, esperaba 200 (%s)", rec.Code, rec.Body)
	}
	if f.issued != 1 {
		t.Errorf("esperaba 1 token emitido, obtuve %d", f.issued)
	}
}
