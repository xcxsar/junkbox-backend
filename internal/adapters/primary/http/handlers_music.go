package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// MusicHandler expone la búsqueda y el detalle del catálogo musical. Todas
// sus rutas requieren AuthMiddleware.
type MusicHandler struct {
	music *services.MusicService
}

// NewMusicHandler crea el handler.
func NewMusicHandler(music *services.MusicService) *MusicHandler {
	return &MusicHandler{music: music}
}

// Search: GET /api/v1/music/search?q=&type=&limit=
func (h *MusicHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit debe ser un entero")
			return
		}
		limit = n
		if limit == 0 {
			writeError(w, http.StatusBadRequest, "limit debe estar entre 1 y 50")
			return
		}
	}

	result, err := h.music.Search(r.Context(), q.Get("q"), q.Get("type"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Track: GET /api/v1/music/tracks/{id} (id local o ID de Spotify)
func (h *MusicHandler) Track(w http.ResponseWriter, r *http.Request) {
	track, err := h.music.GetTrack(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, track)
}

// Artist: GET /api/v1/music/artists/{id} (UUID local o ID de Spotify)
func (h *MusicHandler) Artist(w http.ResponseWriter, r *http.Request) {
	artist, err := h.music.GetArtist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artist)
}

// Album: GET /api/v1/music/albums/{id} (UUID local o ID de Spotify), con
// sus pistas.
func (h *MusicHandler) Album(w http.ResponseWriter, r *http.Request) {
	album, err := h.music.GetAlbum(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, album)
}
