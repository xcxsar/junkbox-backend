-- name: EnqueueEmail :exec
INSERT INTO email_outbox (to_address, subject, text_body, html_body)
VALUES ($1, $2, $3, $4);

-- ClaimDueEmails bloquea los pendientes vencidos; SKIP LOCKED evita que dos
-- réplicas tomen el mismo correo.
-- name: ClaimDueEmails :many
SELECT id, to_address, subject, text_body, html_body, attempts
FROM email_outbox
WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= NOW()
ORDER BY next_attempt_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkEmailSent :exec
UPDATE email_outbox
SET sent_at = NOW(), attempts = attempts + 1, last_error = NULL
WHERE id = $1;

-- name: MarkEmailRetry :exec
UPDATE email_outbox
SET attempts = attempts + 1, next_attempt_at = $2, last_error = $3
WHERE id = $1;

-- name: MarkEmailFailed :exec
UPDATE email_outbox
SET attempts = attempts + 1, failed_at = NOW(), last_error = $2
WHERE id = $1;

-- PurgeOldOutbox borra los correos ya resueltos: contienen enlaces con token.
-- name: PurgeOldOutbox :execrows
DELETE FROM email_outbox
WHERE (sent_at IS NOT NULL OR failed_at IS NOT NULL)
  AND created_at < NOW() - INTERVAL '24 hours';
