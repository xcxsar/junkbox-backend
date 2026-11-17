-- +goose Up

-- Respaldo "populares" del feed de Inicio (ListPopularTracks): sin este
-- índice ordenar las 1.16M canciones por popularidad tarda ~600 ms. Es
-- parcial porque solo se recomiendan canciones con embedding.
CREATE INDEX IF NOT EXISTS idx_song_metadata_popularity ON song_metadata (popularity DESC NULLS LAST, id)
    WHERE embedding IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_song_metadata_popularity;
