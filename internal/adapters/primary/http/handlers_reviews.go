package httpapi

import (
	"net/http"
	"strconv"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/dto"
	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// ReviewHandler expone las reseñas. Todas sus rutas requieren
// AuthMiddleware.
type ReviewHandler struct {
	reviews *services.ReviewService
}

// NewReviewHandler crea el handler.
func NewReviewHandler(reviews *services.ReviewService) *ReviewHandler {
	return &ReviewHandler{reviews: reviews}
}

// Create: POST /api/v1/reviews
func (h *ReviewHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var req dto.CreateReviewRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	review, err := h.reviews.Create(r.Context(), claims.UserID, req.TargetType, req.TargetID, services.ReviewInput{
		Title:   req.Title,
		Content: req.Content,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

// List: GET /api/v1/reviews?target_type=&target_id=&limit=&page=
func (h *ReviewHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := intQuery(w, q.Get("limit"), "limit")
	if !ok {
		return
	}
	page, ok := intQuery(w, q.Get("page"), "page")
	if !ok {
		return
	}
	if (q.Has("limit") && limit == 0) || (q.Has("page") && page == 0) {
		// 0 es el "no enviado" del servicio; explícito es inválido.
		writeError(w, http.StatusBadRequest, "limit y page deben ser enteros positivos")
		return
	}
	result, err := h.reviews.List(r.Context(), q.Get("target_type"), q.Get("target_id"), limit, page)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Get: GET /api/v1/reviews/{id}
func (h *ReviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	review, err := h.reviews.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// Update: PUT /api/v1/reviews/{id}
func (h *ReviewHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	var req dto.UpdateReviewRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	review, err := h.reviews.Update(r.Context(), claims.UserID, id, services.ReviewInput{
		Title:   req.Title,
		Content: req.Content,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// Delete: DELETE /api/v1/reviews/{id}
func (h *ReviewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	if err := h.reviews.Delete(r.Context(), claims.UserID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: "reseña eliminada"})
}

// intQuery lee un parámetro entero opcional; vacío devuelve 0.
func intQuery(w http.ResponseWriter, raw, name string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, name+" debe ser un entero")
		return 0, false
	}
	return n, true
}
