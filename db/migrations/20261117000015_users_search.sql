-- +goose Up

-- SEARCH-04: búsqueda de usuarios por username y nombre visible, sin
-- tildes ni mayúsculas (f_unaccent, migración 20261117000012). El índice
-- sirve para los prefijos (LIKE 'q%', desde 3 caracteres) y para la
-- coincidencia aproximada (<<%). La expresión de SearchUsers debe coincidir
-- con esta.
CREATE INDEX IF NOT EXISTS idx_users_search_trgm ON users
    USING gin (lower(f_unaccent(username || ' ' || coalesce(display_name, ''))) gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_users_search_trgm;
