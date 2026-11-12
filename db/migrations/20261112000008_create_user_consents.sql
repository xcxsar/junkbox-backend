-- +goose Up
-- Evidencia del consentimiento (LFPDPPP): una fila por documento aceptado al
-- registrarse, con la versión vigente en el servidor (TERMS_VERSION).
CREATE TYPE consent_document AS ENUM ('terms', 'privacy');

CREATE TABLE user_consents (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    document consent_document NOT NULL,
    version VARCHAR(50) NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, document, version)
);

-- +goose Down
DROP TABLE IF EXISTS user_consents;
DROP TYPE IF EXISTS consent_document;
