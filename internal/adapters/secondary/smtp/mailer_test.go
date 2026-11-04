package smtp

import "testing"

func TestNewValidaConfig(t *testing.T) {
	ok := Config{Host: "smtp.resend.com", Port: 587, Username: "resend", Password: "re_x", From: "JunkBox <no-reply@junkbox.app>", TLS: TLSStartTLS}
	if _, err := New(ok); err != nil {
		t.Fatalf("config válida rechazada: %v", err)
	}
	mailpit := Config{Host: "localhost", Port: 1025, From: "a@b.co", TLS: TLSNone}
	if _, err := New(mailpit); err != nil {
		t.Fatalf("config de Mailpit rechazada: %v", err)
	}
	for name, cfg := range map[string]Config{
		"sin host":      {Port: 587, From: "a@b.co", TLS: TLSStartTLS},
		"sin remitente": {Host: "h", Port: 587, TLS: TLSStartTLS},
		"tls inválido":  {Host: "h", Port: 587, From: "a@b.co", TLS: "ssl"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: esperaba error", name)
		}
	}
}
