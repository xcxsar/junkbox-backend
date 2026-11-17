package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/dto"
	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// ListComments: GET /api/v1/reviews/{id}/comments?cursor=&limit= (raíces)
func (h *SocialHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	h.commentList(w, r, h.comments.List)
}

// ListReplies: GET /api/v1/comments/{id}/replies?cursor=&limit=
func (h *SocialHandler) ListReplies(w http.ResponseWriter, r *http.Request) {
	h.commentList(w, r, h.comments.Replies)
}

// CreateComment: POST /api/v1/reviews/{id}/comments. Con parent_id es una
// respuesta.
func (h *SocialHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	reviewID, ok := uuidParam(w, r)
	if !ok {
		return
	}
	var req dto.CreateCommentRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	var parentID *uuid.UUID
	if req.ParentID != nil {
		id, err := uuid.Parse(*req.ParentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "parent_id debe ser un UUID")
			return
		}
		parentID = &id
	}
	comment, err := h.comments.Create(r.Context(), claims.UserID, reviewID, parentID, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

// UpdateComment: PUT /api/v1/comments/{id}
func (h *SocialHandler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	var req dto.UpdateCommentRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	comment, err := h.comments.Update(r.Context(), claims.UserID, id, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

// DeleteComment: DELETE /api/v1/comments/{id}
func (h *SocialHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	if err := h.comments.Delete(r.Context(), claims.UserID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageResponse{Message: "comentario eliminado"})
}

func (h *SocialHandler) commentList(w http.ResponseWriter, r *http.Request, list func(ctx context.Context, id uuid.UUID, cursor string, limit int) (*domain.CommentPage, error)) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"), services.MaxPageLimit)
	if !ok {
		return
	}
	page, err := list(r.Context(), id, q.Get("cursor"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
