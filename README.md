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

1. Copia la plantilla de variables de entorno ubicada en `db/.env.template` hacia `db/.env`:

```bash
cp db/.env.template db/.env
```

2. Configura los parámetros en `db/.env`:

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

> `db/.env` contiene secretos locales y está ignorado en Git mediante `.gitignore`.

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

El catálogo musical de Junkbox utiliza la tabla `song_metadata` con soporte de búsqueda semántica mediante vectores de características de audio (`vector(10)` en `pgvector`).

Para garantizar la replicabilidad del proyecto en cualquier entorno limpio sin depender de transferencias manuales de volúmenes Docker:
1. La migración inicial crea la tabla y sus índices B-tree (`artist_name`, `track_id`, `genre`).
2. El script `db/scripts/ingest_songs.sql` carga el CSV fuente con `COPY` y calcula los vectores con MinMax Scaling (loudness y tempo). Al final construye el índice vectorial IVFFlat (`lists = 1000`, distancia coseno):
   ```bash
   make migrate-up
   make ingest-songs                       # usa ./dataset.csv
   make ingest-songs DATASET=ruta/al.csv   # CSV en otra ubicación
   ```
   El script aborta si `song_metadata` ya contiene datos, así que no duplica el catálogo.
3. `dataset.csv` (~175 MB) está en `.gitignore` y se distribuye por separado.
4. Ver detalles del pipeline en [`implementacion.md`](implementacion.md#28-especificación-del-pipeline-de-ingesta-desde-csv).

> El `Down` de la migración inicial **no** elimina `song_metadata`; el catálogo solo se destruye con `make db-reset`, que pide confirmación.

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
  *(Ejecuta `go run . server --config db/.env`; por defecto escucha en `http://localhost:8080`)*.

---

## Puesta en Marcha Rápida

```bash
cp db/.env.template db/.env   # y ajustar contraseña / JWT_SECRET
make db-up
make migrate-up
make ingest-songs             # solo en una base de datos nueva
make seed-up                  # 4 usuarios del equipo, contraseña Password123!
make run-server
```

---

## API REST

Base: `http://localhost:8080/api/v1`

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/healthz` (sin prefijo) | — | Healthcheck (`200 ok`) |
| `POST` | `/auth/register` | — | Registro (`username`, `email`, `password`, `display_name?`, `avatar_url?`) → `201` + tokens + perfil |
| `POST` | `/auth/login` | — | Login con `identifier` (email o username) y `password` → tokens + perfil |
| `POST` | `/auth/refresh` | — | Rota el `refresh_token`; reusar uno viejo revoca toda la sesión |
| `GET` | `/auth/me` | Bearer | Perfil del usuario autenticado |
| `POST` | `/auth/logout` | Bearer | Revoca el access token actual y el `refresh_token`; con `all_sessions: true` cierra todas las sesiones |

Access Token: JWT HS256 de 15 minutos (`sub`, `username`, `jti`, `iat`, `exp`). Refresh Token: opaco, de 7 días, guardado como hash SHA-256. Errores: `{"error": "..."}` con 400, 401, 409 o 500. Detalles y ejemplos cURL en [`implementacion.md`](implementacion.md#4-guía-de-verificación-y-pruebas-con-curl).
