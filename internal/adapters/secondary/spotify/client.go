// Package spotify implementa ports.MusicProvider sobre la Web API de
// Spotify con el flujo OAuth2 Client Credentials.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

const (
	defaultAccountsURL = "https://accounts.spotify.com"
	defaultAPIURL      = "https://api.spotify.com"
	// pageSize es el máximo de resultados por petición de /v1/search para
	// apps en Development Mode (desde febrero de 2026); se pagina con offset.
	pageSize = 10
	// tokenSkew renueva el token si le queda menos que esto de vida.
	tokenSkew = 60 * time.Second
	// maxRetryWait es la espera máxima ante un 429 antes de reintentar una
	// vez; con un Retry-After mayor se devuelve el error.
	maxRetryWait = 5 * time.Second
)

// Config configura el cliente. AccountsURL, APIURL y HTTPClient son
// opcionales (se sobreescriben en tests).
type Config struct {
	ClientID     string
	ClientSecret string
	AccountsURL  string
	APIURL       string
	HTTPClient   *http.Client
}

// Client es un cliente de Spotify seguro para uso concurrente. Cachea el
// token de aplicación y lo renueva antes de que expire.
type Client struct {
	cfg  Config
	http *http.Client

	mu        sync.RWMutex
	token     string
	expiresAt time.Time
}

// New crea el cliente.
func New(cfg Config) *Client {
	if cfg.AccountsURL == "" {
		cfg.AccountsURL = defaultAccountsURL
	}
	if cfg.APIURL == "" {
		cfg.APIURL = defaultAPIURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{cfg: cfg, http: httpClient}
}

// APIError es una respuesta de error de Spotify.
type APIError struct {
	Status  int
	Message string
	retry   time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spotify: HTTP %d: %s", e.Status, e.Message)
}

// RetryAfter es la espera indicada por Spotify en un 429 (0 si no aplica).
func (e *APIError) RetryAfter() time.Duration { return e.retry }

// --- búsqueda ---

// SearchTracks busca canciones y las mapea con su artista principal y álbum.
func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]domain.ImportedTrack, error) {
	var out []domain.ImportedTrack
	err := c.search(ctx, query, "track", limit, func(body []byte) (int, bool, error) {
		var resp struct {
			Tracks page[trackObject] `json:"tracks"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return 0, false, err
		}
		for _, t := range resp.Tracks.Items {
			out = append(out, t.toDomain())
		}
		return len(resp.Tracks.Items), resp.Tracks.Next != nil, nil
	})
	return out, err
}

// SearchArtists busca artistas.
func (c *Client) SearchArtists(ctx context.Context, query string, limit int) ([]domain.ImportedArtist, error) {
	var out []domain.ImportedArtist
	err := c.search(ctx, query, "artist", limit, func(body []byte) (int, bool, error) {
		var resp struct {
			Artists page[artistObject] `json:"artists"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return 0, false, err
		}
		for _, a := range resp.Artists.Items {
			out = append(out, a.toDomain())
		}
		return len(resp.Artists.Items), resp.Artists.Next != nil, nil
	})
	return out, err
}

// SearchAlbums busca álbumes.
func (c *Client) SearchAlbums(ctx context.Context, query string, limit int) ([]domain.ImportedAlbum, error) {
	var out []domain.ImportedAlbum
	err := c.search(ctx, query, "album", limit, func(body []byte) (int, bool, error) {
		var resp struct {
			Albums page[albumObject] `json:"albums"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return 0, false, err
		}
		for _, a := range resp.Albums.Items {
			out = append(out, a.toDomain())
		}
		return len(resp.Albums.Items), resp.Albums.Next != nil, nil
	})
	return out, err
}

// --- detalle por ID ---

// GetArtist obtiene el artista completo (con géneros e imagen). En
// Development Mode no existe la versión por lotes (GET /v1/artists?ids=).
func (c *Client) GetArtist(ctx context.Context, id string) (domain.ImportedArtist, error) {
	var a artistObject
	if err := c.getObject(ctx, "/v1/artists/"+url.PathEscape(id), &a); err != nil {
		return domain.ImportedArtist{}, fmt.Errorf("spotify: artista %s: %w", id, err)
	}
	return a.toDomain(), nil
}

// GetTrack obtiene una canción con su álbum y artista principal.
func (c *Client) GetTrack(ctx context.Context, id string) (domain.ImportedTrack, error) {
	var t trackObject
	if err := c.getObject(ctx, "/v1/tracks/"+url.PathEscape(id), &t); err != nil {
		return domain.ImportedTrack{}, fmt.Errorf("spotify: canción %s: %w", id, err)
	}
	return t.toDomain(), nil
}

// getObject hace un GET y decodifica la respuesta en out. Un 404 o un 400
// (ID inválido) se traducen a domain.ErrNotFound.
func (c *Client) getObject(ctx context.Context, path string, out any) error {
	body, err := c.get(ctx, path)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusBadRequest) {
			return domain.ErrNotFound
		}
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decodificar respuesta: %w", err)
	}
	return nil
}

// search pagina /v1/search de pageSize en pageSize hasta juntar limit
// resultados o agotar la búsqueda. parse devuelve cuántos ítems leyó y si
// hay página siguiente.
func (c *Client) search(ctx context.Context, query, searchType string, limit int, parse func([]byte) (int, bool, error)) error {
	for offset := 0; offset < limit; offset += pageSize {
		n := min(pageSize, limit-offset)
		params := url.Values{
			"q":      {query},
			"type":   {searchType},
			"limit":  {strconv.Itoa(n)},
			"offset": {strconv.Itoa(offset)},
		}
		body, err := c.get(ctx, "/v1/search?"+params.Encode())
		if err != nil {
			return err
		}
		got, hasNext, err := parse(body)
		if err != nil {
			return fmt.Errorf("spotify: decodificar búsqueda: %w", err)
		}
		if got < n || !hasNext {
			return nil
		}
	}
	return nil
}

// get hace un GET autenticado. Ante un 401 renueva el token y reintenta una
// vez; ante un 429 con Retry-After corto espera y reintenta una vez.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		body, err := c.do(req)
		if err == nil {
			return body, nil
		}
		lastErr = err
		apiErr, ok := err.(*APIError)
		if !ok {
			return nil, err
		}
		switch {
		case apiErr.Status == http.StatusUnauthorized:
			c.invalidateToken()
		case apiErr.Status == http.StatusTooManyRequests && apiErr.retry <= maxRetryWait:
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(apiErr.retry):
			}
		default:
			return nil, err
		}
	}
	return nil, lastErr
}

// --- token de aplicación (Client Credentials) ---

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	token, valid := c.token, time.Until(c.expiresAt) > tokenSkew
	c.mu.RUnlock()
	if valid {
		return token, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Otra goroutine pudo renovarlo mientras esperábamos el lock.
	if time.Until(c.expiresAt) > tokenSkew {
		return c.token, nil
	}

	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AccountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("spotify: obtener token: %w", err)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.AccessToken == "" {
		return "", fmt.Errorf("spotify: respuesta de token inválida")
	}
	c.token = resp.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.expiresAt = time.Time{}
	c.mu.Unlock()
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spotify: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("spotify: leer respuesta: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	apiErr := &APIError{Status: resp.StatusCode, Message: errorMessage(body)}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
		apiErr.retry = time.Duration(secs) * time.Second
	}
	return nil, apiErr
}

// errorMessage extrae el mensaje de los dos formatos de error de Spotify:
// {"error":{"message":...}} (Web API) y {"error":"...","error_description":...} (cuentas).
func errorMessage(body []byte) string {
	var webAPI struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &webAPI) == nil && webAPI.Error.Message != "" {
		return webAPI.Error.Message
	}
	var accounts struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &accounts) == nil && accounts.Error != "" {
		return strings.TrimSpace(accounts.Error + " " + accounts.Description)
	}
	return http.StatusText(http.StatusBadGateway)
}

// --- objetos de la Web API ---

type page[T any] struct {
	Items []T     `json:"items"`
	Next  *string `json:"next"`
}

type image struct {
	URL   string `json:"url"`
	Width *int   `json:"width"`
}

type artistObject struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Genres     []string `json:"genres"`
	Popularity *int32   `json:"popularity"` // ausente en Development Mode
	Images     []image  `json:"images"`
}

type albumObject struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	AlbumType   string         `json:"album_type"`
	ReleaseDate string         `json:"release_date"` // YYYY, YYYY-MM o YYYY-MM-DD
	TotalTracks int32          `json:"total_tracks"`
	Images      []image        `json:"images"`
	Artists     []artistObject `json:"artists"`
}

type trackObject struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Popularity  *int32 `json:"popularity"` // ausente en Development Mode
	DurationMs  *int32 `json:"duration_ms"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	Artists []artistObject `json:"artists"`
	Album   albumObject    `json:"album"`
}

func (a artistObject) toDomain() domain.ImportedArtist {
	return domain.ImportedArtist{
		SpotifyID:  a.ID,
		Name:       a.Name,
		Genres:     a.Genres,
		Popularity: a.Popularity,
		ImageURL:   largestImage(a.Images),
	}
}

func (a albumObject) toDomain() domain.ImportedAlbum {
	album := domain.ImportedAlbum{
		SpotifyID:   a.ID,
		Title:       a.Name,
		AlbumType:   a.AlbumType,
		ImageURL:    largestImage(a.Images),
		TotalTracks: a.TotalTracks,
	}
	if len(a.Artists) > 0 {
		album.Artist = a.Artists[0].toDomain()
	}
	if len(a.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(a.ReleaseDate[:4]); err == nil {
			year := int32(y)
			album.ReleaseYear = &year
		}
	}
	return album
}

func (t trackObject) toDomain() domain.ImportedTrack {
	track := domain.ImportedTrack{
		SpotifyID:  t.ID,
		Name:       t.Name,
		Album:      t.Album.toDomain(),
		Popularity: t.Popularity,
		DurationMs: t.DurationMs,
	}
	if len(t.Artists) > 0 {
		track.Artist = t.Artists[0].toDomain()
	}
	if isrc := strings.ToUpper(strings.ReplaceAll(t.ExternalIDs.ISRC, "-", "")); len(isrc) == 12 {
		track.ISRC = &isrc
	}
	return track
}

// largestImage devuelve la URL de la imagen más ancha (Spotify suele
// ordenarlas de mayor a menor, pero no lo garantiza).
func largestImage(images []image) *string {
	best, bestWidth := -1, -1
	for i, img := range images {
		w := 0
		if img.Width != nil {
			w = *img.Width
		}
		if img.URL != "" && w > bestWidth {
			best, bestWidth = i, w
		}
	}
	if best < 0 {
		return nil
	}
	return &images[best].URL
}
