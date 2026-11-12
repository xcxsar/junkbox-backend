-- +goose Up

-- Enriquecimiento del catálogo con Spotify para que la búsqueda devuelva
-- imágenes. Los DEFAULT constantes no reescriben las 1.16M filas de
-- song_metadata.

-- enriched_at marca un artista ya consultado en GET /v1/artists/{id}, tenga
-- o no imagen, para no volver a pedirlo. enrich_attempts y
-- next_enrich_attempt_at controlan los reintentos ante errores del
-- proveedor y sirven de lease para el worker de respaldo.
ALTER TABLE artists
    ADD COLUMN enriched_at TIMESTAMPTZ,
    ADD COLUMN enrich_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN next_enrich_attempt_at TIMESTAMPTZ;

-- Canciones del CSV sin álbum: se enlazan con GET /v1/tracks/{id}.
ALTER TABLE song_metadata
    ADD COLUMN link_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN next_link_attempt_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE song_metadata
    DROP COLUMN IF EXISTS next_link_attempt_at,
    DROP COLUMN IF EXISTS link_attempts;

ALTER TABLE artists
    DROP COLUMN IF EXISTS next_enrich_attempt_at,
    DROP COLUMN IF EXISTS enrich_attempts,
    DROP COLUMN IF EXISTS enriched_at;
