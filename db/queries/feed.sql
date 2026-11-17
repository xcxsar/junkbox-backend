-- Feeds de reseñas de Inicio (HOME-04). Devuelven las mismas columnas que
-- ListReviewsByTarget (reviews.sql). Paginan con keyset: se pide un
-- elemento de más para saber si hay otra página, y el cursor es la última
-- fila entregada. Todos los campos del cursor van juntos: si cursor_id es
-- NULL se devuelve la primera página.

-- ListPopularFeed ("Para ti"): reseñas de otros usuarios agrupadas por el día
-- local de la zona tz (del más reciente al más antiguo) y, dentro de cada
-- día, por popularity_score (Me gusta + 2·comentarios). El orden es
-- (día, puntaje, fecha, id) descendente en todas las columnas, así que la
-- comparación de filas con el cursor es un solo "<". cursor_day viaja como
-- texto (AAAA-MM-DD) para que el driver no lo convierta de zona.
-- name: ListPopularFeed :many
SELECT
    r.id, r.user_id, r.song_id, r.album_id, r.artist_id, r.title, r.content,
    r.likes_count, r.created_at, r.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    rat.score AS rating_score,
    r.comments_count, r.popularity_score,
    EXISTS (SELECT 1 FROM review_likes l WHERE l.review_id = r.id AND l.user_id = sqlc.arg(viewer_id))::bool AS liked_by_me,
    EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = sqlc.arg(viewer_id) AND f.followee_id = r.user_id)::bool AS following_author
FROM reviews r
JOIN users u ON u.id = r.user_id
LEFT JOIN ratings rat ON rat.user_id = r.user_id
    AND (rat.song_id = r.song_id OR rat.album_id = r.album_id OR rat.artist_id = r.artist_id)
WHERE r.user_id <> sqlc.arg(viewer_id)
  AND (
    sqlc.narg(cursor_id)::uuid IS NULL
    OR ((r.created_at AT TIME ZONE sqlc.arg(tz)::text)::date, r.popularity_score, r.created_at, r.id)
       < (sqlc.narg(cursor_day)::text::date, sqlc.narg(cursor_score)::int, sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY (r.created_at AT TIME ZONE sqlc.arg(tz)::text)::date DESC, r.popularity_score DESC, r.created_at DESC, r.id DESC
LIMIT sqlc.arg(max_results)::int;

-- ListFollowingFeed ("Siguiendo"): reseñas de los usuarios que sigue
-- viewer_id, de la más nueva a la más antigua.
-- name: ListFollowingFeed :many
SELECT
    r.id, r.user_id, r.song_id, r.album_id, r.artist_id, r.title, r.content,
    r.likes_count, r.created_at, r.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    rat.score AS rating_score,
    r.comments_count, r.popularity_score,
    EXISTS (SELECT 1 FROM review_likes l WHERE l.review_id = r.id AND l.user_id = sqlc.arg(viewer_id))::bool AS liked_by_me,
    TRUE::bool AS following_author
FROM reviews r
JOIN follows fo ON fo.followee_id = r.user_id AND fo.follower_id = sqlc.arg(viewer_id)
JOIN users u ON u.id = r.user_id
LEFT JOIN ratings rat ON rat.user_id = r.user_id
    AND (rat.song_id = r.song_id OR rat.album_id = r.album_id OR rat.artist_id = r.artist_id)
WHERE sqlc.narg(cursor_id)::uuid IS NULL
   OR (r.created_at, r.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg(max_results)::int;
