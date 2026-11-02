-- Crea los artistas del catálogo histórico a partir de song_metadata y
-- enlaza song_metadata.artist_id. Idempotente: solo inserta los nombres que
-- aún no existen y solo actualiza canciones sin artist_id.
-- Uso: make backfill-artists   (requiere make migrate-up)

\echo 'Creando artistas desde song_metadata...'
INSERT INTO artists (name, genres)
SELECT s.artist_name,
       coalesce(array_agg(DISTINCT s.genre) FILTER (WHERE s.genre IS NOT NULL), '{}')
FROM song_metadata s
WHERE s.artist_name IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM artists a WHERE a.name = s.artist_name)
GROUP BY s.artist_name;

-- Si hay canciones por enlazar, el UPDATE reescribe casi toda la tabla: es
-- mucho más rápido quitar el índice GIN de búsqueda (migración
-- 20261027000004) y reconstruirlo al final que mantenerlo fila por fila.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM song_metadata WHERE artist_id IS NULL AND artist_name IS NOT NULL) THEN
    DROP INDEX IF EXISTS idx_song_metadata_search_trgm;
  END IF;
END
$$;

-- Si hay homónimos (un artista de Spotify y otro del CSV), se prefiere el
-- del CSV (spotify_id NULL) y luego el más antiguo.
\echo 'Enlazando song_metadata.artist_id...'
UPDATE song_metadata s
SET artist_id = a.id
FROM (
    SELECT DISTINCT ON (name) name, id
    FROM artists
    ORDER BY name, (spotify_id IS NOT NULL), created_at
) a
WHERE s.artist_id IS NULL
  AND s.artist_name = a.name;

\echo 'Construyendo índice de búsqueda por trigramas (si se quitó)...'
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);

ANALYZE artists;
ANALYZE song_metadata;

\echo 'Backfill de artistas completado.'
