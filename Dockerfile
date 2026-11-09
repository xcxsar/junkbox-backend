# syntax=docker/dockerfile:1

# Imágenes del backend para Kubernetes (ver deploy/k8s):
#   docker build --target server  -t junkbox-backend:dev .
#   docker build --target migrate -t junkbox-migrate:dev .

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/junkbox-backend .
# goose aplica las migraciones desde el Job de Kubernetes.
RUN CGO_ENABLED=0 GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.29.0

# Job de migraciones: goose lee la conexión de GOOSE_DBSTRING.
FROM gcr.io/distroless/static-debian12:nonroot AS migrate
COPY --from=build /out/goose /goose
COPY db/migrations /migrations
ENV GOOSE_DRIVER=postgres GOOSE_MIGRATION_DIR=/migrations
ENTRYPOINT ["/goose", "up"]

# API REST: la configuración llega por variables de entorno (envFrom).
FROM gcr.io/distroless/static-debian12:nonroot AS server
COPY --from=build /out/junkbox-backend /junkbox-backend
EXPOSE 8080
ENTRYPOINT ["/junkbox-backend", "server"]
