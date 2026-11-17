package services

import (
	"context"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
)

// Enriquecimiento de imágenes compartido por los listados de canciones (la
// búsqueda y el feed de Inicio).

// withDefaults completa los valores en cero de la configuración.
func (e SearchEnrichment) withDefaults() SearchEnrichment {
	if e.Budget <= 0 {
		e.Budget = DefaultEnrichBudget
	}
	if e.Concurrency <= 0 {
		e.Concurrency = DefaultEnrichConcurrency
	}
	return e
}

// trackEnrichmentState es lo que decide si a una canción de un listado le
// falta pedir a Spotify su portada o la foto de su artista.
type trackEnrichmentState struct {
	SpotifyID            string
	AlbumID              *uuid.UUID
	LinkAttempts         int32
	HasArtistImage       bool
	ArtistSpotifyID      string
	ArtistEnriched       bool
	ArtistEnrichAttempts int32
}

// jobs devuelve los jobs de enriquecimiento que necesita la canción.
func (t trackEnrichmentState) jobs() []domain.EnrichmentJob {
	var jobs []domain.EnrichmentJob
	// Las canciones del CSV no tienen álbum: enlazarlas trae la portada y
	// reclama a su artista.
	if t.AlbumID == nil && spotifyIDPattern.MatchString(t.SpotifyID) && t.LinkAttempts < MaxEnrichAttempts {
		jobs = append(jobs, domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: t.SpotifyID})
	}
	if !t.HasArtistImage && artistEnrichable(t.ArtistSpotifyID, t.ArtistEnriched, t.ArtistEnrichAttempts) {
		jobs = append(jobs, domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: t.ArtistSpotifyID})
	}
	return jobs
}

// resolveEnrichment pide a Spotify, en paralelo y dentro del presupuesto,
// lo que piden los jobs; lo que no termina a tiempo pasa al worker de
// respaldo. ok es false si no hubo nada que pedir.
func resolveEnrichment(ctx context.Context, cfg SearchEnrichment, jobs []domain.EnrichmentJob) (got enrichment, ok bool) {
	if cfg.Enricher == nil || len(jobs) == 0 {
		return enrichment{}, false
	}
	got, left := cfg.Enricher.Resolve(ctx, jobs, cfg.Budget, cfg.Concurrency)
	if len(left) > 0 && cfg.Fallback != nil {
		cfg.Fallback.Enqueue(left...)
	}
	return got, true
}

// applyTrackEnrichment aplica a las canciones las portadas e imágenes de
// artista resueltas. artists es el spotify_id del artista de cada canción
// ("" si no se conoce).
func applyTrackEnrichment(tracks []domain.Track, artists []string, got enrichment) {
	for i := range tracks {
		t := &tracks[i]
		artistID := artists[i]
		if song, ok := got.songs[t.SpotifyID]; ok {
			if t.AlbumTitle == nil {
				t.AlbumTitle = song.AlbumTitle
			}
			t.AlbumImageURL = song.AlbumImageURL
			if song.ArtistImageURL != nil {
				t.ArtistImageURL = song.ArtistImageURL
			}
			artistID = song.ArtistSpotifyID
		}
		if t.ArtistImageURL == nil {
			t.ArtistImageURL = got.artistImages[artistID]
		}
	}
}
