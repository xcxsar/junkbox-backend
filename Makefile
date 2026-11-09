.PHONY: db-up db-down db-ps db-logs db-reset db-shell migrate-up migrate-down migrate-status migrate-create seed-create seed-up seed-down ingest-songs backfill-artists sqlc build vet test run-server \
	k8s-up k8s-cluster k8s-deps k8s-images k8s-base k8s-secrets k8s-migrate k8s-apply k8s-redeploy k8s-status k8s-logs k8s-down

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

# --- Kubernetes local (kind): 4 réplicas detrás de Envoy Gateway ---
# Postgres y Mailpit siguen en docker compose, unidos a la red Docker "kind".
KIND_CLUSTER := junkbox
K8S_DIR := deploy/k8s
K8S_SECRETS := $(K8S_DIR)/secrets
KUBECTL := kubectl --context kind-$(KIND_CLUSTER)
KUBECTL_NS := $(KUBECTL) -n junkbox
KIND_COMPOSE := docker compose -f db/docker-compose.yml -f deploy/compose.kind.yml --env-file .env
ENVOY_GATEWAY_VERSION := v1.9.3
REPLICAS := 1 2 3 4

# Levanta todo en orden: cluster → Postgres/Mailpit → imágenes → base → secretos → migraciones → réplicas.
k8s-up: k8s-cluster k8s-deps k8s-images k8s-base k8s-secrets k8s-migrate k8s-apply

# Crea el cluster (si no existe) e instala Envoy Gateway con los CRDs de Gateway API.
k8s-cluster:
	@kind get clusters 2>/dev/null | grep -qx $(KIND_CLUSTER) || kind create cluster --config $(K8S_DIR)/kind-config.yaml
	@$(KUBECTL) apply --server-side -f https://github.com/envoyproxy/gateway/releases/download/$(ENVOY_GATEWAY_VERSION)/install.yaml
	@$(KUBECTL) wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available

# Recrea los contenedores de Postgres y Mailpit unidos también a la red "kind"
# (el volumen con el catálogo se conserva). Un `make db-up` posterior los saca de esa red.
k8s-deps:
	@$(KIND_COMPOSE) up -d --wait

k8s-images:
	@docker build --target server -t junkbox-backend:dev .
	@docker build --target migrate -t junkbox-migrate:dev .
	@kind load docker-image --name $(KIND_CLUSTER) junkbox-backend:dev junkbox-migrate:dev

# Namespace, ConfigMap, Service y Gateway (lo que no depende de los secretos).
k8s-base:
	@$(KUBECTL) apply -f $(K8S_DIR)/gatewayclass.yaml
	@$(KUBECTL) apply -k $(K8S_DIR)/base

# Crea o actualiza los Secrets desde deploy/k8s/secrets/*.env (ver los .template).
k8s-secrets:
	@for f in common $(addprefix freqblog-,$(REPLICAS)); do \
		[ -f $(K8S_SECRETS)/$$f.env ] || { echo "Falta $(K8S_SECRETS)/$$f.env (copiar del .template)"; exit 1; }; \
	done
	@$(KUBECTL_NS) create secret generic junkbox-common --from-env-file=$(K8S_SECRETS)/common.env --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@for n in $(REPLICAS); do \
		$(KUBECTL_NS) create secret generic freqblog-$$n --from-env-file=$(K8S_SECRETS)/freqblog-$$n.env --dry-run=client -o yaml | $(KUBECTL) apply -f - || exit 1; \
	done

# El Job es inmutable: se borra y se vuelve a crear para correr goose up otra vez.
k8s-migrate:
	@$(KUBECTL_NS) delete job migrate --ignore-not-found
	@$(KUBECTL) apply -k $(K8S_DIR)/migrate
	@$(KUBECTL_NS) wait --timeout=2m job/migrate --for=condition=Complete || { $(KUBECTL_NS) logs job/migrate; exit 1; }
	@$(KUBECTL_NS) logs job/migrate

k8s-apply:
	@$(KUBECTL) apply -k $(K8S_DIR)
	@for n in $(REPLICAS); do $(KUBECTL_NS) rollout status deployment/backend-$$n --timeout=2m || exit 1; done

# Tras cambiar el código: reconstruye, recarga las imágenes y reinicia las réplicas de a una.
k8s-redeploy: k8s-images
	@for n in $(REPLICAS); do \
		$(KUBECTL_NS) rollout restart deployment/backend-$$n && \
		$(KUBECTL_NS) rollout status deployment/backend-$$n --timeout=2m || exit 1; \
	done

k8s-status:
	@$(KUBECTL_NS) get pods,services,gateways,httproutes,jobs -o wide

k8s-logs:
	@$(KUBECTL_NS) logs -f -l app=backend --prefix --tail=50 --max-log-requests=8

# Borra el cluster. Postgres, Mailpit y el volumen del catálogo no se tocan.
k8s-down:
	@kind delete cluster --name $(KIND_CLUSTER)
