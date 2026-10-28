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

\echo 'Cargando CSV en song_metadata...'
\copy song_metadata (raw_index, artist_name, track_name, track_id, popularity, year, genre, danceability, energy, key, loudness, mode, speechiness, acousticness, instrumentalness, liveness, valence, tempo, duration_ms, time_signature) FROM pstdin WITH (FORMAT csv, HEADER true)

-- Embedding de 10 dimensiones con MinMax sobre los límites globales del catálogo:
-- loudness en [-58.1, 6.172] dB y tempo en [0, 250] BPM; el resto ya está en [0, 1]
\echo 'Calculando embeddings...'
UPDATE song_metadata
SET embedding = ARRAY[
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

ANALYZE song_metadata;

\echo 'Construyendo índice IVFFlat...'
SET maintenance_work_mem = '1GB';
CREATE INDEX IF NOT EXISTS idx_song_metadata_embedding_ivfflat ON song_metadata
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 1000);

\echo 'Ingesta completada.'
