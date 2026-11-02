// Package freqblog implementa ports.AudioFeaturesProvider sobre la API de
// FreqBlog, que reemplaza al endpoint /v1/audio-features que Spotify cerró
// en noviembre de 2024. Usa su ruta compatible con Spotify:
// GET /v1/audio-features?ids=<ISRC o Spotify ID, hasta 100>.
package freqblog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

const (
	defaultBaseURL = "https://api.freqblog.com"
	// maxIDsPerRequest es el máximo de identificadores por petición.
	maxIDsPerRequest = 100
)

// Config configura el cliente. BaseURL y HTTPClient son opcionales.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Client es un cliente de FreqBlog seguro para uso concurrente.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New crea el cliente.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// Los lotes de ISRC pueden tardar: FreqBlog resuelve en MusicBrainz.
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{apiKey: cfg.APIKey, baseURL: strings.TrimRight(cfg.BaseURL, "/"), http: httpClient}
}

// APIError es una respuesta de error de FreqBlog.
type APIError struct {
	Status int
	Detail string
	retry  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("freqblog: HTTP %d: %s", e.Status, e.Detail)
}

// RetryAfter es la espera indicada en un 429 (0 si no aplica).
func (e *APIError) RetryAfter() time.Duration { return e.retry }

// features es un objeto de audio features con la forma de Spotify. Los
// campos que FreqBlog no mide llegan en null.
type features struct {
	Danceability     *float64 `json:"danceability"`
	Energy           *float64 `json:"energy"`
	Key              *int32   `json:"key"`
	Loudness         *float64 `json:"loudness"`
	Mode             *int32   `json:"mode"`
	Speechiness      *float64 `json:"speechiness"`
	Acousticness     *float64 `json:"acousticness"`
	Instrumentalness *float64 `json:"instrumentalness"`
	Liveness         *float64 `json:"liveness"`
	Valence          *float64 `json:"valence"`
	Tempo            *float64 `json:"tempo"`
}

// GetAudioFeatures consulta los IDs (ISRC o Spotify ID) en lotes de 100.
// Los IDs sin datos no aparecen en el mapa: FreqBlog devuelve null para
// ellos (y, si es un ISRC que aún no tiene, lo encola para más tarde).
func (c *Client) GetAudioFeatures(ctx context.Context, ids []string) (map[string]*domain.AudioFeatures, error) {
	out := make(map[string]*domain.AudioFeatures, len(ids))
	for start := 0; start < len(ids); start += maxIDsPerRequest {
		chunk := ids[start:min(start+maxIDsPerRequest, len(ids))]
		results, err := c.fetch(ctx, chunk)
		if err != nil {
			return nil, err
		}
		// La respuesta conserva el orden y la longitud de la entrada.
		for i, f := range results {
			if i < len(chunk) && f != nil {
				out[chunk[i]] = f.toDomain()
			}
		}
	}
	return out, nil
}

func (c *Client) fetch(ctx context.Context, ids []string) ([]*features, error) {
	endpoint := c.baseURL + "/v1/audio-features?" + url.Values{"ids": {strings.Join(ids, ",")}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("freqblog: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("freqblog: leer respuesta: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{Status: resp.StatusCode, Detail: errorDetail(body, resp.StatusCode)}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			apiErr.retry = time.Duration(secs) * time.Second
		}
		return nil, apiErr
	}

	var parsed struct {
		AudioFeatures []*features `json:"audio_features"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("freqblog: decodificar respuesta: %w", err)
	}
	return parsed.AudioFeatures, nil
}

// errorDetail extrae el mensaje de {"detail": "..."} (o de una lista de
// errores de validación) y si no, usa el texto del código HTTP.
func errorDetail(body []byte, status int) string {
	var e struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Detail) > 0 {
		var s string
		if json.Unmarshal(e.Detail, &s) == nil {
			return s
		}
		return string(e.Detail)
	}
	return http.StatusText(status)
}

func (f *features) toDomain() *domain.AudioFeatures {
	af := &domain.AudioFeatures{
		Danceability:     f.Danceability,
		Energy:           f.Energy,
		Key:              f.Key,
		Loudness:         f.Loudness,
		Mode:             f.Mode,
		Speechiness:      f.Speechiness,
		Acousticness:     f.Acousticness,
		Instrumentalness: f.Instrumentalness,
		Liveness:         f.Liveness,
		Valence:          f.Valence,
		Tempo:            f.Tempo,
		// time_signature se omite: FreqBlog lo asume 4 en todas sus
		// canciones, no lo mide.
	}
	if af.Key != nil && *af.Key < 0 { // -1: tonalidad no detectada
		af.Key = nil
	}
	return af
}
