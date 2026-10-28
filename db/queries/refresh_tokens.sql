-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (
    user_id, token_hash, family_id, expires_at
) VALUES (
    $1, $2, $3, $4
)
RETURNING id, user_id, token_hash, family_id, replaced_by, revoked, revoked_at, expires_at, created_at;

-- name: GetRefreshTokenByHash :one
SELECT id, user_id, token_hash, family_id, replaced_by, revoked, revoked_at, expires_at, created_at
FROM refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE token_hash = $1 AND revoked = FALSE;

-- name: SetRefreshTokenReplacedBy :exec
UPDATE refresh_tokens
SET replaced_by = $2
WHERE id = $1;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE family_id = $1 AND revoked = FALSE;

-- name: RevokeAllRefreshTokensByUser :exec
UPDATE refresh_tokens
SET revoked = TRUE, revoked_at = NOW()
WHERE user_id = $1 AND revoked = FALSE;
