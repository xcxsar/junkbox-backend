package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Discografía de un artista y pistas completas de un álbum (SEARCH-02).
const (
	// DiscographyTTL es cada cuánto se vuelven a pedir a Spotify los
	// álbumes de un artista (y a completar sus canciones), para recoger
	// lanzamientos nuevos.
	DiscographyTTL = 7 * 24 * time.Hour
	// maxAlbumsToFill es cuántos álbumes, como máximo, se completan con
	// Spotify (una llamada cada uno) para las canciones de un artista.
	maxAlbumsToFill = 5
)

// ArtistAlbums devuelve los álbumes y sencillos de un artista, del más
// reciente al más antiguo. La primera vez (y pasado DiscographyTTL) los
// pide a Spotify dentro del presupuesto; si falla, responde los que hay.
func (s *MusicService) ArtistAlbums(ctx context.Context, id string, limit int) ([]domain.Album, error) {
	limit, err := listLimit(limit)
	if err != nil {
		return nil, err
	}
	artist, err := s.artistRow(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.syncArtistAlbums(ctx, artist); err != nil {
		log.Printf("álbumes del artista %s: %v", artist.ID, err)
	}

	rows, err := s.catalog.ListAlbumsByArtist(ctx, sqlc.ListAlbumsByArtistParams{ArtistID: artist.ID, MaxResults: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("music: listar álbumes del artista: %w", err)
	}
	out := make([]domain.Album, len(rows))
	for i, r := range rows {
		out[i] = albumFromRow(sqlc.GetAlbumRow{ID: r.ID, SpotifyID: r.SpotifyID, Title: r.Title, ArtistID: r.ArtistID, ArtistName: r.ArtistName, ReleaseYear: r.ReleaseYear, AlbumType: r.AlbumType, ImageUrl: r.ImageUrl, TotalTracks: r.TotalTracks, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// ArtistTracks devuelve las canciones de un artista, por popularidad. Si en
// el catálogo hay menos que limit, completa con las pistas de sus álbumes
// más recientes (una vez cada DiscographyTTL). Como la búsqueda, completa
// portadas e imágenes antes de responder.
func (s *MusicService) ArtistTracks(ctx context.Context, id string, limit int) ([]domain.Track, error) {
	limit, err := listLimit(limit)
	if err != nil {
		return nil, err
	}
	artist, err := s.artistRow(ctx, id)
	if err != nil {
		return nil, err
	}

	rows, err := s.listArtistTracks(ctx, artist.ID, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) < limit && s.provider != nil && artist.SpotifyID.Valid && stale(artist.TracksSyncedAt) {
		if err := s.fillArtistTracks(ctx, artist, limit-len(rows)); err != nil {
			log.Printf("canciones del artista %s: %v", artist.ID, err)
		}
		if rows, err = s.listArtistTracks(ctx, artist.ID, limit); err != nil {
			return nil, err
		}
	}

	res := trackResults(rows)
	s.enrichResults(ctx, &res)
	return res.results.([]domain.Track), nil
}

// listArtistTracks lee las canciones del artista como filas de búsqueda
// para reutilizar trackResults.
func (s *MusicService) listArtistTracks(ctx context.Context, artistID uuid.UUID, limit int) ([]sqlc.SearchTracksTrigramRow, error) {
	rows, err := s.catalog.ListTracksByArtist(ctx, sqlc.ListTracksByArtistParams{ArtistID: artistID, MaxResults: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("music: listar canciones del artista: %w", err)
	}
	out := make([]sqlc.SearchTracksTrigramRow, len(rows))
	for i, r := range rows {
		out[i] = sqlc.SearchTracksTrigramRow{
			ID: r.ID, TrackID: r.TrackID, TrackName: r.TrackName, ArtistName: r.ArtistName, Year: r.Year, Genre: r.Genre,
			Popularity: r.Popularity, DurationMs: r.DurationMs, PendingEmbedding: r.PendingEmbedding, HasEmbedding: r.HasEmbedding,
			AlbumTitle: r.AlbumTitle, AlbumImageUrl: r.AlbumImageUrl, ArtistImageUrl: r.ArtistImageUrl,
			ArtistSpotifyID: r.ArtistSpotifyID, ArtistEnriched: r.ArtistEnriched, ArtistEnrichAttempts: r.ArtistEnrichAttempts,
			AlbumID: r.AlbumID, LinkAttempts: r.LinkAttempts,
		}
	}
	return out, nil
}

// syncArtistAlbums pide a Spotify los álbumes del artista y los guarda, si
// nunca se hizo o ya venció DiscographyTTL. Llamadas simultáneas para el
// mismo artista comparten una sola consulta.
func (s *MusicService) syncArtistAlbums(ctx context.Context, artist sqlc.GetArtistRow) error {
	if s.provider == nil || !artist.SpotifyID.Valid || !stale(artist.AlbumsSyncedAt) {
		return nil
	}
	_, err, _ := s.inflight.Do("albums:"+artist.ID.String(), func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, s.enrich.Budget)
		defer cancel()
		found, err := s.provider.ArtistAlbums(ctx, artist.SpotifyID.String, MaxSearchLimit)
		if err != nil {
			return nil, err
		}
		if _, err := s.importer.ImportAlbums(ctx, found); err != nil {
			return nil, err
		}
		return nil, s.catalog.MarkArtistAlbumsSynced(ctx, artist.ID)
	})
	return err
}

// fillArtistTracks completa las pistas de los álbumes más recientes del
// artista (primero álbumes, luego sencillos) hasta sumar missing canciones
// suyas o completar maxAlbumsToFill álbumes, todo dentro del presupuesto.
// Solo marca al artista si terminó sin errores.
func (s *MusicService) fillArtistTracks(ctx context.Context, artist sqlc.GetArtistRow, missing int) error {
	_, err, _ := s.inflight.Do("tracks:"+artist.ID.String(), func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, s.enrich.Budget)
		defer cancel()
		if err := s.syncArtistAlbums(ctx, artist); err != nil {
			return nil, err
		}
		albums, err := s.catalog.ListAlbumsToFillByArtist(ctx, sqlc.ListAlbumsToFillByArtistParams{ArtistID: artist.ID, MaxResults: maxAlbumsToFill})
		if err != nil {
			return nil, err
		}
		added := 0
		for _, al := range albums {
			if added >= missing {
				break
			}
			saved, err := s.fillAlbumTracks(ctx, al.ID, al.SpotifyID.String)
			if err != nil {
				return nil, err
			}
			for _, song := range saved {
				if song.ArtistSpotifyID == artist.SpotifyID.String {
					added++
				}
			}
		}
		return nil, s.catalog.MarkArtistTracksSynced(ctx, artist.ID)
	})
	return err
}

// fillAlbumTracks importa todas las pistas del álbum desde Spotify, encola
// las nuevas para embedding y marca el álbum para no volver a pedirlas. Un
// álbum que ya no existe en Spotify también se marca.
func (s *MusicService) fillAlbumTracks(ctx context.Context, albumID uuid.UUID, spotifyID string) ([]domain.ImportedSong, error) {
	v, err, _ := s.inflight.Do("album:"+albumID.String(), func() (any, error) {
		_, tracks, err := s.provider.AlbumWithTracks(ctx, spotifyID)
		if errors.Is(err, domain.ErrNotFound) {
			return []domain.ImportedSong(nil), s.catalog.MarkAlbumTracksSynced(ctx, albumID)
		}
		if err != nil {
			return nil, err
		}
		saved, err := s.importer.ImportTracks(ctx, tracks)
		if err != nil {
			return nil, err
		}
		var jobs []domain.EmbeddingJob
		for _, song := range saved {
			if song.PendingEmbedding {
				jobs = append(jobs, embeddingJob(song))
			}
		}
		if s.queue != nil && len(jobs) > 0 {
			s.queue.Enqueue(jobs...)
		}
		return saved, s.catalog.MarkAlbumTracksSynced(ctx, albumID)
	})
	if err != nil {
		return nil, fmt.Errorf("completar pistas del álbum %s: %w", spotifyID, err)
	}
	return v.([]domain.ImportedSong), nil
}

// completeAlbumTracks completa las pistas que le faltan al álbum (una sola
// vez, dentro del presupuesto). Devuelve true si importó algo.
func (s *MusicService) completeAlbumTracks(ctx context.Context, r sqlc.GetAlbumRow, have int) bool {
	if s.provider == nil || !r.SpotifyID.Valid || r.TracksSyncedAt.Valid || have >= int(r.TotalTracks) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, s.enrich.Budget)
	defer cancel()
	saved, err := s.fillAlbumTracks(ctx, r.ID, r.SpotifyID.String)
	if err != nil {
		log.Printf("detalle de álbum: %v", err)
		return false
	}
	return len(saved) > 0
}

func (s *MusicService) artistRow(ctx context.Context, id string) (sqlc.GetArtistRow, error) {
	localID, spotifyID, err := parseEntityID(id)
	if err != nil {
		return sqlc.GetArtistRow{}, err
	}
	r, err := s.catalog.GetArtist(ctx, sqlc.GetArtistParams{ID: localID, SpotifyID: spotifyID})
	if err != nil {
		return sqlc.GetArtistRow{}, notFoundOr(err, "music: obtener artista")
	}
	return r, nil
}

// listLimit valida el limit de los listados (0 usa DefaultSearchLimit).
func listLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultSearchLimit, nil
	}
	if limit < 1 || limit > MaxSearchLimit {
		return 0, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxSearchLimit))
	}
	return limit, nil
}

// stale indica si una sincronización nunca se hizo o ya venció.
func stale(syncedAt sql.NullTime) bool {
	return !syncedAt.Valid || time.Since(syncedAt.Time) > DiscographyTTL
}
