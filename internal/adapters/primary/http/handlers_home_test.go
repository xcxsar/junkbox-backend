package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/services"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// popularOnlyStore no tiene semilla para nadie: el feed sale de populares.
type popularOnlyStore struct{ userID uuid.UUID }

func (popularOnlyStore) GetRecommendationSeed(context.Context, uuid.UUID) (sqlc.GetRecommendationSeedRow, error) {
	return sqlc.GetRecommendationSeedRow{}, sql.ErrNoRows
}

func (popularOnlyStore) RecommendSimilarTracks(context.Context, sqlc.RecommendSimilarTracksParams) ([]sqlc.RecommendSimilarTracksRow, error) {
	return nil, nil
}

func (s popularOnlyStore) ListPopularTracks(_ context.Context, arg sqlc.ListPopularTracksParams) ([]sqlc.ListPopularTracksRow, error) {
	if arg.UserID != s.userID {
		return nil, nil
	}
	return []sqlc.ListPopularTracksRow{{
		ID:        42,
		TrackID:   sql.NullString{String: "3n3Ppam7vgaVa1iaRUc9Lp", Valid: true},
		TrackName: sql.NullString{String: "Flowers", Valid: true},
	}}, nil
}

func newHomeRouter(userID uuid.UUID) http.Handler {
	svc := services.NewRecommendationService(popularOnlyStore{userID: userID}, services.SearchEnrichment{})
	return NewRouter(RouterOptions{Auth: &AuthHandler{}, Home: NewHomeHandler(svc), JWT: staticJWT{userID: userID}})
}

func getHome(h http.Handler, query, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/home"+query, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHomeRequiereBearer(t *testing.T) {
	if rec := getHome(newHomeRouter(uuid.New()), "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sin token: status %d, esperaba 401", rec.Code)
	}
}

func TestHomeValidaLimit(t *testing.T) {
	h := newHomeRouter(uuid.New())
	for _, q := range []string{"?limit=abc", "?limit=0", "?limit=51", "?limit=-1"} {
		if rec := getHome(h, q, "valid"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, esperaba 400 (%s)", q, rec.Code, rec.Body)
		}
	}
}

func TestHomeRespuesta(t *testing.T) {
	rec := getHome(newHomeRouter(uuid.New()), "", "valid")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body)
	}
	var body struct {
		Sections []struct {
			ID       string           `json:"id"`
			Title    string           `json:"title"`
			Strategy string           `json:"strategy"`
			Seed     *json.RawMessage `json:"seed"`
			Items    []map[string]any `json:"items"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	if len(body.Sections) != 1 {
		t.Fatalf("esperaba una sección: %s", rec.Body)
	}
	s := body.Sections[0]
	if s.ID != "made_for_you" || s.Title != "Hecho para tus oídos" || s.Strategy != "popular" || s.Seed != nil {
		t.Fatalf("sección inesperada: %s", rec.Body)
	}
	// type y los campos de la canción van al mismo nivel.
	if len(s.Items) != 1 || s.Items[0]["type"] != "track" || s.Items[0]["id"] != float64(42) || s.Items[0]["track_name"] != "Flowers" {
		t.Fatalf("items inesperados: %s", rec.Body)
	}
}
