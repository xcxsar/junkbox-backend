package domain

// Secciones del feed de Inicio (GET /home).
const (
	HomeSectionMadeForYou      = "made_for_you"
	HomeSectionMadeForYouTitle = "Hecho para tus oídos"
)

// Estrategias con las que se arma una sección.
const (
	// StrategySimilarToLast: canciones acústicamente parecidas a la última
	// que el usuario calificó o reseñó (y de su género, si se conoce).
	StrategySimilarToLast = "similar_to_last"
	// StrategyPopular: el usuario no tiene semilla; canciones populares.
	StrategyPopular = "popular"
)

// Tipos de elemento de una sección.
const (
	HomeItemTrack = "track"
)

// HomeFeed es la respuesta de GET /home.
type HomeFeed struct {
	Sections []HomeSection `json:"sections"`
}

// HomeSection es una fila del feed. Seed es nil con StrategyPopular.
type HomeSection struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Strategy string              `json:"strategy"`
	Seed     *RecommendationSeed `json:"seed"`
	Items    []HomeItem          `json:"items"`
}

// RecommendationSeed es la canción en la que se basa una sección.
type RecommendationSeed struct {
	ID         int32   `json:"id"`
	SpotifyID  string  `json:"spotify_id"`
	TrackName  string  `json:"track_name"`
	ArtistName string  `json:"artist_name"`
	Genre      *string `json:"genre"`
}

// HomeItem es un elemento de una sección: type más los campos de la
// entidad al mismo nivel (por ahora solo canciones).
type HomeItem struct {
	Type string `json:"type"`
	Track
}
