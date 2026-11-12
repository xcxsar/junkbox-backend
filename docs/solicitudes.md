# Solicitudes del frontend

Registro de las peticiones del equipo de frontend al backend: qué se pidió, qué se decidió y cómo quedó el contrato de la API. Cada solicitud conserva su ID original.

---

### AUTH-03 · Aceptación de términos y privacidad

- **Estado:** Resuelta · **Opción elegida:** (a) · **Fecha:** 2026-11-12
- **Responsable:** Backend

#### Qué se pidió

El formulario de registro tiene la casilla "Acepto los Términos de Servicio y la Política de Privacidad", pero `RegisterRequest` no tenía ese campo y, por `DisallowUnknownFields`, enviar `terms_accepted` devolvía `400 "solicitud inválida"`. Se pidió (a) aceptar `terms_accepted` como obligatorio y, opcionalmente, guardar fecha y versión, o (b) confirmar que bastaba con validarlo en el frontend.

#### Decisión

Se implementó la **opción (a)**, guardando fecha y versión:

- Validar solo en el frontend no basta: cualquier cliente que llame directo a la API podría crear una cuenta sin aceptar.
- Guardar fecha y versión sirve como evidencia del consentimiento (LFPDPPP).
- **La versión la define el servidor** (`TERMS_VERSION`), no el cliente, para que no se pueda enviar una versión arbitraria.
- Solo se guarda fecha y versión; no se guardan IP ni User-Agent, para minimizar los datos personales.

#### Contrato de la API

`POST /api/v1/auth/register`

```json
{
  "username": "melomano",
  "email": "melomano@junkbox.local",
  "password": "Password123!",
  "display_name": "Crítico Musical",
  "avatar_url": "https://…",
  "terms_accepted": true
}
```

| Caso | Respuesta |
| --- | --- |
| `terms_accepted: true` y datos válidos | `201` con `access_token`, `refresh_token`, `expires_in` y `user` (sin cambios) |
| `terms_accepted: false` | `400 {"error": "debes aceptar los Términos de Servicio y la Política de Privacidad"}` |
| `terms_accepted` ausente | `400` con el mismo mensaje (ausente cuenta como `false`) |

La validación de términos se hace antes que las demás, así que si la casilla no está marcada, ese es el error que llega aunque otros campos también fallen.

#### Persistencia

- Migración `db/migrations/20261112000008_create_user_consents.sql`: tabla `user_consents (user_id, document, version, accepted_at)` con `document` ∈ {`terms`, `privacy`}. Se guarda una fila por documento para poder versionarlos por separado más adelante.
- El usuario y sus consentimientos se insertan en **una sola transacción** (`internal/adapters/secondary/postgres/registration_tx.go`), así que no puede quedar una cuenta sin consentimiento registrado.
- La versión sale de la variable `TERMS_VERSION` (default `"2026-11-12"`, máx. 50 caracteres), definida en `.env.template` y en `deploy/k8s/base/configmap.yaml`.
- Los usuarios del seed (cuentas internas del equipo) no tienen filas en `user_consents`.

#### Qué tiene que hacer el frontend

1. Añadir `"terms_accepted": true` al body de registro (hoy no se envía).
2. Mantener el botón "Crear cuenta" deshabilitado hasta marcar la casilla. El backend también lo valida.
3. Mostrar el mensaje del `400` si llegara (por ejemplo, desde un cliente desactualizado).
4. Cuando se publique una nueva versión de `/terminos` o `/privacidad`, avisar a backend para actualizar `TERMS_VERSION`.

#### Pendiente / fuera de alcance

- Revisión legal del texto de `/terminos` y `/privacidad` (propuesta LFPDPPP).
- Pedir que los usuarios existentes vuelvan a aceptar cuando cambie la versión (requeriría un endpoint y un flujo nuevos).

#### Verificación

- `go test ./...`: `TestRegisterSinAceptarTerminos`, versión guardada en `TestRegisterOK`, `TestRegisterExigeTerminos` (handler) y `TestTermsVersion` (config).
- Prueba manual contra Postgres: `true` → `201` y dos filas (`terms`, `privacy`, `2026-11-12`); `false` y campo ausente → `400`. `migrate-down`/`migrate-up` aplican sin errores.
