-- +goose Up

-- SEARCH-02: discografía de un artista y pistas completas de un álbum.
-- Las marcas *_synced_at evitan volver a consultar Spotify: los álbumes y
-- las canciones de un artista se re-sincronizan pasado un TTL; las pistas
-- de un álbum, una sola vez. disc_number y track_number ordenan las pistas
-- del álbum (nulos en las canciones del CSV hasta que se importa su álbum).
ALTER TABLE artists
    ADD COLUMN albums_synced_at TIMESTAMPTZ,
    ADD COLUMN tracks_synced_at TIMESTAMPTZ;

ALTER TABLE albums ADD COLUMN tracks_synced_at TIMESTAMPTZ;

ALTER TABLE song_metadata
    ADD COLUMN disc_number INT,
    ADD COLUMN track_number INT;

-- +goose Down
ALTER TABLE song_metadata DROP COLUMN IF EXISTS track_number, DROP COLUMN IF EXISTS disc_number;
ALTER TABLE albums DROP COLUMN IF EXISTS tracks_synced_at;
ALTER TABLE artists DROP COLUMN IF EXISTS tracks_synced_at, DROP COLUMN IF EXISTS albums_synced_at;
