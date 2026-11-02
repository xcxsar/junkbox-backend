// Package config carga y valida la configuración del backend desde
// variables de entorno, con soporte opcional de un archivo .env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config es la configuración fuertemente tipada del backend.
type Config struct {
	// DatabaseURL es el DSN de PostgreSQL
	// (ej. postgres://user:pass@localhost:5432/junkbox_db?sslmode=disable).
	DatabaseURL string
	// JWTSecret es la clave HMAC-SHA256 para firmar los Access Tokens.
	// Debe tener al menos 32 caracteres.
	JWTSecret string
	// AccessTTL es la vigencia del Access Token (default 15 minutos).
	AccessTTL time.Duration
	// RefreshTTL es la vigencia del Refresh Token (default 7 días).
	RefreshTTL time.Duration
	// HTTPAddr es la dirección de escucha del servidor HTTP (default :8080).
	HTTPAddr string
	// SpotifyClientID / SpotifyClientSecret son las credenciales del flujo
	// Client Credentials. Opcionales: sin ellas la búsqueda es solo local.
	SpotifyClientID     string
	SpotifyClientSecret string
	// FreqBlogAPIKey es la clave de FreqBlog para obtener audio features.
	// Opcional: sin ella no arranca el worker de embeddings.
	FreqBlogAPIKey string
	// SearchFallbackThreshold es el puntaje mínimo del mejor resultado
	// local para no consultar Spotify, en (0, 1] (default 0.8).
	SearchFallbackThreshold float64
}

// SpotifyEnabled indica si hay credenciales de Spotify.
func (c *Config) SpotifyEnabled() bool {
	return c.SpotifyClientID != "" && c.SpotifyClientSecret != ""
}

// Load lee la configuración del entorno. Si envFile no está vacío, carga
// ese archivo .env primero (sin pisar variables ya presentes en el
// entorno, que tienen prioridad). Luego valida los valores requeridos.
func Load(envFile string) (*Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("config: cargar %s: %w", envFile, err)
		}
	}

	cfg := &Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		HTTPAddr:    envOr("HTTP_ADDR", ":8080"),

		SpotifyClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		SpotifyClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		FreqBlogAPIKey:      os.Getenv("FREQBLOG_API_KEY"),
	}

	accessTTL, err := durationOr("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	cfg.AccessTTL = accessTTL

	refreshTTL, err := durationOr("REFRESH_TOKEN_TTL", 168*time.Hour)
	if err != nil {
		return nil, err
	}
	cfg.RefreshTTL = refreshTTL

	threshold, err := floatOr("SEARCH_FALLBACK_THRESHOLD", 0.8)
	if err != nil {
		return nil, err
	}
	cfg.SearchFallbackThreshold = threshold

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL es requerida")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("config: JWT_SECRET es requerido y debe tener al menos 32 caracteres (actual: %d)", len(c.JWTSecret))
	}
	if c.AccessTTL <= 0 {
		return fmt.Errorf("config: ACCESS_TOKEN_TTL debe ser positivo")
	}
	if c.RefreshTTL <= 0 {
		return fmt.Errorf("config: REFRESH_TOKEN_TTL debe ser positivo")
	}
	if (c.SpotifyClientID == "") != (c.SpotifyClientSecret == "") {
		return fmt.Errorf("config: SPOTIFY_CLIENT_ID y SPOTIFY_CLIENT_SECRET deben definirse juntos")
	}
	if c.SearchFallbackThreshold <= 0 || c.SearchFallbackThreshold > 1 {
		return fmt.Errorf("config: SEARCH_FALLBACK_THRESHOLD debe estar en (0, 1]")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// durationOr parsea una duración Go (ej. "15m", "168h") o devuelve el
// fallback si la variable no está definida.
func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s inválida (%q): %w", key, v, err)
	}
	return d, nil
}

// floatOr parsea un número decimal o devuelve el fallback si la variable no
// está definida.
func floatOr(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s inválida (%q): %w", key, v, err)
	}
	return f, nil
}
