-- +goose Up

-- SEARCH-01: la búsqueda de artistas y álbumes también ignora tildes
-- ("Zoe" encuentra a Zoé). Usa f_unaccent de la migración 20261117000012;
-- las expresiones de SearchArtistsTrigram y SearchAlbumsTrigram deben
-- coincidir con las de estos índices.
CREATE INDEX IF NOT EXISTS idx_artists_name_unaccent_trgm ON artists
    USING gin (f_unaccent(name) gin_trgm_ops);
DROP INDEX IF EXISTS idx_artists_name_trgm;

CREATE INDEX IF NOT EXISTS idx_albums_search_unaccent_trgm ON albums
    USING gin (f_unaccent(title || ' ' || artist_name) gin_trgm_ops);
DROP INDEX IF EXISTS idx_albums_search_trgm;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_artists_name_trgm ON artists USING gin (name gin_trgm_ops);
DROP INDEX IF EXISTS idx_artists_name_unaccent_trgm;
CREATE INDEX IF NOT EXISTS idx_albums_search_trgm ON albums USING gin ((title || ' ' || artist_name) gin_trgm_ops);
DROP INDEX IF EXISTS idx_albums_search_unaccent_trgm;
