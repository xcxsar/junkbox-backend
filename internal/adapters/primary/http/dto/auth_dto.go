// Package dto define los Data Transfer Objects de la API REST.
package dto

import "github.com/xcxsar/junkbox-backend/internal/core/domain"

// RegisterRequest es el cuerpo de POST /api/v1/auth/register.
type RegisterRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// LoginRequest es el cuerpo de POST /api/v1/auth/login. Identifier admite
// email o username.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// RefreshRequest es el cuerpo de POST /api/v1/auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest es el cuerpo de POST /api/v1/auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
	// AllSessions cierra todas las sesiones del usuario, no solo la actual.
	AllSessions bool `json:"all_sessions,omitempty"`
}

// AuthResponse es la respuesta de register y login.
type AuthResponse struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token"`
	ExpiresIn    int64               `json:"expires_in"`
	User         *domain.UserProfile `json:"user"`
}

// ErrorResponse es el cuerpo estándar de error.
type ErrorResponse struct {
	Error string `json:"error"`
}
