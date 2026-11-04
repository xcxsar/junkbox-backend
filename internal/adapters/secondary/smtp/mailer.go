// Package smtp implementa ports.Mailer sobre SMTP (Resend, Mailpit o
// cualquier otro servidor).
package smtp

import (
	"context"
	"fmt"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
)

// Modos de cifrado admitidos en Config.TLS.
const (
	TLSStartTLS = "starttls" // STARTTLS obligatorio (puerto 587, p. ej. Resend)
	TLSImplicit = "tls"      // TLS implícito (puerto 465)
	TLSNone     = "none"     // sin cifrado (solo para Mailpit en local)
)

// Config son los datos de conexión. Sin Username no se autentica.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // remitente, p. ej. "JunkBox <no-reply@tu-dominio>"
	TLS      string // TLSStartTLS, TLSImplicit o TLSNone
	Timeout  time.Duration
}

// Mailer envía cada correo abriendo una conexión SMTP. Con el volumen de
// correos de cuenta no compensa mantener una conexión abierta.
type Mailer struct {
	cfg  Config
	opts []mail.Option
}

var _ ports.Mailer = (*Mailer)(nil)

// New valida la configuración y crea el mailer.
func New(cfg Config) (*Mailer, error) {
	if cfg.Host == "" || cfg.From == "" {
		return nil, fmt.Errorf("smtp: host y remitente son obligatorios")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	opts := []mail.Option{mail.WithPort(cfg.Port), mail.WithTimeout(cfg.Timeout)}
	switch cfg.TLS {
	case TLSStartTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case TLSImplicit:
		opts = append(opts, mail.WithSSL())
	case TLSNone:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default:
		return nil, fmt.Errorf("smtp: modo TLS inválido %q", cfg.TLS)
	}
	if cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password),
		)
	}
	// Validar las opciones una vez al arrancar en lugar de en cada envío.
	if _, err := mail.NewClient(cfg.Host, opts...); err != nil {
		return nil, fmt.Errorf("smtp: configuración inválida: %w", err)
	}
	return &Mailer{cfg: cfg, opts: opts}, nil
}

// Send envía el correo con cuerpo de texto y alternativa HTML.
func (m *Mailer) Send(ctx context.Context, email domain.Email) error {
	msg := mail.NewMsg()
	if err := msg.From(m.cfg.From); err != nil {
		return fmt.Errorf("smtp: remitente: %w", err)
	}
	if err := msg.To(email.To); err != nil {
		return fmt.Errorf("smtp: destinatario: %w", err)
	}
	msg.Subject(email.Subject)
	msg.SetDate()
	msg.SetMessageID()
	msg.SetBodyString(mail.TypeTextPlain, email.Text)
	if email.HTML != "" {
		msg.AddAlternativeString(mail.TypeTextHTML, email.HTML)
	}

	client, err := mail.NewClient(m.cfg.Host, m.opts...)
	if err != nil {
		return fmt.Errorf("smtp: cliente: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("smtp: enviar: %w", err)
	}
	return nil
}
