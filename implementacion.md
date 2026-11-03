# Guía de Implementación del Backend de Junkbox

Este documento establece la especificación técnica, diseño de arquitectura y guía paso a paso para la implementación de las **Fases 1, 2 y 3** del backend de **Junkbox** en **Go**.

---

## Tabla de Contenidos

- [1. Resumen y Contexto del Proyecto](#1-resumen-y-contexto-del-proyecto)
- [2. Fase 1: Infraestructura de Datos, Goose, SQLC y Make](#2-fase-1-infraestructura-de-datos-goose-sqlc-y-make)
  - [2.1 Análisis Técnico: Reproducibilidad del Volumen Docker vs. Ingesta CSV](#21-análisis-técnico-reproducibilidad-del-volumen-docker-vs-ingesta-csv)
  - [2.2 Transición Segura del Contenedor Docker y Preservación de Datos](#22-transición-segura-del-contenedor-docker-y-preservación-de-datos)
  - [2.3 Archivo `db/docker-compose.yml`](#23-archivo-dbdocker-composeyml)
  - [2.4 Configuración de Entorno: `.env.template` y `.env`](#24-configuración-de-entorno-envtemplate-y-env)
  - [2.5 Automatización con `Makefile` (con Salvaguarda en `db-reset`)](#25-automatización-con-makefile-con-salvaguarda-en-db-reset)
  - [2.6 Migración Inicial de Goose: `init_song_metadata`](#26-migración-inicial-de-goose-init_song_metadata)
  - [2.7 Configuración del Compilador SQLC: `sqlc.yaml`](#27-configuración-del-compilador-sqlc-sqlcyaml)
  - [2.8 Especificación del Pipeline de Ingesta desde CSV](#28-especificación-del-pipeline-de-ingesta-desde-csv)
- [3. Fase 2: Usuarios, Autenticación (Sin Roles) y Seeding](#3-fase-2-usuarios-autenticación-sin-roles-y-seeding)
  - [3.1 Migración de Esquema de Usuarios: `create_users_table`](#31-migración-de-esquema-de-usuarios-create_users_table)
  - [3.2 Migración de Sesiones y Revocaciones: `create_tokens_and_revocations`](#32-migración-de-sesiones-y-revocaciones-create_tokens_and_revocations)
  - [3.3 Consultas SQL para SQLC (`db/queries/`)](#33-consultas-sql-para-sqlc-dbqueries)
  - [3.4 Poblado de Datos de Prueba (Seeding) y Truncate](#34-poblado-de-datos-de-prueba-seeding-y-truncate)
  - [3.5 Arquitectura de Código en Go (Clean Architecture / Puertos y Adaptadores)](#35-arquitectura-de-código-en-go-clean-architecture--puertos-y-adaptadores)
    - [3.5.1 Dominio y Casos de Uso (`internal/core/services/auth_service.go`)](#351-dominio-y-casos-de-uso-internalcoreservicesauth_servicego)
    - [3.5.2 Adaptadores Secundarios de Seguridad (`internal/adapters/secondary/security/`)](#352-adaptadores-secundarios-de-seguridad-internaladapterssecondarysecurity)
    - [3.5.3 Adaptadores Primarios HTTP Chi (`internal/adapters/primary/http/`)](#353-adaptadores-primarios-http-chi-internaladaptersprimaryhttp)
    - [3.5.4 Pruebas Unitarias (`make test`)](#354-pruebas-unitarias-make-test)
- [4. Fase 3: Integración de la API de Spotify, Catálogo y Worker de Embeddings](#4-fase-3-integración-de-la-api-de-spotify-catálogo-y-worker-de-embeddings)
  - [4.1 Arquitectura del Flujo de Búsqueda y Fallback (Cache-Through)](#41-arquitectura-del-flujo-de-búsqueda-y-fallback-cache-through)
  - [4.2 Esquema Relacional del Catálogo Musical](#42-esquema-relacional-del-catálogo-musical)
  - [4.3 Consultas SQL para SQLC (`db/queries/catalog.sql`)](#43-consultas-sql-para-sqlc-dbqueriescatalogsql)
  - [4.4 Cliente de Spotify e Importación Transaccional](#44-cliente-de-spotify-e-importación-transaccional)
  - [4.5 Worker de Embeddings con FreqBlog](#45-worker-de-embeddings-con-freqblog)
  - [4.6 Núcleo: `MusicService`](#46-núcleo-musicservice)
  - [4.7 Adaptadores HTTP Chi: Endpoints de Música Protegidos con JWT](#47-adaptadores-http-chi-endpoints-de-música-protegidos-con-jwt)
- [5. Guía de Verificación y Pruebas con cURL](#5-guía-de-verificación-y-pruebas-con-curl)

---

## 1. Resumen y Contexto del Proyecto

**Junkbox** es una plataforma orientada a la crítica musical, calificaciones en escala de 1 a 5 estrellas, perfiles sociales entre usuarios y entrega de recomendaciones personalizadas basadas en similitud vectorial con `pgvector` y la API de Spotify.

El backend sigue el patrón arquitectónico hexagonal probado en `t-box-backend`, adaptado a los requerimientos funcionales del WBS de Junkbox:

- Eliminación total del modelo de control de acceso basado en roles empresariales (**RBAC**) de flotas (`roles`, `permissions`, `fleets`).
- Modelo de usuario social con nombre visible, handle (@username) único y avatar.
- Flujo de autenticación completo con registro público (`/register`), inicio de sesión (`/login`), rotación segura de tokens (`/refresh`), revocación en denylist (`/logout`) y consulta de perfil (`/me`).
- Inclusión del catálogo base musical en la tabla `song_metadata` sobre la base de datos `junkbox_db`.

---

## 2. Fase 1: Infraestructura de Datos, Goose, SQLC y Make

### 2.1 Análisis Técnico: Reproducibilidad del Volumen Docker vs. Ingesta CSV

El usuario planteó una decisión arquitectónica clave: **No marcar el volumen como `external: true` en Docker Compose**, prefiriendo complementar la infraestructura con un script de migración e ingesta desde el archivo CSV de Spotify (con normalización y vectorización).

#### Justificación y Análisis de la Decisión:

1. **Portabilidad entre Entornos (Clonado Limpio):** Si un volumen se marca como `external: true`, cualquier colaborador del equipo o pipeline de CI/CD que clone el repositorio y ejecute `docker compose up` experimentará un error inmediato (`volume postgres-junkbox-data not found`), exigiendo crear volúmenes manualmente. Al declararlo como volumen gestionado estándar con nombre fijo (`name: postgres-junkbox-data`), Docker Compose lo reutiliza si ya existe en la máquina, o lo crea de manera transparente si el entorno es nuevo.
2. **Determinismo e Idempotencia:** Un volumen binario de base de datos es una "caja negra" sujeta a incompatibilidades de versiones de PostgreSQL y no puede versionarse en Git. Disponer de un pipeline de ingesta estructurado que procese el CSV crudo garantiza que el catálogo de 1.16M de canciones pueda reconstruirse en cualquier momento y en cualquier servidor de producción.
3. **Manejo en la Máquina Actual:** Para la máquina donde ya existe el volumen `postgres-junkbox-data` con datos cargados, declarar el volumen en Docker Compose con su nombre nativo garantiza que **no se pierda ni un solo registro**, mientras que el script de ingesta queda como herramienta estándar de inicialización para nuevos desarrolladores.

---

### 2.2 Transición Segura del Contenedor Docker y Preservación de Datos

El contenedor manual existente `postgres-junkbox` debe removerse y ceder su lugar al servicio orquestado por Docker Compose sin destruir el volumen `postgres-junkbox-data`.

#### Paso 1: Renombrar la Base de Datos Existente (de `spotify_rec` a `junkbox_db`)

Si el contenedor manual `postgres-junkbox` está encendido:

```bash
# Conectar a la base de datos de mantenimiento (postgres) y renombrar
docker exec -i postgres-junkbox psql -U postgres -d postgres -c "ALTER DATABASE spotify_rec RENAME TO junkbox_db;"
```

Renombrar la tabla `songs` a `song_metadata` **junto con su clave primaria, secuencia e índices**. Renombrar solo la tabla dejaría los índices como `idx_songs_*`, y la migración inicial (que usa `CREATE INDEX IF NOT EXISTS idx_song_metadata_*`) crearía índices duplicados sobre 1.16M filas:

```bash
docker exec -i postgres-junkbox psql -U postgres -d junkbox_db -v ON_ERROR_STOP=1 -1 -c "
ALTER TABLE songs RENAME TO song_metadata;
ALTER INDEX songs_pkey RENAME TO song_metadata_pkey;
ALTER SEQUENCE songs_id_seq RENAME TO song_metadata_id_seq;
ALTER INDEX idx_songs_track_id RENAME TO idx_song_metadata_track_id;
ALTER INDEX idx_songs_artist_name RENAME TO idx_song_metadata_artist_name;
ALTER INDEX idx_songs_genre RENAME TO idx_song_metadata_genre;
ALTER INDEX idx_songs_embedding_ivfflat RENAME TO idx_song_metadata_embedding_ivfflat;"
```

> **Credenciales del volumen existente:** el contenedor original se creó con `POSTGRES_USER=postgres` y `POSTGRES_PASSWORD=postgres`. Como el volumen ya está inicializado, Postgres ignora esas variables en arranques posteriores, así que el `.env` de esta máquina debe usar `postgres` como contraseña (ver §2.4).

#### Paso 2: Detener y Eliminar ÚNICAMENTE el Contenedor (Sin tocar el volumen)

```bash
# Detiene el contenedor
docker stop postgres-junkbox

# Elimina el contenedor manual (NUNCA usar el flag -v)
docker rm postgres-junkbox
```

> **Verificación de Seguridad:** Ejecuta `docker volume ls | grep postgres-junkbox-data`. El volumen debe permanecer intacto.

> **Aviso esperado de Compose:** al ejecutar `make db-up` sobre este volumen, Docker Compose muestra `volume "postgres-junkbox-data" already exists but was not created by Docker Compose`. Es inofensivo: Compose reutiliza el volumen tal cual. No se marca como `external: true` por las razones de §2.1.

---

### 2.3 Archivo `db/docker-compose.yml`

Ubicación: `db/docker-compose.yml`

```yaml
name: junkbox-backend

services:
  postgres:
    image: pgvector/pgvector:pg16
    container_name: postgres-junkbox
    restart: unless-stopped
    env_file:
      - ../.env
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - postgres-junkbox-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
      interval: 5s
      timeout: 5s
      retries: 5
      start_period: 20s

volumes:
  postgres-junkbox-data:
    name: postgres-junkbox-data
```

---

### 2.4 Configuración de Entorno: `.env.template` y `.env`

Ubicación: `.env.template`

```bash
# Configuración del Motor PostgreSQL
# Nota: si el volumen postgres-junkbox-data ya está inicializado, POSTGRES_USER y
# POSTGRES_PASSWORD deben coincidir con los originales (Postgres los ignora tras el primer arranque).
POSTGRES_DB="junkbox_db"
POSTGRES_USER="postgres"
POSTGRES_PASSWORD="junkbox_secure_dev_password"
POSTGRES_PORT=5432

# Cadena de conexión para Goose y la aplicación Go
DATABASE_URL="postgres://postgres:junkbox_secure_dev_password@localhost:5432/junkbox_db?sslmode=disable"

# Secreto HMAC-SHA256 para firmar Access Tokens JWT (mínimo 32 caracteres)
# Generar con: openssl rand -hex 32
JWT_SECRET="generate_a_secure_32_byte_hex_secret_here"

# Duración de tokens
ACCESS_TOKEN_TTL="15m"
REFRESH_TOKEN_TTL="168h" # 7 días

# Dirección de escucha del servidor HTTP
HTTP_ADDR=":8080"

# Spotify Web API (flujo Client Credentials). Opcional: sin credenciales la
# búsqueda solo consulta el catálogo local.
SPOTIFY_CLIENT_ID=""
SPOTIFY_CLIENT_SECRET=""

# FreqBlog (audio features que reemplazan al /v1/audio-features de Spotify).
# Opcional: sin clave, las canciones importadas quedan con pending_embedding.
FREQBLOG_API_KEY=""

# Puntaje mínimo (strict_word_similarity, 0 a 1) del mejor resultado local para
# no consultar Spotify. Con 0.8, a una consulta de tres palabras no le puede
# faltar ninguna en el resultado local.
SEARCH_FALLBACK_THRESHOLD=0.8
```

`.env` se obtiene copiando la plantilla. En un entorno limpio basta con cambiar la contraseña y `JWT_SECRET` (las variables de Spotify y FreqBlog son opcionales; ver §4.7); en la máquina con el volumen preexistente `POSTGRES_PASSWORD` y `DATABASE_URL` deben usar la contraseña original (`postgres`).

`.env` y `dataset.csv` (~175 MB, por encima del límite de 100 MB de GitHub) están listados en `.gitignore`; el CSV se distribuye por separado.

---

### 2.5 Automatización con `Makefile` (con Salvaguarda en `db-reset`)

Ubicación: `Makefile`

Se introduce una confirmación obligatoria en `db-reset` para evitar la pérdida no deseada del catálogo musical indexado, y el target `ingest-songs` para cargar el catálogo desde el CSV (§2.8). El listado refleja el estado actual: `backfill-artists` y `DATASET ?= data/spotify_data.csv` se agregaron en la Fase 3 (§4.2).

```makefile
.PHONY: db-up db-down db-ps db-logs db-reset db-shell migrate-up migrate-down migrate-status migrate-create seed-create seed-up seed-down ingest-songs backfill-artists sqlc build vet test run-server

DB_COMPOSE := docker compose -f db/docker-compose.yml --env-file .env
GOOSE_CMD := set -a; . ./.env; set +a; goose -dir db/migrations postgres "$$DATABASE_URL"

SEED_DIR := db/seed
TRUNCATE_SQL := db/scripts/truncate_all.sql
INGEST_SQL := db/scripts/ingest_songs.sql
BACKFILL_SQL := db/scripts/backfill_artists.sql
DATASET ?= data/spotify_data.csv
PSQL_CMD := set -a; . ./.env; set +a; $(DB_COMPOSE) exec -T postgres psql -v ON_ERROR_STOP=1 --single-transaction -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"

db-up:
	@$(DB_COMPOSE) up -d --wait

db-down:
	@$(DB_COMPOSE) down

db-ps:
	@$(DB_COMPOSE) ps

db-logs:
	@$(DB_COMPOSE) logs -f --tail=50

# db-reset exige confirmación interactiva para evitar borrar por accidente el dataset de canciones
db-reset:
	@printf "⚠️  PELIGRO: Esto destruirá el contenedor Y EL VOLUMEN con todo el dataset de canciones. ¿Estás absolutamente seguro? [y/N] "; \
	read ans; \
	case "$$ans" in [yY]|[yY][eE][sS]) ;; *) echo "Operación cancelada."; exit 1 ;; esac; \
	$(DB_COMPOSE) down -v
	@$(DB_COMPOSE) up -d --wait

db-shell:
	@set -a; . ./.env; set +a; $(DB_COMPOSE) exec postgres psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"

migrate-up:
	@$(GOOSE_CMD) up

migrate-down:
	@$(GOOSE_CMD) down

migrate-status:
	@$(GOOSE_CMD) status

migrate-create:
	@if [ -z "$(name)" ]; then echo "Uso: make migrate-create name=nombre_de_migracion"; exit 1; fi
	@goose -dir db/migrations postgres "placeholder" create $(name) sql

seed-create:
	@if [ -z "$(name)" ]; then echo "Uso: make seed-create name=nombre"; exit 1; fi
	@f="$(SEED_DIR)/$$(date +%Y%m%d%H%M%S)_$(name).sql"; \
	{ \
		echo "-- Seed: $(name)"; \
		echo "-- Aplicado por 'make seed-up' en orden alfabético (LC_ALL=C)."; \
		echo "-- Recomendado: usar ON CONFLICT para idempotencia."; \
		echo ""; \
	} > "$$f"; \
	echo "Creado $$f"

seed-up:
	@files=$$(ls -1 $(SEED_DIR)/*.sql 2>/dev/null | LC_ALL=C sort); \
	if [ -z "$$files" ]; then echo "No hay archivos de seed en $(SEED_DIR)."; exit 0; fi; \
	for f in $$files; do \
		echo "==> $$f"; \
		$(PSQL_CMD) < "$$f" || exit 1; \
	done
	@echo "Seeds aplicados correctamente."

seed-down:
	@printf "Esto vaciará las tablas de datos (conservando el historial de migraciones). ¿Continuar? [y/N] "; \
	read ans; \
	case "$$ans" in [yY]|[yY][eE][sS]) ;; *) echo "Cancelado."; exit 1 ;; esac; \
	$(PSQL_CMD) < $(TRUNCATE_SQL)

# Carga el catálogo desde $(DATASET): el script se copia al contenedor y el CSV entra por stdin (pstdin)
ingest-songs:
	@if [ ! -f "$(DATASET)" ]; then echo "No se encontró $(DATASET). Uso: make ingest-songs DATASET=ruta/al.csv"; exit 1; fi
	@$(DB_COMPOSE) cp $(INGEST_SQL) postgres:/tmp/ingest_songs.sql
	@$(PSQL_CMD) -f /tmp/ingest_songs.sql < "$(DATASET)"

# Crea los artistas y enlaza song_metadata.artist_id en un volumen cargado antes
# de la Fase 3 (ingest-songs ya lo hace en la carga). Idempotente.
backfill-artists:
	@$(PSQL_CMD) < $(BACKFILL_SQL)

sqlc:
	@sqlc generate

build:
	@go build ./...

vet:
	@go vet ./...

test:
	@go test ./...

run-server:
	@go run . server --config .env
```

---

### 2.6 Migración Inicial de Goose: `init_song_metadata`

Ubicación: `db/migrations/20261027000001_init_song_metadata.sql`

Esta migración asegura que la extensión `vector` esté presente y define la estructura canónica de `song_metadata` con cláusulas defensivas `IF NOT EXISTS` para convivir armoniosamente tanto en entornos limpios como en el volumen preexistente:

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS song_metadata (
    id SERIAL PRIMARY KEY,
    raw_index INT,
    artist_name TEXT,
    track_name TEXT,
    track_id VARCHAR(64),
    popularity INT,
    year INT,
    genre VARCHAR(64),
    danceability DOUBLE PRECISION,
    energy DOUBLE PRECISION,
    key INT,
    loudness DOUBLE PRECISION,
    mode INT,
    speechiness DOUBLE PRECISION,
    acousticness DOUBLE PRECISION,
    instrumentalness DOUBLE PRECISION,
    liveness DOUBLE PRECISION,
    valence DOUBLE PRECISION,
    tempo DOUBLE PRECISION,
    duration_ms INT,
    time_signature INT,
    -- [danceability, energy, loudness_norm, speechiness, acousticness, instrumentalness, liveness, valence, tempo_norm, mode]
    embedding vector(10)
);

CREATE INDEX IF NOT EXISTS idx_song_metadata_artist_name ON song_metadata(artist_name);
CREATE INDEX IF NOT EXISTS idx_song_metadata_track_id ON song_metadata(track_id);
CREATE INDEX IF NOT EXISTS idx_song_metadata_genre ON song_metadata(genre);

-- El índice vectorial IVFFlat (idx_song_metadata_embedding_ivfflat) no se crea aquí:
-- IVFFlat calcula sus centroides con los datos existentes, así que se construye en
-- db/scripts/ingest_songs.sql después de la carga masiva.

-- +goose Down
-- Intencionalmente no se elimina song_metadata: un `make migrate-down` de más no debe
-- destruir el catálogo de 1.16M canciones. Para borrarlo usar `make db-reset` (con confirmación).
SELECT 1;
```

Decisiones de diseño:

- **Índice IVFFlat fuera de la migración:** IVFFlat agrupa los vectores en `lists` centroides calculados a partir de las filas presentes al crear el índice. Crearlo sobre una tabla vacía (entorno limpio) produce centroides sin sentido y un recall muy bajo. Por eso el índice se construye al final de la ingesta (§2.8). En el volumen existente el índice ya está presente con el nombre `idx_song_metadata_embedding_ivfflat` (§2.2).
- **`Down` sin `DROP TABLE`:** la reversión de esta migración no borra el catálogo; la única vía destructiva es `make db-reset`, que pide confirmación.
- Sobre el volumen existente, todas las sentencias `IF NOT EXISTS` son no-op: la migración solo registra la versión en `goose_db_version`.

---

### 2.7 Configuración del Compilador SQLC: `sqlc.yaml`

Ubicación: `sqlc.yaml`

Mapea tipos de datos específicos de PostgreSQL a tipos nativos de Go (UUIDs a `github.com/google/uuid` y omisión de punteros innecesarios):

```yaml
version: "2"
sql:
  - engine: "postgresql"
    schema: "db/migrations"
    queries: "db/queries"
    gen:
      go:
        package: "sqlc"
        out: "internal/store/sqlc"
        sql_package: "database/sql"
        emit_prepared_queries: false
        emit_interface: true
        emit_exact_table_names: false
        emit_json_tags: true
        json_tags_case_style: "snake"
        overrides:
          # UUIDs no nulos
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
            nullable: false
          # UUIDs nulos (ej. refresh_tokens.replaced_by)
          - db_type: "uuid"
            go_type:
              import: "github.com/google/uuid"
              type: "UUID"
              pointer: true
            nullable: true
```

`sqlc generate` falla si `db/queries/` no contiene consultas, así que la Fase 1 incluye `db/queries/song_metadata.sql` con dos consultas iniciales (`GetSongByTrackID`, `CountSongs`). Omiten la columna `embedding`, porque sqlc no conoce el tipo `vector` y lo mapea a `interface{}`:

```sql
-- name: GetSongByTrackID :one
SELECT id, raw_index, artist_name, track_name, track_id, popularity, year, genre,
       danceability, energy, key, loudness, mode, speechiness, acousticness,
       instrumentalness, liveness, valence, tempo, duration_ms, time_signature
FROM song_metadata
WHERE track_id = $1
LIMIT 1;

-- name: CountSongs :one
SELECT COUNT(*) FROM song_metadata;
```

---

### 2.8 Especificación del Pipeline de Ingesta desde CSV

Ubicación: `db/scripts/ingest_songs.sql` · Ejecución: `make ingest-songs` (o `make ingest-songs DATASET=ruta/al.csv`; por defecto `data/spotify_data.csv`)

El pipeline es SQL puro ejecutado por `psql` dentro del contenedor, sin dependencias de Python ni de Go. Reproduce exactamente el procedimiento con el que se cargó el volumen original:

1. **Requisito:** haber ejecutado `make migrate-up`, que crea la tabla vacía.
2. **Guardia de idempotencia:** si `song_metadata` ya tiene filas, el script aborta (`RAISE EXCEPTION`) y no duplica el catálogo.
3. **Carga masiva:** `make ingest-songs` copia el script al contenedor (`docker compose cp`) y envía el CSV por la entrada estándar. `\copy ... FROM pstdin` lo carga mediante `COPY`. La primera columna del CSV (sin nombre) corresponde a `raw_index`.
4. **Artistas (desde la Fase 3):** antes de vectorizar, se crea un artista por cada `artist_name` distinto. El mismo `UPDATE` del paso 5 enlaza `artist_id`, así la tabla se reescribe una sola vez (§4.2).
5. **Vectorización:** un `UPDATE` arma el vector de 10 dimensiones `[danceability, energy, loudness_norm, speechiness, acousticness, instrumentalness, liveness, valence, tempo_norm, mode]`, con MinMax sobre los límites globales del catálogo:
   - `loudness_norm = (loudness + 58.1) / (6.172 + 58.1)` (rango observado: −58.1 a 6.172 dB).
   - `tempo_norm = tempo / 250` (rango 0 a 250 BPM).
   - El resto de las variables ya está en $[0, 1]$; `mode` (0/1) se convierte a float.
6. **Índices:** con los datos cargados se construye `idx_song_metadata_embedding_ivfflat` (`ivfflat`, `vector_cosine_ops`, `lists = 1000`, `maintenance_work_mem = 1GB`). Desde la Fase 3 también se reconstruye `idx_song_metadata_search_trgm` (GIN de búsqueda), que se quita al empezar la carga porque mantenerlo fila por fila es mucho más lento.
7. Todo corre en una sola transacción (`--single-transaction`, `ON_ERROR_STOP=1`): si algo falla, la tabla queda vacía.

> **Verificación:** en un Postgres temporal, el script de la Fase 1 cargó 1.159.764 filas en unos 50 s, y el checksum de `(raw_index, track_id, embedding)` coincidió exactamente con el del volumen original. Con los artistas y el GIN de la Fase 3, la carga completa tarda **1 min 37 s**.

```sql
-- Ingesta del catálogo musical desde dataset.csv (recibido por la entrada estándar de psql).
-- Uso: make ingest-songs   (requiere haber ejecutado make migrate-up)

-- Aborta si el catálogo ya está cargado, para no duplicar filas
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM song_metadata) THEN
    RAISE EXCEPTION 'song_metadata ya contiene datos; ingesta cancelada';
  END IF;
END
$$;

-- El índice GIN de búsqueda (migración 20261027000004) se reconstruye al final:
-- mantenerlo fila por fila durante la carga y el UPDATE es mucho más lento.
DROP INDEX IF EXISTS idx_song_metadata_search_trgm;

\echo 'Cargando CSV en song_metadata...'
\copy song_metadata (raw_index, artist_name, track_name, track_id, popularity, year, genre, danceability, energy, key, loudness, mode, speechiness, acousticness, instrumentalness, liveness, valence, tempo, duration_ms, time_signature) FROM pstdin WITH (FORMAT csv, HEADER true)

-- Un artista por cada artist_name distinto (igual que db/scripts/backfill_artists.sql),
-- para enlazar artist_id en el mismo UPDATE que calcula los embeddings y no
-- reescribir la tabla dos veces.
\echo 'Creando artistas...'
INSERT INTO artists (name, genres)
SELECT s.artist_name,
       coalesce(array_agg(DISTINCT s.genre) FILTER (WHERE s.genre IS NOT NULL), '{}')
FROM song_metadata s
WHERE s.artist_name IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM artists a WHERE a.name = s.artist_name)
GROUP BY s.artist_name;

-- Embedding de 10 dimensiones con MinMax sobre los límites globales del catálogo:
-- loudness en [-58.1, 6.172] dB y tempo en [0, 250] BPM; el resto ya está en [0, 1]
\echo 'Calculando embeddings y enlazando artistas...'
UPDATE song_metadata s
SET artist_id = (
        SELECT a.id FROM artists a
        WHERE a.name = s.artist_name
        ORDER BY (a.spotify_id IS NOT NULL), a.created_at
        LIMIT 1
    ),
    embedding = ARRAY[
    danceability,
    energy,
    (loudness - (-58.1)) / (6.172 - (-58.1)),
    speechiness,
    acousticness,
    instrumentalness,
    liveness,
    valence,
    (tempo - 0.0) / 250.0,
    mode::float
]::vector
WHERE embedding IS NULL;

ANALYZE artists;
ANALYZE song_metadata;

\echo 'Construyendo índice IVFFlat...'
SET maintenance_work_mem = '1GB';
CREATE INDEX IF NOT EXISTS idx_song_metadata_embedding_ivfflat ON song_metadata
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 1000);

\echo 'Construyendo índice de búsqueda por trigramas...'
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);

\echo 'Ingesta completada.'
```

---

## 3. Fase 2: Usuarios, Autenticación (Sin Roles) y Seeding

### 3.1 Migración de Esquema de Usuarios: `create_users_table`

Ubicación: `db/migrations/20261027000002_create_users_table.sql`

Define el usuario adaptado a la red social de música: con handle `@username` único, nombre visible (`display_name`) y foto de perfil (`avatar_url`), sin jerarquía de roles.

No se crean `idx_users_email` ni `idx_users_username`: las restricciones `UNIQUE` ya generan esos índices B-tree, y duplicarlos solo encarecería las escrituras. Username y email se guardan normalizados en minúsculas (lo hace `AuthService`, §3.5.1), así que la unicidad es efectivamente insensible a mayúsculas.

```sql
-- +goose Up
-- Las restricciones UNIQUE de username y email ya crean sus índices B-tree.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    hashed_password TEXT NOT NULL,
    display_name VARCHAR(100),
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trigger_users_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS users;
```

---

### 3.2 Migración de Sesiones y Revocaciones: `create_tokens_and_revocations`

Ubicación: `db/migrations/20261027000003_create_tokens_and_revocations.sql`

Establece las tres estructuras de seguridad criptográfica de `t-box`:

1. `refresh_tokens`: Tokens opacos almacenados como hash SHA-256 con detección de reutilización y familias de tokens (`family_id`).
2. `revoked_access_tokens`: Denylist por `jti` para invalidación inmediata de JWT al hacer logout.
3. `user_access_revocations`: Marca temporal de corte global (`revoked_before`) para cerrar todas las sesiones del usuario a la vez.

```sql
-- +goose Up
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash CHAR(64) UNIQUE NOT NULL,
    family_id UUID NOT NULL,
    replaced_by UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT refresh_tokens_revoked_state
        CHECK (
            (revoked AND revoked_at IS NOT NULL)
            OR (NOT revoked AND revoked_at IS NULL)
        )
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_family_id ON refresh_tokens(family_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

CREATE TABLE revoked_access_tokens (
    jti UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_revoked_access_tokens_expires_at ON revoked_access_tokens(expires_at);

CREATE TABLE user_access_revocations (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    revoked_before TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS user_access_revocations;
DROP TABLE IF EXISTS revoked_access_tokens;
DROP TABLE IF EXISTS refresh_tokens;
```

---

### 3.3 Consultas SQL para SQLC (`db/queries/`)

#### 1. Usuarios: `db/queries/users.sql`

```sql
-- name: CreateUser :one
INSERT INTO users (
    username, email, hashed_password, display_name, avatar_url
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING id, username, email, display_name, avatar_url, created_at, updated_at;

-- name: GetUserByEmail :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByUsername :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE id = $1;

-- name: ExistsUserByEmailOrUsername :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE email = $1 OR username = $2
);
```

#### 2. Refresh Tokens: `db/queries/refresh_tokens.sql`

```sql
-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (
    user_id, token_hash, family_id, expires_at
) VALUES (
    $1, $2, $3, $4
)
RETURNING id, user_id, token_hash, family_id, replaced_by, revoked, revoked_at, expires_at, created_at;

-- name: GetRefreshTokenByHash :one
SELECT id, user_id, token_hash, family_id, replaced_by, revoked, revoked_at, expires_at, created_at
FROM refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE token_hash = $1 AND revoked = FALSE;

-- name: SetRefreshTokenReplacedBy :exec
UPDATE refresh_tokens
SET replaced_by = $2
WHERE id = $1;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE family_id = $1 AND revoked = FALSE;

-- name: RevokeAllRefreshTokensByUser :exec
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE user_id = $1 AND revoked = FALSE;
```

#### 3. Denylist de Access Tokens: `db/queries/revoked_tokens.sql`

```sql
-- name: RevokeAccessToken :exec
INSERT INTO revoked_access_tokens (jti, user_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsAccessTokenRevoked :one
SELECT EXISTS (
    SELECT 1 FROM revoked_access_tokens WHERE jti = $1
);

-- name: RevokeAllUserAccessTokens :exec
INSERT INTO user_access_revocations (user_id, revoked_before)
VALUES ($1, NOW())
ON CONFLICT (user_id) DO UPDATE SET revoked_before = EXCLUDED.revoked_before;

-- name: GetUserAccessRevocation :one
SELECT revoked_before
FROM user_access_revocations
WHERE user_id = $1;

-- name: PurgeExpiredRevokedTokens :execrows
DELETE FROM revoked_access_tokens
WHERE expires_at < NOW();
```

---

### 3.4 Poblado de Datos de Prueba (Seeding) y Truncate

#### Archivo de Seed: `db/seed/20261027000001_users.sql`

Genera los 4 usuarios correspondientes a los integrantes del equipo descritos en el reporte oficial del proyecto, con contraseñas hasheadas en **bcrypt** (coste 12) usando la clave común de desarrollo `Password123!`:

```sql
-- Seed de Usuarios del Equipo Junkbox
-- Hash bcrypt (coste 12) para 'Password123!': $2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe

INSERT INTO users (id, username, email, hashed_password, display_name, avatar_url) VALUES
('b1000000-0000-0000-0000-000000000001', 'cesar_beltran', 'cesar@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'César David Beltrán Loredo', 'https://api.dicebear.com/7.x/bottts/svg?seed=cesar'),
('b2000000-0000-0000-0000-000000000002', 'carlos_andrade', 'carlos@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'Carlos Eduardo Andrade Gámez', 'https://api.dicebear.com/7.x/bottts/svg?seed=carlos'),
('b3000000-0000-0000-0000-000000000003', 'omar_delangel', 'omar@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'Omar Del Angel Villa', 'https://api.dicebear.com/7.x/bottts/svg?seed=omar'),
('b4000000-0000-0000-0000-000000000004', 'raymundo_sanchez', 'raymundo@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'José Raymundo Sánchez Alcalde', 'https://api.dicebear.com/7.x/bottts/svg?seed=raymundo')
ON CONFLICT (id) DO UPDATE SET
    username = EXCLUDED.username,
    email = EXCLUDED.email,
    hashed_password = EXCLUDED.hashed_password,
    display_name = EXCLUDED.display_name,
    avatar_url = EXCLUDED.avatar_url;
```

> El test `TestSeedHashCorrespondeAPassword123` (`internal/adapters/secondary/security/jwt_test.go`) verifica que este hash corresponde a `Password123!`. Gracias a `ON CONFLICT (id) DO UPDATE`, `make seed-up` es idempotente.

#### Script de Truncate Seguro: `db/scripts/truncate_all.sql`

Creado en la Fase 1 y usado por `make seed-down`. Vacía las tablas de negocio pero **omite** `goose_db_version` y el catálogo (`song_metadata` y, desde la Fase 3, `artists` y `albums`), para no perder las 1.16M canciones al vaciar los datos de prueba:

```sql
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public'
      -- artists y albums se excluyen: TRUNCATE ... CASCADE sobre ellas vaciaría
      -- también song_metadata a través de sus claves foráneas.
      AND tablename NOT IN ('goose_db_version', 'song_metadata', 'artists', 'albums')
    ORDER BY tablename
  LOOP
    EXECUTE format('TRUNCATE TABLE %I RESTART IDENTITY CASCADE', r.tablename);
  END LOOP;
END
$$;
```

---

### 3.5 Arquitectura de Código en Go (Clean Architecture / Puertos y Adaptadores)

El código parte del de `t-box-backend` (sin RBAC, flotas, MQTT ni SSE). Dependencias: `go-chi/chi/v5`, `golang-jwt/jwt/v5`, `google/uuid`, `jackc/pgx/v5` (driver de `database/sql`), `joho/godotenv`, `spf13/cobra` y `golang.org/x/crypto/bcrypt`.

Estructura de directorios:

```text
junkbox-backend/
├── .env.template                # plantilla de variables de entorno (copiar a .env)
├── main.go                      # invoca cmd.Execute()
├── cmd/
│   ├── root.go                  # CLI Cobra, flag --config
│   └── server.go                # subcomando `server`: arma dependencias, purga denylist, shutdown elegante
├── config/
│   └── config.go                # DATABASE_URL, JWT_SECRET (≥32), ACCESS/REFRESH_TOKEN_TTL, HTTP_ADDR; F3: SPOTIFY_*, FREQBLOG_API_KEY, SEARCH_FALLBACK_THRESHOLD
├── db/
│   ├── docker-compose.yml
│   ├── migrations/
│   ├── queries/                 # song_metadata.sql, users.sql, refresh_tokens.sql, revoked_tokens.sql, catalog.sql (F3)
│   ├── scripts/                 # ingest_songs.sql, truncate_all.sql, backfill_artists.sql (F3)
│   └── seed/
├── internal/
│   ├── adapters/
│   │   ├── primary/
│   │   │   └── http/
│   │   │       ├── dto/
│   │   │       │   └── auth_dto.go
│   │   │       ├── middleware/
│   │   │       │   ├── auth_middleware.go        # AuthMiddleware + RevocationChecker
│   │   │       │   └── auth_middleware_test.go
│   │   │       ├── handlers_auth.go
│   │   │       ├── handlers_music.go             # F3: /music/search, tracks, artists, albums
│   │   │       └── router.go
│   │   └── secondary/
│   │       ├── freqblog/                         # F3: audio features (reemplazo de Spotify)
│   │       │   ├── client.go
│   │       │   └── client_test.go
│   │       ├── postgres/
│   │       │   ├── catalog_importer.go           # F3: importación transaccional desde Spotify
│   │       │   └── db.go                         # pool database/sql + pgx, con ping
│   │       ├── security/
│   │       │   ├── hasher.go
│   │       │   ├── jwt.go
│   │       │   └── jwt_test.go
│   │       └── spotify/                          # F3: Client Credentials + /v1/search
│   │           ├── client.go
│   │           └── client_test.go
│   ├── core/
│   │   ├── domain/
│   │   │   ├── music.go         # F3: Track, Artist, Album, SearchResult, Imported*, EmbeddingJob
│   │   │   └── user.go          # UserProfile, TokenPair, AccessClaims, errores de dominio
│   │   ├── ports/
│   │   │   ├── auth_ports.go    # UserStore, TokenStore, RevocationStore, PasswordHasher, TokenManager
│   │   │   └── music_ports.go   # F3: CatalogStore, CatalogImporter, MusicProvider, AudioFeaturesProvider, EmbeddingStore, EmbeddingQueue
│   │   └── services/
│   │       ├── audio_features_worker.go       # F3
│   │       ├── audio_features_worker_test.go
│   │       ├── auth_service.go
│   │       ├── auth_service_test.go
│   │       ├── music_service.go               # F3
│   │       └── music_service_test.go
│   └── store/
│       └── sqlc/                # generado por `make sqlc`
├── Makefile
├── README.md
├── implementacion.md
├── sqlc.yaml
└── go.mod
```

Las dependencias apuntan hacia el núcleo. `domain` no importa nada del proyecto y `services` solo depende de `domain`, `ports` y de los tipos de fila de `sqlc`. Los claims del JWT viajan como `domain.AccessClaims`, así que el núcleo no conoce `golang-jwt` (en t-box el servicio importaba el adaptador `security`). `*sqlc.Queries` implementa los tres stores.

---

#### 3.5.1 Dominio y Casos de Uso (`internal/core/services/auth_service.go`)

Puertos (`internal/core/ports/auth_ports.go`):

- `UserStore`: `CreateUser`, `GetUserByEmail`, `GetUserByUsername`, `GetUserByID`, `ExistsUserByEmailOrUsername`. Las tres `GetUserBy*` seleccionan todas las columnas, así que sqlc devuelve el mismo tipo `sqlc.User` y no hacen falta conversores.
- `TokenStore`: operaciones de `refresh_tokens`.
- `RevocationStore`: operaciones de `revoked_access_tokens` y `user_access_revocations`.
- `PasswordHasher`: `Hash` / `Compare` (bcrypt).
- `TokenManager`: `GenerateAccess(userID, username)`, `ParseAccess(token) (*domain.AccessClaims, error)`, `TTL()`.

Errores de dominio (`internal/core/domain/user.go`): `ErrInvalidCredentials`, `ErrUserAlreadyExists`, `ErrInvalidRefresh`, `ErrRefreshReuse` y el tipo `*ValidationError`, cuyo mensaje se devuelve al cliente.

**Métodos de `AuthService`:**

1. `Register(ctx, RegisterInput) (*TokenPair, *UserProfile, error)`:
   - Normaliza: `trim` en todos los campos; username y email a minúsculas.
   - Valida y devuelve `ValidationError` sin tocar la BD si algo no cumple:
     - username con `^[a-z0-9_.]{3,50}$`;
     - email parseable por `net/mail`, sin nombre visible y de ≤255 caracteres;
     - password de al menos 8 caracteres y como máximo 72 bytes (bcrypt ignora lo que pase de 72), con al menos una letra, un número y un carácter especial (puntuación o símbolo Unicode);
     - `display_name` de ≤100 caracteres;
     - `avatar_url`, si viene, debe ser una URL `http(s)` con host.
   - Comprueba la unicidad con `ExistsUserByEmailOrUsername`. Si dos registros simultáneos pasan esa comprobación, el `INSERT` falla con SQLSTATE `23505`, que también se traduce a `ErrUserAlreadyExists`. Se detecta con `errors.As` contra `interface{ SQLState() string }`, que implementa `*pgconn.PgError`, sin acoplar el núcleo a pgx.
   - Genera el hash bcrypt (coste 12), inserta el usuario (`display_name`/`avatar_url` vacíos → `NULL`) y emite el `TokenPair` inicial con un `family_id` nuevo.
2. `Login(ctx, identifier, password) (*TokenPair, *UserProfile, error)`:
   - Normaliza el `identifier` a minúsculas y busca por email si contiene `@`, o por username si no.
   - Si el usuario no existe, compara igualmente la contraseña contra un hash bcrypt fijo, para que la respuesta tarde lo mismo y no revele qué cuentas existen. Usuario inexistente y contraseña incorrecta devuelven el mismo `ErrInvalidCredentials`.
   - Emite el par de tokens con una familia nueva.
3. `Refresh(ctx, rawRefreshToken) (*TokenPair, error)`:
   - Calcula el SHA-256 del token y lo busca en `refresh_tokens`. Si no existe o expiró, devuelve `ErrInvalidRefresh`.
   - **Detección de reuso:** si el token ya estaba revocado, revoca toda la familia (`RevokeRefreshTokenFamily`) y devuelve `ErrRefreshReuse`.
   - **Carrera de rotación doble:** si `RevokeRefreshToken` afecta 0 filas (otra petición lo rotó entre la lectura y la revocación), se trata como reuso.
   - Si es válido: lo revoca, emite un refresh token nuevo con el mismo `family_id`, lo enlaza en `replaced_by` y emite un access token nuevo con el username actual.
4. `Logout(ctx, claims, rawRefreshToken, allSessions) error`:
   - Revoca el refresh token enviado (idempotente).
   - Agrega el `jti` del access token actual a `revoked_access_tokens`, con su `exp` para poder purgarlo.
   - Si `allSessions == true`, registra un corte global en `user_access_revocations` y revoca todos los refresh tokens del usuario.
   - Enviar un refresh token ya revocado después del logout dispara la detección de reuso y revoca su familia; es intencional.
5. `Me(ctx, claims) (*UserProfile, error)`: resuelve el perfil a partir de `claims.UserID`. Si el usuario ya no existe, devuelve `ErrInvalidCredentials` (401).

`UserProfile` (JSON): `id`, `username`, `email`, `display_name` (string o `null`), `avatar_url` (string o `null`) y `created_at`.

---

#### 3.5.2 Adaptadores Secundarios de Seguridad (`internal/adapters/secondary/security/`)

1. **`hasher.go`**:
   Implementa `PasswordHasher` con `bcrypt.GenerateFromPassword(pwd, 12)` (`DefaultBcryptCost`) y `bcrypt.CompareHashAndPassword(hash, pwd)`.
2. **`jwt.go`**:
   Genera tokens JWT HS256 con estos claims:
   - `sub`: `user_id` (UUID).
   - `username`: handle único.
   - `jti`: UUID aleatorio, distinto en cada token.
   - `iat`: marca de tiempo de emisión.
   - `exp`: expiración (`ACCESS_TOKEN_TTL`, por defecto 15 minutos).
   - `iss`: `junkbox-backend`.

   Al validar exige `alg=HS256` (rechaza `none` y otros métodos), el emisor correcto, `exp` e `iat` presentes, `sub` como UUID y un `jti` no vacío. El resultado se convierte a `domain.AccessClaims`.

---

#### 3.5.3 Adaptadores Primarios HTTP Chi (`internal/adapters/primary/http/`)

1. **DTOs (`dto/auth_dto.go`):**
   - `RegisterRequest`: `username`, `email`, `password`, `display_name` (opcional), `avatar_url` (opcional).
   - `LoginRequest`: `identifier` (email o username), `password`.
   - `RefreshRequest`: `refresh_token`.
   - `LogoutRequest`: `refresh_token`, `all_sessions` (booleano opcional).
   - `AuthResponse`: `access_token`, `refresh_token`, `expires_in`, `user`. Lo devuelven register y login; refresh devuelve solo el par de tokens.
2. **Handlers (`handlers_auth.go`):**
   - El cuerpo se limita a 1 MiB y se rechazan los campos desconocidos (400 `solicitud inválida`).
   - Códigos de éxito: `register` → 201; `login`, `refresh`, `logout` y `me` → 200.
   - Mapeo de errores de dominio:

     | Error | HTTP | Mensaje |
     |---|---|---|
     | campos requeridos vacíos / `*ValidationError` | 400 | detalle de la validación |
     | `ErrInvalidCredentials` | 401 | `credenciales inválidas` |
     | `ErrInvalidRefresh` | 401 | `refresh token inválido` |
     | `ErrRefreshReuse` | 401 | `refresh token reutilizado; sesión revocada` |
     | `ErrUserAlreadyExists` | 409 | `el email o el username ya están registrados` |
     | otros | 500 | `error interno` (el detalle solo va al log) |
3. **Middleware (`middleware/auth_middleware.go`):**
   - Extrae el token `Bearer` de la cabecera `Authorization`.
   - Valida firma y expiración con `TokenManager.ParseAccess`.
   - **Verificación contra la denylist (`RevocationChecker`):**
     - Rechaza si el `jti` está en `revoked_access_tokens`.
     - Rechaza si `iat` no es posterior al corte global `user_access_revocations.revoked_before`, truncado a segundos.
     - Como `iat` tiene precisión de segundos, un token emitido en el mismo segundo del corte también se revoca. Así un logout global no deja vivos tokens emitidos justo antes, a costa de que un login en ese mismo segundo deba repetirse.
     - Es fail-closed: si la BD falla, responde 401.
   - Inyecta los `*domain.AccessClaims` en el `context.Context` de la petición (`ClaimsFromContext`).
4. **Router (`router.go`):**

   ```go
   r.Get("/healthz", ...) // 200 "ok"

   r.Route("/api/v1", func(r chi.Router) {
       // Endpoints Públicos
       r.Post("/auth/register", opts.Auth.Register)
       r.Post("/auth/login", opts.Auth.Login)
       r.Post("/auth/refresh", opts.Auth.Refresh)

       // Endpoints Protegidos
       r.Group(func(r chi.Router) {
           r.Use(chimw.AuthMiddleware(opts.JWT, opts.Revocation))
           r.Get("/auth/me", opts.Auth.Me)
           r.Post("/auth/logout", opts.Auth.Logout)
       })
   })
   ```

5. **Servidor (`cmd/server.go`):**
   - Carga la configuración con `--config .env`; las variables del entorno tienen prioridad.
   - Abre el pool (10 conexiones abiertas, 5 inactivas, vida máxima de 1 h) y conecta los adaptadores.
   - Lanza una goroutine que cada hora purga de `revoked_access_tokens` los tokens ya expirados.
   - Ante `SIGINT`/`SIGTERM` hace un shutdown elegante con 10 s de gracia.

---

#### 3.5.4 Pruebas Unitarias (`make test`)

- `services/auth_service_test.go`: usa fakes en memoria de los stores, del hasher y del JWT. Cubre:
  - register: OK, opcionales vacíos, duplicado, carrera `23505` y validaciones;
  - login: por username y por email, usuario inexistente y contraseña incorrecta;
  - refresh: rotación con `replaced_by` y misma familia, reuso, carrera, token inexistente y expirado, revocación del access anterior, access ausente o de otro usuario;
  - logout: idempotente, denylist y `all_sessions`;
  - me: OK y usuario borrado.
- `security/jwt_test.go`: ida y vuelta de claims, `jti` único, token expirado, secreto distinto, tokens basura o `alg=none`, el hasher y la verificación del hash del seed.
- `middleware/auth_middleware_test.go`: middleware (sin token, malformado, inválido, válido, revocado, fail-closed) y `RevocationChecker` (denylist, `jti` malformado, corte global antes, después y en el mismo segundo, error de BD).

---

## 4. Fase 3: Integración de la API de Spotify, Catálogo y Worker de Embeddings

Esta fase implementa el entregable **1.3.2 del WBS de Junkbox**: búsqueda de música (canciones, artistas y álbumes) con resultados reales, consumo transparente de la API de Spotify mediante un patrón *cache-through*, persistencia relacional del catálogo y cálculo asíncrono de embeddings para el motor de recomendaciones.

#### Decisiones que cambian la especificación original

| Tema | Especificación original | Implementación | Motivo |
|---|---|---|---|
| Audio features de canciones nuevas | `GET api.spotify.com/v1/audio-features` | **FreqBlog** (`GET api.freqblog.com/v1/audio-features`) | Spotify cerró ese endpoint para apps nuevas el 27-nov-2024 (responde 403). FreqBlog ofrece una ruta con la misma forma de respuesta. |
| Identificador para los audio features | Spotify ID | **ISRC** (de `external_ids.isrc`); Spotify ID si no hay ISRC | Con un Spotify ID, FreqBlog devuelve `null` cuando el título es ambiguo. Un ISRC que todavía no tiene lo encola y lo resuelve en unos 15–30 s. |
| Ingesta histórica | Script Python `make ingest-data` | `make ingest-songs` (SQL) crea también los artistas; para volúmenes cargados antes de la Fase 3 se agrega **`make backfill-artists`** (SQL) | El catálogo ya se carga en SQL puro (§2.8), así que no hace falta agregar dependencias de Python. |
| Cuándo consultar Spotify | Solo con 0 resultados locales | Con 0 resultados **o** si el mejor puntaje es menor que `SEARCH_FALLBACK_THRESHOLD` (0.8) | Con 1.16M canciones casi cualquier consulta encuentra algo parecido, así que con la regla original Spotify casi nunca se consultaría. |
| Similitud | `similarity()` / `%` sobre `track_name` | `strict_word_similarity()` / `<<%` sobre `título + ' ' + artista` | Medido sobre el catálogo real: con `word_similarity`, "Grain" puntúa 1.0 contra "Les voyages en **train** / **Gra**nd Corps Malade", y "Espresso Sabrina Carpenter" puntúa 0.68 contra otras canciones de la artista. |
| Artistas | `UNIQUE(name)` | `name` sin unicidad, `spotify_id UNIQUE`, y regla de "reclamo" (§4.4) | `UNIQUE(name)` fusiona a los homónimos. |
| `limit` en Spotify | Hasta 50 por petición | Páginas de 10 con `offset` | Desde febrero de 2026, las apps en Development Mode tienen como máximo `limit=10` en `/v1/search`. |

---

### 4.1 Arquitectura del Flujo de Búsqueda y Fallback (Cache-Through)

1. **Consulta local con trigramas (`pg_trgm`).** El backend busca en `song_metadata`, `artists` o `albums` con `strict_word_similarity`, apoyado en índices GIN de expresión.
2. **Cache-hit.** Si hay resultados y el mejor puntaje es ≥ `SEARCH_FALLBACK_THRESHOLD`, se responden con `source: "local_cache"` sin consumir cuota de Spotify.
3. **Cache-miss.** Si no:
   - Se llama a `GET /v1/search` de Spotify con credenciales de aplicación (*Client Credentials*).
   - En **una transacción** se guardan los artistas, los álbumes y las canciones.
   - Las canciones nuevas quedan con `pending_embedding = TRUE` y `embedding = NULL`; las que ya existían en el CSV conservan su embedding.
   - Se responde de inmediato con `source: "spotify_fallback"`.
4. **Degradación.** Si Spotify falla o no está configurado, se responden los resultados locales aunque tengan puntaje bajo. Solo si tampoco hay resultados locales se responde `502` (Spotify falló) o una lista vacía (Spotify no configurado).
5. **Embeddings asíncronos.** Las canciones pendientes se encolan en el worker (§4.5). El worker pide sus audio features a FreqBlog por lotes, imputa los nulos con la mediana del catálogo y guarda el `vector(10)`.

#### Latencia medida (catálogo de 1.16M canciones, caché caliente)

| Consulta | Tiempo |
|---|---|
| `Yellow Coldplay`, `Espresso Sabrina Carpenter` (título y/o artista) | 15–100 ms |
| `Grain` (una palabra poco común) | ~240 ms |
| `love`, `the`, `a` (una palabra muy común: 30k–340k candidatos) | 0,6–3 s |

**Limitación conocida:** con una sola palabra muy común hay que calcular el puntaje de todos los candidatos para ordenarlos. Se probó un índice GiST (`gist_trgm_ops`, KNN con `<<<->`): baja esos casos a ~1 ms, pero sube las consultas de varias palabras a ~400–800 ms, y el planificador no combina bien ambos índices. No se adoptó. Si hace falta, las alternativas son una ruta KNN solo para consultas de una palabra o búsqueda de texto completo (`tsvector`).

> *Justificación de la consistencia eventual:* según el criterio SMART §4.1 del documento del proyecto, las recomendaciones se generan en procesos por lotes periódicos. Que el vector llegue unos segundos después no afecta al usuario y mantiene baja la latencia de la búsqueda.

```mermaid
sequenceDiagram
    autonumber
    actor Cliente as Cliente Web (React)
    participant API as Backend Go (Chi)
    participant DB as PostgreSQL (junkbox_db)
    participant Spotify as Spotify Web API
    participant Worker as Worker de embeddings (Go)
    participant FreqBlog as FreqBlog API

    Cliente->>API: GET /api/v1/music/search?q=...&type=track (Bearer JWT)
    API->>DB: strict_word_similarity sobre song_metadata
    alt Mejor puntaje ≥ umbral (cache-hit)
        DB-->>API: Resultados locales
        API-->>Cliente: 200 OK (source: local_cache)
    else Sin resultados o puntaje bajo (cache-miss)
        API->>Spotify: GET /v1/search (páginas de 10, Client Credentials)
        Spotify-->>API: Tracks con artista, álbum e ISRC
        API->>DB: Transacción: artists → albums → song_metadata (pending_embedding=TRUE)
        API->>Worker: Enqueue(track_id, isrc)
        API-->>Cliente: 200 OK (source: spotify_fallback)
        Note over Worker, FreqBlog: En segundo plano (lotes de 50 o cada 2 s)
        Worker->>FreqBlog: GET /v1/audio-features?ids=ISRC1,ISRC2,...
        FreqBlog-->>Worker: audio_features (null si aún no la tiene)
        Worker->>DB: UPDATE embedding + pending_embedding=FALSE
        Worker->>DB: o embedding_attempts+1 y reintento con backoff
    end
```

---

### 4.2 Esquema Relacional del Catálogo Musical

Ubicación: `db/migrations/20261027000004_create_catalog_tables.sql`

- Habilita `pg_trgm` y crea `artists` y `albums`.
- Enlaza `song_metadata` con ambas tablas y agrega el estado del embedding: `pending_embedding`, `embedding_attempts` y `next_embedding_attempt_at`, además de `isrc`.
- `track_id` pasa a ser único: el índice único reemplaza al B-tree de la Fase 1 y permite el `ON CONFLICT (track_id)` de los upserts.
- El índice GIN de búsqueda usa la **misma expresión** que la consulta (`coalesce(track_name,'') || ' ' || coalesce(artist_name,'')`). Si no coincide, el planificador no puede usarlo.
- `Down` revierte columnas, índices y tablas, pero **nunca** borra `song_metadata`.
- Sobre el volumen existente (1.16M filas), la migración tardó **10,5 s**, casi todo en construir el GIN y el índice único.

```sql
-- +goose Up

-- Coincidencia difusa (trigramas) para la búsqueda del catálogo.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Artistas. spotify_id es NULL para los artistas creados desde el CSV
-- histórico (db/scripts/backfill_artists.sql). El nombre no es único: puede
-- haber homónimos con distinto spotify_id.
CREATE TABLE IF NOT EXISTS artists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    spotify_id VARCHAR(64) UNIQUE,
    name TEXT NOT NULL,
    genres TEXT[] NOT NULL DEFAULT '{}',
    popularity INT,
    image_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artists_name ON artists(name);
CREATE INDEX IF NOT EXISTS idx_artists_name_trgm ON artists USING gin (name gin_trgm_ops);

CREATE TRIGGER trigger_artists_updated_at
BEFORE UPDATE ON artists
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- Álbumes (solo llegan desde Spotify; el CSV no trae álbumes).
CREATE TABLE IF NOT EXISTS albums (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    spotify_id VARCHAR(64) UNIQUE,
    title TEXT NOT NULL,
    artist_id UUID REFERENCES artists(id) ON DELETE SET NULL,
    artist_name TEXT NOT NULL,
    release_year INT,
    album_type VARCHAR(32) NOT NULL DEFAULT 'album',
    image_url TEXT,
    total_tracks INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Misma expresión que SearchAlbumsTrigram: "Short n' Sweet Sabrina Carpenter".
CREATE INDEX IF NOT EXISTS idx_albums_search_trgm ON albums USING gin ((title || ' ' || artist_name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_albums_artist_id ON albums(artist_id);

CREATE TRIGGER trigger_albums_updated_at
BEFORE UPDATE ON albums
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- Enlace de song_metadata con artistas/álbumes y estado del embedding.
-- pending_embedding marca las canciones importadas desde Spotify cuyo vector
-- aún no calcula el worker; embedding_attempts / next_embedding_attempt_at
-- controlan los reintentos contra FreqBlog.
ALTER TABLE song_metadata
    ADD COLUMN IF NOT EXISTS artist_id UUID REFERENCES artists(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS album_id UUID REFERENCES albums(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS isrc VARCHAR(12),
    ADD COLUMN IF NOT EXISTS pending_embedding BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS embedding_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_embedding_attempt_at TIMESTAMPTZ;

-- track_id pasa a ser único (necesario para ON CONFLICT en los upserts);
-- el índice único reemplaza al B-tree normal de la Fase 1.
CREATE UNIQUE INDEX IF NOT EXISTS idx_song_metadata_spotify_track_id ON song_metadata(track_id);
DROP INDEX IF EXISTS idx_song_metadata_track_id;

CREATE INDEX IF NOT EXISTS idx_song_metadata_artist_id ON song_metadata(artist_id);
CREATE INDEX IF NOT EXISTS idx_song_metadata_album_id ON song_metadata(album_id);

-- Trigramas sobre "título artista": una consulta como "Espresso Sabrina
-- Carpenter" puntúa alto contra la canción correcta. La expresión debe ser
-- idéntica a la de SearchTracksTrigram (db/queries/catalog.sql).
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_song_metadata_pending_embedding ON song_metadata(next_embedding_attempt_at)
    WHERE pending_embedding = TRUE;

-- +goose Down
-- Revierte el enlace y las tablas nuevas, pero nunca borra song_metadata.
DROP INDEX IF EXISTS idx_song_metadata_pending_embedding;
DROP INDEX IF EXISTS idx_song_metadata_search_trgm;
DROP INDEX IF EXISTS idx_song_metadata_album_id;
DROP INDEX IF EXISTS idx_song_metadata_artist_id;
CREATE INDEX IF NOT EXISTS idx_song_metadata_track_id ON song_metadata(track_id);
DROP INDEX IF EXISTS idx_song_metadata_spotify_track_id;

ALTER TABLE song_metadata
    DROP COLUMN IF EXISTS next_embedding_attempt_at,
    DROP COLUMN IF EXISTS embedding_attempts,
    DROP COLUMN IF EXISTS pending_embedding,
    DROP COLUMN IF EXISTS isrc,
    DROP COLUMN IF EXISTS album_id,
    DROP COLUMN IF EXISTS artist_id;

DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS artists;
```

#### Artistas del catálogo histórico

El CSV no trae IDs de artista ni álbumes, así que se crea un artista por cada `artist_name` distinto (64.159 en el catálogo), con sus géneros agregados, y se enlaza `song_metadata.artist_id`:

- **Entorno nuevo:** lo hace `make ingest-songs` (§2.8). Inserta los artistas justo después del `COPY` y enlaza `artist_id` en el **mismo `UPDATE`** que calcula los embeddings, así la tabla se reescribe una sola vez.
  - Además quita el índice GIN de búsqueda antes de la carga y lo reconstruye al final, como ya hacía con el IVFFlat.
  - **Medido en un Postgres limpio:** migraciones + ingesta completa en **1 min 37 s**.
- **Volumen cargado antes de la Fase 3:** `make migrate-up && make backfill-artists` (`db/scripts/backfill_artists.sql`).
  - Es idempotente: solo inserta los nombres que faltan y solo actualiza las canciones sin `artist_id`; la segunda corrida tarda unos 3 s.
  - Si hay canciones por enlazar, quita y reconstruye el GIN de búsqueda.
  - **Medido:** **2 min 16 s** sobre 1.16M filas.
  - La primera versión mantenía el GIN fila por fila y tardaba ~18 min: el `UPDATE` no puede ser HOT porque `artist_id` está indexada, así que toca todos los índices.

> La definición del índice `idx_song_metadata_search_trgm` aparece en la migración y en los dos scripts. Si cambia, hay que cambiarla en los tres sitios (y en `SearchTracksTrigram`).

```sql
-- Crea los artistas del catálogo histórico a partir de song_metadata y
-- enlaza song_metadata.artist_id. Idempotente: solo inserta los nombres que
-- aún no existen y solo actualiza canciones sin artist_id.
-- Uso: make backfill-artists   (requiere make migrate-up)

\echo 'Creando artistas desde song_metadata...'
INSERT INTO artists (name, genres)
SELECT s.artist_name,
       coalesce(array_agg(DISTINCT s.genre) FILTER (WHERE s.genre IS NOT NULL), '{}')
FROM song_metadata s
WHERE s.artist_name IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM artists a WHERE a.name = s.artist_name)
GROUP BY s.artist_name;

-- Si hay canciones por enlazar, el UPDATE reescribe casi toda la tabla: es
-- mucho más rápido quitar el índice GIN de búsqueda (migración
-- 20261027000004) y reconstruirlo al final que mantenerlo fila por fila.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM song_metadata WHERE artist_id IS NULL AND artist_name IS NOT NULL) THEN
    DROP INDEX IF EXISTS idx_song_metadata_search_trgm;
  END IF;
END
$$;

-- Si hay homónimos (un artista de Spotify y otro del CSV), se prefiere el
-- del CSV (spotify_id NULL) y luego el más antiguo.
\echo 'Enlazando song_metadata.artist_id...'
UPDATE song_metadata s
SET artist_id = a.id
FROM (
    SELECT DISTINCT ON (name) name, id
    FROM artists
    ORDER BY name, (spotify_id IS NOT NULL), created_at
) a
WHERE s.artist_id IS NULL
  AND s.artist_name = a.name;

\echo 'Construyendo índice de búsqueda por trigramas (si se quitó)...'
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);

ANALYZE artists;
ANALYZE song_metadata;

\echo 'Backfill de artistas completado.'
```

#### `db/scripts/truncate_all.sql`

`make seed-down` excluye también `artists` y `albums`. `TRUNCATE ... CASCADE` sobre `artists` vaciaría `song_metadata` a través de la clave foránea `artist_id`, y con eso se perdería el catálogo.

---

### 4.3 Consultas SQL para SQLC (`db/queries/catalog.sql`)

- **Ninguna consulta selecciona `embedding`:** sqlc no conoce el tipo `vector` y lo mapearía a `interface{}`. Para leer se expone `embedding IS NOT NULL AS has_embedding`; para escribir se pasa el texto `'[a,b,...]'` y se castea con `::text::vector`, sin agregar dependencias de pgvector en Go.
- `GetTrack`, `GetArtist` y `GetAlbum` reciben el id local **o** el ID de Spotify; el otro se pasa vacío (`0`, `uuid.Nil` o `''`).
- `genres TEXT[]`: con `database/sql`, sqlc envuelve los arreglos en `pq.Array`. Por eso `github.com/lib/pq` es dependencia directa; solo se usa para eso, el driver sigue siendo pgx.

```sql
-- Consultas del catálogo musical (Fase 3). Ninguna selecciona la columna
-- embedding: sqlc no conoce el tipo vector, así que se expone como
-- has_embedding y se escribe como texto ('[a,b,...]'::vector).

-- La expresión coalesce(track_name, '') || ' ' || coalesce(artist_name, '')
-- debe coincidir con la del índice idx_song_metadata_search_trgm.
-- strict_word_similarity mide qué tan bien las palabras de la consulta aparecen
-- completas dentro del texto: "Yellow Coldplay" puntúa 1.0 contra "Yellow
-- Coldplay" y a "Espresso Sabrina Carpenter" le falta una palabra en "Nonsense
-- Sabrina Carpenter" (0.67). similarity() desempata a favor del texto más
-- parecido en total (el título exacto antes que uno que solo lo contiene).

-- name: SearchTracksTrigram :many
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.year, s.genre,
    s.popularity, s.duration_ms, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title,
    strict_word_similarity(sqlc.arg(query)::text, coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, ''))::float8 AS score
FROM song_metadata s
LEFT JOIN albums al ON al.id = s.album_id
WHERE sqlc.arg(query)::text <<% (coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, ''))
ORDER BY score DESC,
    similarity(sqlc.arg(query)::text, coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, '')) DESC,
    s.popularity DESC NULLS LAST, s.id
LIMIT sqlc.arg(max_results);

-- name: SearchArtistsTrigram :many
SELECT
    id, spotify_id, name, genres, popularity, image_url, created_at,
    strict_word_similarity(sqlc.arg(query)::text, name)::float8 AS score
FROM artists
WHERE sqlc.arg(query)::text <<% name
ORDER BY score DESC, similarity(sqlc.arg(query)::text, name) DESC, popularity DESC NULLS LAST, id
LIMIT sqlc.arg(max_results);

-- name: SearchAlbumsTrigram :many
SELECT
    id, spotify_id, title, artist_id, artist_name, release_year, album_type,
    image_url, total_tracks, created_at,
    strict_word_similarity(sqlc.arg(query)::text, title || ' ' || artist_name)::float8 AS score
FROM albums
WHERE sqlc.arg(query)::text <<% (title || ' ' || artist_name)
ORDER BY score DESC, similarity(sqlc.arg(query)::text, title || ' ' || artist_name) DESC, id
LIMIT sqlc.arg(max_results);

-- GetTrack acepta el id local o el track_id de Spotify; el otro se pasa
-- vacío (0 / '') para que solo coincida uno de los dos índices.

-- name: GetTrack :one
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.artist_id, s.album_id,
    s.isrc, s.year, s.genre, s.popularity, s.duration_ms,
    s.danceability, s.energy, s.key, s.loudness, s.mode, s.speechiness,
    s.acousticness, s.instrumentalness, s.liveness, s.valence, s.tempo,
    s.time_signature, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title
FROM song_metadata s
LEFT JOIN albums al ON al.id = s.album_id
WHERE s.id = sqlc.arg(id)::int OR s.track_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: GetArtist :one
SELECT id, spotify_id, name, genres, popularity, image_url, created_at
FROM artists
WHERE id = sqlc.arg(id)::uuid OR spotify_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: GetAlbum :one
SELECT
    id, spotify_id, title, artist_id, artist_name, release_year, album_type,
    image_url, total_tracks, created_at
FROM albums
WHERE id = sqlc.arg(id)::uuid OR spotify_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: ListTracksByAlbum :many
SELECT
    id, track_id, track_name, artist_name, year, genre, popularity,
    duration_ms, pending_embedding,
    (embedding IS NOT NULL)::bool AS has_embedding
FROM song_metadata
WHERE album_id = $1
ORDER BY id;

-- --- Importación desde Spotify (CatalogImporter, dentro de una transacción) ---

-- name: GetArtistIDBySpotifyID :one
SELECT id FROM artists WHERE spotify_id = $1;

-- ClaimArtistByName asigna el spotify_id a un artista del CSV homónimo
-- (spotify_id NULL). No devuelve filas si no hay ninguno libre.
-- name: ClaimArtistByName :one
UPDATE artists
SET spotify_id = sqlc.arg(spotify_id),
    genres = CASE WHEN cardinality(sqlc.arg(genres)::text[]) > 0 THEN sqlc.arg(genres)::text[] ELSE genres END,
    popularity = coalesce(sqlc.narg(popularity), popularity),
    image_url = coalesce(sqlc.narg(image_url), image_url)
WHERE id = (
    SELECT a.id FROM artists a
    WHERE a.name = sqlc.arg(name) AND a.spotify_id IS NULL
    ORDER BY a.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id;

-- Los artistas simplificados de una búsqueda de canciones no traen géneros,
-- popularidad ni imagen: en conflicto no se pisan los valores conocidos.
-- name: UpsertArtistBySpotifyID :one
INSERT INTO artists (spotify_id, name, genres, popularity, image_url)
VALUES (sqlc.arg(spotify_id), sqlc.arg(name), sqlc.arg(genres)::text[], sqlc.narg(popularity), sqlc.narg(image_url))
ON CONFLICT (spotify_id) DO UPDATE SET
    name = EXCLUDED.name,
    genres = CASE WHEN cardinality(EXCLUDED.genres) > 0 THEN EXCLUDED.genres ELSE artists.genres END,
    popularity = coalesce(EXCLUDED.popularity, artists.popularity),
    image_url = coalesce(EXCLUDED.image_url, artists.image_url)
RETURNING id;

-- name: UpsertAlbum :one
INSERT INTO albums (
    spotify_id, title, artist_id, artist_name, release_year, album_type, image_url, total_tracks
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (spotify_id) DO UPDATE SET
    title = EXCLUDED.title,
    artist_id = coalesce(EXCLUDED.artist_id, albums.artist_id),
    artist_name = EXCLUDED.artist_name,
    release_year = coalesce(EXCLUDED.release_year, albums.release_year),
    album_type = EXCLUDED.album_type,
    image_url = coalesce(EXCLUDED.image_url, albums.image_url),
    total_tracks = EXCLUDED.total_tracks
RETURNING id;

-- Una canción del CSV que ya tiene embedding conserva sus datos históricos;
-- solo se completan los enlaces y el ISRC. pending_embedding queda TRUE solo
-- si aún no hay vector.
-- name: UpsertSongFromSpotify :one
INSERT INTO song_metadata (
    track_id, track_name, artist_name, popularity, year, duration_ms,
    isrc, artist_id, album_id, pending_embedding
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE
)
ON CONFLICT (track_id) DO UPDATE SET
    track_name = coalesce(song_metadata.track_name, EXCLUDED.track_name),
    artist_name = coalesce(song_metadata.artist_name, EXCLUDED.artist_name),
    popularity = coalesce(EXCLUDED.popularity, song_metadata.popularity),
    year = coalesce(song_metadata.year, EXCLUDED.year),
    duration_ms = coalesce(song_metadata.duration_ms, EXCLUDED.duration_ms),
    isrc = coalesce(EXCLUDED.isrc, song_metadata.isrc),
    artist_id = coalesce(EXCLUDED.artist_id, song_metadata.artist_id),
    album_id = coalesce(EXCLUDED.album_id, song_metadata.album_id),
    pending_embedding = song_metadata.embedding IS NULL
RETURNING
    id, track_id, track_name, artist_name, year, genre, popularity, duration_ms,
    isrc, embedding_attempts, pending_embedding,
    (embedding IS NOT NULL)::bool AS has_embedding;

-- --- Worker de embeddings ---

-- name: ListPendingEmbeddingTracks :many
SELECT track_id, isrc, embedding_attempts
FROM song_metadata
WHERE pending_embedding = TRUE
  AND embedding_attempts < sqlc.arg(max_attempts)::int
  AND (next_embedding_attempt_at IS NULL OR next_embedding_attempt_at <= NOW())
ORDER BY next_embedding_attempt_at NULLS FIRST, id
LIMIT sqlc.arg(max_results);

-- name: UpdateSongAudioFeaturesAndEmbedding :execrows
UPDATE song_metadata
SET
    danceability = sqlc.narg(danceability),
    energy = sqlc.narg(energy),
    key = sqlc.narg(key),
    loudness = sqlc.narg(loudness),
    mode = sqlc.narg(mode),
    speechiness = sqlc.narg(speechiness),
    acousticness = sqlc.narg(acousticness),
    instrumentalness = sqlc.narg(instrumentalness),
    liveness = sqlc.narg(liveness),
    valence = sqlc.narg(valence),
    tempo = sqlc.narg(tempo),
    time_signature = sqlc.narg(time_signature),
    embedding = sqlc.arg(embedding)::text::vector,
    pending_embedding = FALSE,
    next_embedding_attempt_at = NULL
WHERE track_id = sqlc.arg(track_id) AND pending_embedding = TRUE;

-- Solo cuenta si embedding_attempts sigue siendo el que vio el worker: un
-- mismo track que llegó dos veces (cola y rescan) no consume dos intentos.
-- name: MarkEmbeddingAttemptFailed :exec
UPDATE song_metadata
SET embedding_attempts = embedding_attempts + 1,
    next_embedding_attempt_at = NOW() + make_interval(secs => sqlc.arg(retry_in_seconds)::float8)
WHERE track_id = sqlc.arg(track_id)
  AND pending_embedding = TRUE
  AND embedding_attempts = sqlc.arg(attempts)::int;

-- Medianas del catálogo para imputar los audio features que FreqBlog
-- devuelve nulos (speechiness, liveness y a veces instrumentalness).
-- name: GetCatalogFeatureMedians :one
SELECT
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY danceability), 0)::float8 AS danceability,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY energy), 0)::float8 AS energy,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY loudness), 0)::float8 AS loudness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY speechiness), 0)::float8 AS speechiness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY acousticness), 0)::float8 AS acousticness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY instrumentalness), 0)::float8 AS instrumentalness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY liveness), 0)::float8 AS liveness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY valence), 0)::float8 AS valence,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY tempo), 0)::float8 AS tempo,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY mode), 0)::float8 AS mode
FROM song_metadata;
```

---

### 4.4 Cliente de Spotify e Importación Transaccional

**Cliente:** `internal/adapters/secondary/spotify/client.go` (implementa `ports.MusicProvider`).

- **Token de aplicación:** `POST https://accounts.spotify.com/api/token` con `Authorization: Basic base64(client_id:client_secret)` y `grant_type=client_credentials`.
- **Caché del token:** protegida por un `sync.RWMutex` con doble comprobación. El token se renueva si le quedan menos de 60 s; ante un `401` se invalida y la petición se reintenta una vez.
- **Búsqueda:** `GET /v1/search?type=track|artist|album&q=...&limit=10&offset=...`, paginando hasta completar el `limit` pedido. Desde febrero de 2026, en Development Mode el máximo es 10 por página.
- **`429 Too Many Requests`:** si `Retry-After` es ≤ 5 s, espera y reintenta una vez; si es mayor, devuelve el error.
- **Mapeo:**
  - artista principal (`artists[0]`);
  - álbum, con el año tomado de `release_date` y la imagen más ancha;
  - `external_ids.isrc`.
- **Campos opcionales:** `popularity` ya no viene en Development Mode y queda `null`. Los tracks de Spotify no traen género, así que `genre` también queda `null`.
- **Credenciales opcionales:** sin `SPOTIFY_CLIENT_ID` y `SPOTIFY_CLIENT_SECRET`, la búsqueda funciona solo con el catálogo local y el servidor lo avisa en el log al arrancar.

**Importación:** `internal/adapters/secondary/postgres/catalog_importer.go` (implementa `ports.CatalogImporter`). Cada búsqueda importa en **una transacción** (`BeginTx` + `sqlc.New(tx)`) con este orden por canción: artista → álbum (con su artista) → canción. Un error hace rollback de todo el lote.

Regla para resolver el artista (`upsertArtist`):
1. Si ya existe un artista con ese `spotify_id`, se actualiza. Los artistas simplificados no traen géneros, popularidad ni imagen; en ese caso se conservan los valores conocidos.
2. Si no, se **reclama** el artista del CSV con el mismo `name` y `spotify_id IS NULL` (`ClaimArtistByName`, con `FOR UPDATE SKIP LOCKED`) y se le asigna el `spotify_id`.
3. Si tampoco hay ninguno libre, se inserta un artista nuevo. Así, los homónimos de Spotify quedan como filas separadas.

---

### 4.5 Worker de Embeddings con FreqBlog

**Cliente:** `internal/adapters/secondary/freqblog/client.go` (implementa `ports.AudioFeaturesProvider`).

- `GET https://api.freqblog.com/v1/audio-features?ids=<hasta 100 ISRC o Spotify IDs>` con la cabecera `X-Api-Key: $FREQBLOG_API_KEY`.
- La respuesta tiene la forma de Spotify (`{"audio_features": [obj | null, ...]}`) y respeta el orden de la entrada. FreqBlog cobra solo por cada ID resuelto; el plan gratuito incluye 1.000 por mes.
- Campos que FreqBlog no mide en sus análisis propios:
  - `speechiness` y `liveness` llegan en `null`;
  - `instrumentalness` llega en `null` si el modelo aún no procesó la canción;
  - `time_signature` siempre se asume 4, así que se descarta;
  - `key = -1` (no detectada) se guarda como `NULL`.
- Los errores (`401`, `429`, red) se devuelven como `*freqblog.APIError` e incluyen el `Retry-After` si viene.

**Worker:** `internal/core/services/audio_features_worker.go`.

- **Cola:** canal de 5.000 `EmbeddingJob{TrackID, ISRC, Attempts}`. `Enqueue` no bloquea: si la cola está llena descarta el job, que sigue con `pending_embedding = TRUE` en la BD.
- **Lotes:** se envían al juntar **50 canciones** o cada **2 s**. El búfer deduplica por `track_id`.
- **Duplicados:** una canción puede llegar por la cola (búsqueda) y por el rescan. El worker recuerda durante 1 h las canciones ya procesadas y descarta las que vuelven a llegar por la cola, para no gastar cuota de FreqBlog dos veces. Además, `MarkEmbeddingAttemptFailed` solo suma si `embedding_attempts` sigue siendo el que vio el job (control optimista), así que un mismo fallo nunca cuenta doble.
- **Reanudación:** al arrancar y cada **1 min** relee `ListPendingEmbeddingTracks` (pendientes con `embedding_attempts < 5` cuyo `next_embedding_attempt_at` ya venció). Así recupera lo que quedó de una ejecución anterior y lo que se descartó por cola llena.
- **Sin resultado (`null`):** `MarkEmbeddingAttemptFailed` suma un intento y programa el siguiente con backoff `30 s · 2^intentos` (30 s, 1 min, 2 min, 4 min, 8 min). Tras **5 intentos** la canción deja de reintentarse y queda con `pending_embedding = TRUE`.
- **Error del proveedor:** el worker se pausa durante `Retry-After` o, si no viene, 1 min. **No consume intentos** y el lote se reintenta completo.
- **Vector:** se calcula en `BuildEmbedding`, con la misma normalización MinMax que la ingesta histórica:

  $$\text{loudness\_norm} = \frac{\text{loudness} + 58.1}{6.172 + 58.1} \qquad \text{tempo\_norm} = \frac{\text{tempo}}{250}$$

  $$\text{vector}(10) = [\text{danceability}, \text{energy}, \text{loudness\_norm}, \text{speechiness}, \text{acousticness}, \text{instrumentalness}, \text{liveness}, \text{valence}, \text{tempo\_norm}, \text{mode}]$$

  - **Imputación:** si un atributo llega nulo, se usa la mediana de esa columna en `song_metadata` (`GetCatalogFeatureMedians`, calculada una vez al arrancar el worker). En la columna acústica se guarda `NULL`; solo el vector lleva el valor imputado.
  - Cada componente se acota a $[0, 1]$.
- **Apagado elegante:** al cancelarse el contexto, el worker vacía el canal, envía el búfer con un plazo de 10 s y solo entonces `cmd/server.go` cierra el pool.
- **Sin clave:** si `FREQBLOG_API_KEY` no está definida, el worker no arranca y las canciones importadas quedan pendientes.

> **Limitación conocida:** los valores de FreqBlog salen de modelos propios y **no están en la misma escala que los de Spotify** (su energía tiene mediana ≈ 0.65 y su valencia ≈ 0.48). Los embeddings de las canciones importadas son aproximados y no del todo comparables con los 1.16M del CSV. Conviene tenerlo en cuenta al evaluar las recomendaciones.

---

### 4.6 Núcleo: `MusicService`

`internal/core/services/music_service.go` depende solo de los puertos (`ports.CatalogStore`, `CatalogImporter`, `MusicProvider` y `EmbeddingQueue`).

- **`Search(ctx, q, type, limit)`:**
  - Valida: `q` no vacío y de ≤ 200 caracteres; `type` ∈ {`track`, `artist`, `album`}, `track` por defecto; `limit` entre 1 y 50, 20 por defecto.
  - Aplica el flujo de §4.1.
- **`GetTrack(id)`:** acepta un entero positivo (id local) o un ID de Spotify (base62 de 22 caracteres). `audio_features` es `null` si la canción todavía no tiene ninguno.
- **`GetArtist(id)` / `GetAlbum(id)`:** aceptan un UUID local o un ID de Spotify. El detalle del álbum incluye sus pistas (`tracks`).

Errores de dominio nuevos: `ErrNotFound` (404) y `ErrMusicProviderUnavailable` (502). Los de validación reutilizan `*ValidationError` (400).

---

### 4.7 Adaptadores HTTP Chi: Endpoints de Música Protegidos con JWT

Las rutas están en el grupo protegido por `AuthMiddleware` (`router.go`), en `handlers_music.go`. Las respuestas serializan directamente los tipos de `domain`, como ya hacía `UserProfile`, así que no hay DTOs nuevos.

| Método | Ruta | Descripción |
|---|---|---|
| `GET` | `/api/v1/music/search?q=&type=&limit=` | Búsqueda cache-through |
| `GET` | `/api/v1/music/tracks/{id}` | Detalle, audio features y estado del embedding |
| `GET` | `/api/v1/music/artists/{id}` | Detalle del artista |
| `GET` | `/api/v1/music/albums/{id}` | Detalle del álbum con sus pistas |

| Error | HTTP | Mensaje |
|---|---|---|
| `*ValidationError`, `limit` no numérico | 400 | detalle |
| `ErrNotFound` | 404 | `no encontrado` |
| `ErrMusicProviderUnavailable` | 502 | `servicio de música no disponible` |
| otros | 500 | `error interno` |

#### Configuración (`.env`)

| Variable | Requerida | Default | Uso |
|---|---|---|---|
| `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET` | No (ambas o ninguna) | — | Respaldo de la búsqueda en Spotify |
| `FREQBLOG_API_KEY` | No | — | Worker de embeddings |
| `SEARCH_FALLBACK_THRESHOLD` | No | `0.8` | Puntaje local mínimo, en $(0, 1]$, para no consultar Spotify |

#### Pruebas (`make test`)

- `services/music_service_test.go`:
  - cache-hit;
  - consulta a Spotify por 0 resultados y por puntaje bajo, con encolado del ISRC;
  - degradación cuando Spotify o la importación fallan;
  - proveedor no configurado;
  - validaciones;
  - resolución de IDs y 404;
  - pistas del álbum.
- `services/audio_features_worker_test.go`:
  - fórmula del vector, imputación y acotado;
  - lote por tamaño y por tiempo;
  - backoff al no haber resultado;
  - un 429 no consume intentos;
  - reanudación al arrancar;
  - el shutdown envía el búfer;
  - cola llena y canciones con intentos agotados.
- `spotify/client_test.go` (con `httptest`):
  - mapeo;
  - caché y renovación del token;
  - paginación de 10 en 10;
  - 429 con espera corta y larga;
  - 401 → token nuevo;
  - credenciales inválidas.
- `freqblog/client_test.go` (con `httptest`):
  - mapeo de `null` y de `key = -1`;
  - lotes de 100;
  - 429 con `Retry-After`;
  - 401.

---

## 5. Guía de Verificación y Pruebas con cURL

### 1. Levantar Contenedor y Aplicar Migraciones

```bash
make db-up
make migrate-up
make seed-up
make run-server
```

### 2. Probar Registro de Usuario (`POST /api/v1/auth/register`)

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

_Respuesta esperada: `201 Created` con par de tokens y objeto de usuario (username y email normalizados a minúsculas). Repetir la petición responde `409 Conflict`; un username inválido (ej. `"x"`) responde `400 Bad Request` con el detalle._

---

### 3. Probar Inicio de Sesión (`POST /api/v1/auth/login`)

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "identifier": "cesar_beltran",
    "password": "Password123!"
  }'
```

_Respuesta esperada: `200 OK` retornando `access_token`, `refresh_token`, `expires_in: 900` y datos del perfil de César Beltrán. También funciona con `"identifier": "cesar@junkbox.local"`; una contraseña incorrecta responde `401`._

---

### 4. Consultar Perfil con Access Token (`GET /api/v1/auth/me`)

```bash
ACCESS_TOKEN="<pegar_token_aqui>"

curl -i -X GET http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

_Respuesta esperada: `200 OK` con `id`, `username`, `email`, `display_name`, `avatar_url` y `created_at`. Sin cabecera `Authorization` responde `401`._

---

### 5. Probar Rotación de Refresh Token (`POST /api/v1/auth/refresh`)

```bash
REFRESH_TOKEN="<pegar_refresh_token_aqui>"

curl -i -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{ \"refresh_token\": \"$REFRESH_TOKEN\" }"
```

_Respuesta esperada: `200 OK` con nuevo par de tokens. El access token enviado (la llave vieja, aunque haya expirado) queda revocado: usarlo en `/auth/me` responde `401`. Sin cabecera `Authorization`, o con un access de otro usuario, responde `401` y el refresh token no se gasta. Si se reintenta enviar el refresh token viejo, debe responder `401 Unauthorized` (`refresh token reutilizado; sesión revocada`). Al revocarse toda la familia, el refresh token **nuevo** también queda inválido y hay que volver a hacer login._

---

### 6. Probar Logout con Revocación Inmediata (`POST /api/v1/auth/logout`)

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{ \"refresh_token\": \"$REFRESH_TOKEN\", \"all_sessions\": false }"
```

_Respuesta esperada: `200 OK` con mensaje `{"message": "sesión cerrada"}`._

_Si inmediatamente se vuelve a intentar `GET /api/v1/auth/me` con ese mismo `$ACCESS_TOKEN`, debe retornar `401 Unauthorized` porque el token ha quedado registrado en la denylist (`revoked_access_tokens`)._

> Usa un `ACCESS_TOKEN`/`REFRESH_TOKEN` de un login nuevo: si en el paso 5 se probó el reuso, esa familia ya quedó revocada (el logout igual responde 200 por ser idempotente).

---

### 7. Probar Cierre de Todas las Sesiones (`all_sessions: true`)

Inicia sesión dos veces con el mismo usuario (sesiones A y B) y cierra la sesión A con `all_sessions: true`:

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN_A" \
  -H "Content-Type: application/json" \
  -d "{ \"refresh_token\": \"$REFRESH_TOKEN_A\", \"all_sessions\": true }"
```

_Respuesta esperada: `200 OK`. Después, `GET /auth/me` con `$ACCESS_TOKEN_B` y `POST /auth/refresh` con `$REFRESH_TOKEN_B` responden `401`. Un login posterior funciona con normalidad. Un login hecho en el mismo segundo del corte también queda invalidado (§3.5.3)._

---

### 8. Probar Búsqueda de Música en Catálogo Local (`GET /api/v1/music/search`)

Busca una canción que ya esté en el catálogo local (`song_metadata`):

```bash
curl -i -X GET "http://localhost:8080/api/v1/music/search?q=Yellow%20Coldplay&type=track&limit=5" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

_Respuesta esperada: `200 OK` con `"source": "local_cache"` y "Yellow" de Coldplay entre los primeros resultados (puntaje 1.0), sin consultar Spotify. Sin la cabecera `Authorization: Bearer` responde `401`; con `type=playlist` o `limit=51`, `400`._

---

### 9. Probar Búsqueda con Fallback a Spotify API (Cache-Miss)

Requiere `SPOTIFY_CLIENT_ID` y `SPOTIFY_CLIENT_SECRET` en `.env`. Busca una canción que no esté en el CSV (posterior a 2023):

```bash
curl -i -X GET "http://localhost:8080/api/v1/music/search?q=Espresso%20Sabrina%20Carpenter&type=track&limit=5" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

_Respuesta esperada: `200 OK` con `"source": "spotify_fallback"`. Localmente solo hay otras canciones de la artista, con puntaje 0.67 < 0.8. La canción, su artista y su álbum quedan guardados en una transacción, y la canción responde con `"pending_embedding": true` y `"has_embedding": false`. Una segunda búsqueda idéntica ya responde `local_cache`. Sin credenciales de Spotify, la misma búsqueda responde los resultados locales con `local_cache`._

---

### 10. Verificar el Cálculo Asíncrono de Embeddings por el Worker

Requiere `FREQBLOG_API_KEY`. Espera unos segundos y consulta el detalle:

```bash
TRACK_SPOTIFY_ID="<spotify_id_obtenido_en_paso_anterior>"

curl -i -X GET "http://localhost:8080/api/v1/music/tracks/$TRACK_SPOTIFY_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

_Respuesta esperada: `200 OK` con `"pending_embedding": false`, `"has_embedding": true` y `audio_features` poblados; `speechiness` y `liveness` en `null`, porque FreqBlog no los mide. Si FreqBlog aún no tenía el ISRC, la canción sigue pendiente y el worker la reintenta (30 s, 1 min, 2 min…). Se puede ver con:_

```bash
make db-shell
# SELECT track_id, isrc, pending_embedding, embedding_attempts, next_embedding_attempt_at
# FROM song_metadata WHERE track_id = '<spotify_id>';
```

El detalle del álbum incluye sus pistas:

```bash
curl -i "http://localhost:8080/api/v1/music/albums/<album_spotify_id_o_uuid>" -H "Authorization: Bearer $ACCESS_TOKEN"
```

---

### 11. Ingesta del Catálogo y Backfill de Artistas

En una base de datos nueva:

```bash
make migrate-up
make ingest-songs                       # usa data/spotify_data.csv; crea artistas y enlaza artist_id
make ingest-songs DATASET=otro.csv      # CSV en otra ubicación
```

Sobre un volumen que ya tenía el catálogo, basta con:

```bash
make migrate-up
make backfill-artists   # idempotente
```

_Salida esperada: en `ingest-songs`, `COPY 1159764`, `INSERT 0 64159` y `UPDATE 1159764` (~1 min 40 s). En `backfill-artists`, la primera vez `INSERT 0 64159` y `UPDATE 1159764` (~2 min 15 s); después, `INSERT 0 0` y `UPDATE 0`. Para comprobarlo: `SELECT count(*) FROM song_metadata WHERE artist_id IS NULL AND artist_name IS NOT NULL;` debe devolver `0`._

---

### 12. `make seed-down` Conserva el Catálogo

```bash
make seed-down   # confirmar con y
```

_`song_metadata`, `artists` y `albums` conservan sus filas; solo se vacían usuarios y tokens._
