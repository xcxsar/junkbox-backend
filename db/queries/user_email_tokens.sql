-- name: InsertEmailToken :exec
INSERT INTO user_email_tokens (user_id, purpose, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: CountRecentEmailTokens :one
SELECT COUNT(*)
FROM user_email_tokens
WHERE user_id = $1 AND purpose = $2 AND created_at > NOW() - INTERVAL '1 hour';

-- name: InvalidateEmailTokens :exec
UPDATE user_email_tokens
SET used_at = NOW()
WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL;

-- ConsumeEmailToken marca el token como usado solo si sigue vigente; al ser
-- un único UPDATE, dos peticiones concurrentes no pueden consumirlo dos veces.
-- name: ConsumeEmailToken :one
UPDATE user_email_tokens
SET used_at = NOW()
WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > NOW()
RETURNING user_id;

-- name: PurgeExpiredEmailTokens :execrows
DELETE FROM user_email_tokens
WHERE expires_at < NOW();
