-- Calificaciones (Fase 4). Un voto por usuario y objetivo: votar de nuevo
-- reemplaza el puntaje. Hay un upsert por objetivo porque cada ON CONFLICT
-- necesita su propio índice único parcial. El trigger trigger_ratings_stats
-- mantiene ratings_count / avg_rating del objetivo.
-- inserted distingue alta (201) de actualización (200): xmax = 0 solo en la
-- versión de fila recién insertada.

-- name: UpsertSongRating :one
INSERT INTO ratings (user_id, song_id, score)
VALUES (sqlc.arg(user_id), sqlc.arg(song_id)::int, sqlc.arg(score))
ON CONFLICT (song_id, user_id) WHERE song_id IS NOT NULL
DO UPDATE SET score = EXCLUDED.score
RETURNING id, user_id, song_id, album_id, artist_id, score, created_at, updated_at,
    (xmax::text = '0')::bool AS inserted;

-- name: UpsertAlbumRating :one
INSERT INTO ratings (user_id, album_id, score)
VALUES (sqlc.arg(user_id), sqlc.arg(album_id)::uuid, sqlc.arg(score))
ON CONFLICT (album_id, user_id) WHERE album_id IS NOT NULL
DO UPDATE SET score = EXCLUDED.score
RETURNING id, user_id, song_id, album_id, artist_id, score, created_at, updated_at,
    (xmax::text = '0')::bool AS inserted;

-- name: UpsertArtistRating :one
INSERT INTO ratings (user_id, artist_id, score)
VALUES (sqlc.arg(user_id), sqlc.arg(artist_id)::uuid, sqlc.arg(score))
ON CONFLICT (artist_id, user_id) WHERE artist_id IS NOT NULL
DO UPDATE SET score = EXCLUDED.score
RETURNING id, user_id, song_id, album_id, artist_id, score, created_at, updated_at,
    (xmax::text = '0')::bool AS inserted;

-- GetUserRating busca el voto del usuario sobre un objetivo. Solo uno de
-- song_id / album_id / artist_id llega con valor; las comparaciones con NULL
-- se descartan y queda la del índice único correspondiente.
-- name: GetUserRating :one
SELECT id, user_id, song_id, album_id, artist_id, score, created_at, updated_at
FROM ratings
WHERE user_id = sqlc.arg(user_id)
  AND (song_id = sqlc.narg(song_id)::int
       OR album_id = sqlc.narg(album_id)::uuid
       OR artist_id = sqlc.narg(artist_id)::uuid);

-- name: GetRatingByID :one
SELECT id, user_id, song_id, album_id, artist_id, score, created_at, updated_at
FROM ratings
WHERE id = $1;

-- name: DeleteRating :execrows
DELETE FROM ratings WHERE id = $1;
