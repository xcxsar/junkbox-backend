-- +goose Up

-- Estadísticas desnormalizadas de calificaciones en el catálogo. Las mantiene
-- el trigger de ratings con deltas (no recorre la tabla en cada voto).
-- avg_rating es NULL mientras no haya votos. Los DEFAULT constantes no
-- reescriben las 1.16M filas de song_metadata.
ALTER TABLE song_metadata
    ADD COLUMN ratings_count INT NOT NULL DEFAULT 0,
    ADD COLUMN ratings_sum NUMERIC(12,1) NOT NULL DEFAULT 0,
    ADD COLUMN avg_rating NUMERIC(3,2);

ALTER TABLE albums
    ADD COLUMN ratings_count INT NOT NULL DEFAULT 0,
    ADD COLUMN ratings_sum NUMERIC(12,1) NOT NULL DEFAULT 0,
    ADD COLUMN avg_rating NUMERIC(3,2);

ALTER TABLE artists
    ADD COLUMN ratings_count INT NOT NULL DEFAULT 0,
    ADD COLUMN ratings_sum NUMERIC(12,1) NOT NULL DEFAULT 0,
    ADD COLUMN avg_rating NUMERIC(3,2);

-- Calificaciones: de 0.5 a 5.0 estrellas en pasos de 0.5. Cada fila apunta a
-- exactamente uno de canción, álbum o artista (arco exclusivo).
CREATE TABLE ratings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    song_id INT REFERENCES song_metadata(id) ON DELETE CASCADE,
    album_id UUID REFERENCES albums(id) ON DELETE CASCADE,
    artist_id UUID REFERENCES artists(id) ON DELETE CASCADE,
    score NUMERIC(2,1) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ratings_single_target CHECK (num_nonnulls(song_id, album_id, artist_id) = 1),
    CONSTRAINT ratings_score_range CHECK (score >= 0.5 AND score <= 5.0 AND score * 2 = trunc(score * 2))
);

-- Un voto por usuario y objetivo (destino de los ON CONFLICT). El objetivo va
-- primero para que el mismo índice sirva al borrado en cascada del catálogo.
CREATE UNIQUE INDEX idx_ratings_song_user ON ratings(song_id, user_id) WHERE song_id IS NOT NULL;
CREATE UNIQUE INDEX idx_ratings_album_user ON ratings(album_id, user_id) WHERE album_id IS NOT NULL;
CREATE UNIQUE INDEX idx_ratings_artist_user ON ratings(artist_id, user_id) WHERE artist_id IS NOT NULL;
CREATE INDEX idx_ratings_user_id ON ratings(user_id);

CREATE TRIGGER trigger_ratings_updated_at
BEFORE UPDATE ON ratings
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- Reseñas: texto sobre un objetivo, una por usuario y objetivo. El puntaje
-- que se muestra junto a la reseña es el voto actual del autor sobre el mismo
-- objetivo (se obtiene con un JOIN; no hay enlace rating_id que pueda quedar
-- huérfano). likes_count queda reservado para una fase futura.
CREATE TABLE reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    song_id INT REFERENCES song_metadata(id) ON DELETE CASCADE,
    album_id UUID REFERENCES albums(id) ON DELETE CASCADE,
    artist_id UUID REFERENCES artists(id) ON DELETE CASCADE,
    title VARCHAR(200),
    content TEXT NOT NULL,
    likes_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT reviews_single_target CHECK (num_nonnulls(song_id, album_id, artist_id) = 1),
    CONSTRAINT reviews_content_length CHECK (char_length(btrim(content)) BETWEEN 5 AND 5000)
);

CREATE UNIQUE INDEX idx_reviews_song_user ON reviews(song_id, user_id) WHERE song_id IS NOT NULL;
CREATE UNIQUE INDEX idx_reviews_album_user ON reviews(album_id, user_id) WHERE album_id IS NOT NULL;
CREATE UNIQUE INDEX idx_reviews_artist_user ON reviews(artist_id, user_id) WHERE artist_id IS NOT NULL;
CREATE INDEX idx_reviews_song_created ON reviews(song_id, created_at DESC) WHERE song_id IS NOT NULL;
CREATE INDEX idx_reviews_album_created ON reviews(album_id, created_at DESC) WHERE album_id IS NOT NULL;
CREATE INDEX idx_reviews_artist_created ON reviews(artist_id, created_at DESC) WHERE artist_id IS NOT NULL;
CREATE INDEX idx_reviews_user_id ON reviews(user_id);

CREATE TRIGGER trigger_reviews_updated_at
BEFORE UPDATE ON reviews
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- adjust_rating_stats suma (o resta) un voto a las estadísticas del objetivo.
-- El UPDATE bloquea la fila del objetivo y, en READ COMMITTED, se reevalúa
-- sobre la versión más reciente: votos concurrentes no se pisan.
-- +goose StatementBegin
CREATE FUNCTION adjust_rating_stats(p_song INT, p_album UUID, p_artist UUID, p_count INT, p_sum NUMERIC)
RETURNS VOID AS $$
BEGIN
    IF p_song IS NOT NULL THEN
        UPDATE song_metadata
        SET ratings_count = ratings_count + p_count,
            ratings_sum = ratings_sum + p_sum,
            avg_rating = round((ratings_sum + p_sum) / nullif(ratings_count + p_count, 0), 2)
        WHERE id = p_song;
    ELSIF p_album IS NOT NULL THEN
        UPDATE albums
        SET ratings_count = ratings_count + p_count,
            ratings_sum = ratings_sum + p_sum,
            avg_rating = round((ratings_sum + p_sum) / nullif(ratings_count + p_count, 0), 2)
        WHERE id = p_album;
    ELSIF p_artist IS NOT NULL THEN
        UPDATE artists
        SET ratings_count = ratings_count + p_count,
            ratings_sum = ratings_sum + p_sum,
            avg_rating = round((ratings_sum + p_sum) / nullif(ratings_count + p_count, 0), 2)
        WHERE id = p_artist;
    END IF;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION apply_rating_delta()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM adjust_rating_stats(OLD.song_id, OLD.album_id, OLD.artist_id, -1, -OLD.score);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM adjust_rating_stats(NEW.song_id, NEW.album_id, NEW.artist_id, 1, NEW.score);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_ratings_stats
AFTER INSERT OR DELETE OR UPDATE OF score, song_id, album_id, artist_id ON ratings
FOR EACH ROW
EXECUTE FUNCTION apply_rating_delta();

-- +goose Down
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS ratings;
DROP FUNCTION IF EXISTS apply_rating_delta();
DROP FUNCTION IF EXISTS adjust_rating_stats(INT, UUID, UUID, INT, NUMERIC);

ALTER TABLE artists
    DROP COLUMN IF EXISTS avg_rating,
    DROP COLUMN IF EXISTS ratings_sum,
    DROP COLUMN IF EXISTS ratings_count;
ALTER TABLE albums
    DROP COLUMN IF EXISTS avg_rating,
    DROP COLUMN IF EXISTS ratings_sum,
    DROP COLUMN IF EXISTS ratings_count;
ALTER TABLE song_metadata
    DROP COLUMN IF EXISTS avg_rating,
    DROP COLUMN IF EXISTS ratings_sum,
    DROP COLUMN IF EXISTS ratings_count;
