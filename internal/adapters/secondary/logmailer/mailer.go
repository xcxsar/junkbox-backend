// Package logmailer implementa ports.Mailer escribiendo los correos en el
// log. Es el driver por defecto en desarrollo: no envía nada, pero permite
// copiar los enlaces de recuperación y verificación desde la consola.
package logmailer

import (
	"context"
	"log"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
)

// Mailer escribe cada correo (en texto plano) en el log estándar.
type Mailer struct{}

var _ ports.Mailer = Mailer{}

// Send registra el correo y nunca falla.
func (Mailer) Send(_ context.Context, email domain.Email) error {
	log.Printf("correo (MAIL_DRIVER=log) para %s — %s\n%s", email.To, email.Subject, email.Text)
	return nil
}
