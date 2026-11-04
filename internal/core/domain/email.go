package domain

// Email es un correo listo para enviar, con versión en texto plano y HTML.
type Email struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// EmailTokenPurpose distingue para qué sirve un token enviado por correo; un
// token de un propósito nunca es válido para otro.
type EmailTokenPurpose string

const (
	PurposePasswordReset     EmailTokenPurpose = "password_reset"
	PurposeEmailVerification EmailTokenPurpose = "email_verification"
)
