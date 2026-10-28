# Guía de Implementación del Backend de Junkbox

Este documento establece la especificación técnica, diseño de arquitectura y guía paso a paso para la implementación de las **Fases 1 y 2** del backend de **Junkbox** en **Go**.

---

## Tabla de Contenidos

- [1. Resumen y Contexto del Proyecto](#1-resumen-y-contexto-del-proyecto)
- [2. Fase 1: Infraestructura de Datos, Goose, SQLC y Make](#2-fase-1-infraestructura-de-datos-goose-sqlc-y-make)
  - [2.1 Análisis Técnico: Reproducibilidad del Volumen Docker vs. Ingesta CSV](#21-análisis-técnico-reproducibilidad-del-volumen-docker-vs-ingesta-csv)
  - [2.2 Transición Segura del Contenedor Docker y Preservación de Datos](#22-transición-segura-del-contenedor-docker-y-preservación-de-datos)
  - [2.3 Archivo `db/docker-compose.yml`](#23-archivo-dbdocker-composeyml)
  - [2.4 Configuración de Entorno: `db/.env.template` y `db/.env`](#24-configuración-de-entorno-dbenvtemplate-y-dbenv)
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
- [4. Guía de Verificación y Pruebas con cURL](#4-guía-de-verificación-y-pruebas-con-curl)

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

> **Credenciales del volumen existente:** el contenedor original se creó con `POSTGRES_USER=postgres` y `POSTGRES_PASSWORD=postgres`. Como el volumen ya está inicializado, Postgres ignora esas variables en arranques posteriores, así que el `db/.env` de esta máquina debe usar `postgres` como contraseña (ver §2.4).

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
      - .env
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

### 2.4 Configuración de Entorno: `db/.env.template` y `db/.env`

Ubicación: `db/.env.template`

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
```

`db/.env` se obtiene copiando la plantilla. En un entorno limpio basta con cambiar contraseña y `JWT_SECRET`; en la máquina con el volumen preexistente `POSTGRES_PASSWORD` y `DATABASE_URL` deben usar la contraseña original (`postgres`).

`db/.env` y `dataset.csv` (~175 MB, por encima del límite de 100 MB de GitHub) están listados en `.gitignore`; el CSV se distribuye por separado.

---

### 2.5 Automatización con `Makefile` (con Salvaguarda en `db-reset`)

Ubicación: `Makefile`

Se introduce una confirmación obligatoria en `db-reset` para evitar la pérdida no deseada del catálogo musical indexado, y el target `ingest-songs` para cargar el catálogo desde el CSV (§2.8).

```makefile
.PHONY: db-up db-down db-ps db-logs db-reset db-shell migrate-up migrate-down migrate-status migrate-create seed-create seed-up seed-down ingest-songs sqlc build vet test run-server

DB_COMPOSE := docker compose -f db/docker-compose.yml --env-file db/.env
GOOSE_CMD := set -a; . db/.env; set +a; goose -dir db/migrations postgres "$$DATABASE_URL"

SEED_DIR := db/seed
TRUNCATE_SQL := db/scripts/truncate_all.sql
INGEST_SQL := db/scripts/ingest_songs.sql
DATASET ?= dataset.csv
PSQL_CMD := set -a; . db/.env; set +a; $(DB_COMPOSE) exec -T postgres psql -v ON_ERROR_STOP=1 --single-transaction -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"

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
	@set -a; . db/.env; set +a; $(DB_COMPOSE) exec postgres psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"

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

sqlc:
	@sqlc generate

build:
	@go build ./...

vet:
	@go vet ./...

test:
	@go test ./...

run-server:
	@go run . server --config db/.env
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

Ubicación: `db/scripts/ingest_songs.sql` · Ejecución: `make ingest-songs` (o `make ingest-songs DATASET=ruta/al.csv`)

El pipeline es SQL puro ejecutado por `psql` dentro del contenedor, sin dependencias de Python ni de Go. Reproduce exactamente el procedimiento con el que se cargó el volumen original:

1. **Requisito:** haber ejecutado `make migrate-up`, que crea la tabla vacía.
2. **Guardia de idempotencia:** si `song_metadata` ya tiene filas, el script aborta (`RAISE EXCEPTION`) y no duplica el catálogo.
3. **Carga masiva:** `make ingest-songs` copia el script al contenedor (`docker compose cp`) y envía `dataset.csv` por la entrada estándar. `\copy ... FROM pstdin` lo carga mediante `COPY`. La primera columna del CSV (sin nombre) corresponde a `raw_index`.
4. **Vectorización:** un `UPDATE` arma el vector de 10 dimensiones `[danceability, energy, loudness_norm, speechiness, acousticness, instrumentalness, liveness, valence, tempo_norm, mode]`, con MinMax sobre los límites globales del catálogo:
   - `loudness_norm = (loudness + 58.1) / (6.172 + 58.1)` (rango observado: −58.1 a 6.172 dB).
   - `tempo_norm = tempo / 250` (rango 0 a 250 BPM).
   - El resto de las variables ya está en $[0, 1]$; `mode` (0/1) se convierte a float.
5. **Índice vectorial:** con los datos cargados se construye `idx_song_metadata_embedding_ivfflat` (`ivfflat`, `vector_cosine_ops`, `lists = 1000`, `maintenance_work_mem = 1GB`).
6. Todo corre en una sola transacción (`--single-transaction`, `ON_ERROR_STOP=1`): si algo falla, la tabla queda vacía.

> **Verificación:** en un Postgres temporal, el script cargó 1.159.764 filas en unos 50 s. El checksum de `(raw_index, track_id, embedding)` coincidió exactamente con el del volumen original.

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

\echo 'Cargando CSV en song_metadata...'
\copy song_metadata (raw_index, artist_name, track_name, track_id, popularity, year, genre, danceability, energy, key, loudness, mode, speechiness, acousticness, instrumentalness, liveness, valence, tempo, duration_ms, time_signature) FROM pstdin WITH (FORMAT csv, HEADER true)

-- Embedding de 10 dimensiones con MinMax sobre los límites globales del catálogo:
-- loudness en [-58.1, 6.172] dB y tempo en [0, 250] BPM; el resto ya está en [0, 1]
\echo 'Calculando embeddings...'
UPDATE song_metadata
SET embedding = ARRAY[
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

ANALYZE song_metadata;

\echo 'Construyendo índice IVFFlat...'
SET maintenance_work_mem = '1GB';
CREATE INDEX IF NOT EXISTS idx_song_metadata_embedding_ivfflat ON song_metadata
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 1000);

\echo 'Ingesta completada.'
```

---

## 3. Fase 2: Usuarios, Autenticación (Sin Roles) y Seeding

### 3.1 Migración de Esquema de Usuarios: `create_users_table`

Ubicación: `db/migrations/20261027000002_create_users_table.sql`

Define el usuario adaptado a la red social de música: con handle `@username` único, nombre visible (`display_name`) y foto de perfil (`avatar_url`), sin jerarquía de roles.

```sql
-- +goose Up
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

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);

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
-- Hash bcrypt para 'Password123!': $2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe

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

#### Script de Truncate Seguro: `db/scripts/truncate_all.sql`

Vacía las tablas de negocio pero **omite** `goose_db_version` y **omite** `song_metadata` (para no perder el catálogo de 1.16M canciones durante un vaciado de datos de prueba):

```sql
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public'
      AND tablename NOT IN ('goose_db_version', 'song_metadata')
    ORDER BY tablename
  LOOP
    EXECUTE format('TRUNCATE TABLE %I RESTART IDENTITY CASCADE', r.tablename);
  END LOOP;
END
$$;
```

---

### 3.5 Arquitectura de Código en Go (Clean Architecture / Puertos y Adaptadores)

Estructura de directorios a generar en `junkbox-backend`:

```text
junkbox-backend/
├── cmd/
│   ├── root.go
│   └── server.go
├── config/
│   └── config.go
├── db/
│   ├── docker-compose.yml
│   ├── .env.template
│   ├── migrations/
│   ├── queries/
│   ├── scripts/
│   └── seed/
├── internal/
│   ├── adapters/
│   │   ├── primary/
│   │   │   └── http/
│   │   │       ├── dto/
│   │   │       │   └── auth_dto.go
│   │   │       ├── middleware/
│   │   │       │   └── auth_middleware.go
│   │   │       ├── handlers_auth.go
│   │   │       └── router.go
│   │   └── secondary/
│   │       ├── postgres/
│   │       │   └── db.go
│   │       └── security/
│   │           ├── hasher.go
│   │           └── jwt.go
│   ├── core/
│   │   ├── domain/
│   │   │   └── user.go
│   │   ├── ports/
│   │   │   └── auth_ports.go
│   │   └── services/
│   │       └── auth_service.go
│   └── store/
│       └── sqlc/
├── Makefile
├── README.md
├── implementacion.md
├── sqlc.yaml
└── go.mod
```

---

#### 3.5.1 Dominio y Casos de Uso (`internal/core/services/auth_service.go`)

Interfaces requeridas por el servicio de autenticación:

- `UserStore`: Operaciones `CreateUser`, `GetUserByEmail`, `GetUserByUsername`, `GetUserByID`, `ExistsUserByEmailOrUsername`.
- `TokenStore`: Operaciones de `refresh_tokens`.
- `RevocationStore`: Operaciones de `revoked_access_tokens` y `user_access_revocations`.
- `PasswordHasher`: Hasheo y verificación con `golang.org/x/crypto/bcrypt`.
- `TokenManager`: Generación de JWTs firmados con claims (`user_id`, `username`, `jti`, `iat`, `exp`).

**Métodos de `AuthService`:**

1. `Register(ctx, username, email, password, displayName, avatarURL) (*TokenPair, *UserProfile, error)`:
   - Valida formato de email y longitud de username/password.
   - Verifica unicidad mediante `ExistsUserByEmailOrUsername`.
   - Genera hash bcrypt (coste 12).
   - Inserta usuario en BD.
   - Emite par `TokenPair` (Access + Refresh inicial con nuevo `family_id`).
2. `Login(ctx, identifier, password) (*TokenPair, *UserProfile, error)`:
   - Busca al usuario por email o por username.
   - Verifica contraseña con bcrypt.
   - Genera token par e inserta nuevo refresh token en BD.
3. `Refresh(ctx, rawRefreshToken) (*TokenPair, error)`:
   - Calcula hash SHA-256 del token provisto y busca en `refresh_tokens`.
   - **Detección de Reuso:** Si el token ya estaba revocado (`revoked == true`), se detecta un posible ataque de suplantación: revoca inmediatamente toda la familia (`RevokeRefreshTokenFamily`) y devuelve `ErrRefreshReuse`.
   - Si es válido: marca el token como revocado, emite nuevo refresh token asignando `family_id` idéntico y enlazando `replaced_by`, y emite nuevo access token.
4. `Logout(ctx, claims, rawRefreshToken, allSessions) error`:
   - Agrega el `jti` del Access Token actual a `revoked_access_tokens`.
   - Revoca el refresh token actual o todos los del usuario si `allSessions == true`.
5. `Me(ctx, claims) (*UserProfile, error)`:
   - Resuelve el perfil del usuario autenticado a partir de su `user_id`.

---

#### 3.5.2 Adaptadores Secundarios de Seguridad (`internal/adapters/secondary/security/`)

1. **`hasher.go`**:
   Implementa `PasswordHasher` mediante `bcrypt.GenerateFromPassword(pwd, 12)` y `bcrypt.CompareHashAndPassword(hash, pwd)`.
2. **`jwt.go`**:
   Genera tokens JWT HMAC-SHA256 conteniendo claims:
   - `sub`: `user_id` (UUID).
   - `username`: handle único.
   - `jti`: identificador criptográfico único por token (`gen_random_uuid`).
   - `iat`: marca de tiempo de emisión.
   - `exp`: fecha de expiración (ej. 15 minutos).

---

#### 3.5.3 Adaptadores Primarios HTTP Chi (`internal/adapters/primary/http/`)

1. **DTOs (`dto/auth_dto.go`):**
   - `RegisterRequest`: `username`, `email`, `password`, `display_name`, `avatar_url`.
   - `LoginRequest`: `identifier` (permite email o username), `password`.
   - `RefreshRequest`: `refresh_token`.
   - `LogoutRequest`: `refresh_token`, `all_sessions` (booleano opcional).
   - `AuthResponse`: `access_token`, `refresh_token`, `expires_in`, `user`.
2. **Handlers (`handlers_auth.go`):**
   - Mapeo de errores de dominio (`ErrInvalidCredentials`, `ErrUserAlreadyExists`, `ErrRefreshReuse`) a códigos HTTP semánticos (400, 401, 409, 500).
3. **Middleware (`middleware/auth_middleware.go`):**
   - Extrae el token `Bearer` de la cabecera `Authorization`.
   - Valida la firma criptográfica y expiración del JWT.
   - **Verificación contra Denylist:** Consulta si el `jti` está en `revoked_access_tokens` o si el token fue emitido antes de `user_access_revocations.revoked_before`.
   - Inyecta los claims en el `context.Context` de la petición.
4. **Router (`router.go`):**

   ```go
   r.Route("/api/v1", func(r chi.Router) {
       // Endpoints Públicos
       r.Post("/auth/register", authHandler.Register)
       r.Post("/auth/login", authHandler.Login)
       r.Post("/auth/refresh", authHandler.Refresh)

       // Endpoints Protegidos
       r.Group(func(r chi.Router) {
           r.Use(chimw.AuthMiddleware(jwtManager, revocationChecker))
           r.Get("/auth/me", authHandler.Me)
           r.Post("/auth/logout", authHandler.Logout)
       })
   })
   ```

---

## 4. Guía de Verificación y Pruebas con cURL

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

_Respuesta esperada: `201 Created` con par de tokens y objeto de usuario._

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

_Respuesta esperada: `200 OK` retornando `access_token`, `refresh_token`, `expires_in: 900` y datos del perfil de César Beltrán._

---

### 4. Consultar Perfil con Access Token (`GET /api/v1/auth/me`)

```bash
ACCESS_TOKEN="<pegar_token_aqui>"

curl -i -X GET http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

_Respuesta esperada: `200 OK` con ID, username, email y avatar._

---

### 5. Probar Rotación de Refresh Token (`POST /api/v1/auth/refresh`)

```bash
REFRESH_TOKEN="<pegar_refresh_token_aqui>"

curl -i -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d "{ \"refresh_token\": \"$REFRESH_TOKEN\" }"
```

_Respuesta esperada: `200 OK` con nuevo par de tokens. Si se reintenta enviar el refresh token viejo, debe responder `401 Unauthorized` por revocación de familia._

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
