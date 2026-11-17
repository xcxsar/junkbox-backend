-- +goose Up

-- Módulo social (HOME-04): seguidores, Me gusta y comentarios de reseñas.

-- follows: follower_id sigue a followee_id. La PK sirve a "¿A sigue a B?" y
-- a la lista de seguidos de A; el índice por followee_id, a la de
-- seguidores de B.
CREATE TABLE follows (
    follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (follower_id, followee_id),
    CONSTRAINT follows_not_self CHECK (follower_id <> followee_id)
);

CREATE INDEX idx_follows_follower_created ON follows(follower_id, created_at DESC, followee_id DESC);
CREATE INDEX idx_follows_followee_created ON follows(followee_id, created_at DESC, follower_id DESC);

-- review_likes: un Me gusta por usuario y reseña.
CREATE TABLE review_likes (
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (review_id, user_id)
);

CREATE INDEX idx_review_likes_user ON review_likes(user_id);

-- review_comments: hilos de un nivel. parent_id NULL es un comentario raíz;
-- una respuesta siempre apunta a un raíz de la misma reseña (lo comprueba
-- check_comment_parent). Un raíz borrado que aún tiene respuestas queda
-- con deleted_at y sin contenido, para no perder la conversación.
CREATE TABLE review_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES review_comments(id) ON DELETE CASCADE,
    content TEXT,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT review_comments_content CHECK (
        (deleted_at IS NULL AND char_length(btrim(content)) BETWEEN 1 AND 1000)
        OR (deleted_at IS NOT NULL AND content IS NULL)
    )
);

CREATE INDEX idx_review_comments_roots ON review_comments(review_id, created_at, id) WHERE parent_id IS NULL;
CREATE INDEX idx_review_comments_replies ON review_comments(parent_id, created_at, id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_review_comments_user ON review_comments(user_id);

CREATE TRIGGER trigger_review_comments_updated_at
BEFORE UPDATE OF content ON review_comments
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
CREATE FUNCTION check_comment_parent()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM review_comments p
        WHERE p.id = NEW.parent_id AND p.review_id = NEW.review_id AND p.parent_id IS NULL
    ) THEN
        RAISE EXCEPTION 'parent_id debe ser un comentario raíz de la misma reseña'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_review_comments_parent
BEFORE INSERT OR UPDATE OF parent_id, review_id ON review_comments
FOR EACH ROW
EXECUTE FUNCTION check_comment_parent();

-- Contadores desnormalizados en reviews (likes_count ya existía, reservado
-- en 20261104000006). popularity_score ordena el feed "Para ti": un
-- comentario vale el doble que un Me gusta. Los DEFAULT constantes no
-- reescriben la tabla.
ALTER TABLE reviews
    ADD COLUMN comments_count INT NOT NULL DEFAULT 0,
    ADD COLUMN popularity_score INT GENERATED ALWAYS AS (likes_count + 2 * comments_count) STORED;

CREATE INDEX idx_reviews_created ON reviews(created_at DESC, id DESC);
CREATE INDEX idx_reviews_user_created ON reviews(user_id, created_at DESC, id DESC);

-- Los contadores se mantienen con deltas, como adjust_rating_stats: el
-- UPDATE bloquea la fila de la reseña y Me gusta concurrentes no se pisan.
-- Si la reseña se está borrando en cascada, el UPDATE no encuentra la fila.
-- +goose StatementBegin
CREATE FUNCTION apply_review_like_delta()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE reviews SET likes_count = likes_count + 1 WHERE id = NEW.review_id;
    ELSE
        UPDATE reviews SET likes_count = likes_count - 1 WHERE id = OLD.review_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_review_likes_count
AFTER INSERT OR DELETE ON review_likes
FOR EACH ROW
EXECUTE FUNCTION apply_review_like_delta();

-- comments_count cuenta los comentarios visibles (raíces y respuestas).
-- +goose StatementBegin
CREATE FUNCTION apply_review_comment_delta()
RETURNS TRIGGER AS $$
DECLARE
    delta INT := 0;
    target UUID;
BEGIN
    IF TG_OP = 'INSERT' THEN
        target := NEW.review_id;
        IF NEW.deleted_at IS NULL THEN delta := 1; END IF;
    ELSIF TG_OP = 'DELETE' THEN
        target := OLD.review_id;
        IF OLD.deleted_at IS NULL THEN delta := -1; END IF;
    ELSE
        target := NEW.review_id;
        IF (OLD.deleted_at IS NULL) <> (NEW.deleted_at IS NULL) THEN
            delta := CASE WHEN NEW.deleted_at IS NULL THEN 1 ELSE -1 END;
        END IF;
    END IF;
    IF delta <> 0 THEN
        UPDATE reviews SET comments_count = comments_count + delta WHERE id = target;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_review_comments_count
AFTER INSERT OR DELETE OR UPDATE OF deleted_at ON review_comments
FOR EACH ROW
EXECUTE FUNCTION apply_review_comment_delta();

-- +goose Down
DROP TABLE IF EXISTS review_comments;
DROP TABLE IF EXISTS review_likes;
DROP TABLE IF EXISTS follows;
DROP FUNCTION IF EXISTS apply_review_comment_delta();
DROP FUNCTION IF EXISTS apply_review_like_delta();
DROP FUNCTION IF EXISTS check_comment_parent();
DROP INDEX IF EXISTS idx_reviews_user_created;
DROP INDEX IF EXISTS idx_reviews_created;
ALTER TABLE reviews
    DROP COLUMN IF EXISTS popularity_score,
    DROP COLUMN IF EXISTS comments_count;
