// Package middleware contiene los middlewares HTTP del router Chi:
// autenticación JWT y verificación de revocación.
package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

type contextKey int

// claimsKey es la clave de contexto donde se guardan los AccessClaims.
const claimsKey contextKey = iota

// ErrMissingToken indica que no se envió cabecera Authorization.
var ErrMissingToken = errors.New("middleware: token ausente")

// JWTValidator parsea y valida Access Tokens (interfaz para tests).
type JWTValidator interface {
	ParseAccess(token string) (*domain.AccessClaims, error)
}

// TokenRevocation decide si un token fue revocado (interfaz para tests).
type TokenRevocation interface {
	IsRevoked(ctx context.Context, claims *domain.AccessClaims) (bool, error)
}

// AuthMiddleware exige un Access Token válido en `Authorization: Bearer`,
// comprueba que no esté revocado (denylist y corte global) y guarda los
// claims en el contexto de la petición. Si revocation es nil, se omite la
// comprobación (útil en tests de rutas que no la necesitan).
func AuthMiddleware(validator JWTValidator, revocation TokenRevocation) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := ClaimsFromRequest(r, validator)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "no autorizado")
				return
			}
			if revocation != nil {
				revoked, err := revocation.IsRevoked(r.Context(), claims)
				if err != nil || revoked {
					// fail-closed: ante error de BD también se rechaza.
					writeJSONError(w, http.StatusUnauthorized, "no autorizado")
					return
				}
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromRequest extrae y valida el Bearer token de la petición.
func ClaimsFromRequest(r *http.Request, validator JWTValidator) (*domain.AccessClaims, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, ErrMissingToken
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return nil, ErrMissingToken
	}
	return validator.ParseAccess(token)
}

// ClaimsFromContext recupera los AccessClaims guardados por AuthMiddleware.
func ClaimsFromContext(ctx context.Context) (*domain.AccessClaims, bool) {
	claims, ok := ctx.Value(claimsKey).(*domain.AccessClaims)
	return claims, ok
}

// RevocationStore son las queries necesarias para validar revocación.
// La implementa *sqlc.Queries.
type RevocationStore interface {
	IsAccessTokenRevoked(ctx context.Context, jti uuid.UUID) (bool, error)
	GetUserAccessRevocation(ctx context.Context, userID uuid.UUID) (time.Time, error)
}

// RevocationChecker valida si un Access Token fue revocado de forma
// individual (por jti) o global (corte por usuario anterior al iat).
type RevocationChecker struct {
	store RevocationStore
}

// NewRevocationChecker crea el checker.
func NewRevocationChecker(store RevocationStore) *RevocationChecker {
	return &RevocationChecker{store: store}
}

// IsRevoked devuelve true si el token debe rechazarse. Consulta:
//  1. La denylist por jti.
//  2. El corte global del usuario: si el token se emitió (iat) antes del
//     corte, queda invalidado aunque su jti no esté en la denylist.
//
// Ante un error de base de datos devuelve el error (fail-closed: el
// middleware responde 401 en lugar de dejar pasar un token dudoso).
func (c *RevocationChecker) IsRevoked(ctx context.Context, claims *domain.AccessClaims) (bool, error) {
	jti, err := uuid.Parse(claims.JTI)
	if err != nil {
		// jti malformado: token emitido por otro sistema o manipulado.
		return true, nil
	}
	revoked, err := c.store.IsAccessTokenRevoked(ctx, jti)
	if err != nil {
		return false, err
	}
	if revoked {
		return true, nil
	}

	revokedBefore, err := c.store.GetUserAccessRevocation(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Sin corte global para el usuario: nada que comparar.
			return false, nil
		}
		return false, err
	}
	// iat va con precisión de segundos (JWT NumericDate, truncado hacia
	// abajo), mientras que el corte se guarda con NOW() (microsegundos). Con
	// iat no se puede distinguir si un token del mismo segundo del corte se
	// emitió antes o después de él, así que se revoca (fail-closed): de lo
	// contrario un token emitido justo antes del logout global seguiría vivo
	// hasta su exp. El costo es que un login en ese mismo segundo, posterior
	// al corte, debe repetirse.
	if !claims.IssuedAt.After(revokedBefore.Truncate(time.Second)) {
		return true, nil
	}
	return false, nil
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
