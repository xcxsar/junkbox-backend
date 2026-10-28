package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

// fakeValidator devuelve claims predefinidos o error.
type fakeValidator struct {
	claims *domain.AccessClaims
	err    error
}

func (f *fakeValidator) ParseAccess(string) (*domain.AccessClaims, error) {
	return f.claims, f.err
}

// fakeRevocation implementa TokenRevocation para los tests.
type fakeRevocation struct {
	revoked bool
	err     error
}

func (f *fakeRevocation) IsRevoked(context.Context, *domain.AccessClaims) (bool, error) {
	return f.revoked, f.err
}

func protectedRouter(v JWTValidator, rev TokenRevocation) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(v, rev))
		r.Get("/me", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := ClaimsFromContext(r.Context()); !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	})
	return r
}

func validClaims() *domain.AccessClaims {
	return &domain.AccessClaims{UserID: uuid.New(), Username: "a", JTI: uuid.NewString(), IssuedAt: time.Now()}
}

func doGet(h http.Handler, auth string) int {
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestAuthMiddlewareSinToken401(t *testing.T) {
	if code := doGet(protectedRouter(&fakeValidator{claims: validClaims()}, nil), ""); code != http.StatusUnauthorized {
		t.Errorf("esperaba 401, obtuve %d", code)
	}
}

func TestAuthMiddlewareMalformado401(t *testing.T) {
	h := protectedRouter(&fakeValidator{claims: validClaims()}, nil)
	if code := doGet(h, "Token abc"); code != http.StatusUnauthorized {
		t.Errorf("esperaba 401, obtuve %d", code)
	}
}

func TestAuthMiddlewareInvalido401(t *testing.T) {
	h := protectedRouter(&fakeValidator{err: errors.New("firma inválida")}, nil)
	if code := doGet(h, "Bearer tok"); code != http.StatusUnauthorized {
		t.Errorf("esperaba 401, obtuve %d", code)
	}
}

func TestAuthMiddlewareValido200(t *testing.T) {
	h := protectedRouter(&fakeValidator{claims: validClaims()}, &fakeRevocation{})
	if code := doGet(h, "Bearer tok"); code != http.StatusOK {
		t.Errorf("esperaba 200, obtuve %d", code)
	}
}

func TestAuthMiddlewareTokenRevocado401(t *testing.T) {
	h := protectedRouter(&fakeValidator{claims: validClaims()}, &fakeRevocation{revoked: true})
	if code := doGet(h, "Bearer tok"); code != http.StatusUnauthorized {
		t.Errorf("token revocado: esperaba 401, obtuve %d", code)
	}
}

func TestAuthMiddlewareErrorRevocacionFailClosed(t *testing.T) {
	// Si el checker falla (BD caída), el middleware debe rechazar (fail-closed).
	h := protectedRouter(&fakeValidator{claims: validClaims()}, &fakeRevocation{err: errors.New("db down")})
	if code := doGet(h, "Bearer tok"); code != http.StatusUnauthorized {
		t.Errorf("error de revocación: esperaba 401 (fail-closed), obtuve %d", code)
	}
}

// --- RevocationChecker ---

type fakeRevocationStore struct {
	revokedJTI    map[uuid.UUID]bool
	revokedBefore *time.Time
	err           error
}

func (f *fakeRevocationStore) IsAccessTokenRevoked(_ context.Context, jti uuid.UUID) (bool, error) {
	return f.revokedJTI[jti], f.err
}

func (f *fakeRevocationStore) GetUserAccessRevocation(context.Context, uuid.UUID) (time.Time, error) {
	if f.revokedBefore == nil {
		return time.Time{}, sql.ErrNoRows
	}
	return *f.revokedBefore, nil
}

func TestRevocationChecker(t *testing.T) {
	claims := validClaims()
	claims.IssuedAt = time.Now().Truncate(time.Second)
	before := claims.IssuedAt.Add(-time.Minute)
	after := claims.IssuedAt.Add(time.Minute)
	sameSecond := claims.IssuedAt.Add(500 * time.Millisecond)

	cases := []struct {
		name    string
		store   *fakeRevocationStore
		claims  *domain.AccessClaims
		revoked bool
		wantErr bool
	}{
		{"sin revocación", &fakeRevocationStore{}, claims, false, false},
		{"jti en denylist", &fakeRevocationStore{revokedJTI: map[uuid.UUID]bool{uuid.MustParse(claims.JTI): true}}, claims, true, false},
		{"jti malformado", &fakeRevocationStore{}, &domain.AccessClaims{UserID: claims.UserID, JTI: "x", IssuedAt: claims.IssuedAt}, true, false},
		{"corte global posterior al iat", &fakeRevocationStore{revokedBefore: &after}, claims, true, false},
		{"corte global anterior al iat", &fakeRevocationStore{revokedBefore: &before}, claims, false, false},
		// Mismo segundo: no se sabe si el token es previo al corte → revocado.
		{"corte en el mismo segundo", &fakeRevocationStore{revokedBefore: &sameSecond}, claims, true, false},
		{"error de BD", &fakeRevocationStore{err: errors.New("db down")}, claims, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revoked, err := NewRevocationChecker(tc.store).IsRevoked(context.Background(), tc.claims)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if revoked != tc.revoked {
				t.Errorf("revoked = %v, esperaba %v", revoked, tc.revoked)
			}
		})
	}
}
