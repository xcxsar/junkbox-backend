-- Ingesta del catálogo musical desde dataset.csv (recibido por la entrada estándar de psql).
-- Uso: make ingest-songs   (requiere haber ejecutado make migrate-up)

-- Aborta si el catálogo ya está cargado, para no duplicar filas
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM song_metadata) THEN
    RAISE EXCEPTION 'song_metadata ya contiene datos; ingesta cancelada';
  END IF;
END
$$;

-- El índice GIN de búsqueda (migración 20261027000004) se reconstruye al final:
-- mantenerlo fila por fila durante la carga y el UPDATE es mucho más lento.
DROP INDEX IF EXISTS idx_song_metadata_search_trgm;

\echo 'Cargando CSV en song_metadata...'
\copy song_metadata (raw_index, artist_name, track_name, track_id, popularity, year, genre, danceability, energy, key, loudness, mode, speechiness, acousticness, instrumentalness, liveness, valence, tempo, duration_ms, time_signature) FROM pstdin WITH (FORMAT csv, HEADER true)

-- Un artista por cada artist_name distinto (igual que db/scripts/backfill_artists.sql),
-- para enlazar artist_id en el mismo UPDATE que calcula los embeddings y no
-- reescribir la tabla dos veces.
\echo 'Creando artistas...'
INSERT INTO artists (name, genres)
SELECT s.artist_name,
       coalesce(array_agg(DISTINCT s.genre) FILTER (WHERE s.genre IS NOT NULL), '{}')
FROM song_metadata s
WHERE s.artist_name IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM artists a WHERE a.name = s.artist_name)
GROUP BY s.artist_name;

-- Embedding de 10 dimensiones con MinMax sobre los límites globales del catálogo:
-- loudness en [-58.1, 6.172] dB y tempo en [0, 250] BPM; el resto ya está en [0, 1]
\echo 'Calculando embeddings y enlazando artistas...'
UPDATE song_metadata s
SET artist_id = (
        SELECT a.id FROM artists a
        WHERE a.name = s.artist_name
        ORDER BY (a.spotify_id IS NOT NULL), a.created_at
        LIMIT 1
    ),
    embedding = ARRAY[
    danceability,
    energy,
    (loudness - (-58.1)) / (6.172 - (-58.1)),
    speechiness,
    acousticness,
    instrumentalness,
    liveness,
    valence,
    (tempo - 0.0) / 250.0,
    mode::float
]::vector
WHERE embedding IS NULL;

ANALYZE artists;
ANALYZE song_metadata;

\echo 'Construyendo índice IVFFlat...'
SET maintenance_work_mem = '1GB';
CREATE INDEX IF NOT EXISTS idx_song_metadata_embedding_ivfflat ON song_metadata
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 1000);

\echo 'Construyendo índice de búsqueda por trigramas...'
CREATE INDEX IF NOT EXISTS idx_song_metadata_search_trgm ON song_metadata
    USING gin ((coalesce(track_name, '') || ' ' || coalesce(artist_name, '')) gin_trgm_ops);

\echo 'Ingesta completada.'
