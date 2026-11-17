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
- La versión sale de la variable `TERMS_VERSION` (default `"2026-11-12"`, máx. 50 caracteres), definida en `.env.template`.
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

---

### HOME-04 · Feed de reseñas "Para ti (Popular)" y "Siguiendo"

- **Estado:** Resuelta · **Fecha:** 2026-11-16
- **Responsable:** Backend

#### Qué se pidió

Las tabs de Inicio y `ReviewCard` (nodo `14296:10728`) necesitan reseñas con autor, canción, calificación, texto, número de Me gusta y de comentarios, si el usuario sigue al autor y si la guardó. El backend no tenía seguidores, Me gusta ni comentarios. Se pidió como mínimo `GET /api/v1/feed?tab=popular|following&cursor=…` con paginación por cursor.

Producto definió el orden:

- **Para ti:** las reseñas del día ordenadas por popularidad; cuando se acaban las de un día, siguen las del anterior.
- **Siguiendo:** las reseñas de los usuarios que sigues, de la más nueva a la más antigua.

#### Decisión

Se implementó el feed y, para alimentarlo, la parte del módulo social que faltaba:

- **Seguidores:** seguir y dejar de seguir, listas de seguidores y seguidos, y perfil público con contadores.
- **Me gusta** en reseñas.
- **Comentarios** en hilos de un nivel (comentario raíz → respuestas).

Reglas del feed:

- **Popularidad** = `likes_count + 2 × comments_count`; un comentario vale el doble porque cuesta más escribirlo. Desempate: la más nueva.
- **"El día"** se calcula en la zona horaria del cliente (`?tz=`, IANA). Sin `tz` se usa `America/Mazatlan`.
- **"Para ti"** no incluye tus propias reseñas. Ambas tabs incluyen reseñas de canciones, álbumes y artistas.
- **Paginación por cursor opaco** (keyset): estable aunque lleguen reseñas nuevas mientras se pagina.
- **Guardados:** quedan fuera de esta solicitud. `saved_by_me` se envía siempre `false` para que la tarjeta no cambie cuando existan.

`ReviewCard` recibe el mismo objeto en todas partes: `GET /reviews`, `GET /reviews/{id}`, `POST`/`PUT` y el feed traen ahora `likes_count`, `comments_count`, `liked_by_me`, `saved_by_me` y `following_author`, además de `target.summary` (HOME-02) y `rating_score`.

#### Contrato de la API

`GET /api/v1/feed?tab=popular&tz=America/Mazatlan&limit=20&cursor=…`

| Parámetro | Valores |
| --- | --- |
| `tab` | `popular` o `following` (obligatorio) |
| `tz` | Zona IANA, solo para `popular`. Default `America/Mazatlan` |
| `limit` | 1–50, default 20 |
| `cursor` | `next_cursor` de la página anterior; omitirlo pide la primera |

```json
{
  "tab": "popular",
  "timezone": "America/Mazatlan",
  "limit": 20,
  "next_cursor": "eyJzIjoiZmVlZDpwb3B1bGFyIiwi…",
  "reviews": [
    {
      "id": "…",
      "target": { "type": "song", "id": "3", "summary": { "track_name": "Do Not Let Me Go", "album_image_url": "https://i.scdn.co/image/…", "…": "…" } },
      "title": null,
      "content": "…",
      "rating_score": 4.5,
      "likes_count": 2,
      "comments_count": 1,
      "liked_by_me": true,
      "saved_by_me": false,
      "following_author": false,
      "author": { "id": "…", "username": "omar_delangel", "display_name": "Omar Del Angel Villa", "avatar_url": "…" },
      "created_at": "…",
      "updated_at": "…"
    }
  ]
}
```

| Caso | Respuesta |
| --- | --- |
| Página con más resultados | `200`, `next_cursor` con valor |
| Última página | `200`, `next_cursor: null` |
| "Siguiendo" sin seguir a nadie | `200`, `reviews: []` |
| `tab` inválido, `tz` inválida, `limit` fuera de 1–50 | `400` |
| Cursor mal formado, de la otra tab o de otra `tz` | `400` |
| Sin Bearer | `401` |

Endpoints sociales nuevos (detalle y ejemplos en el README, endpoints 25–27):

| Método | Ruta | Respuesta |
| --- | --- | --- |
| `GET` | `/users/{id}` (UUID o username) | Perfil: `followers_count`, `following_count`, `reviews_count`, `followed_by_me`, `is_me` |
| `POST` / `DELETE` | `/users/{id}/follow` | Perfil actualizado; idempotente; seguirte a ti mismo → `400` |
| `GET` | `/users/{id}/followers`, `/users/{id}/following` | `{ users, next_cursor }` |
| `POST` / `DELETE` | `/reviews/{id}/like` | `{ review_id, liked, likes_count }`; idempotente |
| `GET` / `POST` | `/reviews/{id}/comments` | Raíces paginadas / crear (`content`, `parent_id` opcional) → `201` |
| `GET` | `/comments/{id}/replies` | Respuestas paginadas |
| `PUT` / `DELETE` | `/comments/{id}` | Solo el autor (`403` si no) |

Un raíz borrado que tiene respuestas sale como `deleted: true` y `content: null`.

#### Qué tiene que hacer el frontend

1. Mostrar las tabs "Para ti" y "Siguiendo" con `GET /feed?tab=popular&tz=<zona del navegador>` y `GET /feed?tab=following`. La zona sale de `Intl.DateTimeFormat().resolvedOptions().timeZone`.
2. Paginación infinita: enviar `next_cursor` tal cual y parar cuando sea `null`. Deduplicar por `id`: si una reseña gana Me gusta mientras se pagina "Para ti", puede cambiar de página. Si cambia la zona, empezar sin cursor.
3. En `ReviewCard`: autor (`author`), objetivo (`target.summary`), `rating_score`, `content`, `likes_count`/`liked_by_me` (botón con `POST`/`DELETE /reviews/{id}/like`), `comments_count`, `following_author` (botón con `POST`/`DELETE /users/{author.id}/follow`). Ocultar "guardar" mientras `saved_by_me` no tenga endpoint.
4. `rating_score` va de 0.5 a 5.0 en pasos de 0.5 (no de 1 a 5) y es `null` si el autor no calificó.

#### Pendiente / fuera de alcance

- Guardados (`saved_by_me` y su listado).
- Me gusta en comentarios, menciones y notificaciones (las notificaciones están fuera del alcance del proyecto).
- Si el volumen crece, acotar "Para ti" por rango de fechas para usar el índice (ver `implementacion.md` §10.2).

#### Verificación

- `go test ./...`: `feed_service_test.go` (cursor de ida y vuelta, día local del cursor, última página, un resumen por objetivo, "Siguiendo", validaciones de `tab`, `tz`, cursor y `limit`), `social_service_test.go`, `comment_service_test.go`, `TestReviewCamposSocialesSonDelViewer`, `TestRutasSocialesRequierenBearer` y `TestFeedDefaultTimezone`. La migración aplica y revierte (`migrate-down`/`up`).
- Prueba manual contra Postgres con los usuarios del seed (`implementacion.md` §10.7): orden por día y puntaje, exclusión de las propias, recorrido completo con `limit=1`, "Siguiendo" antes y después de seguir, errores `400` y borrado lógico. Los datos de prueba se borraron.

---

### HOME-05 · Sugerencias de usuarios ("Conecta por lo que suena")

- **Estado:** Resuelta · **Fecha:** 2026-11-16
- **Responsable:** Backend

#### Qué se pidió

El panel de escritorio sugiere usuarios con gustos parecidos ("También escucha Hoke") con un botón Seguir. No había usuarios públicos ni seguidores en la API. Se propuso `GET /api/v1/users/suggestions?limit=…` → `{ id, username, display_name, avatar_url, reason }`.

#### Decisión

Se implementó `GET /api/v1/users/suggestions` con el contrato propuesto. `reason` es un objeto, con el texto listo para mostrar y los datos para armar otro. Los perfiles y seguidores ya existían desde HOME-04.

- **"Gustos parecidos"** combina dos señales:
  1. **Artistas en común:** artistas que ambos calificaron con 3.5 o más (al artista, a un álbum suyo o a una canción suya). Van primero; más artistas en común = más arriba.
  2. **Perfil acústico:** el promedio de los embeddings de las canciones y álbumes que cada uno calificó con 3.5 o más (el mismo motor de recomendaciones de HOME-01). Rellena con usuarios sin artistas en común.
- **"Le gusta"** es una calificación de 3.5 o más. Las reseñas sin voto no cuentan.
- **Respaldo:** si no hay suficientes coincidencias, se completa con los usuarios con más seguidores ("Popular en JunkBox"). Así el panel nunca sale vacío, ni siquiera para un usuario nuevo.
- **Cálculo por lotes:** el servidor recalcula al arrancar y cada hora (`SUGGESTIONS_REFRESH_INTERVAL`); `make suggestions-refresh` lo fuerza. Al leer se quitan los usuarios que ya sigues, así que seguir a alguien lo saca del panel al instante.
- Nunca se sugiere al propio usuario ni a quien ya sigue.

#### Contrato de la API

`GET /api/v1/users/suggestions?limit=5` (Bearer). `limit` de 1 a 50, default 10.

```json
{
  "limit": 5,
  "computed_at": "2026-11-17T05:26:49Z",
  "users": [
    {
      "id": "b2000000-0000-0000-0000-000000000002",
      "username": "carlos_andrade",
      "display_name": "Carlos Eduardo Andrade Gámez",
      "avatar_url": "https://api.dicebear.com/7.x/bottts/svg?seed=carlos",
      "reason": {
        "type": "common_artists",
        "text": "También escucha Coldplay",
        "common_count": 1,
        "artists": [ { "id": "c028925a-aa66-4c16-8c9f-61ec2accfc31", "name": "Coldplay", "image_url": null } ],
        "similarity": 0.872
      }
    }
  ]
}
```

| `reason.type` | `reason.text` | Otros campos |
| --- | --- | --- |
| `common_artists` | "También escucha Hoke" / "… Hoke y Feid" / "… Hoke, Feid y 3 más" | `common_count`, `artists` (hasta 3, los más populares), `similarity` |
| `similar_taste` | "Gustos parecidos a los tuyos" | `similarity` (0 a 1) |
| `popular` | "Popular en JunkBox" | — |

| Caso | Respuesta |
| --- | --- |
| Usuario con gustos registrados | `200`, primero las personalizadas, luego populares hasta `limit` |
| Usuario nuevo o sin calificaciones ≥ 3.5 | `200`, solo populares, `computed_at: null` |
| `limit` no entero, `0` o fuera de 1–50 | `400` |
| Sin Bearer | `401` |

#### Qué tiene que hacer el frontend

1. Mostrar el panel con `GET /users/suggestions?limit=<n>`. Usar `reason.text` tal cual o componer otro texto con `reason.artists` (`name`, `image_url`).
2. Botón Seguir: `POST /users/{id}/follow` (HOME-04). Quitar la tarjeta en el momento; al volver a pedir la lista ya no aparece.
3. `artists[].image_url` puede ser `null` (no se piden fotos a Spotify en esta ruta): usar un placeholder.
4. Las sugerencias personalizadas cambian como mucho cada hora; no hace falta pedirlas a cada rato.

#### Pendiente / fuera de alcance

- Recalcular a un usuario en cuanto califica (hoy espera a la siguiente ronda horaria).
- Escalar el cálculo con un índice vectorial si crece el número de usuarios (`implementacion.md` §11.2).
- El username `suggestions` queda reservado de hecho: `/users/suggestions` es esta ruta, así que ese perfil solo se abre por UUID.

#### Verificación

- `go test ./...`: `suggestion_service_test.go` (textos del motivo, relleno con populares sin repetir, usuario sin cálculo, `limit`, umbral del cálculo), `TestRutasSocialesRequierenBearer` y `TestSuggestionsRefreshInterval`. La migración aplica y revierte.
- Prueba manual contra Postgres con los usuarios del seed (`implementacion.md` §11.6): artistas en común primero, luego perfil acústico y populares; seguir a alguien lo quita sin recalcular; un usuario sin gustos recibe populares. Los datos de prueba se borraron.

---

### HOME-06 · Vista previa de audio

- **Estado:** Descartada · **Fecha:** 2026-11-16
- **Responsable:** Backend y Producto

#### Qué se pidió

`MiniPlayer`, `SnippetPlayer` ("Fragmento · 30 s") y el botón QuickPlay de `MusicCard` reproducen audio, pero ninguna respuesta trae una URL de vista previa, y Spotify dejó de enviar `preview_url` a las apps nuevas. Se pidió decidir si habría reproducción y de qué fuente, y en ese caso añadir `preview_url: string | null` a `TrackSummary`.

#### Decisión

Se descarta. El 2026-11-06 el propietario decidió reproducir **canciones completas con el Web Playback SDK de Spotify** (requiere Premium), a partir del enlace de cuenta de Configuración (SPOT-01). Con eso, la vista previa de 30 s deja de ser necesaria para el reproductor.

- No se añade `preview_url` a `TrackSummary`. Además, Spotify ya no lo envía a esta app, así que el campo saldría siempre `null`.
- Para reproducir, el frontend ya tiene `spotify_id` en `TrackSummary` y `TrackDetail`, que es lo que necesita el SDK (`spotify:track:<spotify_id>`).

#### Qué tiene que hacer el frontend

1. Mantener ocultos `SnippetPlayer` ("Fragmento · 30 s") y el QuickPlay decorativo hasta que exista el reproductor del SDK.
2. Conectar `MiniPlayer` y QuickPlay al Web Playback SDK cuando se resuelva SPOT-01, usando `spotify_id`.

#### Pendiente / fuera de alcance

- SPOT-01 (enlace de la cuenta de Spotify y token de usuario para el SDK) se atiende como solicitud aparte.
- Una canción importada de un dataset sin `spotify_id` válido no se podrá reproducir; hoy todas las del catálogo tienen el ID de Spotify.

#### Verificación

- Sin cambios de código.

---

### SEARCH-01 · Un nombre de artista que coincide con palabras de títulos no consulta Spotify

- **Estado:** Resuelta · **Fecha:** 2026-11-17
- **Responsable:** Backend

#### Qué se pidió

`type=track` busca con `strict_word_similarity` sobre "título + artista" y solo consulta Spotify si el mejor puntaje es menor que 0.8. Se reportaron dos casos límite (comprobados 2026-11-06):

- **"Hoke"**, un artista que no está en el dataset, puntúa 1.0 contra títulos como "Deewane Hoke Hum", así que nunca se consultaba Spotify ni aparecían sus canciones.
- **"Blinding Lights"** sola ponía primero versiones de otros artistas: empatan a 1.0 y el desempate por `similarity` favorecía los nombres de artista cortos.

Se propusieron dos opciones: traer las canciones del artista cuando `type=artist` coincide exactamente, u ordenar por `popularity` antes que por `similarity`.

#### Decisión

Se midieron las alternativas contra el catálogo local (1.16M canciones) el 2026-11-17:

| Alternativa | Resultado | Veredicto |
| --- | --- | --- |
| `popularity` antes que `similarity` | Arregla "Blinding Lights" (The Weeknd pasa del 6.º al 1.º), "Hello" (Adele), "Halo" (Beyoncé) y "Shape of You" (Ed Sheeran), pero rompe las consultas de una palabra: "Yellow" → *Bodak Yellow*, "Stay" → *Stay With Me*, "Yellow Coldplay" → una versión *fingerstyle* antes que la de Coldplay | Descartada tal cual |
| **Coincidencia exacta y después `popularity`** | Arregla los mismos casos sin esas regresiones | **Adoptada** |
| Artista exacto → sus canciones de Spotify | *Get Artist's Top Tracks* no es una opción segura en Development Mode. Habría que buscar el artista y después sus canciones (1–3 llamadas extra por búsqueda) y solo cubre nombres de artista | Descartada |
| **Coincidencia parcial → Spotify** | Reusa el respaldo de Spotify que ya existe. De 17 consultas de prueba, solo "Hoke", "Weeknd", "Bad Bunny" y "Tiny Desk" van a Spotify. Bad Bunny tampoco figura como artista principal en el dataset, así que también mejora | **Adoptada** |

Cómo queda `type=track` (`artist` y `album` no cambian):

- **Tildes:** la búsqueda de canciones las ignora tanto en el filtro y el puntaje como en la comparación exacta. Antes, "Zoe" no encontraba **ninguna** de las 56 canciones de Zoé (puntaje 0.33, no pasaba el filtro) y "Beyonce" no encontraba a Beyoncé. Se añadió la extensión `unaccent` con una función `f_unaccent` IMMUTABLE, y el índice GIN de búsqueda se reconstruyó sobre ella (migración `20261117000012`, ≈9 s en local). `type=artist` y `type=album` también las ignoran (migración `20261117000013`, índices `idx_artists_name_unaccent_trgm` e `idx_albums_search_unaccent_trgm`).
- Una canción es **coincidencia exacta** si su título, su artista o "título artista" es la consulta, sin distinguir mayúsculas ni tildes.
- **Orden:** puntaje, después las exactas, después `popularity` y al final `similarity`.
- **Cuándo se consulta Spotify:** además de los casos de antes (sin resultados o puntaje < 0.8), cuando la mejor canción local no es exacta. Entonces se responden primero las canciones de Spotify y después las locales que no repite, hasta `limit`, con `source: "spotify_fallback"`. Si Spotify falla o no trae nada, se responden las locales como antes.
- Lo que llega de Spotify se guarda como siempre. La segunda búsqueda de "Hoke" ya es exacta (sus canciones quedan con `artist_name = "Hoke"`) y responde `local_cache`.

#### Contrato de la API

Sin campos nuevos. Cambian el orden de `/music/search?type=track` y los casos en que responde `source: "spotify_fallback"`.

| Consulta | Antes | Ahora |
| --- | --- | --- |
| `Hoke` (1.ª vez) | `local_cache`: "Kitni Bechain Hoke", "Deewane Hoke Hum"… | `spotify_fallback`: canciones de Hoke primero y después las locales |
| `Hoke` (2.ª vez) | igual que la 1.ª | `local_cache`: "Olympique – Hoke", "Infrarrojo / Ultravioleta – Hoke", después las locales |
| `Blinding Lights` | Roostz, Lofiwaala, Vendredi… | The Weeknd, Revelries, Saint Asonia… |
| `Stay` | BTS, EXO, EDX… | Zedd, Rihanna, BLACKPINK… |
| `Zoe` | ninguna canción de Zoé | Zoé: "Labios Rotos - En Vivo", "Azul", "Soñé - En Vivo"… (`local_cache`) |
| `Beyonce` | sin canciones de Beyoncé | Beyoncé: "CUFF IT", "AMERICA HAS A PROBLEM"… (`local_cache`) |

Las canciones que llegan de Spotify tienen `popularity: null` (Development Mode ya no la envía), así que dentro de un empate quedan después de las del dataset.

#### Qué tiene que hacer el frontend

Nada obligatorio. El Buscador puede mantener las tres búsquedas (`track`, `album`, `artist`) de `FrontEnd/implementacion.md` §6, y la lista de canciones ya trae las del artista desde la primera búsqueda.

#### Pendiente / fuera de alcance

- Una consulta que nunca es exacta en el catálogo ("Weeknd", "Tiny Desk") consulta Spotify en cada búsqueda: el cache-through no la convierte en `local_cache`. La primera página de Spotify puede traer canciones cuyo título contiene la palabra ("WEEKND – Bohemio") antes que las del artista.

#### Verificación

- `go test ./...`: casos nuevos en `music_service_test.go` (`TestSearchCoincidenciaParcialConsultaSpotifyYMezcla`, `TestSearchCoincidenciaExactaNoConsultaSpotify`, `TestSearchCoincidenciaParcialSpotifyFallaRespondeLocales`).
- Prueba manual contra el servidor local: `Hoke` respondió `spotify_fallback` en ≈1,8 s (búsqueda, importación e imágenes) y la segunda vez `local_cache` en ≈35 ms, con las canciones de Hoke primero. `Blinding Lights`, `Yellow`, `Stay`, `The Weeknd` y `Yellow Coldplay` responden `local_cache` con el orden de la tabla. `Weeknd` responde `spotify_fallback`.
- `EXPLAIN ANALYZE` de la consulta con "Love" y "Blinding Lights The Weeknd": el orden nuevo no cambia el plan (mismo *Bitmap Index Scan*) ni los tiempos (≈550 ms y ≈47 ms). Con `f_unaccent` la consulta usa `idx_song_metadata_search_unaccent_trgm` y los tiempos se mantienen ("Love" ≈330 ms, "Blinding Lights The Weeknd" ≈50 ms, "Yellow Coldplay" ≈15 ms, "Grain" ≈135 ms).
- Sin tildes, contra el servidor local: `Zoe` y `zoé` devuelven las mismas canciones de Zoé, `Cancion` trae "Canción" y "Cancion", y `Beyonce` trae las de Beyoncé (todas `local_cache`). En artistas, `Zoe` trae primero a Zoé y `Beyonce` a Beyoncé; en álbumes, `Zoe` trae los de Zoé (consultas < 1 ms con los índices nuevos). Las dos migraciones se probaron en ambos sentidos (`down` restaura los índices originales y quita `unaccent`).

---

### SEARCH-02 · Canciones y álbumes de un artista

- **Estado:** Resuelta · **Fecha:** 2026-11-17
- **Responsable:** Backend

#### Qué se pidió

El Buscador (tarjeta Artistas) y la futura Ficha de Artista (`14244:52049`) necesitan la discografía, pero `GET /music/artists/{id}` no la trae. Se pidió:

- `GET /music/artists/{id}/tracks?limit=` (por popularidad).
- `GET /music/artists/{id}/albums?limit=` (con `image_url`).
- `404` si el artista no existe.
- Completar desde Spotify la lista de pistas de `GET /music/albums/{id}`: "BBO" de Hoke tenía `total_tracks: 11` y `tracks[]` traía 1 (comprobado 2026-11-06).

#### Decisión

Medido contra Spotify (Development Mode) y el catálogo local el 2026-11-17:

| Fuente | Resultado |
| --- | --- |
| *Get Artist's Top Tracks* | **403**: Spotify lo quitó para Development Mode. No hay forma directa de pedir los "éxitos" de un artista |
| Búsqueda `artist:"Hoke"` | Errática: `total 5`, y la página siguiente repite resultados. La búsqueda simple "Hoke" trae 3–4 canciones suyas por cada 10 |
| `GET /v1/artists/{id}/albums` | Funciona, 10 por página. The Weeknd tiene 81 álbumes y sencillos, Zoé 43 y Hoke 27 |
| `GET /v1/albums/{id}` | Trae el álbum y hasta 50 pistas en una llamada (BBO: 11) |

Con eso:

- **Álbumes del artista:** se piden a Spotify la primera vez y luego cada 7 días (hasta 50, `album,single`), se guardan y se responden desde la base. Orden: año descendente y, dentro del mismo año, álbumes antes que sencillos.
- **Canciones del artista:** primero las del catálogo, por `popularity`. Si hay menos que `limit`, se completan con las pistas de sus álbumes más recientes (primero álbumes, luego sencillos, hasta 5 álbumes), una vez por semana. Se descartó la búsqueda por el ruido medido.
- **Pistas de un álbum:** si hay menos que `total_tracks`, se piden a Spotify una sola vez por álbum y quedan guardadas en orden de disco y pista (columnas nuevas `disc_number` y `track_number`).
- Todo ocurre antes de responder, con el presupuesto de 2 s (`SEARCH_ENRICH_BUDGET`). Si Spotify falla o no alcanza, se responde lo que hay y se reintenta en la siguiente petición. Las canciones importadas se encolan para su embedding.
- Migración `20261117000014_artist_discography.sql`: marcas `albums_synced_at` y `tracks_synced_at` en `artists`, `tracks_synced_at` en `albums`, y `disc_number`/`track_number` en `song_metadata`.

#### Contrato de la API

`GET /api/v1/music/artists/{id}/tracks?limit=` (`id`: UUID local o ID de Spotify; `limit` 1–50, por defecto 20):

```json
{
  "count": 20,
  "results": [
    {
      "id": 1160032,
      "spotify_id": "…",
      "track_name": "Aimbot",
      "artist_name": "Hoke",
      "album_title": "TRES CREUS",
      "album_image_url": "https://i.scdn.co/image/…",
      "artist_image_url": "https://i.scdn.co/image/…",
      "popularity": null,
      "…": "resto de campos de Track, como en /music/search?type=track"
    }
  ]
}
```

`GET /api/v1/music/artists/{id}/albums?limit=`:

```json
{
  "count": 8,
  "results": [
    {
      "id": "…",
      "spotify_id": "…",
      "title": "TRES CREUS",
      "artist_id": "…",
      "artist_name": "Hoke",
      "release_year": 2024,
      "album_type": "album",
      "image_url": "https://i.scdn.co/image/…",
      "total_tracks": 10,
      "created_at": "…"
    }
  ]
}
```

| Caso | Respuesta |
| --- | --- |
| Artista inexistente | `404 {"error": "no encontrado"}` |
| `limit` fuera de 1–50 o `id` inválido | `400` |
| Spotify falla o no está configurado | `200` con lo que hay en el catálogo |

`GET /music/albums/{id}` no cambia de forma: `tracks[]` llega completo y en orden de disco y pista.

#### Qué tiene que hacer el frontend

1. Usar `/music/artists/{id}/tracks` y `/music/artists/{id}/albums` en la tarjeta Artistas y en la Ficha de Artista, y quitar la alternativa de `FrontEnd/implementacion.md` §7 (búsquedas filtradas por `artist_name`).
2. Esperar que la primera visita a un artista o álbum nuevo tarde más (medido: ≈1,9 s los álbumes de Hoke, ≈0,4 s sus canciones). Las siguientes salen de la base en ≈40–70 ms.
3. Mantener el recuadro neutro cuando `image_url` o `album_image_url` sean `null`.

#### Pendiente / fuera de alcance

- **No hay "éxitos" reales** para artistas fuera del dataset: Spotify no envía `popularity` en Development Mode, así que sus canciones salen en orden de álbum (del más reciente al más antiguo).
- La discografía se limita a 50 lanzamientos propios (`album,single`), sin recopilaciones ni apariciones (`appears_on`).
- Un álbum cuyo artista principal en Spotify es otro (una colaboración) queda asociado a ese otro artista.
- Una misma canción puede aparecer dos veces si existe en el dataset y en otro lanzamiento con distinto ID de Spotify.
- La sincronización completa del catálogo (CAT-01) sigue fuera de alcance.

#### Verificación

- `go test ./...`: `artist_discography_test.go` (sincronización única y por TTL, Spotify caído, artista sin `spotify_id`, completar canciones con pistas de álbumes respetando `limit` y el tope de 5, pistas de otro artista que no cuentan, 404 y `limit` inválido, y completitud del álbum: faltantes, ya sincronizado, 404 en Spotify, Spotify caído) y `client_test.go` (paginación de `ArtistAlbums` y de `AlbumWithTracks`, y mapeo de `track_number`).
- Migración probada en ambos sentidos.
- Prueba manual contra el servidor local:
  - Hoke: álbumes de 2 a 27 (1.ª llamada ≈1,9 s, después ≈4 ms) y canciones de 2 a 20 (≈0,4 s, después ≈3 ms).
  - "BBO": 11 de 11 pistas en orden.
  - "Aztlán" de Zoé: de 1 a 12 pistas (≈0,2 s).
  - The Weeknd: 5 canciones del dataset por popularidad, con portada y sin consultar Spotify. Sus álbumes salen desde "Hurry Up Tomorrow" (2025).
  - Artista inexistente → 404; `limit=0` o `51` → 400.

---

### SEARCH-03 · Calificación en los resultados

- **Estado:** Resuelta · **Fecha:** 2026-11-17
- **Responsable:** Backend

#### Qué se pidió

Figma muestra "★ 4,6" en cada canción, álbum y artista, pero los resultados de búsqueda no traían `ratings` (solo el detalle). Se pidió añadir `ratings: { count, average | null }` a cada elemento de `results`.

#### Decisión

Se añade en **todos los listados**, no solo en la búsqueda, para que cualquier tarjeta pueda mostrar la estrella con el mismo objeto. Las estadísticas ya existían en el catálogo (`ratings_count` y `avg_rating` en canciones, álbumes y artistas, actualizadas por el trigger de calificaciones), así que solo se leen dos columnas más de la misma fila, sin consultas extra.

- Siempre llega el objeto. `average` es `null` si nadie ha calificado el elemento, con la misma forma que el detalle.
- Las canciones que llegan del respaldo de Spotify o al completar álbumes también lo traen (`{0, null}` si son nuevas).
- Los resúmenes de reseñas (`target.summary`) siguen **sin** `ratings`, como se decidió en HOME-02.

#### Contrato de la API

Cada elemento lleva:

```json
"ratings": { "count": 3, "average": 4.67 }
```

| Endpoint | Elementos con `ratings` |
| --- | --- |
| `GET /music/search?type=track|artist|album` | cada canción, artista o álbum de `results` |
| `GET /music/artists/{id}/tracks` y `/albums` (SEARCH-02) | cada elemento de `results` |
| `GET /music/albums/{id}` | el álbum (ya lo tenía) y cada pista de `tracks[]` |
| `GET /home` | cada canción de `sections[].items` |
| `GET /music/tracks/{id}`, `/artists/{id}` | sin cambios (ya lo tenían) |
| `target.summary` de las reseñas | no lo trae |

Es un cambio aditivo: ningún campo cambia de nombre ni de tipo.

#### Qué tiene que hacer el frontend

1. Mostrar la estrella con `ratings.average` en filas y tarjetas, y ocultarla cuando sea `null`.
2. Quitar la petición al detalle que hacía el álbum destacado solo para obtener la calificación.

#### Pendiente / fuera de alcance

- Nada.

#### Verificación

- `go test ./...`: `TestSearchTraeCalificaciones` (los tres tipos, con y sin calificaciones), `TestDiscografiaYPistasTraenCalificaciones`, `TestGetTrackTraeCalificaciones`, `TestHomeTraeCalificaciones` y `TestSummarizeTargetCancionCompletaCaratula`, que ahora comprueba que el resumen sale sin `ratings`.
- Integración (`make test-db`): `TestImportTraeLasCalificacionesExistentes` (`internal/adapters/secondary/postgres/catalog_importer_test.go`). Importa una canción, la califica junto con su álbum y su artista, la vuelve a importar y comprueba que `ImportTracks`, `ImportAlbums` e `ImportArtists` devuelven esas calificaciones, que es el camino del respaldo de Spotify y de completar álbumes. Sin la lectura de `ratings_count`/`avg_rating` en el importador, la prueba falla.
- Prueba manual contra el servidor local con un usuario temporal que calificó "Olympique" (4,5), el álbum "BBO" (4) y al artista Hoke (5): las tres aparecen con sus `ratings` en la búsqueda, la discografía, el detalle del álbum y sus pistas; los elementos sin calificar salen con `{"count": 0, "average": null}` (también en `/home`); y el `target.summary` de una reseña salió sin `ratings`. Las calificaciones, la reseña y el usuario de prueba se borraron, y las estadísticas volvieron a 0.
