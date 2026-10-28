// Package domain define las entidades y errores del núcleo de negocio,
// independientes de HTTP, de la base de datos y de la librería JWT.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errores del caso de uso de autenticación. Los handlers los traducen a HTTP.
var (
	// ErrInvalidCredentials: usuario inexistente o contraseña incorrecta.
	// Deliberadamente único para no revelar cuál de los dos falló.
	ErrInvalidCredentials = errors.New("auth: credenciales inválidas")
	// ErrUserAlreadyExists: el email o el username ya están registrados.
	ErrUserAlreadyExists = errors.New("auth: el usuario ya existe")
	// ErrInvalidRefresh: el refresh token no existe o expiró.
	ErrInvalidRefresh = errors.New("auth: refresh token inválido")
	// ErrRefreshReuse: se detectó reutilización de un refresh token ya
	// revocado; toda la familia de tokens fue revocada.
	ErrRefreshReuse = errors.New("auth: refresh token reutilizado; familia revocada")
)

// ValidationError indica datos de entrada inválidos. Su mensaje es apto
// para mostrarse al cliente (400).
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// NewValidationError crea un ValidationError con el mensaje dado.
func NewValidationError(msg string) error { return &ValidationError{Msg: msg} }

// UserProfile es el perfil público del usuario devuelto por register/login/me.
type UserProfile struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName *string   `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	CreatedAt   time.Time `json:"created_at"`
}

// TokenPair es la respuesta de register/login/refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresInSec int64  `json:"expires_in"` // vigencia del access token en segundos
}

// AccessClaims son los datos del Access Token que consume el núcleo
// (sub, username, jti, iat, exp). El adaptador JWT los serializa.
type AccessClaims struct {
	UserID    uuid.UUID
	Username  string
	JTI       string
	IssuedAt  time.Time
	ExpiresAt time.Time
}
