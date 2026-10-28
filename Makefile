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
