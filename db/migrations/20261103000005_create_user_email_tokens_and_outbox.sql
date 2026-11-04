-- +goose Up
-- Verificación de correo: las cuentas existentes (seed) se dan por verificadas.
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;
UPDATE users SET email_verified_at = created_at;

CREATE TYPE email_token_purpose AS ENUM ('password_reset', 'email_verification');

-- Tokens de un solo uso enviados por correo (recuperación de contraseña y
-- verificación de email). Solo se guarda el SHA-256, como en refresh_tokens.
CREATE TABLE user_email_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose email_token_purpose NOT NULL,
    token_hash CHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_email_tokens_user_purpose ON user_email_tokens(user_id, purpose);
CREATE INDEX idx_user_email_tokens_expires_at ON user_email_tokens(expires_at);

-- Outbox transaccional: los correos se encolan en la misma transacción que
-- los crea y un worker los envía con reintentos.
CREATE TABLE email_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    to_address TEXT NOT NULL,
    subject TEXT NOT NULL,
    text_body TEXT NOT NULL,
    html_body TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_email_outbox_pending ON email_outbox(next_attempt_at)
    WHERE sent_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS email_outbox;
DROP TABLE IF EXISTS user_email_tokens;
DROP TYPE IF EXISTS email_token_purpose;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
