// Package ports define las interfaces (puertos) que el núcleo necesita de
// los adaptadores secundarios. *sqlc.Queries implementa los stores.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// UserStore son las queries de usuarios usadas por auth.
type UserStore interface {
	CreateUser(ctx context.Context, arg sqlc.CreateUserParams) (sqlc.CreateUserRow, error)
	GetUserByEmail(ctx context.Context, email string) (sqlc.User, error)
	GetUserByUsername(ctx context.Context, username string) (sqlc.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (sqlc.User, error)
	ExistsUserByEmailOrUsername(ctx context.Context, arg sqlc.ExistsUserByEmailOrUsernameParams) (bool, error)
}

// TokenStore son las operaciones sobre refresh_tokens. RevokeRefreshToken
// devuelve filas afectadas para detectar la carrera de rotación doble.
type TokenStore interface {
	InsertRefreshToken(ctx context.Context, arg sqlc.InsertRefreshTokenParams) (sqlc.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (sqlc.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) (int64, error)
	SetRefreshTokenReplacedBy(ctx context.Context, arg sqlc.SetRefreshTokenReplacedByParams) error
	RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID) error
	RevokeAllRefreshTokensByUser(ctx context.Context, userID uuid.UUID) error
}

// RevocationStore son las operaciones de revocación de access tokens
// (denylist por jti y corte global por usuario).
type RevocationStore interface {
	RevokeAccessToken(ctx context.Context, arg sqlc.RevokeAccessTokenParams) error
	IsAccessTokenRevoked(ctx context.Context, jti uuid.UUID) (bool, error)
	RevokeAllUserAccessTokens(ctx context.Context, userID uuid.UUID) error
	GetUserAccessRevocation(ctx context.Context, userID uuid.UUID) (time.Time, error)
}

// PasswordHasher hashea y verifica contraseñas.
type PasswordHasher interface {
	Hash(plain []byte) (string, error)
	Compare(hash string, plain []byte) error
}

// TokenManager emite y valida Access Tokens.
type TokenManager interface {
	GenerateAccess(userID uuid.UUID, username string) (string, error)
	ParseAccess(token string) (*domain.AccessClaims, error)
	TTL() time.Duration
}
