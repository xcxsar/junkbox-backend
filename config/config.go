// Package config carga y valida la configuración del backend desde
// variables de entorno, con soporte opcional de un archivo .env.
package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	// SearchEnrichBudget es cuánto espera la búsqueda a Spotify para
	// completar imágenes de artistas y portadas que faltan (default 2s). Lo
	// que no termina a tiempo lo completa un worker en segundo plano.
	SearchEnrichBudget time.Duration
	// CORSAllowedOrigins son los orígenes (esquema://host[:puerto]) del
	// frontend que pueden llamar a la API desde el navegador
	// (default http://localhost:5173).
	CORSAllowedOrigins []string
	// TermsVersion es la versión vigente de los Términos de Servicio y la
	// Política de Privacidad; se guarda como aceptada en cada registro
	// (default "2026-11-12"). Actualizarla al publicar documentos nuevos.
	TermsVersion string

	// FrontendURL es el origen del frontend al que apuntan los enlaces de
	// los correos (default: el primer origen de CORS_ALLOWED_ORIGINS).
	FrontendURL string
	// PasswordResetTTL es la vigencia del enlace de recuperación (default 30m).
	PasswordResetTTL time.Duration
	// EmailVerificationTTL es la vigencia del enlace de verificación (default 24h).
	EmailVerificationTTL time.Duration
	// MailDriver elige cómo se envían los correos: "log" (los escribe en la
	// consola; default) o "smtp".
	MailDriver string
	// MailFrom es el remitente, p. ej. "JunkBox <no-reply@tu-dominio>".
	// Obligatorio con MAIL_DRIVER=smtp.
	MailFrom string
	// SMTP* configuran el driver smtp (Resend: smtp.resend.com, 587,
	// usuario "resend", contraseña = API key; Mailpit: localhost, 1025,
	// sin usuario, SMTP_TLS=none).
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	// SMTPTLS es "starttls" (default), "tls" (implícito, 465) o "none".
	SMTPTLS string
}

// Drivers de correo admitidos en MAIL_DRIVER.
const (
	MailDriverLog  = "log"
	MailDriverSMTP = "smtp"
)

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

		CORSAllowedOrigins: listOr("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		TermsVersion:       strings.TrimSpace(envOr("TERMS_VERSION", "2026-11-12")),

		FrontendURL:  os.Getenv("FRONTEND_URL"),
		MailDriver:   envOr("MAIL_DRIVER", MailDriverLog),
		MailFrom:     os.Getenv("MAIL_FROM"),
		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPTLS:      envOr("SMTP_TLS", "starttls"),
	}
	if cfg.FrontendURL == "" && len(cfg.CORSAllowedOrigins) > 0 {
		cfg.FrontendURL = cfg.CORSAllowedOrigins[0]
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

	enrichBudget, err := durationOr("SEARCH_ENRICH_BUDGET", 2*time.Second)
	if err != nil {
		return nil, err
	}
	cfg.SearchEnrichBudget = enrichBudget

	resetTTL, err := durationOr("PASSWORD_RESET_TTL", 30*time.Minute)
	if err != nil {
		return nil, err
	}
	cfg.PasswordResetTTL = resetTTL

	verifyTTL, err := durationOr("EMAIL_VERIFICATION_TTL", 24*time.Hour)
	if err != nil {
		return nil, err
	}
	cfg.EmailVerificationTTL = verifyTTL

	smtpPort, err := intOr("SMTP_PORT", 587)
	if err != nil {
		return nil, err
	}
	cfg.SMTPPort = smtpPort

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
	if c.SearchEnrichBudget <= 0 {
		return fmt.Errorf("config: SEARCH_ENRICH_BUDGET debe ser positivo")
	}
	// user_consents.version es VARCHAR(50).
	if c.TermsVersion == "" || len(c.TermsVersion) > 50 {
		return fmt.Errorf("config: TERMS_VERSION debe tener entre 1 y 50 caracteres")
	}
	// Una lista vacía o un comodín abrirían la API a cualquier origen.
	if len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("config: CORS_ALLOWED_ORIGINS no puede estar vacía")
	}
	for _, origin := range c.CORSAllowedOrigins {
		if !validOrigin(origin) {
			return fmt.Errorf("config: CORS_ALLOWED_ORIGINS contiene un origen inválido (%q): se espera http(s)://host[:puerto], sin ruta ni comodines", origin)
		}
	}
	return c.validateMail()
}

// validateMail comprueba la configuración de los correos de cuenta.
func (c *Config) validateMail() error {
	if !validOrigin(c.FrontendURL) {
		return fmt.Errorf("config: FRONTEND_URL inválida (%q): se espera http(s)://host[:puerto], sin ruta", c.FrontendURL)
	}
	if c.PasswordResetTTL <= 0 || c.EmailVerificationTTL <= 0 {
		return fmt.Errorf("config: PASSWORD_RESET_TTL y EMAIL_VERIFICATION_TTL deben ser positivos")
	}
	switch c.MailDriver {
	case MailDriverLog:
		return nil
	case MailDriverSMTP:
	default:
		return fmt.Errorf("config: MAIL_DRIVER debe ser %q o %q (actual: %q)", MailDriverLog, MailDriverSMTP, c.MailDriver)
	}
	if c.MailFrom == "" || c.SMTPHost == "" {
		return fmt.Errorf("config: MAIL_DRIVER=smtp requiere MAIL_FROM y SMTP_HOST")
	}
	if _, err := mail.ParseAddress(c.MailFrom); err != nil {
		return fmt.Errorf("config: MAIL_FROM inválido (%q): %w", c.MailFrom, err)
	}
	if c.SMTPPort <= 0 || c.SMTPPort > 65535 {
		return fmt.Errorf("config: SMTP_PORT inválido (%d)", c.SMTPPort)
	}
	switch c.SMTPTLS {
	case "starttls", "tls", "none":
	default:
		return fmt.Errorf("config: SMTP_TLS debe ser starttls, tls o none (actual: %q)", c.SMTPTLS)
	}
	return nil
}

// validOrigin acepta un origen exacto http(s)://host[:puerto], sin ruta,
// query ni comodines.
func validOrigin(origin string) bool {
	if strings.Contains(origin, "*") {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
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

// intOr parsea un entero o devuelve el fallback si la variable no está
// definida.
func intOr(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s inválida (%q): %w", key, v, err)
	}
	return n, nil
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

// listOr parsea una lista separada por comas (descartando elementos vacíos)
// o devuelve el fallback si la variable no está definida.
func listOr(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
