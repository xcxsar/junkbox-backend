package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const frontendOrigin = "http://localhost:5173"

func newTestRouter(origins []string) http.Handler {
	return NewRouter(RouterOptions{Auth: &AuthHandler{}, AllowedOrigins: origins})
}

func preflight(h http.Handler, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORSPreflightFrontend(t *testing.T) {
	rec := preflight(newTestRouter([]string{frontendOrigin}), frontendOrigin)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != frontendOrigin {
		t.Fatalf("Allow-Origin = %q, esperaba %q", got, frontendOrigin)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Errorf("faltan Allow-Methods/Allow-Headers: %v", rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("no deben habilitarse credenciales")
	}
}

func TestCORSPreflightOtroOrigen(t *testing.T) {
	rec := preflight(newTestRouter([]string{frontendOrigin}), "http://evil.example")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("un origen no permitido no debe recibir Allow-Origin, obtuve %q", got)
	}
}

func TestCORSListaVaciaRechazaTodo(t *testing.T) {
	rec := preflight(newTestRouter(nil), frontendOrigin)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("con lista vacía no debe permitirse ningún origen, obtuve %q", got)
	}
}

func TestCORSPeticionSimple(t *testing.T) {
	h := newTestRouter([]string{frontendOrigin})
	for origin, want := range map[string]string{frontendOrigin: frontendOrigin, "": ""} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Origin %q: status %d", origin, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("Origin %q: Allow-Origin = %q, esperaba %q", origin, got, want)
		}
	}
}
