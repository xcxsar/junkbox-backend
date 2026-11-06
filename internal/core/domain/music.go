package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errores del catálogo musical. Los handlers los traducen a HTTP.
var (
	// ErrNotFound: la canción, el artista o el álbum no existe.
	ErrNotFound = errors.New("music: no encontrado")
	// ErrMusicProviderUnavailable: no hay resultados locales y Spotify no
	// está configurado o falló.
	ErrMusicProviderUnavailable = errors.New("music: proveedor de música no disponible")
)

// Tipos de entidad que admite la búsqueda.
const (
	SearchTypeTrack  = "track"
	SearchTypeArtist = "artist"
	SearchTypeAlbum  = "album"
)

// Origen de los resultados de búsqueda.
const (
	SourceLocalCache      = "local_cache"
	SourceSpotifyFallback = "spotify_fallback"
)

// SearchResult es la respuesta de GET /music/search. Results contiene
// []Track, []Artist o []Album según Type.
type SearchResult struct {
	Source  string `json:"source"`
	Query   string `json:"query"`
	Type    string `json:"type"`
	Count   int    `json:"count"`
	Results any    `json:"results"`
}

// Track es una canción en listados (búsqueda y pistas de un álbum).
type Track struct {
	ID               int32   `json:"id"`
	SpotifyID        string  `json:"spotify_id"`
	TrackName        string  `json:"track_name"`
	ArtistName       string  `json:"artist_name"`
	AlbumTitle       *string `json:"album_title,omitempty"`
	Year             *int32  `json:"year"`
	Genre            *string `json:"genre"`
	Popularity       *int32  `json:"popularity"`
	DurationMs       *int32  `json:"duration_ms"`
	HasEmbedding     bool    `json:"has_embedding"`
	PendingEmbedding bool    `json:"pending_embedding"`
}

// TrackDetail es la respuesta de GET /music/tracks/{id}.
type TrackDetail struct {
	Track
	ArtistID      *uuid.UUID     `json:"artist_id"`
	AlbumID       *uuid.UUID     `json:"album_id"`
	ISRC          *string        `json:"isrc"`
	AudioFeatures *AudioFeatures `json:"audio_features"`
	Ratings       RatingStats    `json:"ratings"`
}

// AudioFeatures son los atributos acústicos de una canción. Los campos nil
// no están disponibles (p. ej. FreqBlog no mide speechiness ni liveness).
type AudioFeatures struct {
	Danceability     *float64 `json:"danceability"`
	Energy           *float64 `json:"energy"`
	Key              *int32   `json:"key"`
	Loudness         *float64 `json:"loudness"`
	Mode             *int32   `json:"mode"`
	Speechiness      *float64 `json:"speechiness"`
	Acousticness     *float64 `json:"acousticness"`
	Instrumentalness *float64 `json:"instrumentalness"`
	Liveness         *float64 `json:"liveness"`
	Valence          *float64 `json:"valence"`
	Tempo            *float64 `json:"tempo"`
	TimeSignature    *int32   `json:"time_signature"`
}

// Artist es un artista del catálogo. Ratings solo se llena en el detalle.
type Artist struct {
	ID         uuid.UUID    `json:"id"`
	SpotifyID  *string      `json:"spotify_id"`
	Name       string       `json:"name"`
	Genres     []string     `json:"genres"`
	Popularity *int32       `json:"popularity"`
	ImageURL   *string      `json:"image_url"`
	CreatedAt  time.Time    `json:"created_at"`
	Ratings    *RatingStats `json:"ratings,omitempty"`
}

// Album es un álbum del catálogo. Tracks y Ratings solo se llenan en el
// detalle.
type Album struct {
	ID          uuid.UUID    `json:"id"`
	SpotifyID   *string      `json:"spotify_id"`
	Title       string       `json:"title"`
	ArtistID    *uuid.UUID   `json:"artist_id"`
	ArtistName  string       `json:"artist_name"`
	ReleaseYear *int32       `json:"release_year"`
	AlbumType   string       `json:"album_type"`
	ImageURL    *string      `json:"image_url"`
	TotalTracks int32        `json:"total_tracks"`
	CreatedAt   time.Time    `json:"created_at"`
	Tracks      []Track      `json:"tracks,omitempty"`
	Ratings     *RatingStats `json:"ratings,omitempty"`
}

// --- Entidades importadas desde Spotify (entrada de CatalogImporter) ---

// ImportedArtist es un artista tal como llega de Spotify. Los artistas
// simplificados (dentro de una canción o álbum) no traen géneros,
// popularidad ni imagen.
type ImportedArtist struct {
	SpotifyID  string
	Name       string
	Genres     []string
	Popularity *int32
	ImageURL   *string
}

// ImportedAlbum es un álbum tal como llega de Spotify.
type ImportedAlbum struct {
	SpotifyID   string
	Title       string
	Artist      ImportedArtist
	ReleaseYear *int32
	AlbumType   string
	ImageURL    *string
	TotalTracks int32
}

// ImportedTrack es una canción tal como llega de Spotify.
type ImportedTrack struct {
	SpotifyID  string
	Name       string
	Artist     ImportedArtist // artista principal
	Album      ImportedAlbum
	ISRC       *string
	Popularity *int32
	DurationMs *int32
}

// ImportedSong es una canción ya persistida por CatalogImporter, con lo
// necesario para responder y para encolarla en el worker de embeddings.
type ImportedSong struct {
	Track
	ISRC              *string
	EmbeddingAttempts int32
}

// --- Worker de embeddings ---

// EmbeddingJob es una canción pendiente de embedding. Se consulta a
// FreqBlog por ISRC si existe (más fiable) o por el Spotify ID.
type EmbeddingJob struct {
	TrackID  string
	ISRC     string
	Attempts int32
}

// LookupID devuelve el identificador con el que se consulta a FreqBlog.
func (j EmbeddingJob) LookupID() string {
	if j.ISRC != "" {
		return j.ISRC
	}
	return j.TrackID
}
