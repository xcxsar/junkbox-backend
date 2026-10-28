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
