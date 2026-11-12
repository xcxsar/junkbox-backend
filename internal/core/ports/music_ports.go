package ports

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// CatalogStore son las queries de lectura del catálogo musical.
type CatalogStore interface {
	SearchTracksTrigram(ctx context.Context, arg sqlc.SearchTracksTrigramParams) ([]sqlc.SearchTracksTrigramRow, error)
	SearchArtistsTrigram(ctx context.Context, arg sqlc.SearchArtistsTrigramParams) ([]sqlc.SearchArtistsTrigramRow, error)
	SearchAlbumsTrigram(ctx context.Context, arg sqlc.SearchAlbumsTrigramParams) ([]sqlc.SearchAlbumsTrigramRow, error)
	GetTrack(ctx context.Context, arg sqlc.GetTrackParams) (sqlc.GetTrackRow, error)
	GetArtist(ctx context.Context, arg sqlc.GetArtistParams) (sqlc.GetArtistRow, error)
	GetAlbum(ctx context.Context, arg sqlc.GetAlbumParams) (sqlc.GetAlbumRow, error)
	ListTracksByAlbum(ctx context.Context, albumID *uuid.UUID) ([]sqlc.ListTracksByAlbumRow, error)
}

// CatalogImporter persiste en una sola transacción lo que devuelve Spotify
// (artistas → álbumes → canciones). Si algo falla no queda nada a medias.
// Devuelve las entidades ya persistidas, en el mismo orden de entrada.
type CatalogImporter interface {
	ImportTracks(ctx context.Context, tracks []domain.ImportedTrack) ([]domain.ImportedSong, error)
	ImportArtists(ctx context.Context, artists []domain.ImportedArtist) ([]domain.Artist, error)
	ImportAlbums(ctx context.Context, albums []domain.ImportedAlbum) ([]domain.Album, error)
}

// MusicProvider busca en un catálogo externo (Spotify).
type MusicProvider interface {
	SearchTracks(ctx context.Context, query string, limit int) ([]domain.ImportedTrack, error)
	SearchArtists(ctx context.Context, query string, limit int) ([]domain.ImportedArtist, error)
	SearchAlbums(ctx context.Context, query string, limit int) ([]domain.ImportedAlbum, error)
}

// MusicMetadataProvider obtiene una entidad del catálogo externo por su ID
// (Spotify). Si no existe, devuelve un error que envuelve domain.ErrNotFound.
type MusicMetadataProvider interface {
	GetArtist(ctx context.Context, id string) (domain.ImportedArtist, error)
	GetTrack(ctx context.Context, id string) (domain.ImportedTrack, error)
}

// EnrichmentStore son las queries del enriquecimiento con Spotify. Los
// claims los usa solo el worker de respaldo.
type EnrichmentStore interface {
	UpdateArtistEnrichment(ctx context.Context, arg sqlc.UpdateArtistEnrichmentParams) (sql.NullString, error)
	MarkArtistEnrichmentFailed(ctx context.Context, arg sqlc.MarkArtistEnrichmentFailedParams) error
	MarkSongLinkFailed(ctx context.Context, arg sqlc.MarkSongLinkFailedParams) error
	ClaimArtistsForEnrichment(ctx context.Context, arg sqlc.ClaimArtistsForEnrichmentParams) ([]sql.NullString, error)
	ClaimSongsForLinking(ctx context.Context, arg sqlc.ClaimSongsForLinkingParams) ([]sql.NullString, error)
}

// EnrichmentQueue recibe lo que la búsqueda no alcanzó a completar dentro
// de su presupuesto. No debe bloquear.
type EnrichmentQueue interface {
	Enqueue(jobs ...domain.EnrichmentJob)
}

// AudioFeaturesProvider obtiene audio features por ISRC o Spotify ID
// (FreqBlog). Los IDs que no se resuelven no aparecen en el mapa. Un error
// significa que el proveedor no respondió (credencial, cuota, red); si
// implementa RetryAfterError, indica cuánto esperar.
type AudioFeaturesProvider interface {
	GetAudioFeatures(ctx context.Context, ids []string) (map[string]*domain.AudioFeatures, error)
}

// RetryAfterError lo implementan los errores de un proveedor que pide
// esperar antes de reintentar (HTTP 429 con Retry-After).
type RetryAfterError interface {
	error
	RetryAfter() time.Duration
}

// EmbeddingStore son las queries del worker de embeddings.
type EmbeddingStore interface {
	ClaimPendingEmbeddingTracks(ctx context.Context, arg sqlc.ClaimPendingEmbeddingTracksParams) ([]sqlc.ClaimPendingEmbeddingTracksRow, error)
	UpdateSongAudioFeaturesAndEmbedding(ctx context.Context, arg sqlc.UpdateSongAudioFeaturesAndEmbeddingParams) (int64, error)
	MarkEmbeddingAttemptFailed(ctx context.Context, arg sqlc.MarkEmbeddingAttemptFailedParams) error
	GetCatalogFeatureMedians(ctx context.Context) (sqlc.GetCatalogFeatureMediansRow, error)
}

// EmbeddingQueue recibe las canciones recién importadas que necesitan
// embedding. No debe bloquear.
type EmbeddingQueue interface {
	Enqueue(jobs ...domain.EmbeddingJob)
}
