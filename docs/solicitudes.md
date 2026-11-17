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

---

### HOME-02 · Carátula en el resumen de canción

- **Estado:** Resuelta · **Fecha:** 2026-11-16
- **Responsable:** Backend

#### Qué se pidió

`MusicCard` y `ReviewCard` muestran carátula, pero se reportó que `TrackSummary`/`TrackDetail` no traían imagen y que las canciones del CSV no tenían álbum de Spotify vinculado (las 8 de la plantilla tenían `album_id: null`, comprobado 2026-11-06). Se pidió añadir `image_url` (la carátula del álbum o sencillo) a `Track` y `TrackDetail`, también en `/music/search?type=track`, y enlazar las canciones del dataset con su álbum.

#### Decisión

La mayor parte ya estaba resuelta desde el 2026-11-12 (posterior a la comprobación del 2026-11-06):

- `Track` trae **`album_image_url`** (carátula) y `artist_image_url` (foto del artista) en `/music/search?type=track`, en las pistas de `/music/albums/{id}` y en `/home` (HOME-01).
- La búsqueda y `/home` enlazan en el momento las canciones del CSV con su álbum de Spotify (`album_id` y `album_title` quedan guardados) y piden las imágenes que faltan antes de responder.

Lo que faltaba era **`GET /music/tracks/{id}`**: solo leía la base de datos, así que una canción del CSV que nadie había buscado salía sin carátula ni álbum. Ahora el detalle completa la carátula y la foto igual que la búsqueda, y devuelve `album_id`, `album_title` y `album_image_url` ya enlazados.

Además, para que `ReviewCard` no tenga que pedir el detalle de cada canción, **las reseñas traen el resumen de su objetivo** en `target.summary`:

- En `POST /reviews`, `GET /reviews`, `GET /reviews/{id}` y `PUT /reviews/{id}`, en cada reseña. En `GET /reviews` también en `target` de la página; como todas las reseñas de la página son del mismo objetivo, se calcula una sola vez.
- Para los tres tipos de objetivo, con los mismos objetos que ya conoce el frontend: canción → `TrackSummary` (con `album_image_url` y `artist_image_url`); álbum → `AlbumSummary` sin `tracks` ni `ratings`; artista → `Artist` sin `ratings`.
- Las imágenes que faltan se completan con Spotify igual que en el detalle. `GET /music/artists/{id}` también completa ahora la foto del artista.
- Es aditivo: `target.type` y `target.id` no cambian, y las calificaciones no traen `summary`.

Otras decisiones:

- **Se mantiene el nombre `album_image_url`** y no se agrega `image_url`: sería un campo duplicado, y en una canción `image_url` es ambiguo junto a `artist_image_url`.
- **No hay enlace en lote** de las 1.16M canciones: Spotify (Development Mode) ya no tiene endpoints por lotes, así que serían 1.16M llamadas. Cada canción se enlaza la primera vez que se muestra (búsqueda, `/home` o detalle) y queda guardada.
- Calificar o reseñar no llama a Spotify; solo leer el detalle.

#### Contrato de la API

Sin campos nuevos. `GET /api/v1/music/tracks/{id}`:

```json
{
  "id": 1,
  "track_name": "I Won't Give Up",
  "artist_name": "Jason Mraz",
  "album_id": "1847347e-5054-48c1-ba3b-b83eb60becb6",
  "album_title": "Love Is a Four Letter Word (Deluxe Edition)",
  "album_image_url": "https://i.scdn.co/image/ab67616d0000b27346e379c721504777a62bd9b8",
  "artist_image_url": "https://i.scdn.co/image/ab6761610000e5ebead36c5fd05aae7d7f3463c6",
  "…": "resto de campos sin cambios"
}
```

| Caso | `album_image_url` |
| --- | --- |
| Canción ya enlazada | URL de la carátula (sin llamar a Spotify) |
| Canción del CSV no enlazada, Spotify responde dentro del presupuesto (2 s) | URL de la carátula; `album_id` y `album_title` también llegan |
| Spotify no responde a tiempo | `null` en esta respuesta; el worker la enlaza y la siguiente lectura ya la trae |
| La canción no existe en Spotify o no tiene álbum con imagen | `null` |

Reseñas (`POST`/`GET`/`PUT /api/v1/reviews…`):

```json
{
  "id": "a65bd2e0-f5ce-4e39-8bf4-66b38bdb0bfa",
  "target": {
    "type": "song",
    "id": "3",
    "summary": {
      "id": 3,
      "spotify_id": "7BRCa8MPiyuvr2VU3O9W0F",
      "track_name": "Do Not Let Me Go",
      "artist_name": "Joshua Hyslop",
      "album_title": "Where The Mountain Meets The Valley",
      "album_image_url": "https://i.scdn.co/image/ab67616d0000b273e6655a69a59d319e2d3b00b4",
      "artist_image_url": "https://i.scdn.co/image/ab6761610000e5ebfc8d3ec0f5fb1e2e6cda01aa",
      "year": 2012,
      "genre": "acoustic",
      "…": "resto de campos de TrackSummary"
    }
  },
  "content": "…",
  "…": "resto de campos de la reseña sin cambios"
}
```

| `target.type` | `target.summary` |
| --- | --- |
| `song` | `TrackSummary` (como en `/music/search?type=track`) |
| `album` | Álbum como en `/music/albums/{id}`, sin `tracks` ni `ratings` (`image_url` es la carátula) |
| `artist` | Artista como en `/music/artists/{id}`, sin `ratings` |
| objetivo borrado | `summary` ausente |

#### Qué tiene que hacer el frontend

1. Leer la carátula de `album_image_url` (no `image_url`) en `Track` y `TrackDetail`, incluidos los resultados del Buscador.
2. Quitar los álbumes fijados a mano en la plantilla de Inicio y sus llamadas a `/music/albums/{id}` o `/music/search?type=album`: Inicio ya usa `GET /home` (HOME-01), que trae la carátula.
3. Mantener el recuadro neutro cuando `album_image_url` sea `null`.
4. En `ReviewCard`, leer título, artista y carátula de `review.target.summary` y quitar la petición a `/music/tracks/{id}` (o a álbum/artista) por cada reseña.

#### Pendiente / fuera de alcance

- Enlace en lote de todo el dataset (ver arriba).

#### Verificación

- `go test ./...`: casos nuevos en `music_service_test.go` (`TestGetTrackEnlazaCancionDelCSVConSuAlbum`, `TestGetTrackYaEnlazadaNoConsultaSpotify`, `TestGetTrackSinSpotifyRespondeLoDelCatalogo`, `TestGetTrackRespetaElPresupuestoYDelegaAlWorker`, `TestResolveTargetNoConsultaSpotify`, `TestSummarizeTarget*`, `TestGetArtistYaEnriquecidoNoConsultaSpotify`) y en `review_service_test.go` (`TestReviewTraeElResumenDeSuObjetivo`, `TestListReviewsResumeElObjetivoUnaVez`, `TestReviewObjetivoInexistenteSaleSinResumen`, `TestReviewErrorAlResumirSePropaga`).
- Prueba manual contra el servidor local: `GET /music/tracks/1` (canción del CSV sin álbum) respondió con carátula, `album_id` y `album_title` en ≈0,8 s; la segunda lectura, en ≈40 ms sin llamar a Spotify. Calificar la canción `3` (sin álbum) no la enlazó. Reseñar después esa canción, un álbum y un artista sin foto devolvió los tres resúmenes con imagen (la canción quedó enlazada y la foto del artista guardada); `GET /reviews` de la canción respondió en ≈35 ms con el resumen en la página y en la reseña. Las respuestas de calificaciones no cambiaron. Las calificaciones y reseñas de prueba se borraron.

---

### HOME-03 · Detalle de varias canciones en una llamada

- **Estado:** No necesaria (cerrada) · **Fecha:** 2026-11-16
- **Responsable:** Backend

#### Qué se pidió

La plantilla de Inicio hacía una petición `GET /music/tracks/{id}` por canción (unas 12). Se pidió `GET /api/v1/music/tracks?ids=<id1>,<id2>,…` (hasta unos 50 IDs, locales o de Spotify) → `200 { "results": [TrackSummary…] }`, omitiendo los IDs que no existan y con `400` si la lista está vacía o es demasiado larga. La propia solicitud indicaba que dejaba de importar si HOME-01 devolvía los datos completos.

#### Decisión

No se implementa: después de HOME-01 y HOME-02 ya no hay ninguna pantalla que tenga que resolver una lista de IDs de canciones.

- **Inicio:** `GET /api/v1/home` (HOME-01) devuelve en una sola llamada `sections[0].items`. Cada elemento es `type` más **el mismo objeto `TrackSummary`** que devuelven la búsqueda y el detalle, con `album_image_url` y `artist_image_url` ya completados. La plantilla de 8 IDs, sus `GET /music/tracks/{id}` y las llamadas de álbum de la alternativa de HOME-02 dejan de hacer falta. Eso cubre las ~12 peticiones.
- **Reseñas:** desde HOME-02, cada reseña trae `target.summary`, así que `ReviewCard` tampoco tiene que pedir canciones.
- **Resto de la API:** la búsqueda, el detalle de álbum (con sus pistas), `/home` y las reseñas ya devuelven los objetos completos. Ningún endpoint devuelve solo IDs de canciones.
- **Costo:** cada ID sin carátula puede requerir enlazar la canción con Spotify (una llamada por ID, porque Development Mode ya no tiene endpoints por lotes). Con 50 IDs serían hasta 50–100 llamadas dentro del presupuesto de 2 s, o habría que responder sin imágenes. Sin un caso de uso real, no compensa.

#### Qué tiene que hacer el frontend

1. En Inicio, usar `GET /api/v1/home` (HOME-01) y quitar las peticiones en paralelo a `/music/tracks/{id}` de la plantilla.
2. En `ReviewCard`, usar `review.target.summary` (HOME-02).
3. La renovación única y compartida del token sigue siendo buena práctica, pero ya no se dispara por estas ráfagas de peticiones.

#### Cuándo retomarla

Si aparece una pantalla que guarde listas de canciones por ID (p. ej. canciones guardadas o listas del usuario), es preferible que ese endpoint devuelva los resúmenes embebidos, como las reseñas. Si aun así hiciera falta el endpoint genérico, el contrato propuesto sirve tal cual (`ids` separados por coma, máximo 50, IDs inexistentes omitidos, `400` si la lista está vacía o es demasiado larga). Las imágenes deberían salir de la base de datos y, como mucho, completarse con el mismo presupuesto acotado.

#### Verificación

- Revisado en el código: los elementos de `/home` se construyen con `domain.Track` (`internal/core/services/recommendation_service.go`), el mismo tipo de `/music/search?type=track`. Las reseñas incluyen `target.summary` (`internal/core/services/review_service.go`).
- Sin cambios de código.
