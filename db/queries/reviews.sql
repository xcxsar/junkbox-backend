-- Reseñas (Fase 4). Una por usuario y objetivo; el índice único parcial
-- rechaza la segunda con 23505. rating_score es el voto actual del autor
-- sobre el mismo objetivo (NULL si no calificó).
-- En los filtros por objetivo solo uno de song_id / album_id / artist_id
-- llega con valor; las comparaciones con NULL se descartan.

-- name: CreateReview :one
INSERT INTO reviews (user_id, song_id, album_id, artist_id, title, content)
VALUES (
    sqlc.arg(user_id), sqlc.narg(song_id)::int, sqlc.narg(album_id)::uuid,
    sqlc.narg(artist_id)::uuid, sqlc.narg(title), sqlc.arg(content)
)
RETURNING id;

-- name: GetReviewByID :one
SELECT
    r.id, r.user_id, r.song_id, r.album_id, r.artist_id, r.title, r.content,
    r.likes_count, r.created_at, r.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    rat.score AS rating_score
FROM reviews r
JOIN users u ON u.id = r.user_id
LEFT JOIN ratings rat ON rat.user_id = r.user_id
    AND (rat.song_id = r.song_id OR rat.album_id = r.album_id OR rat.artist_id = r.artist_id)
WHERE r.id = $1;

-- name: ListReviewsByTarget :many
SELECT
    r.id, r.user_id, r.song_id, r.album_id, r.artist_id, r.title, r.content,
    r.likes_count, r.created_at, r.updated_at,
    u.username AS author_username,
    u.display_name AS author_display_name,
    u.avatar_url AS author_avatar_url,
    rat.score AS rating_score
FROM reviews r
JOIN users u ON u.id = r.user_id
LEFT JOIN ratings rat ON rat.user_id = r.user_id
    AND (rat.song_id = r.song_id OR rat.album_id = r.album_id OR rat.artist_id = r.artist_id)
WHERE r.song_id = sqlc.narg(song_id)::int
   OR r.album_id = sqlc.narg(album_id)::uuid
   OR r.artist_id = sqlc.narg(artist_id)::uuid
ORDER BY r.created_at DESC, r.id
LIMIT sqlc.arg(max_results) OFFSET sqlc.arg(skip);

-- name: CountReviewsByTarget :one
SELECT count(*)
FROM reviews
WHERE song_id = sqlc.narg(song_id)::int
   OR album_id = sqlc.narg(album_id)::uuid
   OR artist_id = sqlc.narg(artist_id)::uuid;

-- name: UpdateReview :execrows
UPDATE reviews
SET title = sqlc.narg(title), content = sqlc.arg(content)
WHERE id = sqlc.arg(id);

-- name: DeleteReview :execrows
DELETE FROM reviews WHERE id = $1;
