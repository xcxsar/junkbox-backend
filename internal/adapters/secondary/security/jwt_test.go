package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestGenerateAndParseAccess(t *testing.T) {
	m := NewJWTManager(testSecret, 15*time.Minute)
	userID := uuid.New()

	token, err := m.GenerateAccess(userID, "cesar_beltran")
	if err != nil {
		t.Fatalf("GenerateAccess: %v", err)
	}

	parsed, err := m.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if parsed.UserID != userID || parsed.Username != "cesar_beltran" {
		t.Errorf("claims roundtrip mismatch: %+v", parsed)
	}
	if _, err := uuid.Parse(parsed.JTI); err != nil {
		t.Errorf("jti debe ser un UUID: %q", parsed.JTI)
	}
	if got := parsed.ExpiresAt.Sub(parsed.IssuedAt); got != 15*time.Minute {
		t.Errorf("exp - iat = %v, esperaba 15m", got)
	}
}

func TestGenerateAccessJTIUnico(t *testing.T) {
	m := NewJWTManager(testSecret, time.Minute)
	a, _ := m.GenerateAccess(uuid.New(), "a")
	b, _ := m.GenerateAccess(uuid.New(), "a")
	pa, _ := m.ParseAccess(a)
	pb, _ := m.ParseAccess(b)
	if pa.JTI == pb.JTI {
		t.Error("cada token debe tener un jti distinto")
	}
}

func TestParseAccessExpirado(t *testing.T) {
	m := NewJWTManager(testSecret, -time.Minute) // TTL negativo: token ya vencido
	token, err := m.GenerateAccess(uuid.New(), "a")
	if err != nil {
		t.Fatalf("GenerateAccess: %v", err)
	}
	if _, err := m.ParseAccess(token); !errors.Is(err, ErrExpiredToken) {
		t.Errorf("esperaba ErrExpiredToken, obtuve %v", err)
	}
}

func TestParseAccessSecretoDistinto(t *testing.T) {
	m := NewJWTManager(testSecret, time.Minute)
	other := NewJWTManager(strings.Repeat("f", 32), time.Minute)

	token, err := m.GenerateAccess(uuid.New(), "a")
	if err != nil {
		t.Fatalf("GenerateAccess: %v", err)
	}
	if _, err := other.ParseAccess(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("esperaba ErrInvalidToken con otro secreto, obtuve %v", err)
	}
}

func TestParseAccessTokenBasura(t *testing.T) {
	m := NewJWTManager(testSecret, time.Minute)
	// incluye un JWT alg=none sin firma, que debe rechazarse.
	for _, tok := range []string{"", "abc", "eyJhbGciOiJub25lIn0.e30."} {
		if _, err := m.ParseAccess(tok); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: esperaba ErrInvalidToken, obtuve %v", tok, err)
		}
	}
}

func TestHasher(t *testing.T) {
	h := NewHasher(4) // coste mínimo: el test no mide seguridad
	hash, err := h.Hash([]byte("Password123!"))
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h.Compare(hash, []byte("Password123!")) != nil {
		t.Error("Compare debe aceptar la contraseña correcta")
	}
	if h.Compare(hash, []byte("otra")) == nil {
		t.Error("Compare debe rechazar una contraseña incorrecta")
	}
}

func TestSeedHashCorrespondeAPassword123(t *testing.T) {
	// Hash usado en db/seed/20261027000001_users.sql.
	const seedHash = "$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe"
	if err := NewHasher(0).Compare(seedHash, []byte("Password123!")); err != nil {
		t.Errorf("el hash del seed no corresponde a Password123!: %v", err)
	}
}

func TestParseAccessAllowExpiredAceptaVencido(t *testing.T) {
	m := NewJWTManager(testSecret, -time.Minute)
	userID := uuid.New()
	token, err := m.GenerateAccess(userID, "a")
	if err != nil {
		t.Fatalf("GenerateAccess: %v", err)
	}
	parsed, err := m.ParseAccessAllowExpired(token)
	if err != nil {
		t.Fatalf("ParseAccessAllowExpired: %v", err)
	}
	if parsed.UserID != userID || parsed.JTI == "" {
		t.Errorf("claims inesperados: %+v", parsed)
	}
}

func TestParseAccessAllowExpiredRechazaFirmaInvalida(t *testing.T) {
	m := NewJWTManager(testSecret, -time.Minute)
	other := NewJWTManager(strings.Repeat("f", 32), time.Minute)
	token, _ := m.GenerateAccess(uuid.New(), "a")
	if _, err := other.ParseAccessAllowExpired(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("esperaba ErrInvalidToken con otro secreto, obtuve %v", err)
	}
	for _, tok := range []string{"", "abc", "eyJhbGciOiJub25lIn0.e30."} {
		if _, err := m.ParseAccessAllowExpired(tok); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: esperaba ErrInvalidToken, obtuve %v", tok, err)
		}
	}
}
