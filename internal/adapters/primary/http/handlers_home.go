package httpapi

import (
	"net/http"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// HomeHandler expone el feed de Inicio. Requiere AuthMiddleware.
type HomeHandler struct {
	recommendations *services.RecommendationService
}

// NewHomeHandler crea el handler.
func NewHomeHandler(recommendations *services.RecommendationService) *HomeHandler {
	return &HomeHandler{recommendations: recommendations}
}

// Home: GET /api/v1/home?limit=
func (h *HomeHandler) Home(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	limit, ok := parseLimit(w, r.URL.Query().Get("limit"), services.MaxHomeLimit)
	if !ok {
		return
	}

	feed, err := h.recommendations.Home(r.Context(), claims.UserID, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, feed)
}
