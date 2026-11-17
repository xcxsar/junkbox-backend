-- Seguidores y Me gusta (HOME-04).

-- GetPublicProfile acepta el UUID o el username; el otro se pasa vacío
-- (uuid.Nil / ''). followed_by_me: viewer_id sigue a este usuario.
-- name: GetPublicProfile :one
SELECT
    u.id, u.username, u.display_name, u.avatar_url, u.created_at,
    (SELECT count(*) FROM follows f WHERE f.followee_id = u.id) AS followers_count,
    (SELECT count(*) FROM follows f WHERE f.follower_id = u.id) AS following_count,
    (SELECT count(*) FROM reviews r WHERE r.user_id = u.id) AS reviews_count,
    EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = sqlc.arg(viewer_id) AND f.followee_id = u.id)::bool AS followed_by_me
FROM users u
WHERE u.id = sqlc.arg(id)::uuid OR u.username = sqlc.arg(username)::text
LIMIT 1;

-- Seguir es idempotente: un segundo POST no falla.
-- name: FollowUser :exec
INSERT INTO follows (follower_id, followee_id)
VALUES (sqlc.arg(follower_id), sqlc.arg(followee_id))
ON CONFLICT DO NOTHING;

-- name: UnfollowUser :exec
DELETE FROM follows
WHERE follower_id = sqlc.arg(follower_id) AND followee_id = sqlc.arg(followee_id);

-- Las listas paginan con keyset sobre (created_at, id del otro usuario),
-- del seguimiento más reciente al más antiguo. followed_by_me es si
-- viewer_id sigue a cada usuario de la lista.

-- name: ListFollowers :many
SELECT
    u.id, u.username, u.display_name, u.avatar_url,
    f.created_at AS followed_at,
    EXISTS (SELECT 1 FROM follows v WHERE v.follower_id = sqlc.arg(viewer_id) AND v.followee_id = u.id)::bool AS followed_by_me
FROM follows f
JOIN users u ON u.id = f.follower_id
WHERE f.followee_id = sqlc.arg(user_id)
  AND (sqlc.narg(cursor_id)::uuid IS NULL
       OR (f.created_at, f.follower_id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY f.created_at DESC, f.follower_id DESC
LIMIT sqlc.arg(max_results)::int;

-- name: ListFollowing :many
SELECT
    u.id, u.username, u.display_name, u.avatar_url,
    f.created_at AS followed_at,
    EXISTS (SELECT 1 FROM follows v WHERE v.follower_id = sqlc.arg(viewer_id) AND v.followee_id = u.id)::bool AS followed_by_me
FROM follows f
JOIN users u ON u.id = f.followee_id
WHERE f.follower_id = sqlc.arg(user_id)
  AND (sqlc.narg(cursor_id)::uuid IS NULL
       OR (f.created_at, f.followee_id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY f.created_at DESC, f.followee_id DESC
LIMIT sqlc.arg(max_results)::int;

-- SearchUsers (SEARCH-04) busca por username y nombre visible sin tildes ni
-- mayúsculas; t es la expresión de idx_users_search_trgm. Coincide por
-- prefijo del username, prefijo de una palabra del nombre ("gar" → "Lily
-- García") o de forma aproximada (<<%, tolera erratas). p es la consulta
-- con %, _ y \ escapados para LIKE. Excluye a viewer_id.
-- name: SearchUsers :many
WITH input AS (
    SELECT lower(f_unaccent(sqlc.arg(query)::text)) AS q
), pattern AS (
    SELECT q, replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_') AS p
    FROM input
)
SELECT
    u.id, u.username, u.display_name, u.avatar_url,
    EXISTS (SELECT 1 FROM follows v WHERE v.follower_id = sqlc.arg(viewer_id) AND v.followee_id = u.id)::bool AS followed_by_me
FROM users u
CROSS JOIN pattern
WHERE u.id <> sqlc.arg(viewer_id)
  AND (lower(f_unaccent(u.username || ' ' || coalesce(u.display_name, ''))) LIKE pattern.p || '%'
       OR lower(f_unaccent(u.username || ' ' || coalesce(u.display_name, ''))) LIKE '% ' || pattern.p || '%'
       OR pattern.q <<% lower(f_unaccent(u.username || ' ' || coalesce(u.display_name, ''))))
ORDER BY
    (u.username = pattern.q) DESC,
    (u.username LIKE pattern.p || '%') DESC,
    (lower(f_unaccent(u.username || ' ' || coalesce(u.display_name, ''))) LIKE '% ' || pattern.p || '%') DESC,
    strict_word_similarity(pattern.q, lower(f_unaccent(u.username || ' ' || coalesce(u.display_name, '')))) DESC,
    (SELECT count(*) FROM follows f WHERE f.followee_id = u.id) DESC,
    u.username
LIMIT sqlc.arg(max_results)::int;

-- Me gusta idempotente. La reseña inexistente se detecta con
-- GetReviewLikesCount (sin filas) antes de escribir.
-- name: LikeReview :exec
INSERT INTO review_likes (review_id, user_id)
VALUES (sqlc.arg(review_id), sqlc.arg(user_id))
ON CONFLICT DO NOTHING;

-- name: UnlikeReview :exec
DELETE FROM review_likes
WHERE review_id = sqlc.arg(review_id) AND user_id = sqlc.arg(user_id);

-- name: GetReviewLikesCount :one
SELECT likes_count FROM reviews WHERE id = $1;
