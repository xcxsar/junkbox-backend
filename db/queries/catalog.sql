-- Consultas del catálogo musical (Fase 3). Ninguna selecciona la columna
-- embedding: sqlc no conoce el tipo vector, así que se expone como
-- has_embedding y se escribe como texto ('[a,b,...]'::vector).

-- La expresión coalesce(track_name, '') || ' ' || coalesce(artist_name, '')
-- debe coincidir con la del índice idx_song_metadata_search_trgm.
-- strict_word_similarity mide qué tan bien las palabras de la consulta aparecen
-- completas dentro del texto: "Yellow Coldplay" puntúa 1.0 contra "Yellow
-- Coldplay" y a "Espresso Sabrina Carpenter" le falta una palabra en "Nonsense
-- Sabrina Carpenter" (0.67). similarity() desempata a favor del texto más
-- parecido en total (el título exacto antes que uno que solo lo contiene).

-- name: SearchTracksTrigram :many
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.year, s.genre,
    s.popularity, s.duration_ms, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title,
    strict_word_similarity(sqlc.arg(query)::text, coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, ''))::float8 AS score
FROM song_metadata s
LEFT JOIN albums al ON al.id = s.album_id
WHERE sqlc.arg(query)::text <<% (coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, ''))
ORDER BY score DESC,
    similarity(sqlc.arg(query)::text, coalesce(s.track_name, '') || ' ' || coalesce(s.artist_name, '')) DESC,
    s.popularity DESC NULLS LAST, s.id
LIMIT sqlc.arg(max_results);

-- name: SearchArtistsTrigram :many
SELECT
    id, spotify_id, name, genres, popularity, image_url, created_at,
    strict_word_similarity(sqlc.arg(query)::text, name)::float8 AS score
FROM artists
WHERE sqlc.arg(query)::text <<% name
ORDER BY score DESC, similarity(sqlc.arg(query)::text, name) DESC, popularity DESC NULLS LAST, id
LIMIT sqlc.arg(max_results);

-- name: SearchAlbumsTrigram :many
SELECT
    id, spotify_id, title, artist_id, artist_name, release_year, album_type,
    image_url, total_tracks, created_at,
    strict_word_similarity(sqlc.arg(query)::text, title || ' ' || artist_name)::float8 AS score
FROM albums
WHERE sqlc.arg(query)::text <<% (title || ' ' || artist_name)
ORDER BY score DESC, similarity(sqlc.arg(query)::text, title || ' ' || artist_name) DESC, id
LIMIT sqlc.arg(max_results);

-- GetTrack acepta el id local o el track_id de Spotify; el otro se pasa
-- vacío (0 / '') para que solo coincida uno de los dos índices.

-- name: GetTrack :one
SELECT
    s.id, s.track_id, s.track_name, s.artist_name, s.artist_id, s.album_id,
    s.isrc, s.year, s.genre, s.popularity, s.duration_ms,
    s.danceability, s.energy, s.key, s.loudness, s.mode, s.speechiness,
    s.acousticness, s.instrumentalness, s.liveness, s.valence, s.tempo,
    s.time_signature, s.pending_embedding,
    (s.embedding IS NOT NULL)::bool AS has_embedding,
    al.title AS album_title
FROM song_metadata s
LEFT JOIN albums al ON al.id = s.album_id
WHERE s.id = sqlc.arg(id)::int OR s.track_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: GetArtist :one
SELECT id, spotify_id, name, genres, popularity, image_url, created_at
FROM artists
WHERE id = sqlc.arg(id)::uuid OR spotify_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: GetAlbum :one
SELECT
    id, spotify_id, title, artist_id, artist_name, release_year, album_type,
    image_url, total_tracks, created_at
FROM albums
WHERE id = sqlc.arg(id)::uuid OR spotify_id = sqlc.arg(spotify_id)::text
LIMIT 1;

-- name: ListTracksByAlbum :many
SELECT
    id, track_id, track_name, artist_name, year, genre, popularity,
    duration_ms, pending_embedding,
    (embedding IS NOT NULL)::bool AS has_embedding
FROM song_metadata
WHERE album_id = $1
ORDER BY id;

-- --- Importación desde Spotify (CatalogImporter, dentro de una transacción) ---

-- name: GetArtistIDBySpotifyID :one
SELECT id FROM artists WHERE spotify_id = $1;

-- ClaimArtistByName asigna el spotify_id a un artista del CSV homónimo
-- (spotify_id NULL). No devuelve filas si no hay ninguno libre.
-- name: ClaimArtistByName :one
UPDATE artists
SET spotify_id = sqlc.arg(spotify_id),
    genres = CASE WHEN cardinality(sqlc.arg(genres)::text[]) > 0 THEN sqlc.arg(genres)::text[] ELSE genres END,
    popularity = coalesce(sqlc.narg(popularity), popularity),
    image_url = coalesce(sqlc.narg(image_url), image_url)
WHERE id = (
    SELECT a.id FROM artists a
    WHERE a.name = sqlc.arg(name) AND a.spotify_id IS NULL
    ORDER BY a.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id;

-- Los artistas simplificados de una búsqueda de canciones no traen géneros,
-- popularidad ni imagen: en conflicto no se pisan los valores conocidos.
-- name: UpsertArtistBySpotifyID :one
INSERT INTO artists (spotify_id, name, genres, popularity, image_url)
VALUES (sqlc.arg(spotify_id), sqlc.arg(name), sqlc.arg(genres)::text[], sqlc.narg(popularity), sqlc.narg(image_url))
ON CONFLICT (spotify_id) DO UPDATE SET
    name = EXCLUDED.name,
    genres = CASE WHEN cardinality(EXCLUDED.genres) > 0 THEN EXCLUDED.genres ELSE artists.genres END,
    popularity = coalesce(EXCLUDED.popularity, artists.popularity),
    image_url = coalesce(EXCLUDED.image_url, artists.image_url)
RETURNING id;

-- name: UpsertAlbum :one
INSERT INTO albums (
    spotify_id, title, artist_id, artist_name, release_year, album_type, image_url, total_tracks
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (spotify_id) DO UPDATE SET
    title = EXCLUDED.title,
    artist_id = coalesce(EXCLUDED.artist_id, albums.artist_id),
    artist_name = EXCLUDED.artist_name,
    release_year = coalesce(EXCLUDED.release_year, albums.release_year),
    album_type = EXCLUDED.album_type,
    image_url = coalesce(EXCLUDED.image_url, albums.image_url),
    total_tracks = EXCLUDED.total_tracks
RETURNING id;

-- Una canción del CSV que ya tiene embedding conserva sus datos históricos;
-- solo se completan los enlaces y el ISRC. pending_embedding queda TRUE solo
-- si aún no hay vector.
-- name: UpsertSongFromSpotify :one
INSERT INTO song_metadata (
    track_id, track_name, artist_name, popularity, year, duration_ms,
    isrc, artist_id, album_id, pending_embedding
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE
)
ON CONFLICT (track_id) DO UPDATE SET
    track_name = coalesce(song_metadata.track_name, EXCLUDED.track_name),
    artist_name = coalesce(song_metadata.artist_name, EXCLUDED.artist_name),
    popularity = coalesce(EXCLUDED.popularity, song_metadata.popularity),
    year = coalesce(song_metadata.year, EXCLUDED.year),
    duration_ms = coalesce(song_metadata.duration_ms, EXCLUDED.duration_ms),
    isrc = coalesce(EXCLUDED.isrc, song_metadata.isrc),
    artist_id = coalesce(EXCLUDED.artist_id, song_metadata.artist_id),
    album_id = coalesce(EXCLUDED.album_id, song_metadata.album_id),
    pending_embedding = song_metadata.embedding IS NULL
RETURNING
    id, track_id, track_name, artist_name, year, genre, popularity, duration_ms,
    isrc, embedding_attempts, pending_embedding,
    (embedding IS NOT NULL)::bool AS has_embedding;

-- --- Worker de embeddings ---

-- name: ListPendingEmbeddingTracks :many
SELECT track_id, isrc, embedding_attempts
FROM song_metadata
WHERE pending_embedding = TRUE
  AND embedding_attempts < sqlc.arg(max_attempts)::int
  AND (next_embedding_attempt_at IS NULL OR next_embedding_attempt_at <= NOW())
ORDER BY next_embedding_attempt_at NULLS FIRST, id
LIMIT sqlc.arg(max_results);

-- name: UpdateSongAudioFeaturesAndEmbedding :execrows
UPDATE song_metadata
SET
    danceability = sqlc.narg(danceability),
    energy = sqlc.narg(energy),
    key = sqlc.narg(key),
    loudness = sqlc.narg(loudness),
    mode = sqlc.narg(mode),
    speechiness = sqlc.narg(speechiness),
    acousticness = sqlc.narg(acousticness),
    instrumentalness = sqlc.narg(instrumentalness),
    liveness = sqlc.narg(liveness),
    valence = sqlc.narg(valence),
    tempo = sqlc.narg(tempo),
    time_signature = sqlc.narg(time_signature),
    embedding = sqlc.arg(embedding)::text::vector,
    pending_embedding = FALSE,
    next_embedding_attempt_at = NULL
WHERE track_id = sqlc.arg(track_id) AND pending_embedding = TRUE;

-- Solo cuenta si embedding_attempts sigue siendo el que vio el worker: un
-- mismo track que llegó dos veces (cola y rescan) no consume dos intentos.
-- name: MarkEmbeddingAttemptFailed :exec
UPDATE song_metadata
SET embedding_attempts = embedding_attempts + 1,
    next_embedding_attempt_at = NOW() + make_interval(secs => sqlc.arg(retry_in_seconds)::float8)
WHERE track_id = sqlc.arg(track_id)
  AND pending_embedding = TRUE
  AND embedding_attempts = sqlc.arg(attempts)::int;

-- Medianas del catálogo para imputar los audio features que FreqBlog
-- devuelve nulos (speechiness, liveness y a veces instrumentalness).
-- name: GetCatalogFeatureMedians :one
SELECT
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY danceability), 0)::float8 AS danceability,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY energy), 0)::float8 AS energy,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY loudness), 0)::float8 AS loudness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY speechiness), 0)::float8 AS speechiness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY acousticness), 0)::float8 AS acousticness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY instrumentalness), 0)::float8 AS instrumentalness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY liveness), 0)::float8 AS liveness,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY valence), 0)::float8 AS valence,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY tempo), 0)::float8 AS tempo,
    coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY mode), 0)::float8 AS mode
FROM song_metadata;
