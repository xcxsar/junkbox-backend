package config

import (
	"reflect"
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
