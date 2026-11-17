-- Comentarios de reseñas (HOME-04). Hilos de un nivel: parent_id NULL es un
-- raíz y las respuestas siempre cuelgan de un raíz. Un raíz borrado con
-- respuestas queda con deleted_at y content NULL. Se listan del más
-- antiguo al más nuevo con keyset sobre (created_at, id).

-- name: CreateComment :one
INSERT INTO review_comments (review_id, user_id, parent_id, content)
VALUES (sqlc.arg(review_id), sqlc.arg(user_id), sqlc.narg(parent_id), sqlc.arg(content))
RETURNING id;

-- name: GetComment :one
SELECT
    c.id, c.review_id, c.user_id, c.parent_id, c.content, c.deleted_at,
    c.created_at, c.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    (SELECT count(*) FROM review_comments rp WHERE rp.parent_id = c.id) AS replies_count
FROM review_comments c
JOIN users u ON u.id = c.user_id
WHERE c.id = $1;

-- name: ListRootComments :many
SELECT
    c.id, c.review_id, c.user_id, c.parent_id, c.content, c.deleted_at,
    c.created_at, c.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    (SELECT count(*) FROM review_comments rp WHERE rp.parent_id = c.id) AS replies_count
FROM review_comments c
JOIN users u ON u.id = c.user_id
WHERE c.review_id = sqlc.arg(review_id) AND c.parent_id IS NULL
  AND (sqlc.narg(cursor_id)::uuid IS NULL
       OR (c.created_at, c.id) > (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY c.created_at, c.id
LIMIT sqlc.arg(max_results)::int;

-- name: ListReplies :many
SELECT
    c.id, c.review_id, c.user_id, c.parent_id, c.content, c.deleted_at,
    c.created_at, c.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    0::bigint AS replies_count
FROM review_comments c
JOIN users u ON u.id = c.user_id
WHERE c.parent_id = sqlc.arg(parent_id)
  AND (sqlc.narg(cursor_id)::uuid IS NULL
       OR (c.created_at, c.id) > (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY c.created_at, c.id
LIMIT sqlc.arg(max_results)::int;

-- name: UpdateComment :execrows
UPDATE review_comments
SET content = sqlc.arg(content)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- SoftDeleteComment deja el raíz como "comentario eliminado".
-- name: SoftDeleteComment :execrows
UPDATE review_comments
SET content = NULL, deleted_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: DeleteComment :execrows
DELETE FROM review_comments WHERE id = $1;

-- name: CountReplies :one
SELECT count(*) FROM review_comments WHERE parent_id = $1;

-- name: ReviewExists :one
SELECT EXISTS (SELECT 1 FROM reviews WHERE id = $1);

-- --- Borrado (postgres.CommentRemover, dentro de una transacción) ---

-- LockComment bloquea el comentario: una respuesta nueva toma FOR KEY SHARE
-- sobre su raíz y espera a que termine el borrado.
-- name: LockComment :one
SELECT id, parent_id, deleted_at FROM review_comments WHERE id = $1 FOR UPDATE;

-- DeleteEmptyDeletedRoot borra un raíz ya eliminado que se quedó sin
-- respuestas.
-- name: DeleteEmptyDeletedRoot :exec
DELETE FROM review_comments p
WHERE p.id = $1 AND p.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM review_comments c WHERE c.parent_id = p.id);
