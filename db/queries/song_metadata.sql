-- name: GetSongByTrackID :one
SELECT id, raw_index, artist_name, track_name, track_id, popularity, year, genre,
       danceability, energy, key, loudness, mode, speechiness, acousticness,
       instrumentalness, liveness, valence, tempo, duration_ms, time_signature
FROM song_metadata
WHERE track_id = $1
LIMIT 1;

-- name: CountSongs :one
SELECT COUNT(*) FROM song_metadata;
