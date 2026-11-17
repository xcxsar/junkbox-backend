-- +goose Up

-- Sugerencias de usuarios (HOME-05). Las calcula por lotes el worker de
-- sugerencias (o `junkbox-backend suggestions refresh`); el endpoint solo
-- lee, filtra lo que ya sigues y completa con populares.

-- user_taste_profiles: perfil acústico de cada usuario, el promedio de los
-- embeddings de las canciones y álbumes que calificó con 3.5 o más,
-- ponderado por la calificación. items es cuántas canciones/álbumes lo
-- forman.
CREATE TABLE user_taste_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    embedding vector(10) NOT NULL,
    items INT NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- user_suggestions: sugerencias precalculadas, ya ordenadas (rank 1 = la
-- primera). kind es la señal que la produjo: common_artists (artistas que
-- ambos calificaron con 3.5 o más) o similar_taste (perfil acústico
-- cercano). common_artist_ids va ordenado por popularidad del artista.
CREATE TABLE user_suggestions (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    suggested_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rank INT NOT NULL,
    kind VARCHAR(20) NOT NULL,
    common_count INT NOT NULL DEFAULT 0,
    common_artist_ids UUID[] NOT NULL DEFAULT '{}',
    similarity DOUBLE PRECISION,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, suggested_id),
    CONSTRAINT user_suggestions_kind CHECK (kind IN ('common_artists', 'similar_taste')),
    CONSTRAINT user_suggestions_not_self CHECK (user_id <> suggested_id)
);

CREATE INDEX idx_user_suggestions_rank ON user_suggestions(user_id, rank);

-- +goose Down
DROP TABLE IF EXISTS user_suggestions;
DROP TABLE IF EXISTS user_taste_profiles;
