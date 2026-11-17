-- Recomendaciones del feed de Inicio (HOME-01). Port de recommend_songs del
-- notebook de ../recommendation-engine: similitud coseno (<=>) contra el
-- embedding de una canción semilla, con filtro opcional de género.
-- El embedding viaja como texto ('[a,b,...]'): sqlc no conoce el tipo vector.
-- Las columnas de las canciones son las de SearchTracksTrigram (sin score)
-- para poder completar imágenes igual que la búsqueda, y son idénticas en
-- RecommendSimilarTracks y ListPopularTracks: el servicio convierte una fila
-- en la otra.

-- GetRecommendationSeed devuelve la canción con embedding que el usuario
-- calificó o reseñó más recientemente (por updated_at: editar cuenta). Las
-- calificaciones y reseñas de álbumes o artistas no cuentan.
-- name: GetRecommendationSeed :one
WITH activity AS (
    SELECT r.song_id, r.updated_at FROM ratings r
    WHERE r.user_id = sqlc.arg(user_id) AND r.song_id IS NOT NULL
    UNION ALL
    SELECT v.song_id, v.updated_at FROM reviews v
    WHERE v.user_id = sqlc.arg(user_id) AND v.song_id IS NOT NULL
)
SELECT s.id, s.track_id, s.track_name, s.artist_name, s.genre,
    s.embedding::text AS embedding
FROM activity a
JOIN song_metadata s ON s.id = a.song_id
WHERE s.embedding IS NOT NULL
ORDER BY a.updated_at DESC, s.id
LIMIT 1;

-- RecommendSimilarTracks excluye la semilla, sus duplicados (mismo título y
-- artista con otro track_id) y lo que el usuario ya calificó o reseñó.
-- Con genre NULL no filtra por género.
-- El índice IVFFlat solo revisa ivfflat.probes listas: con un filtro de
-- género puede devolver menos de max_results. El adaptador ejecuta esta
-- consulta con ivfflat.iterative_scan = relaxed_order, que sigue revisando
-- listas hasta llenar el LIMIT pero entrega un orden aproximado; el CTE
-- materializado lo reordena por distancia (patrón de la doc. de pgvector).
-- name: RecommendSimilarTracks :many
WITH nearest AS MATERIALIZED (
    SELECT n.id, n.embedding <=> sqlc.arg(embedding)::text::vector AS distance
    FROM song_metadata n
    WHERE n.id <> sqlc.arg(seed_id)::int
      AND n.embedding IS NOT NULL
      AND (sqlc.narg(genre)::text IS NULL OR n.genre = sqlc.narg(genre)::text)
      AND NOT (lower(coalesce(n.track_name, '')) = lower(sqlc.arg(seed_track_name)::text)
           AND lower(coalesce(n.artist_name, '')) = lower(sqlc.arg(seed_artist_name)::text))
      AND NOT EXISTS (SELECT 1 FROM ratings r WHERE r.song_id = n.id AND r.user_id = sqlc.arg(user_id))
      AND NOT EXISTS (SELECT 1 FROM reviews v WHERE v.song_id = n.id AND v.user_id = sqlc.arg(user_id))
    ORDER BY distance
    LIMIT sqlc.arg(max_results)::int
)
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.year, s.genre,
    s.popularity, s.duration_ms, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title,
    al.image_url AS album_image_url,
    ar.image_url AS artist_image_url,
    ar.spotify_id AS artist_spotify_id,
    (ar.enriched_at IS NOT NULL)::bool AS artist_enriched,
    coalesce(ar.enrich_attempts, 0)::int AS artist_enrich_attempts,
    s.album_id, s.link_attempts
FROM nearest
JOIN song_metadata s ON s.id = nearest.id
LEFT JOIN albums al ON al.id = s.album_id
LEFT JOIN artists ar ON ar.id = s.artist_id
ORDER BY nearest.distance, s.id;

-- ListPopularTracks es el respaldo cuando el usuario no tiene semilla: las
-- canciones más populares (Spotify) con embedding, sin lo que ya calificó o
-- reseñó y sin repetir título y artista. Los candidatos salen de
-- idx_song_metadata_popularity; se piden de más para que deduplicar no
-- deje la lista corta.
-- name: ListPopularTracks :many
WITH candidates AS (
    SELECT c.id, c.popularity,
        lower(coalesce(c.track_name, '')) AS track_key,
        lower(coalesce(c.artist_name, '')) AS artist_key
    FROM song_metadata c
    WHERE c.embedding IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM ratings r WHERE r.song_id = c.id AND r.user_id = sqlc.arg(user_id))
      AND NOT EXISTS (SELECT 1 FROM reviews v WHERE v.song_id = c.id AND v.user_id = sqlc.arg(user_id))
    ORDER BY c.popularity DESC NULLS LAST, c.id
    LIMIT sqlc.arg(max_results)::int * 4
), unique_songs AS (
    SELECT DISTINCT ON (track_key, artist_key) id
    FROM candidates
    ORDER BY track_key, artist_key, popularity DESC NULLS LAST, id
)
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.year, s.genre,
    s.popularity, s.duration_ms, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title,
    al.image_url AS album_image_url,
    ar.image_url AS artist_image_url,
    ar.spotify_id AS artist_spotify_id,
    (ar.enriched_at IS NOT NULL)::bool AS artist_enriched,
    coalesce(ar.enrich_attempts, 0)::int AS artist_enrich_attempts,
    s.album_id, s.link_attempts
FROM unique_songs u
JOIN song_metadata s ON s.id = u.id
LEFT JOIN albums al ON al.id = s.album_id
LEFT JOIN artists ar ON ar.id = s.artist_id
ORDER BY s.popularity DESC NULLS LAST, s.id
LIMIT sqlc.arg(max_results)::int;
