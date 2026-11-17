package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

// Pruebas de integración: necesitan una base con las migraciones aplicadas
// en TEST_DATABASE_URL (make test-db usa la de .env). Sin ella se omiten,
// así go test ./... no depende de PostgreSQL. Cada prueba crea sus filas
// con IDs de Spotify únicos y las borra al terminar.

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL no definida: se omite la prueba de integración")
	}
	db, err := Open(context.Background(), url, Options{})
	if err != nil {
		t.Fatalf("abrir base de pruebas: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func checkRatings(t *testing.T, what string, got *domain.RatingStats, count int32, avg *float64) {
	t.Helper()
	if got == nil || got.Count != count || (avg == nil) != (got.Average == nil) || (avg != nil && *got.Average != *avg) {
		t.Fatalf("%s: ratings = %s, se esperaba %s", what, formatRatings(got), formatRatings(&domain.RatingStats{Count: count, Average: avg}))
	}
}

func formatRatings(r *domain.RatingStats) string {
	if r == nil {
		return "nil"
	}
	if r.Average == nil {
		return fmt.Sprintf("{%d null}", r.Count)
	}
	return fmt.Sprintf("{%d %g}", r.Count, *r.Average)
}

func avgp(v float64) *float64 { return &v }

// SEARCH-03: lo que devuelve la importación (respaldo de Spotify en la
// búsqueda y pistas completadas de un álbum) trae las calificaciones que
// el objetivo ya tenía en el catálogo, no {0, null}.
func TestImportTraeLasCalificacionesExistentes(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	imp := NewCatalogImporter(db)

	suffix := uuid.NewString()[:8]
	artist := domain.ImportedArtist{SpotifyID: "test-artist-" + suffix, Name: "Prueba SEARCH-03 " + suffix}
	album := domain.ImportedAlbum{SpotifyID: "test-album-" + suffix, Title: "Álbum de prueba", Artist: artist, AlbumType: "album", TotalTracks: 1}
	track := domain.ImportedTrack{SpotifyID: "test-track-" + suffix, Name: "Canción de prueba", Artist: artist, Album: album}
	userID := uuid.New()
	t.Cleanup(func() {
		// Primero las calificaciones (referencian al usuario y a los
		// objetivos), luego la canción (referencia álbum y artista).
		exec(t, db, `DELETE FROM ratings WHERE user_id = $1`, userID)
		exec(t, db, `DELETE FROM users WHERE id = $1`, userID)
		exec(t, db, `DELETE FROM song_metadata WHERE track_id = $1`, track.SpotifyID)
		exec(t, db, `DELETE FROM albums WHERE spotify_id = $1`, album.SpotifyID)
		exec(t, db, `DELETE FROM artists WHERE spotify_id = $1`, artist.SpotifyID)
	})

	// Recién importada: sin calificaciones.
	songs, err := imp.ImportTracks(ctx, []domain.ImportedTrack{track})
	if err != nil {
		t.Fatalf("ImportTracks: %v", err)
	}
	checkRatings(t, "canción nueva", songs[0].Ratings, 0, nil)

	// Un usuario la califica, y también a su álbum y a su artista; el
	// trigger actualiza ratings_count y avg_rating de cada objetivo.
	exec(t, db, `INSERT INTO users (id, username, email, hashed_password) VALUES ($1, $2, $3, 'x')`,
		userID, "test_s03_"+suffix, "test_s03_"+suffix+"@example.invalid")
	exec(t, db, `INSERT INTO ratings (user_id, song_id, score) VALUES ($1, $2, 4.5)`, userID, songs[0].ID)
	exec(t, db, `INSERT INTO ratings (user_id, album_id, score) SELECT $1, id, 4.0 FROM albums WHERE spotify_id = $2`, userID, album.SpotifyID)
	exec(t, db, `INSERT INTO ratings (user_id, artist_id, score) SELECT $1, id, 5.0 FROM artists WHERE spotify_id = $2`, userID, artist.SpotifyID)

	// Spotify la devuelve otra vez (otra búsqueda o completar el álbum).
	songs, err = imp.ImportTracks(ctx, []domain.ImportedTrack{track})
	if err != nil {
		t.Fatalf("ImportTracks (de nuevo): %v", err)
	}
	if songs[0].ID == 0 {
		t.Fatalf("canción sin id: %+v", songs[0])
	}
	checkRatings(t, "canción ya calificada", songs[0].Ratings, 1, avgp(4.5))

	albums, err := imp.ImportAlbums(ctx, []domain.ImportedAlbum{album})
	if err != nil {
		t.Fatalf("ImportAlbums: %v", err)
	}
	checkRatings(t, "álbum ya calificado", albums[0].Ratings, 1, avgp(4))

	artists, err := imp.ImportArtists(ctx, []domain.ImportedArtist{artist})
	if err != nil {
		t.Fatalf("ImportArtists: %v", err)
	}
	checkRatings(t, "artista ya calificado", artists[0].Ratings, 1, avgp(5))
}
