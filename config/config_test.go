package config

import (
	"reflect"
	"strings"
	"testing"
)

func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
}

func TestCORSOriginsDefault(t *testing.T) {
	setRequired(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"http://localhost:5173"}; !reflect.DeepEqual(cfg.CORSAllowedOrigins, want) {
		t.Errorf("origins = %v, esperaba %v", cfg.CORSAllowedOrigins, want)
	}
}

func TestCORSOriginsLista(t *testing.T) {
	setRequired(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", " http://localhost:5173 , https://junkbox.app,")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"http://localhost:5173", "https://junkbox.app"}; !reflect.DeepEqual(cfg.CORSAllowedOrigins, want) {
		t.Errorf("origins = %v, esperaba %v", cfg.CORSAllowedOrigins, want)
	}
}

func TestCORSOriginsInvalidos(t *testing.T) {
	for _, v := range []string{"*", "https://*.junkbox.app", "localhost:5173", "http://localhost:5173/", "ftp://x.com", ","} {
		t.Run(v, func(t *testing.T) {
			setRequired(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", v)
			if _, err := Load(""); err == nil {
				t.Fatalf("esperaba error para %q", v)
			}
		})
	}
}

func TestMailDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://junkbox.app,http://localhost:5173")
	for _, k := range []string{"FRONTEND_URL", "MAIL_DRIVER", "SMTP_PORT", "SMTP_TLS", "PASSWORD_RESET_TTL", "EMAIL_VERIFICATION_TTL"} {
		t.Setenv(k, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FrontendURL != "https://junkbox.app" {
		t.Errorf("FRONTEND_URL debe caer al primer origen de CORS, obtuve %q", cfg.FrontendURL)
	}
	if cfg.MailDriver != MailDriverLog || cfg.SMTPPort != 587 || cfg.SMTPTLS != "starttls" {
		t.Errorf("defaults de correo inesperados: %+v", cfg)
	}
	if cfg.PasswordResetTTL.Minutes() != 30 || cfg.EmailVerificationTTL.Hours() != 24 {
		t.Errorf("TTL por defecto inesperados: %v / %v", cfg.PasswordResetTTL, cfg.EmailVerificationTTL)
	}
}

func TestMailSMTPValido(t *testing.T) {
	setRequired(t)
	t.Setenv("MAIL_DRIVER", "smtp")
	t.Setenv("MAIL_FROM", "JunkBox <no-reply@junkbox.app>")
	t.Setenv("SMTP_HOST", "smtp.resend.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_TLS", "starttls")
	if _, err := Load(""); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestMailInvalido(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"driver desconocido": {"MAIL_DRIVER": "sendgrid"},
		"smtp sin remitente": {"MAIL_DRIVER": "smtp", "SMTP_HOST": "smtp.resend.com", "MAIL_FROM": ""},
		"smtp sin host":      {"MAIL_DRIVER": "smtp", "MAIL_FROM": "a@b.co", "SMTP_HOST": ""},
		"remitente inválido": {"MAIL_DRIVER": "smtp", "MAIL_FROM": "no es correo", "SMTP_HOST": "h"},
		"puerto inválido":    {"MAIL_DRIVER": "smtp", "MAIL_FROM": "a@b.co", "SMTP_HOST": "h", "SMTP_PORT": "70000"},
		"tls inválido":       {"MAIL_DRIVER": "smtp", "MAIL_FROM": "a@b.co", "SMTP_HOST": "h", "SMTP_TLS": "ssl"},
		"frontend con ruta":  {"FRONTEND_URL": "http://localhost:5173/app"},
		"ttl negativo":       {"PASSWORD_RESET_TTL": "-1m"},
	} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(""); err == nil {
				t.Fatal("esperaba error")
			}
		})
	}
}

func TestTermsVersion(t *testing.T) {
	setRequired(t)
	t.Setenv("TERMS_VERSION", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TermsVersion != "2026-11-12" {
		t.Errorf("TERMS_VERSION default = %q", cfg.TermsVersion)
	}

	t.Setenv("TERMS_VERSION", " 2027-01-01 ")
	if cfg, err = Load(""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TermsVersion != "2027-01-01" {
		t.Errorf("TERMS_VERSION = %q, esperaba 2027-01-01", cfg.TermsVersion)
	}

	for _, v := range []string{"   ", strings.Repeat("v", 51)} {
		t.Setenv("TERMS_VERSION", v)
		if _, err := Load(""); err == nil {
			t.Errorf("esperaba error para TERMS_VERSION=%q", v)
		}
	}
}
