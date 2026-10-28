// Package httpapi define los adaptadores de entrada de la API REST (Chi).
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/dto"
	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// AuthHandler expone los endpoints de autenticación.
type AuthHandler struct {
	auth *services.AuthService
	// previousAccess valida el access token anterior en /auth/refresh
	// tolerando que haya expirado.
	previousAccess middleware.JWTValidator
}

// ExpiredAccessParser parsea Access Tokens verificando la firma pero
// tolerando un exp vencido. Lo implementa *security.JWTManager.
type ExpiredAccessParser interface {
	ParseAccessAllowExpired(token string) (*domain.AccessClaims, error)
}

// expiredAccessValidator adapta ExpiredAccessParser a JWTValidator para
// reutilizar middleware.ClaimsFromRequest.
type expiredAccessValidator struct{ p ExpiredAccessParser }

func (v expiredAccessValidator) ParseAccess(token string) (*domain.AccessClaims, error) {
	return v.p.ParseAccessAllowExpired(token)
}

// NewAuthHandler crea el handler.
func NewAuthHandler(auth *services.AuthService, jwt ExpiredAccessParser) *AuthHandler {
	return &AuthHandler{auth: auth, previousAccess: expiredAccessValidator{jwt}}
}

// Register: POST /api/v1/auth/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email y password son requeridos")
		return
	}

	pair, profile, err := h.auth.Register(r.Context(), services.RegisterInput{
		Username:    req.Username,
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
		AvatarURL:   req.AvatarURL,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, authResponse(pair, profile))
}

// Login: POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Identifier == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "identifier y password son requeridos")
		return
	}

	pair, profile, err := h.auth.Login(r.Context(), req.Identifier, req.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authResponse(pair, profile))
}

// Refresh: POST /api/v1/auth/refresh. Exige el access token anterior en
// `Authorization: Bearer` (puede estar expirado): sin la llave vieja no se
// entrega una nueva.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	claims, err := middleware.ClaimsFromRequest(r, h.previousAccess)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var req dto.RefreshRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token es requerido")
		return
	}

	pair, err := h.auth.Refresh(r.Context(), req.RefreshToken, claims)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

// Logout: POST /api/v1/auth/logout (requiere AuthMiddleware: el access
// token de la petición se revoca en la denylist).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var req dto.LogoutRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token es requerido")
		return
	}
	if err := h.auth.Logout(r.Context(), claims, req.RefreshToken, req.AllSessions); err != nil {
		writeServiceError(w, err)
		return
	}
	// Idempotente: siempre 200 aunque el token ya estuviera revocado.
	writeJSON(w, http.StatusOK, map[string]string{"message": "sesión cerrada"})
}

// Me: GET /api/v1/auth/me (requiere AuthMiddleware).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	profile, err := h.auth.Me(r.Context(), claims)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func authResponse(pair *domain.TokenPair, profile *domain.UserProfile) dto.AuthResponse {
	return dto.AuthResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresInSec,
		User:         profile,
	}
}

// writeServiceError traduce los errores de dominio a códigos HTTP.
func writeServiceError(w http.ResponseWriter, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Msg)
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "credenciales inválidas")
	case errors.Is(err, domain.ErrRefreshReuse):
		// Familia revocada: el cliente debe volver a login.
		writeError(w, http.StatusUnauthorized, "refresh token reutilizado; sesión revocada")
	case errors.Is(err, domain.ErrInvalidRefresh):
		writeError(w, http.StatusUnauthorized, "refresh token inválido")
	case errors.Is(err, domain.ErrUserAlreadyExists):
		writeError(w, http.StatusConflict, "el email o el username ya están registrados")
	default:
		log.Printf("error interno: %v", err)
		writeError(w, http.StatusInternalServerError, "error interno")
	}
}

// --- utilidades de respuesta ---

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)) // 1 MiB máx.
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "solicitud inválida")
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, dto.ErrorResponse{Error: msg})
}
