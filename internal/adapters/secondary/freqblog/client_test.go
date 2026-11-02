package freqblog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "fb_test", BaseURL: srv.URL})
}

func TestGetAudioFeaturesMapeaYOmiteNulos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio-features" || r.Header.Get("X-Api-Key") != "fb_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.URL.Query().Get("ids"); got != "USUM72401994,unknown" {
			t.Errorf("ids = %q", got)
		}
		_, _ = w.Write([]byte(`{"audio_features":[
			{"id":"USUM72401994","danceability":0.7,"energy":0.77,"key":-1,"loudness":-5.4,"mode":1,
			 "speechiness":null,"liveness":null,"acousticness":0.1,"instrumentalness":0.0,"valence":0.69,
			 "tempo":104.0,"time_signature":4},
			null
		]}`))
	})

	got, err := c.GetAudioFeatures(context.Background(), []string{"USUM72401994", "unknown"})
	if err != nil {
		t.Fatalf("GetAudioFeatures: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, el ID sin datos no debía aparecer", len(got))
	}
	f := got["USUM72401994"]
	if *f.Energy != 0.77 || *f.Tempo != 104.0 || *f.Mode != 1 {
		t.Fatalf("features mal mapeados: %+v", f)
	}
	if f.Speechiness != nil || f.Liveness != nil {
		t.Fatal("speechiness/liveness nulos debían quedar nil")
	}
	if f.Key != nil {
		t.Fatal("key -1 (no detectada) debía quedar nil")
	}
	if f.TimeSignature != nil {
		t.Fatal("time_signature (asumido por FreqBlog) debía omitirse")
	}
}

func TestGetAudioFeaturesDivideEnLotesDe100(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		if len(ids) > maxIDsPerRequest {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		items := make([]string, len(ids))
		for i, id := range ids {
			items[i] = fmt.Sprintf(`{"id":%q,"energy":0.5}`, id)
		}
		_, _ = w.Write([]byte(`{"audio_features":[` + strings.Join(items, ",") + `]}`))
	})

	ids := make([]string, 150)
	for i := range ids {
		ids[i] = fmt.Sprintf("ID%010d", i)
	}
	got, err := c.GetAudioFeatures(context.Background(), ids)
	if err != nil {
		t.Fatalf("GetAudioFeatures: %v", err)
	}
	if len(got) != 150 || calls.Load() != 2 {
		t.Fatalf("len = %d, llamadas = %d; se esperaban 150 y 2", len(got), calls.Load())
	}
}

func TestGetAudioFeatures429DevuelveRetryAfter(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"Monthly quota exceeded"}`))
	})

	_, err := c.GetAudioFeatures(context.Background(), []string{"x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests || apiErr.RetryAfter() != 30*time.Second {
		t.Fatalf("err = %v, se esperaba APIError 429 con Retry-After de 30s", err)
	}
	if !strings.Contains(err.Error(), "Monthly quota exceeded") {
		t.Fatalf("el error debía incluir el detalle: %v", err)
	}
}

func TestGetAudioFeatures401(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Missing API key","error_code":"KEY_MISSING"}`))
	})

	_, err := c.GetAudioFeatures(context.Background(), []string{"x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.RetryAfter() != 0 {
		t.Fatalf("err = %v, se esperaba APIError 401", err)
	}
}
