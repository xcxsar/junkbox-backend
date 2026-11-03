# Junkbox Backend

Backend para **Junkbox**: Plataforma web de reseñas musicales, calificaciones (1 a 5 estrellas), red social y sistema de recomendaciones personalizadas con búsqueda vectorial sobre un catálogo musical masivo.

Este servicio está construido en **Go** siguiendo los principios de arquitectura limpia / puertos y adaptadores (Clean Architecture), utilizando **PostgreSQL 16 + pgvector** para almacenamiento relacional y vectorial, **Goose** para control de versiones del esquema de base de datos, **SQLC** para generación de consultas SQL estrictamente tipadas en Go, y **Make** como herramienta de automatización para desarrollo y despliegue local.

---

## Dependencias Requeridas

### 1. Docker y Docker Compose

Requerido para orquestar el contenedor de base de datos PostgreSQL con soporte nativo de extensiones vectoriales (`pgvector`).

- **macOS / Windows**: Instalar [Docker Desktop](https://www.docker.com/products/docker-desktop/).
- **Linux**: Instalar Docker Engine y el plugin Docker Compose según la [documentación oficial](https://docs.docker.com/engine/install/).

Verificar instalación:

```bash
docker compose version
```

> **Imagen utilizada:** `pgvector/pgvector:pg16`. El volumen de datos asociado se denomina `postgres-junkbox-data`.

---

### 2. Goose (Migraciones de Base de Datos)

Herramienta de migraciones de base de datos en SQL puro:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Verificar que `$GOPATH/bin` esté en tu `$PATH`:

```bash
goose --version
```

---

### 3. SQLC (Compilador SQL a Go)

Generador de código Go tipo-seguro a partir de sentencias SQL nativas:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

Verificar instalación:

```bash
sqlc version
```

---

### 4. Make

#### macOS
```bash
# Vía CLI Tools de Xcode
xcode-select --install

# O vía Homebrew
brew install make
```

#### Linux
```bash
# Ubuntu / Debian
sudo apt update && sudo apt install make

# Fedora / RHEL
sudo dnf install make

# Arch Linux
sudo pacman -S make
```

#### Windows
Ejecutar PowerShell como Administrador:
```powershell
winget install GnuWin32.Make
# O mediante Chocolatey
choco install make
```

---

## Configuración del Entorno de Base de Datos

1. Copia la plantilla de variables de entorno ubicada en `.env.template` hacia `.env`:

```bash
cp .env.template .env
```

2. Configura los parámetros en `.env`:

- `POSTGRES_DB`: Nombre de la base de datos principal (`junkbox_db`).
- `POSTGRES_USER`: Usuario administrador de PostgreSQL (`postgres`).
- `POSTGRES_PASSWORD`: Contraseña del usuario.
- `POSTGRES_PORT`: Puerto de conexión en el host (por defecto `5432`).
- `DATABASE_URL`: URL completa de conexión PostgreSQL utilizada por Goose y el servidor Go.
- `JWT_SECRET`: Secreto criptográfico HMAC-SHA256 (mínimo 32 caracteres) para firmar Access Tokens. Genera uno seguro con:
  ```bash
  openssl rand -hex 32
  ```
- `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL`: vigencia de los tokens en formato de duración Go (por defecto `15m` y `168h`).
- `HTTP_ADDR`: dirección de escucha del servidor (por defecto `:8080`; también se puede pasar `--addr`).
- `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET` (opcionales; se definen las dos o ninguna): credenciales de la app en el [Spotify Developer Dashboard](https://developer.spotify.com/dashboard), usadas con el flujo OAuth2 Client Credentials. Sin ellas, la búsqueda solo consulta el catálogo local.
- `FREQBLOG_API_KEY` (opcional): clave de [FreqBlog](https://freqblog.com), de donde salen los audio features de las canciones importadas desde Spotify. Spotify cerró `/v1/audio-features` para apps nuevas en noviembre de 2024. Sin la clave, esas canciones quedan sin embedding (`pending_embedding: true`). El plan gratuito incluye 1.000 canciones resueltas al mes.
- `SEARCH_FALLBACK_THRESHOLD` (opcional, por defecto `0.8`): puntaje mínimo (`strict_word_similarity`, de 0 a 1) del mejor resultado local para no consultar Spotify.

> `.env` contiene secretos locales y está ignorado en Git mediante `.gitignore`.

> Si reutilizas un volumen `postgres-junkbox-data` ya inicializado, `POSTGRES_PASSWORD` (y la contraseña de `DATABASE_URL`) deben coincidir con la contraseña con la que se creó. Postgres ignora estas variables una vez que el volumen tiene datos.

---

## Gestión de Base de Datos (Make)

Todas las tareas operativas se gestionan desde la raíz del proyecto a través del `Makefile`:

### Ciclo de Vida del Contenedor Docker

- **Iniciar base de datos en segundo plano:**
  ```bash
  make db-up
  ```
  *(Espera a que el healthcheck de PostgreSQL confirme disponibilidad antes de devolver el control)*.

- **Detener base de datos:**
  ```bash
  make db-down
  ```

- **Ver estado y salud del contenedor:**
  ```bash
  make db-ps
  ```

- **Inspeccionar registros (logs en vivo):**
  ```bash
  make db-logs
  ```

- **Acceder a consola interactiva `psql`:**
  ```bash
  make db-shell
  ```

- **Reinicio completo de base de datos (con confirmación interactiva de seguridad):**
  ```bash
  make db-reset
  ```
  > **Nota de Seguridad:** A diferencia de entornos volátiles, `make db-reset` solicita confirmación interactiva `[y/N]` antes de destruir el contenedor y el volumen de datos (`down -v`), protegiendo el catálogo de canciones indexadas.

---

## Control de Esquema: Migraciones (Goose)

- **Aplicar todas las migraciones pendientes:**
  ```bash
  make migrate-up
  ```

- **Revertir la última migración aplicada:**
  ```bash
  make migrate-down
  ```

- **Consultar estado actual de migraciones aplicadas:**
  ```bash
  make migrate-status
  ```

- **Crear una nueva migración:**
  ```bash
  make migrate-create name=nombre_de_la_migracion
  ```

---

## Poblado de Datos de Prueba (Seeding)

- **Crear un nuevo archivo de seed:**
  ```bash
  make seed-create name=nombre_del_seed
  ```

- **Aplicar todos los seeds ordenadamente:**
  ```bash
  make seed-up
  ```

- **Vaciar tablas respetando el historial de migraciones:**
  ```bash
  make seed-down
  ```
  *(Ejecuta `db/scripts/truncate_all.sql` solicitando confirmación del usuario)*.

---

## Generación de Código SQL (SQLC)

Compila las consultas SQL ubicadas en `db/queries/` contra el esquema de `db/migrations/` para generar código Go fuertemente tipado en `internal/store/sqlc/`:

```bash
make sqlc
```

---

## Reproducibilidad del Catálogo de Canciones (`song_metadata`)

El catálogo musical de Junkbox está en la tabla `song_metadata` (1.159.764 canciones), con vectores de características de audio (`vector(10)` en `pgvector`) para la búsqueda por similitud.

Para reconstruirlo en cualquier entorno limpio, sin copiar volúmenes Docker a mano:
1. Las migraciones crean `song_metadata`, `artists` y `albums`, la extensión `pg_trgm` para búsqueda difusa y los índices B-tree y trigrama.
2. `make ingest-songs` carga el CSV con SQL puro (`db/scripts/ingest_songs.sql`), sin dependencias de Python. Calcula los vectores con MinMax (loudness y tempo), crea los artistas, enlaza `song_metadata.artist_id` y construye los índices IVFFlat y de búsqueda. En un Postgres limpio tarda ~1 min 40 s:
   ```bash
   make migrate-up
   make ingest-songs                       # usa ./data/spotify_data.csv
   make ingest-songs DATASET=ruta/al.csv   # CSV en otra ubicación
   ```
3. Si el volumen ya tenía el catálogo de antes de la Fase 3, basta con `make migrate-up && make backfill-artists`. Es idempotente; la primera vez tarda ~2 min 15 s.
4. La carpeta `data/` con los CSV está en `.gitignore` y se distribuye por separado.
5. El pipeline está detallado en [`implementacion.md`](implementacion.md#28-especificación-del-pipeline-de-ingesta-desde-csv) y el backfill en [§4.2](implementacion.md#42-esquema-relacional-del-catálogo-musical).

> Ni el `Down` de las migraciones ni `make seed-down` eliminan `song_metadata`, `artists` o `albums`. El catálogo solo se destruye con `make db-reset`, que pide confirmación interactiva.

---

## Compilación y Ejecución del Servidor

- **Validar código (linter Go):**
  ```bash
  make vet
  ```

- **Ejecutar pruebas unitarias y de integración:**
  ```bash
  make test
  ```

- **Compilar binarios:**
  ```bash
  make build
  ```

- **Iniciar servidor API REST:**
  ```bash
  make run-server
  ```
  *(Ejecuta `go run . server --config .env`; por defecto escucha en `http://localhost:8080`)*.

---

## Puesta en Marcha Rápida

```bash
cp .env.template .env   # y ajustar contraseña, JWT_SECRET, credenciales de Spotify y FREQBLOG_API_KEY
make db-up
make migrate-up
make ingest-songs             # solo en una base de datos nueva (requiere data/spotify_data.csv)
make seed-up                  # 4 usuarios del equipo, contraseña Password123!
make run-server
```

---

## API REST

Base URL: `http://localhost:8080/api/v1`

El backend utiliza tokens de acceso **JWT HS256** (15 minutos de vigencia) con claims `sub`, `username`, `jti`, `iat`, `exp`, y **Refresh Tokens** opacos (7 días de vigencia) con hash SHA-256 en base de datos, rotación por familias y denylist de revocación inmediata.

### Resumen de Endpoints

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/healthz` (sin prefijo) | — | Comprobación de estado y conectividad del servicio |
| `POST` | `/api/v1/auth/register` | — | Registro público de nuevos usuarios |
| `POST` | `/api/v1/auth/login` | — | Inicio de sesión con correo o nombre de usuario |
| `POST` | `/api/v1/auth/refresh` | Bearer | Rotación e intercambio seguro de refresh token |
| `GET` | `/api/v1/auth/me` | Bearer | Consulta del perfil del usuario autenticado |
| `POST` | `/api/v1/auth/logout` | Bearer | Cierre de sesión y revocación en denylist |
| `GET` | `/api/v1/music/search` | Bearer | Búsqueda difusa en catálogo local con fallback a Spotify |
| `GET` | `/api/v1/music/tracks/{id}` | Bearer | Detalle de canción, métricas acústicas y estado de embedding |
| `GET` | `/api/v1/music/artists/{id}` | Bearer | Detalle de artista y catálogo relacionado |
| `GET` | `/api/v1/music/albums/{id}` | Bearer | Detalle de álbum y pistas asociadas |

---

### Detalle de Endpoints y Ejemplos de Uso

#### 1. Healthcheck (`GET /healthz`)
Verifica que el servidor esté activo y respondiendo.

```bash
curl -i -X GET http://localhost:8080/healthz
```

**Respuesta (`200 OK`):**
```text
ok
```

---

#### 2. Registro de Usuario (`POST /api/v1/auth/register`)
Crea una nueva cuenta de usuario en la plataforma. Emite un par inicial de tokens y el perfil.

**Cuerpo de la petición (JSON):**
- `username` (string, obligatorio): Nombre de usuario único (handle).
- `email` (string, obligatorio): Correo electrónico válido y único.
- `password` (string, obligatorio): Contraseña de mínimo 8 caracteres que incluya letras, números y caracteres especiales (máximo 72 bytes).
- `display_name` (string, opcional): Nombre visible.
- `avatar_url` (string, opcional): Enlace a imagen de perfil.

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "melomano_pro",
    "email": "melomano@junkbox.local",
    "password": "Password123!",
    "display_name": "Crítico Musical",
    "avatar_url": "https://api.dicebear.com/7.x/bottts/svg?seed=melomano"
  }'
```

**Respuesta (`201 Created`):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "63f7b2c5897914de42f10b7593...",
  "expires_in": 900,
  "user": {
    "id": "e43b1234-abcd-4def-9123-123456789abc",
    "username": "melomano_pro",
    "email": "melomano@junkbox.local",
    "display_name": "Crítico Musical",
    "avatar_url": "https://api.dicebear.com/7.x/bottts/svg?seed=melomano",
    "created_at": "2026-10-31T12:00:00Z"
  }
}
```

---

#### 3. Inicio de Sesión (`POST /api/v1/auth/login`)
Autentica con contraseña y permite identificarse mediante `username` o `email`.

**Cuerpo de la petición (JSON):**
- `identifier` (string, obligatorio): Nombre de usuario o correo electrónico.
- `password` (string, obligatorio): Contraseña.

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "identifier": "cesar_beltran",
    "password": "Password123!"
  }'
```

**Respuesta (`200 OK`):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "a89d71b30ef28c8942ba64e29...",
  "expires_in": 900,
  "user": {
    "id": "b1000000-0000-0000-0000-000000000001",
    "username": "cesar_beltran",
    "email": "cesar@junkbox.local",
    "display_name": "César David Beltrán Loredo",
    "avatar_url": "https://api.dicebear.com/7.x/bottts/svg?seed=cesar",
    "created_at": "2026-10-27T00:00:00Z"
  }
}
```

---

#### 4. Rotación de Refresh Token (`POST /api/v1/auth/refresh`)
Rota el refresh token y renueva el access token. Requiere enviar el access token anterior (la llave vieja, aunque esté expirada) en la cabecera `Authorization`.

**Cuerpo de la petición (JSON):**
- `refresh_token` (string, obligatorio): Token de refresco opaco.

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "a89d71b30ef28c8942ba64e29..."
  }'
```

**Respuesta (`200 OK`):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9_nuevo...",
  "refresh_token": "f29b47a11c875d9e034ac1258_nuevo...",
  "expires_in": 900
}
```

---

#### 5. Perfil de Usuario Autenticado (`GET /api/v1/auth/me`)
Devuelve la información del usuario autenticado a partir de su JWT.

```bash
curl -i -X GET http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "id": "b1000000-0000-0000-0000-000000000001",
  "username": "cesar_beltran",
  "email": "cesar@junkbox.local",
  "display_name": "César David Beltrán Loredo",
  "avatar_url": "https://api.dicebear.com/7.x/bottts/svg?seed=cesar",
  "created_at": "2026-10-27T00:00:00Z"
}
```

---

#### 6. Cierre de Sesión (`POST /api/v1/auth/logout`)
Invalida el access token actual en la denylist y revoca el refresh token. Con `all_sessions: true`, invalida todas las sesiones del usuario mediante corte global.

**Cuerpo de la petición (JSON):**
- `refresh_token` (string, obligatorio): Token de refresco de la sesión.
- `all_sessions` (boolean, opcional, por defecto `false`): Si es `true`, cierra todas las sesiones activas del usuario.

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "f29b47a11c875d9e034ac1258...",
    "all_sessions": false
  }'
```

**Respuesta (`200 OK`):**
```json
{
  "message": "sesión cerrada"
}
```

---

#### 7. Búsqueda de Música (`GET /api/v1/music/search`)
Busca por coincidencia difusa de palabras (`pg_trgm`, `strict_word_similarity` sobre "título + artista") en la base de datos local.

Consulta a Spotify si no hay resultados locales o si el mejor puntaje es menor que `SEARCH_FALLBACK_THRESHOLD` (0.8). En ese caso guarda las canciones, artistas y álbumes en PostgreSQL y encola las canciones nuevas para calcular su vector de recomendaciones en segundo plano.

Si Spotify falla o no está configurado, responde los resultados locales que haya.

Las consultas de título y/o artista responden en 15–250 ms. Una sola palabra muy común ("love", "the") puede tardar 1–3 s; la limitación y sus alternativas están en [`implementacion.md` §4.1](implementacion.md#41-arquitectura-del-flujo-de-búsqueda-y-fallback-cache-through).

**Parámetros de consulta (Query params):**
- `q` (string, obligatorio): Texto a buscar (ej. título de canción, nombre de artista o álbum).
- `type` (string, opcional, por defecto `track`): Tipo de entidad: `track`, `artist` o `album`.
- `limit` (int, opcional, por defecto `20`, máx. `50`).

##### Ejemplo A: Coincidencia en Catálogo Local (Cache-Hit)
```bash
curl -i -X GET "http://localhost:8080/api/v1/music/search?q=Grain%20Dan%20Telfer&type=track&limit=1" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "source": "local_cache",
  "query": "Grain Dan Telfer",
  "type": "track",
  "count": 1,
  "results": [
    {
      "id": 10924,
      "spotify_id": "1TIHAkrLbRALyt4O4Oh28i",
      "track_name": "Grain",
      "artist_name": "Dan Telfer",
      "year": 2012,
      "genre": "comedy",
      "popularity": 5,
      "duration_ms": 352307,
      "has_embedding": true,
      "pending_embedding": false
    }
  ]
}
```

##### Ejemplo B: Canción Nueva / No Existente Localmente (Cache-Miss con Fallback a Spotify)

Localmente solo hay otras canciones de la artista (puntaje 0.67 < 0.8). Spotify ya no envía `popularity` a las apps en Development Mode, y sus tracks no traen género.
```bash
curl -i -X GET "http://localhost:8080/api/v1/music/search?q=Espresso%20Sabrina%20Carpenter&type=track" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "source": "spotify_fallback",
  "query": "Espresso Sabrina Carpenter",
  "type": "track",
  "count": 1,
  "results": [
    {
      "id": 1159765,
      "spotify_id": "2qSkXiYOKEzfk3vhRpIQUR",
      "track_name": "Espresso",
      "artist_name": "Sabrina Carpenter",
      "album_title": "Short n' Sweet",
      "year": 2024,
      "genre": null,
      "popularity": null,
      "duration_ms": 175459,
      "has_embedding": false,
      "pending_embedding": true
    }
  ]
}
```

---

#### 8. Detalle de Canción (`GET /api/v1/music/tracks/{id}`)
Devuelve los metadatos completos de una canción: atributos acústicos y si ya tiene vector de recomendación (`has_embedding`).

El parámetro `{id}` puede ser el ID de Spotify o el ID entero local. `audio_features` es `null` mientras la canción no tenga ninguno.

En las canciones importadas desde Spotify, los atributos vienen de FreqBlog: `speechiness`, `liveness` y `time_signature` quedan en `null`, porque FreqBlog no los mide, y en el vector se usa la mediana del catálogo.

```bash
curl -i -X GET "http://localhost:8080/api/v1/music/tracks/2qSkXiYOKEzfk3vhRpIQUR" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "id": 1159765,
  "spotify_id": "2qSkXiYOKEzfk3vhRpIQUR",
  "track_name": "Espresso",
  "artist_name": "Sabrina Carpenter",
  "album_title": "Short n' Sweet",
  "year": 2024,
  "genre": null,
  "popularity": null,
  "duration_ms": 175459,
  "has_embedding": true,
  "pending_embedding": false,
  "artist_id": "c1000000-0000-0000-0000-000000000001",
  "album_id": "d1000000-0000-0000-0000-000000000001",
  "isrc": "USUM72401994",
  "audio_features": {
    "danceability": 0.70,
    "energy": 0.77,
    "key": 0,
    "loudness": -5.48,
    "mode": 1,
    "speechiness": null,
    "acousticness": 0.11,
    "instrumentalness": 0.0,
    "liveness": null,
    "valence": 0.69,
    "tempo": 104.0,
    "time_signature": null
  }
}
```

---

#### 9. Detalle de Artista (`GET /api/v1/music/artists/{id}`)
Obtiene los datos del artista (géneros, popularidad, imagen) por ID de Spotify o UUID local. Los artistas que vienen del CSV no tienen `spotify_id` hasta que una búsqueda en Spotify los "reclama" por nombre. `popularity` es `null` si Spotify no la envía.

```bash
curl -i -X GET "http://localhost:8080/api/v1/music/artists/74KM79TiuVKeVCqs8QtB0B" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "id": "c1000000-0000-0000-0000-000000000001",
  "spotify_id": "74KM79TiuVKeVCqs8QtB0B",
  "name": "Sabrina Carpenter",
  "genres": ["dance pop", "pop"],
  "popularity": null,
  "image_url": "https://i.scdn.co/image/ab6761610000e5eb...",
  "created_at": "2026-10-31T12:00:00Z"
}
```

---

#### 10. Detalle de Álbum (`GET /api/v1/music/albums/{id}`)
Obtiene los datos del álbum, su carátula, su artista y sus pistas (las que ya están en el catálogo local), por ID de Spotify o UUID local.

```bash
curl -i -X GET "http://localhost:8080/api/v1/music/albums/2lIZef49vdWi0LMePaA4Sp" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Respuesta (`200 OK`):**
```json
{
  "id": "d1000000-0000-0000-0000-000000000001",
  "spotify_id": "2lIZef49vdWi0LMePaA4Sp",
  "title": "Short n' Sweet",
  "artist_id": "c1000000-0000-0000-0000-000000000001",
  "artist_name": "Sabrina Carpenter",
  "release_year": 2024,
  "album_type": "album",
  "image_url": "https://i.scdn.co/image/ab67616d0000b273...",
  "total_tracks": 12,
  "created_at": "2026-10-31T12:00:00Z",
  "tracks": [
    {
      "id": 1159765,
      "spotify_id": "2qSkXiYOKEzfk3vhRpIQUR",
      "track_name": "Espresso",
      "artist_name": "Sabrina Carpenter",
      "album_title": "Short n' Sweet",
      "year": 2024,
      "genre": null,
      "popularity": null,
      "duration_ms": 175459,
      "has_embedding": true,
      "pending_embedding": false
    }
  ]
}
```

---

### Formato Estándar de Errores

Todas las respuestas con error devuelven formato JSON uniforme:

```json
{
  "error": "descripción legible del error"
}
```

- `400 Bad Request`: parámetros faltantes, JSON malformado o campos con formato inválido (p. ej. `type` distinto de `track`/`artist`/`album`, `limit` fuera de 1–50 o un `{id}` que no es ni un ID local ni un ID de Spotify).
- `401 Unauthorized`: Token ausente, firma expirada, token en denylist o refresh token ya utilizado.
- `404 Not Found`: Canción, artista o álbum no encontrado.
- `409 Conflict`: `username` o `email` ya registrado.
- `500 Internal Server Error`: fallo de conexión con la base de datos u otro error interno.
- `502 Bad Gateway`: no hay resultados locales y Spotify falló (`servicio de música no disponible`).

Para una guía de verificación completa paso a paso con scripts automatizados de prueba, consulta [`implementacion.md`](implementacion.md#5-guía-de-verificación-y-pruebas-con-curl).
