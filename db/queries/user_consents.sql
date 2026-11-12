-- name: InsertUserConsent :exec
INSERT INTO user_consents (user_id, document, version)
VALUES ($1, $2, $3);
