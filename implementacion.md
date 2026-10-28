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
    - [3.5.4 Pruebas Unitarias (`make test`)](#354-pruebas-unitarias-make-test)
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

# Dirección de escucha del servidor HTTP
HTTP_ADDR=":8080"
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

Creado en la Fase 1 y usado por `make seed-down`. Vacía las tablas de negocio pero **omite** `goose_db_version` y **omite** `song_metadata` (para no perder el catálogo de 1.16M canciones durante un vaciado de datos de prueba):

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

El código parte del de `t-box-backend` (sin RBAC, flotas, MQTT ni SSE). Dependencias: `go-chi/chi/v5`, `golang-jwt/jwt/v5`, `google/uuid`, `jackc/pgx/v5` (driver de `database/sql`), `joho/godotenv`, `spf13/cobra` y `golang.org/x/crypto/bcrypt`.

Estructura de directorios:

```text
junkbox-backend/
├── main.go                      # invoca cmd.Execute()
├── cmd/
│   ├── root.go                  # CLI Cobra, flag --config
│   └── server.go                # subcomando `server`: arma dependencias, purga denylist, shutdown elegante
├── config/
│   └── config.go                # DATABASE_URL, JWT_SECRET (≥32), ACCESS/REFRESH_TOKEN_TTL, HTTP_ADDR
├── db/
│   ├── docker-compose.yml
│   ├── .env.template
│   ├── migrations/
│   ├── queries/                 # song_metadata.sql, users.sql, refresh_tokens.sql, revoked_tokens.sql
│   ├── scripts/                 # ingest_songs.sql, truncate_all.sql
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
│   │   │       └── router.go
│   │   └── secondary/
│   │       ├── postgres/
│   │       │   └── db.go                         # pool database/sql + pgx, con ping
│   │       └── security/
│   │           ├── hasher.go
│   │           ├── jwt.go
│   │           └── jwt_test.go
│   ├── core/
│   │   ├── domain/
│   │   │   └── user.go          # UserProfile, TokenPair, AccessClaims, errores de dominio
│   │   ├── ports/
│   │   │   └── auth_ports.go    # UserStore, TokenStore, RevocationStore, PasswordHasher, TokenManager
│   │   └── services/
│   │       ├── auth_service.go
│   │       └── auth_service_test.go
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
     - password de 8 a 72 bytes (bcrypt ignora lo que pase de 72);
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
   - Carga la configuración con `--config db/.env`; las variables del entorno tienen prioridad.
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
