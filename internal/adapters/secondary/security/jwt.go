// Package security implementa los adaptadores criptográficos del backend:
// emisión/validación de JWT y hashing de contraseñas.
package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

// accessClaims es el payload firmado del Access Token: sub (user_id),
// username, jti, iat y exp.
type accessClaims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Errores del manager JWT. El middleware los traduce a 401.
var (
	ErrInvalidToken = errors.New("security: token inválido")
	ErrExpiredToken = errors.New("security: token expirado")
)

// JWTManager emite y valida Access Tokens firmados con HMAC-SHA256 (HS256).
type JWTManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewJWTManager crea un manager. secret debe tener al menos 32 caracteres
// (config ya lo valida al arrancar).
func NewJWTManager(secret string, ttl time.Duration) *JWTManager {
	return &JWTManager{
		secret: []byte(secret),
		issuer: "junkbox-backend",
		ttl:    ttl,
	}
}

// GenerateAccess firma un Access Token con la vigencia configurada. El
// token lleva un jti (UUID) único que permite revocarlo individualmente
// mediante la denylist (revoked_access_tokens).
func (m *JWTManager) GenerateAccess(userID uuid.UUID, username string) (string, error) {
	now := time.Now()
	claims := accessClaims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("security: firmar access token: %w", err)
	}
	return signed, nil
}

// ParseAccess valida firma, método, emisor y vigencia del token y devuelve
// los claims del dominio.
func (m *JWTManager) ParseAccess(tokenStr string) (*domain.AccessClaims, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		return m.secret, nil
	},
		// Exigir explícitamente HS256: rechaza alg=none y otros métodos.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("%w: sub no es un UUID", ErrInvalidToken)
	}
	if claims.ID == "" || claims.IssuedAt == nil {
		return nil, fmt.Errorf("%w: faltan jti o iat", ErrInvalidToken)
	}
	return &domain.AccessClaims{
		UserID:    userID,
		Username:  claims.Username,
		JTI:       claims.ID,
		IssuedAt:  claims.IssuedAt.Time,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}

// TTL devuelve la vigencia configurada de los Access Tokens.
func (m *JWTManager) TTL() time.Duration { return m.ttl }
