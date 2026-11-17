package services

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

// Paginación por cursor (feeds, comentarios y seguidores).
const (
	DefaultPageLimit = 20
	MaxPageLimit     = 50
)

// keysetCursor es la última fila entregada de un listado keyset. Scope
// identifica el listado ("feed:popular", "comments:<reseña>"…) para que un
// cursor no se use en otro. TZ, Day y Score solo los usa el feed "popular".
type keysetCursor struct {
	Scope     string    `json:"s"`
	TZ        string    `json:"z,omitempty"`
	Day       string    `json:"d,omitempty"`
	Score     int32     `json:"p,omitempty"`
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

// encode serializa el cursor como JSON en base64url (opaco para el cliente).
func (c keysetCursor) encode() *string {
	b, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

// decodeCursor interpreta el cursor del cliente. Vacío devuelve nil (primera
// página). Un cursor mal formado o de otro listado es un 400.
func decodeCursor(raw, scope string) (*keysetCursor, error) {
	if raw == "" {
		return nil, nil
	}
	invalid := domain.NewValidationError("cursor inválido")
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	var c keysetCursor
	if err := json.Unmarshal(b, &c); err != nil || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return nil, invalid
	}
	if c.Scope != scope {
		return nil, domain.NewValidationError("el cursor pertenece a otro listado")
	}
	return &c, nil
}

// pageLimit valida limit (0 usa DefaultPageLimit).
func pageLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultPageLimit, nil
	}
	if limit < 1 || limit > MaxPageLimit {
		return 0, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxPageLimit))
	}
	return limit, nil
}

// args devuelve id y fecha del cursor para las queries (NULL si no
// hay cursor).
func (c *keysetCursor) args() (*uuid.UUID, sql.NullTime) {
	if c == nil {
		return nil, sql.NullTime{}
	}
	return &c.ID, sql.NullTime{Time: c.CreatedAt, Valid: true}
}
