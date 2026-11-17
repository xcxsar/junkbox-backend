-- +goose Up

-- SEARCH-01: la búsqueda de canciones ignora tildes ("Zoe" encuentra a
-- Zoé). unaccent() es STABLE (depende del diccionario configurado) y no
-- sirve en un índice; f_unaccent fija el diccionario y se declara
-- IMMUTABLE. Las expresiones de SearchTracksTrigram deben coincidir con la
-- del índice.
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE OR REPLACE FUNCTION f_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

-- Reemplaza al índice sin unaccent (crear el nuevo tarda unos minutos con
-- 1.16M canciones y bloquea las escrituras en song_metadata mientras tanto).
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_unaccent_trgm ON song_metadata
    USING gin (f_unaccent(coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);
DROP INDEX IF EXISTS idx_song_metadata_search_trgm;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);
DROP INDEX IF EXISTS idx_song_metadata_search_unaccent_trgm;
DROP FUNCTION IF EXISTS f_unaccent(text);
DROP EXTENSION IF EXISTS unaccent;
