package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// fakeSpotify simula accounts.spotify.com y api.spotify.com en un solo
// servidor. search responde a /v1/search.
type fakeSpotify struct {
	tokenCalls  atomic.Int32
	searchCalls atomic.Int32
	expiresIn   int
	search      func(w http.ResponseWriter, r *http.Request)
}

func (f *fakeSpotify) start(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/token", func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "client" || secret != "secret" || r.FormValue("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		n := f.tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-" + strconv.Itoa(int(n)), "token_type": "Bearer", "expires_in": f.expiresIn})
	})
	mux.HandleFunc("GET /v1/search", func(w http.ResponseWriter, r *http.Request) {
		f.searchCalls.Add(1)
		f.search(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(Config{ClientID: "client", ClientSecret: "secret", AccountsURL: srv.URL, APIURL: srv.URL})
}

const trackJSON = `{
  "id": "2qSkXiYOKEzfk3vhRpIQUR",
  "name": "Espresso",
  "duration_ms": 175459,
  "external_ids": {"isrc": "USUM72401994"},
  "artists": [{"id": "74KM79TiuVKeVCqs8QtB0B", "name": "Sabrina Carpenter"}],
  "album": {
    "id": "2lIZef49vdWi0LMePaA4Sp",
    "name": "Short n' Sweet",
    "album_type": "album",
    "release_date": "2024-08-23",
    "total_tracks": 12,
    "images": [{"url": "small", "width": 64}, {"url": "big", "width": 640}],
    "artists": [{"id": "74KM79TiuVKeVCqs8QtB0B", "name": "Sabrina Carpenter"}]
  }
}`

func TestSearchTracksMapeaYCacheaToken(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	f.search = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("type") != "track" || r.URL.Query().Get("q") != "espresso" {
			t.Errorf("query inesperada: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"tracks":{"items":[` + trackJSON + `],"next":null}}`))
	}
	c := f.start(t)

	for range 2 {
		tracks, err := c.SearchTracks(context.Background(), "espresso", 5)
		if err != nil {
			t.Fatalf("SearchTracks: %v", err)
		}
		if len(tracks) != 1 {
			t.Fatalf("len = %d", len(tracks))
		}
		tr := tracks[0]
		if tr.SpotifyID != "2qSkXiYOKEzfk3vhRpIQUR" || tr.Artist.Name != "Sabrina Carpenter" || *tr.ISRC != "USUM72401994" {
			t.Fatalf("track mal mapeado: %+v", tr)
		}
		if tr.Popularity != nil {
			t.Fatalf("popularity ausente debía quedar nil")
		}
		if *tr.Album.ReleaseYear != 2024 || *tr.Album.ImageURL != "big" || tr.Album.TotalTracks != 12 || tr.Album.Artist.SpotifyID != "74KM79TiuVKeVCqs8QtB0B" {
			t.Fatalf("álbum mal mapeado: %+v", tr.Album)
		}
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Fatalf("token pedido %d veces, se esperaba 1 (caché)", n)
	}
}

func TestTokenPorExpirarSeRenueva(t *testing.T) {
	f := &fakeSpotify{expiresIn: 30} // menos que tokenSkew
	f.search = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tracks":{"items":[],"next":null}}`))
	}
	c := f.start(t)

	for range 2 {
		if _, err := c.SearchTracks(context.Background(), "x", 5); err != nil {
			t.Fatalf("SearchTracks: %v", err)
		}
	}
	if n := f.tokenCalls.Load(); n != 2 {
		t.Fatalf("token pedido %d veces, se esperaban 2", n)
	}
}

func TestSearchPaginaDeDiezEnDiez(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	f.search = func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit > pageSize {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"status":400,"message":"Invalid limit"}}`))
			return
		}
		items := make([]map[string]any, limit)
		for i := range items {
			items[i] = map[string]any{"id": "a" + r.URL.Query().Get("offset") + strconv.Itoa(i), "name": "Artist"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"artists": map[string]any{"items": items, "next": "más"}})
	}
	c := f.start(t)

	artists, err := c.SearchArtists(context.Background(), "x", 25)
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if len(artists) != 25 || f.searchCalls.Load() != 3 {
		t.Fatalf("len = %d, llamadas = %d; se esperaban 25 y 3", len(artists), f.searchCalls.Load())
	}
}

func TestSearch429ConRetryAfterCortoReintenta(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	f.search = func(w http.ResponseWriter, _ *http.Request) {
		if f.searchCalls.Load() == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"albums":{"items":[],"next":null}}`))
	}
	c := f.start(t)

	if _, err := c.SearchAlbums(context.Background(), "x", 5); err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if f.searchCalls.Load() != 2 {
		t.Fatalf("llamadas = %d, se esperaban 2", f.searchCalls.Load())
	}
}

func TestSearch429ConRetryAfterLargoFalla(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	f.search = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"status":429,"message":"API rate limit exceeded"}}`))
	}
	c := f.start(t)

	_, err := c.SearchTracks(context.Background(), "x", 5)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests || apiErr.RetryAfter().Hours() != 1 {
		t.Fatalf("err = %v, se esperaba APIError 429 con Retry-After", err)
	}
	if f.searchCalls.Load() != 1 {
		t.Fatalf("no debía reintentar (llamadas = %d)", f.searchCalls.Load())
	}
}

func TestSearch401RenuevaTokenUnaVez(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	f.search = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"tracks":{"items":[],"next":null}}`))
	}
	c := f.start(t)

	if _, err := c.SearchTracks(context.Background(), "x", 5); err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	if f.tokenCalls.Load() != 2 {
		t.Fatalf("token pedido %d veces, se esperaban 2", f.tokenCalls.Load())
	}
}

func TestCredencialesInvalidas(t *testing.T) {
	f := &fakeSpotify{expiresIn: 3600}
	c := f.start(t)
	c.cfg.ClientSecret = "otro"

	if _, err := c.SearchTracks(context.Background(), "x", 5); err == nil {
		t.Fatal("se esperaba error con credenciales inválidas")
	}
}
