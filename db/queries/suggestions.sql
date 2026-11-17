-- Sugerencias de usuarios (HOME-05). El cálculo por lotes reconstruye las
-- dos tablas dentro de una transacción (postgres.SuggestionStore):
-- primero los perfiles acústicos y luego las sugerencias, que los usan.
-- "Le gusta" es una calificación >= min_score (3.5); las reseñas sin voto
-- no cuentan.

-- --- Cálculo por lotes ---

-- name: DeleteTasteProfiles :exec
DELETE FROM user_taste_profiles;

-- InsertTasteProfiles promedia, dimensión por dimensión, los embeddings de
-- lo que le gusta a cada usuario, ponderados por la calificación. Un álbum
-- aporta el promedio de sus pistas como un solo elemento.
-- name: InsertTasteProfiles :execrows
WITH items AS (
    SELECT r.user_id, s.embedding, r.score::float8 AS weight
    FROM ratings r
    JOIN song_metadata s ON s.id = r.song_id
    WHERE r.score >= sqlc.arg(min_score) AND s.embedding IS NOT NULL
    UNION ALL
    SELECT r.user_id, t.embedding, r.score::float8 AS weight
    FROM ratings r
    CROSS JOIN LATERAL (
        SELECT avg(st.embedding) AS embedding
        FROM song_metadata st
        WHERE st.album_id = r.album_id AND st.embedding IS NOT NULL
    ) t
    WHERE r.album_id IS NOT NULL AND r.score >= sqlc.arg(min_score) AND t.embedding IS NOT NULL
), counts AS (
    SELECT user_id, count(*)::int AS items FROM items GROUP BY user_id
), dims AS (
    SELECT i.user_id, d.ord, sum(d.x * i.weight) / sum(i.weight) AS x
    FROM items i
    CROSS JOIN LATERAL unnest(i.embedding::real[]) WITH ORDINALITY AS d(x, ord)
    GROUP BY i.user_id, d.ord
)
INSERT INTO user_taste_profiles (user_id, embedding, items)
SELECT dims.user_id, array_agg(dims.x ORDER BY dims.ord)::real[]::vector, counts.items
FROM dims
JOIN counts ON counts.user_id = dims.user_id
GROUP BY dims.user_id, counts.items;

-- name: DeleteSuggestions :exec
DELETE FROM user_suggestions;

-- InsertSuggestions arma, para cada usuario, hasta max_per_user
-- sugerencias: primero quienes comparten artistas que les gustan (más
-- artistas en común = más arriba; desempate por similitud acústica) y luego
-- quienes solo tienen un perfil acústico cercano. Un artista "le gusta" si
-- calificó con >= min_score al artista, a un álbum suyo o a una canción
-- suya. Compara todos los perfiles entre sí: con muchos usuarios habría que
-- acotarlo con un índice vectorial.
-- name: InsertSuggestions :execrows
WITH liked_artists AS (
    SELECT DISTINCT r.user_id, coalesce(r.artist_id, al.artist_id, s.artist_id) AS artist_id
    FROM ratings r
    LEFT JOIN albums al ON al.id = r.album_id
    LEFT JOIN song_metadata s ON s.id = r.song_id
    WHERE r.score >= sqlc.arg(min_score)
      AND coalesce(r.artist_id, al.artist_id, s.artist_id) IS NOT NULL
), common AS (
    SELECT a.user_id, b.user_id AS suggested_id, count(*)::int AS common_count,
        (array_agg(a.artist_id ORDER BY ar.popularity DESC NULLS LAST, ar.ratings_count DESC, ar.name))[1:3] AS artist_ids
    FROM liked_artists a
    JOIN liked_artists b ON b.artist_id = a.artist_id AND b.user_id <> a.user_id
    JOIN artists ar ON ar.id = a.artist_id
    GROUP BY a.user_id, b.user_id
), acoustic AS (
    SELECT p.user_id, q.user_id AS suggested_id, (1 - (p.embedding <=> q.embedding))::float8 AS similarity
    FROM user_taste_profiles p
    JOIN user_taste_profiles q ON q.user_id <> p.user_id
), candidates AS (
    SELECT
        coalesce(c.user_id, si.user_id) AS user_id,
        coalesce(c.suggested_id, si.suggested_id) AS suggested_id,
        coalesce(c.common_count, 0) AS common_count,
        coalesce(c.artist_ids, '{}'::uuid[]) AS artist_ids,
        si.similarity
    FROM common c
    FULL JOIN acoustic si ON si.user_id = c.user_id AND si.suggested_id = c.suggested_id
), ranked AS (
    SELECT candidates.*,
        row_number() OVER (
            PARTITION BY user_id
            ORDER BY common_count DESC, similarity DESC NULLS LAST, suggested_id
        ) AS rank
    FROM candidates
)
INSERT INTO user_suggestions (user_id, suggested_id, rank, kind, common_count, common_artist_ids, similarity)
SELECT user_id, suggested_id, rank::int,
    CASE WHEN common_count > 0 THEN 'common_artists' ELSE 'similar_taste' END,
    common_count, artist_ids, similarity
FROM ranked
WHERE rank <= sqlc.arg(max_per_user)::int;

-- --- Lectura (GET /users/suggestions) ---

-- ListSuggestionsForUser devuelve las sugerencias precalculadas sin los
-- usuarios que viewer_id ya sigue (seguir a alguien lo saca al instante).
-- name: ListSuggestionsForUser :many
SELECT
    us.suggested_id, us.kind, us.common_count, us.common_artist_ids,
    us.similarity, us.computed_at,
    u.username, u.display_name, u.avatar_url
FROM user_suggestions us
JOIN users u ON u.id = us.suggested_id
WHERE us.user_id = sqlc.arg(viewer_id)
  AND NOT EXISTS (
    SELECT 1 FROM follows f WHERE f.follower_id = sqlc.arg(viewer_id) AND f.followee_id = us.suggested_id
  )
ORDER BY us.rank
LIMIT sqlc.arg(max_results)::int;

-- ListPopularUsers completa la lista: los usuarios con más seguidores
-- (desempate: más reseñas), sin viewer_id, sin los que ya sigue y sin los
-- de exclude_ids (ya sugeridos).
-- name: ListPopularUsers :many
SELECT u.id, u.username, u.display_name, u.avatar_url
FROM users u
WHERE u.id <> sqlc.arg(viewer_id)
  AND u.id <> ALL(sqlc.arg(exclude_ids)::uuid[])
  AND NOT EXISTS (
    SELECT 1 FROM follows f WHERE f.follower_id = sqlc.arg(viewer_id) AND f.followee_id = u.id
  )
ORDER BY
    (SELECT count(*) FROM follows f WHERE f.followee_id = u.id) DESC,
    (SELECT count(*) FROM reviews r WHERE r.user_id = u.id) DESC,
    u.created_at, u.id
LIMIT sqlc.arg(max_results)::int;

-- name: ListArtistsByIDs :many
SELECT id, name, image_url FROM artists WHERE id = ANY(sqlc.arg(ids)::uuid[]);
