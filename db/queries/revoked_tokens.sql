-- name: RevokeAccessToken :exec
INSERT INTO revoked_access_tokens (jti, user_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsAccessTokenRevoked :one
SELECT EXISTS (
    SELECT 1 FROM revoked_access_tokens WHERE jti = $1
);

-- name: RevokeAllUserAccessTokens :exec
INSERT INTO user_access_revocations (user_id, revoked_before)
VALUES ($1, NOW())
ON CONFLICT (user_id) DO UPDATE SET revoked_before = EXCLUDED.revoked_before;

-- name: GetUserAccessRevocation :one
SELECT revoked_before
FROM user_access_revocations
WHERE user_id = $1;

-- name: PurgeExpiredRevokedTokens :execrows
DELETE FROM revoked_access_tokens
WHERE expires_at < NOW();
