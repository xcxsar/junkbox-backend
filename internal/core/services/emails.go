package services

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

//go:embed templates/*.txt templates/*.html
var emailTemplatesFS embed.FS

// emailTemplate es un correo con versión en texto plano y HTML. Las
// plantillas HTML se componen con templates/layout.html.
type emailTemplate struct {
	subject string
	text    *texttemplate.Template
	html    *htmltemplate.Template
}

func mustEmailTemplate(name, subject string) emailTemplate {
	return emailTemplate{
		subject: subject,
		text:    texttemplate.Must(texttemplate.ParseFS(emailTemplatesFS, "templates/"+name+".txt")),
		html:    htmltemplate.Must(htmltemplate.ParseFS(emailTemplatesFS, "templates/layout.html", "templates/"+name+".html")),
	}
}

var (
	passwordResetEmail     = mustEmailTemplate("password_reset", "Restablece tu contraseña de JunkBox")
	passwordChangedEmail   = mustEmailTemplate("password_changed", "Tu contraseña de JunkBox se cambió")
	emailVerificationEmail = mustEmailTemplate("email_verification", "Confirma tu correo de JunkBox")
)

// emailData son las variables disponibles en las plantillas.
type emailData struct {
	Subject string
	Name    string
	Link    string
	TTL     string
}

// render construye el correo para el usuario dado.
func (t emailTemplate) render(user sqlc.User, link string, ttl time.Duration) (domain.Email, error) {
	data := emailData{Subject: t.subject, Name: greetingName(user), Link: link, TTL: humanDuration(ttl)}
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return domain.Email{}, fmt.Errorf("correo: plantilla de texto: %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return domain.Email{}, fmt.Errorf("correo: plantilla HTML: %w", err)
	}
	return domain.Email{To: user.Email, Subject: t.subject, Text: text.String(), HTML: html.String()}, nil
}

// greetingName usa el nombre visible si existe y si no, el username.
func greetingName(u sqlc.User) string {
	if u.DisplayName.Valid && u.DisplayName.String != "" {
		return u.DisplayName.String
	}
	return u.Username
}

// humanDuration expresa una vigencia en español ("30 minutos", "24 horas").
func humanDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return plural(int(d/time.Hour), "hora", "horas")
	}
	return plural(int(d.Round(time.Minute)/time.Minute), "minuto", "minutos")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
