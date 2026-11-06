package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/dto"
	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// RatingHandler expone las calificaciones. Todas sus rutas requieren
// AuthMiddleware.
type RatingHandler struct {
	ratings *services.RatingService
}

// NewRatingHandler crea el handler.
func NewRatingHandler(ratings *services.RatingService) *RatingHandler {
	return &RatingHandler{ratings: ratings}
}

// Rate: POST /api/v1/ratings. 201 si el voto es nuevo, 200 si reemplaza
// uno anterior.
func (h *RatingHandler) Rate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var req dto.RatingRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	rating, created, err := h.ratings.Rate(r.Context(), claims.UserID, req.TargetType, req.TargetID, req.Score)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, rating)
}

// Mine: GET /api/v1/ratings/my?target_type=&target_id=
func (h *RatingHandler) Mine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	q := r.URL.Query()
	rating, err := h.ratings.GetMine(r.Context(), claims.UserID, q.Get("target_type"), q.Get("target_id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rating)
}

// Delete: DELETE /api/v1/ratings/{id}
func (h *RatingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	if err := h.ratings.Delete(r.Context(), claims.UserID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: "calificación eliminada"})
}

// uuidParam lee el parámetro {id} de la ruta como UUID; si no lo es,
// responde 400.
func uuidParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id debe ser un UUID")
		return uuid.Nil, false
	}
	return id, true
}
