package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// CatalogImporter persiste en PostgreSQL lo que devuelve Spotify. Cada
// llamada corre en una sola transacción: si algo falla, no queda nada.
type CatalogImporter struct {
	db *sql.DB
}

// NewCatalogImporter crea el importador sobre el pool dado.
func NewCatalogImporter(db *sql.DB) *CatalogImporter {
	return &CatalogImporter{db: db}
}

// ImportTracks guarda artista principal → álbum → canción por cada track.
func (c *CatalogImporter) ImportTracks(ctx context.Context, tracks []domain.ImportedTrack) ([]domain.ImportedSong, error) {
	out := make([]domain.ImportedSong, 0, len(tracks))
	err := c.inTx(ctx, func(q *sqlc.Queries) error {
		for _, t := range tracks {
			artistID, err := upsertArtist(ctx, q, t.Artist)
			if err != nil {
				return err
			}
			albumID, err := upsertAlbum(ctx, q, t.Album)
			if err != nil {
				return err
			}
			row, err := q.UpsertSongFromSpotify(ctx, sqlc.UpsertSongFromSpotifyParams{
				TrackID:    nonEmpty(t.SpotifyID),
				TrackName:  nonEmpty(t.Name),
				ArtistName: nonEmpty(t.Artist.Name),
				Popularity: nullInt32(t.Popularity),
				Year:       nullInt32(t.Album.ReleaseYear),
				DurationMs: nullInt32(t.DurationMs),
				Isrc:       nullString(t.ISRC),
				ArtistID:   &artistID,
				AlbumID:    &albumID,
			})
			if err != nil {
				return fmt.Errorf("guardar canción %s: %w", t.SpotifyID, err)
			}
			albumTitle := t.Album.Title
			song := domain.ImportedSong{
				Track: domain.Track{
					ID:               row.ID,
					SpotifyID:        row.TrackID.String,
					TrackName:        row.TrackName.String,
					ArtistName:       row.ArtistName.String,
					AlbumTitle:       &albumTitle,
					Year:             int32Ptr(row.Year),
					Genre:            stringPtr(row.Genre),
					Popularity:       int32Ptr(row.Popularity),
					DurationMs:       int32Ptr(row.DurationMs),
					HasEmbedding:     row.HasEmbedding,
					PendingEmbedding: row.PendingEmbedding,
				},
				ISRC:              stringPtr(row.Isrc),
				EmbeddingAttempts: row.EmbeddingAttempts,
			}
			out = append(out, song)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ImportArtists guarda los artistas y devuelve su versión persistida.
func (c *CatalogImporter) ImportArtists(ctx context.Context, artists []domain.ImportedArtist) ([]domain.Artist, error) {
	out := make([]domain.Artist, 0, len(artists))
	err := c.inTx(ctx, func(q *sqlc.Queries) error {
		for _, a := range artists {
			id, err := upsertArtist(ctx, q, a)
			if err != nil {
				return err
			}
			row, err := q.GetArtist(ctx, sqlc.GetArtistParams{ID: id})
			if err != nil {
				return fmt.Errorf("leer artista %s: %w", a.SpotifyID, err)
			}
			genres := row.Genres
			if genres == nil {
				genres = []string{}
			}
			out = append(out, domain.Artist{
				ID:         row.ID,
				SpotifyID:  stringPtr(row.SpotifyID),
				Name:       row.Name,
				Genres:     genres,
				Popularity: int32Ptr(row.Popularity),
				ImageURL:   stringPtr(row.ImageUrl),
				CreatedAt:  row.CreatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ImportAlbums guarda artista principal → álbum y devuelve los álbumes
// persistidos.
func (c *CatalogImporter) ImportAlbums(ctx context.Context, albums []domain.ImportedAlbum) ([]domain.Album, error) {
	out := make([]domain.Album, 0, len(albums))
	err := c.inTx(ctx, func(q *sqlc.Queries) error {
		for _, a := range albums {
			id, err := upsertAlbum(ctx, q, a)
			if err != nil {
				return err
			}
			row, err := q.GetAlbum(ctx, sqlc.GetAlbumParams{ID: id})
			if err != nil {
				return fmt.Errorf("leer álbum %s: %w", a.SpotifyID, err)
			}
			out = append(out, domain.Album{
				ID:          row.ID,
				SpotifyID:   stringPtr(row.SpotifyID),
				Title:       row.Title,
				ArtistID:    row.ArtistID,
				ArtistName:  row.ArtistName,
				ReleaseYear: int32Ptr(row.ReleaseYear),
				AlbumType:   row.AlbumType,
				ImageURL:    stringPtr(row.ImageUrl),
				TotalTracks: row.TotalTracks,
				CreatedAt:   row.CreatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *CatalogImporter) inTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: iniciar transacción: %w", err)
	}
	if err := fn(sqlc.New(tx)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("postgres: importar catálogo: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: confirmar transacción: %w", err)
	}
	return nil
}

// upsertArtist resuelve el artista en tres pasos: (1) ya existe con ese
// spotify_id → se actualiza; (2) hay un artista del CSV homónimo sin
// spotify_id → se reclama; (3) si no, se inserta uno nuevo.
func upsertArtist(ctx context.Context, q *sqlc.Queries, a domain.ImportedArtist) (uuid.UUID, error) {
	genres := a.Genres
	if genres == nil {
		genres = []string{}
	}
	upsert := sqlc.UpsertArtistBySpotifyIDParams{
		SpotifyID:  nonEmpty(a.SpotifyID),
		Name:       a.Name,
		Genres:     genres,
		Popularity: nullInt32(a.Popularity),
		ImageUrl:   nullString(a.ImageURL),
	}

	_, err := q.GetArtistIDBySpotifyID(ctx, upsert.SpotifyID)
	switch {
	case err == nil:
		return wrapArtist(q.UpsertArtistBySpotifyID(ctx, upsert))
	case !errors.Is(err, sql.ErrNoRows):
		return uuid.Nil, fmt.Errorf("buscar artista %s: %w", a.SpotifyID, err)
	}

	id, err := q.ClaimArtistByName(ctx, sqlc.ClaimArtistByNameParams{
		SpotifyID:  upsert.SpotifyID,
		Genres:     genres,
		Popularity: upsert.Popularity,
		ImageUrl:   upsert.ImageUrl,
		Name:       a.Name,
	})
	switch {
	case err == nil:
		return id, nil
	case !errors.Is(err, sql.ErrNoRows):
		return uuid.Nil, fmt.Errorf("reclamar artista %s: %w", a.SpotifyID, err)
	}

	return wrapArtist(q.UpsertArtistBySpotifyID(ctx, upsert))
}

func wrapArtist(id uuid.UUID, err error) (uuid.UUID, error) {
	if err != nil {
		return uuid.Nil, fmt.Errorf("guardar artista: %w", err)
	}
	return id, nil
}

func upsertAlbum(ctx context.Context, q *sqlc.Queries, a domain.ImportedAlbum) (uuid.UUID, error) {
	artistID, err := upsertArtist(ctx, q, a.Artist)
	if err != nil {
		return uuid.Nil, err
	}
	albumType := a.AlbumType
	if albumType == "" {
		albumType = "album"
	}
	id, err := q.UpsertAlbum(ctx, sqlc.UpsertAlbumParams{
		SpotifyID:   nonEmpty(a.SpotifyID),
		Title:       a.Title,
		ArtistID:    &artistID,
		ArtistName:  a.Artist.Name,
		ReleaseYear: nullInt32(a.ReleaseYear),
		AlbumType:   albumType,
		ImageUrl:    nullString(a.ImageURL),
		TotalTracks: a.TotalTracks,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("guardar álbum %s: %w", a.SpotifyID, err)
	}
	return id, nil
}

// --- conversión entre punteros y tipos nulos de database/sql ---

func nonEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt32(v *int32) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *v, Valid: true}
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func int32Ptr(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}
