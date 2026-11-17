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

---

### AUTH-06 · El registro acepta correos con dominio sin punto

- **Estado:** Resuelta · **Fecha:** 2026-11-12
- **Responsable:** Backend

#### Qué se pidió

`validateRegister` (`internal/core/services/auth_service.go`) validaba el correo solo con `net/mail.ParseAddress`, que acepta `usuario@dominio` sin punto. Así se podían crear cuentas con correos inexistentes (p. ej. `agsdafsafsdfs@fdsafsadfas`). Se pidió exigir que el dominio tenga al menos un punto y un TLD de 2 o más letras, y responder `400 {"error": "email inválido"}` si no, o confirmar por escrito que se seguirían aceptando.

#### Decisión

Se implementó en el backend: ya no se aceptan estos correos. La validación del frontend (`EMAIL_PATTERN`) no basta porque la API se puede llamar directamente.

Regla nueva (además de `ParseAddress` y el máximo de 255 caracteres):

- El dominio debe tener al menos un punto.
- El último segmento (TLD) debe tener 2 o más letras, o ser un TLD internacional en punycode (`xn--…`), para no rechazar dominios reales con caracteres no latinos.
- Se rechazan los literales IP (`usuario@[127.0.0.1]`) y los TLD numéricos.

#### Contrato de la API

`POST /api/v1/auth/register`: si el correo no cumple la regla responde `400 {"error": "email inválido"}`, el mismo mensaje que ya existía para los correos mal formados.

| Correo | Resultado |
| --- | --- |
| `agsdafsafsdfs@fdsafsadfas` | `400` (dominio sin punto) |
| `a@b.c` | `400` (TLD de 1 letra) |
| `a@b.123` | `400` (TLD numérico) |
| `a@[127.0.0.1]` | `400` (literal IP) |
| `x@junkbox.local` | aceptado |
| `a@sub.dominio.mx` | aceptado |
| `a@xn--80ak6aa92e.xn--p1ai` | aceptado (punycode) |

#### Impacto en el frontend

Ninguno funcional: `EMAIL_PATTERN` en `auth.validation.ts` ya exige `usuario@dominio.tld` y es coherente con la regla del backend. El cambio solo afecta al registro: login y recuperación de contraseña no validan el formato, así que una cuenta ya creada con un correo así podría seguir entrando.

#### Pendiente / fuera de alcance

- Cuentas existentes: en la base de datos local hay **0** cuentas que incumplan la regla. Para revisar otros entornos:
  ```sql
  SELECT id, username, email FROM users
  WHERE email !~ '@[^@]+\.([a-z]{2,63}|xn--[a-z0-9-]+)$';
  ```
- No se comprueba que el dominio exista (registros MX). La verificación por correo sigue siendo la que confirma que el buzón es real.

#### Verificación

- `go test ./...`: casos nuevos en `TestRegisterValidaciones` (sin punto, TLD de 1 letra, TLD numérico, literal IP) y `TestRegisterEmailsValidos` (dominio `.local` del seed, subdominio y punycode).
- Prueba manual contra el servidor local: `agsdafsafsdfs@fdsafsadfas` → `400 {"error":"email inválido"}`; `…@dominio.mx` → `201` (el usuario de prueba se borró).

---

### HOME-01 · Endpoint de recomendaciones / feed de Inicio

- **Estado:** Resuelta · **Fecha:** 2026-11-16
- **Responsable:** Backend

#### Qué se pidió

La sección "Hecho para tus oídos" (`MusicCard`, nodo `14244:50349`) necesitaba una lista de canciones o álbumes recomendados, pero no existía ningún endpoint. El frontend usaba una plantilla fija de 8 IDs del dataset (`features/home/data/homeTemplate.ts`) con un `GET /music/tracks/{id}` por cada ID, así que todos los usuarios veían lo mismo. Se propuso `GET /api/v1/home` con Bearer y respuesta `{ "sections": [ { "id", "title", "items": [ { "type": "track" | "album", … } ] } ] }`.

#### Decisión

Se implementó `GET /api/v1/home` con el contrato propuesto. Por ahora la sección usa **solo** la función `recommend_songs` de `../recommendation-engine` (similitud coseno con pgvector sobre el embedding de 10 dimensiones):

- **Semilla:** la última canción que el usuario **calificó o reseñó** (la más reciente por `updated_at`, así que editar una reseña o cambiar un voto también cuenta). Las calificaciones y reseñas de álbumes o artistas no cuentan. Si esa canción aún no tiene embedding (p. ej. recién importada de Spotify), se usa la anterior que sí lo tenga.
- **Género:** si la semilla tiene género en el catálogo (`song_metadata.genre`, el del CSV), se recomiendan canciones de ese género. Si no lo tiene (canciones importadas de Spotify), se usa la variante sin género. No se usan los géneros del artista en Spotify porque siguen otra taxonomía (`pop rap`, `reggaeton colombiano`) que casi nunca coincide con la del catálogo.
- **Filtros:** no se recomiendan la propia semilla, sus duplicados (mismo título y artista con otro `track_id`; hay ~7.600 en el catálogo) ni las canciones que el usuario ya calificó o reseñó.
- **Respaldo:** sin semilla (usuario nuevo o sin actividad en canciones) se responden las canciones más populares (`popularity` de Spotify), sin las que ya calificó o reseñó y sin repetir título y artista. Si el filtro de género deja la lista vacía, se repite sin género antes de caer en populares.
- **Imágenes:** igual que la búsqueda, las portadas y fotos de artista que faltan se piden a Spotify antes de responder (con presupuesto de tiempo); lo que no alcanza sale en `null` y lo completa el worker de respaldo.
- **Campos extra:** cada sección trae `strategy` (`similar_to_last` o `popular`) y `seed` (la canción semilla, `null` en populares), para que el frontend pueda mostrar "Porque calificaste X". Son aditivos: no rompen el contrato propuesto.
- Por ahora solo hay elementos de tipo `track`.

#### Contrato de la API

`GET /api/v1/home?limit=8` (Bearer). `limit` es opcional, de 1 a 50, default 8.

```json
{
  "sections": [
    {
      "id": "made_for_you",
      "title": "Hecho para tus oídos",
      "strategy": "similar_to_last",
      "seed": {
        "id": 1083842,
        "spotify_id": "5M9kio8FEFiNKyTDGqksy2",
        "track_name": "The Bomb Dot Com V2.0",
        "artist_name": "Sleeping With Sirens",
        "genre": "emo"
      },
      "items": [
        {
          "type": "track",
          "id": 807823,
          "spotify_id": "44jzGaVQytNHTFAqI2ANPg",
          "track_name": "Just Tonight - Demo Version",
          "artist_name": "Jimmy Eat World",
          "album_title": "Futures (Deluxe Edition)",
          "album_image_url": "https://i.scdn.co/image/ab67616d0000b273f7d76f56dcec489d92bcfffc",
          "artist_image_url": "https://i.scdn.co/image/ab6761610000e5ebe223ff60b74bbc40b04ce46c",
          "year": 2004,
          "genre": "emo",
          "popularity": 23,
          "duration_ms": 218040,
          "has_embedding": true,
          "pending_embedding": false
        }
      ]
    }
  ]
}
```

Cada elemento lleva `type` y, al mismo nivel, los mismos campos que una canción de `GET /music/search` (`TrackSummary`).

| Caso | Respuesta |
| --- | --- |
| Usuario con una canción calificada o reseñada | `200`, `strategy: "similar_to_last"` y `seed` con esa canción |
| Usuario sin actividad en canciones | `200`, `strategy: "popular"`, `seed: null` |
| `limit` no entero, `0` o fuera de 1–50 | `400 {"error": "limit debe ser un entero"}` / `400 {"error": "limit debe estar entre 1 y 50"}` |
| Sin Bearer, token expirado o revocado | `401 {"error": "no autorizado"}` |

`items` nunca es `null` (como mucho `[]`).

#### Qué tiene que hacer el frontend

1. Cambiar `homeService.getRecommendations()` para llamar a `GET /api/v1/home` y leer `sections[0].items`.
2. Quitar la plantilla `features/home/data/homeTemplate.ts` y las 8 llamadas a `GET /music/tracks/{id}`.
3. Opcional: usar `seed` para un subtítulo ("Porque calificaste *The Bomb Dot Com V2.0*") cuando `strategy` sea `similar_to_last`.
4. Después de que el usuario califique o reseñe una canción, volver a pedir `/home` para que el feed cambie.

#### Pendiente / fuera de alcance

- Personalizar con el perfil completo del usuario (varias canciones, promedio de embeddings ponderado por su calificación) mediante procesos por lotes, según el criterio SMART §4.1.
- Elementos de tipo `album` y más secciones.
- Los embeddings de las canciones importadas desde Spotify vienen de FreqBlog y no están del todo en la misma escala que los del CSV (`implementacion.md` §4.5): con una de esas canciones como semilla las recomendaciones son menos acertadas.

#### Verificación

- `go test ./...`: `recommendation_service_test.go` (semilla con y sin género, reintento sin género, populares, error de BD, `limit`, imágenes) y `handlers_home_test.go` (401, 400 y forma del JSON).
- Prueba manual contra el servidor local: un usuario que calificó una canción `emo` recibió 8 canciones `emo` con portada y foto (≈0,95 s la primera vez por las llamadas a Spotify, ≈45 ms después). Un usuario sin actividad recibió populares. Con una canción sin género como semilla se recomendaron canciones de varios géneros. La calificación de prueba se borró.
