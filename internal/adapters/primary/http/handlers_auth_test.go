package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xcxsar/junkbox-backend/internal/core/services"
)

// TestRegisterExigeTerminos: terms_accepted es un campo conocido (no da
// "solicitud inválida") y false o ausente responde 400 antes de tocar la BD.
func TestRegisterExigeTerminos(t *testing.T) {
	h := http.HandlerFunc((&AuthHandler{auth: &services.AuthService{}}).Register)
	const want = "debes aceptar los Términos de Servicio y la Política de Privacidad"
	for name, body := range map[string]string{
		"false":   `{"username":"melomano","email":"m@junkbox.local","password":"Password123!","terms_accepted":false}`,
		"ausente": `{"username":"melomano","email":"m@junkbox.local","password":"Password123!"}`,
	} {
		rec := post(h, "/api/v1/auth/register", body, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, esperaba 400", name, rec.Code)
		}
		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["error"] != want {
			t.Errorf("%s: mensaje %q, esperaba %q", name, resp["error"], want)
		}
	}
}
