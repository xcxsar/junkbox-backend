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
