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
