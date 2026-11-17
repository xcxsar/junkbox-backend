package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// SocialHandler expone el feed de reseñas, los perfiles públicos, los
// seguidores, los Me gusta y los comentarios (HOME-04). Todas sus rutas
// requieren AuthMiddleware.
type SocialHandler struct {
	feed        *services.FeedService
	social      *services.SocialService
	comments    *services.CommentService
	suggestions *services.SuggestionService
}

// NewSocialHandler crea el handler.
func NewSocialHandler(feed *services.FeedService, social *services.SocialService, comments *services.CommentService, suggestions *services.SuggestionService) *SocialHandler {
	return &SocialHandler{feed: feed, social: social, comments: comments, suggestions: suggestions}
}

// Feed: GET /api/v1/feed?tab=popular|following&tz=&cursor=&limit=
func (h *SocialHandler) Feed(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"), services.MaxPageLimit)
	if !ok {
		return
	}
	page, err := h.feed.Feed(r.Context(), claims.UserID, q.Get("tab"), q.Get("tz"), q.Get("cursor"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// Suggestions: GET /api/v1/users/suggestions?limit=
func (h *SocialHandler) Suggestions(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	limit, ok := parseLimit(w, r.URL.Query().Get("limit"), services.MaxSuggestionLimit)
	if !ok {
		return
	}
	page, err := h.suggestions.Suggestions(r.Context(), claims.UserID, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// SearchUsers: GET /api/v1/users/search?q=&limit=
func (h *SocialHandler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"), services.MaxUserSearchLimit)
	if !ok {
		return
	}
	result, err := h.social.SearchUsers(r.Context(), claims.UserID, q.Get("q"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Profile: GET /api/v1/users/{id} (UUID o username)
func (h *SocialHandler) Profile(w http.ResponseWriter, r *http.Request) {
	h.profileAction(w, r, h.social.Profile)
}

// Follow: POST /api/v1/users/{id}/follow
func (h *SocialHandler) Follow(w http.ResponseWriter, r *http.Request) {
	h.profileAction(w, r, h.social.Follow)
}

// Unfollow: DELETE /api/v1/users/{id}/follow
func (h *SocialHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	h.profileAction(w, r, h.social.Unfollow)
}

// Followers: GET /api/v1/users/{id}/followers?cursor=&limit=
func (h *SocialHandler) Followers(w http.ResponseWriter, r *http.Request) {
	h.followList(w, r, h.social.Followers)
}

// Following: GET /api/v1/users/{id}/following?cursor=&limit=
func (h *SocialHandler) Following(w http.ResponseWriter, r *http.Request) {
	h.followList(w, r, h.social.Following)
}

// Like: POST /api/v1/reviews/{id}/like
func (h *SocialHandler) Like(w http.ResponseWriter, r *http.Request) {
	h.likeAction(w, r, true)
}

// Unlike: DELETE /api/v1/reviews/{id}/like
func (h *SocialHandler) Unlike(w http.ResponseWriter, r *http.Request) {
	h.likeAction(w, r, false)
}

func (h *SocialHandler) profileAction(w http.ResponseWriter, r *http.Request, action func(ctx context.Context, viewer uuid.UUID, user string) (*domain.PublicProfile, error)) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	profile, err := action(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *SocialHandler) followList(w http.ResponseWriter, r *http.Request, list func(ctx context.Context, viewer uuid.UUID, user, cursor string, limit int) (*domain.FollowPage, error)) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"), services.MaxPageLimit)
	if !ok {
		return
	}
	page, err := list(r.Context(), claims.UserID, chi.URLParam(r, "id"), q.Get("cursor"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *SocialHandler) likeAction(w http.ResponseWriter, r *http.Request, like bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	id, ok := uuidParam(w, r)
	if !ok {
		return
	}
	action := h.social.Unlike
	if like {
		action = h.social.Like
	}
	result, err := action(r.Context(), claims.UserID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
