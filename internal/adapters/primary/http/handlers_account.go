package httpapi

import (
	"log"
	"net/http"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/dto"
	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// Mensajes de respuesta de los endpoints de cuenta.
const (
	msgForgotPassword     = "Si el correo está registrado, recibirás un enlace para restablecer tu contraseña."
	msgPasswordReset      = "Contraseña actualizada. Inicia sesión de nuevo."
	msgEmailVerified      = "Correo verificado."
	msgVerificationResent = "Si tu correo aún no está verificado, recibirás un nuevo enlace."
)

// AccountHandler expone la recuperación de contraseña y la verificación de
// correo. reset y verify se autentican con el token de un solo uso que
// llega por correo; resend exige Bearer.
type AccountHandler struct {
	account *services.AccountEmailService
}

// NewAccountHandler crea el handler.
func NewAccountHandler(account *services.AccountEmailService) *AccountHandler {
	return &AccountHandler{account: account}
}

// ForgotPassword: POST /api/v1/auth/password/forgot. Responde siempre lo
// mismo, exista o no el correo e incluso ante un error interno, para no
// revelar qué cuentas existen.
func (h *AccountHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req dto.ForgotPasswordRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email es requerido")
		return
	}
	if err := h.account.ForgotPassword(r.Context(), req.Email); err != nil {
		log.Printf("recuperación de contraseña: %v", err)
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: msgForgotPassword})
}

// ResetPassword: POST /api/v1/auth/password/reset
func (h *AccountHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req dto.ResetPasswordRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Token == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "token y password son requeridos")
		return
	}
	if err := h.account.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: msgPasswordReset})
}

// VerifyEmail: POST /api/v1/auth/email/verify
func (h *AccountHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req dto.VerifyEmailRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token es requerido")
		return
	}
	if err := h.account.VerifyEmail(r.Context(), req.Token); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: msgEmailVerified})
}

// ResendVerification: POST /api/v1/auth/email/verify/resend (requiere
// AuthMiddleware).
func (h *AccountHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	if err := h.account.ResendVerification(r.Context(), claims); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: msgVerificationResent})
}
