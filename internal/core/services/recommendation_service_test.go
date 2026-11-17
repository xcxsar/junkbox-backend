package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

type fakeRecommendationStore struct {
	seed    *sqlc.GetRecommendationSeedRow // nil: sin semilla (sql.ErrNoRows)
	seedErr error
	// similar devuelve las filas según el filtro de género recibido.
	similar      func(arg sqlc.RecommendSimilarTracksParams) []sqlc.RecommendSimilarTracksRow
	similarCalls []sqlc.RecommendSimilarTracksParams
	popular      []sqlc.ListPopularTracksRow
	popularCalls []sqlc.ListPopularTracksParams
}

func (f *fakeRecommendationStore) GetRecommendationSeed(_ context.Context, _ uuid.UUID) (sqlc.GetRecommendationSeedRow, error) {
	if f.seedErr != nil {
		return sqlc.GetRecommendationSeedRow{}, f.seedErr
	}
	if f.seed == nil {
		return sqlc.GetRecommendationSeedRow{}, sql.ErrNoRows
	}
	return *f.seed, nil
}

func (f *fakeRecommendationStore) RecommendSimilarTracks(_ context.Context, arg sqlc.RecommendSimilarTracksParams) ([]sqlc.RecommendSimilarTracksRow, error) {
	f.similarCalls = append(f.similarCalls, arg)
	if f.similar == nil {
		return nil, nil
	}
	return f.similar(arg), nil
}

func (f *fakeRecommendationStore) ListPopularTracks(_ context.Context, arg sqlc.ListPopularTracksParams) ([]sqlc.ListPopularTracksRow, error) {
	f.popularCalls = append(f.popularCalls, arg)
	return f.popular, nil
}

func seedRow(genre string) *sqlc.GetRecommendationSeedRow {
	return &sqlc.GetRecommendationSeedRow{
		ID:         10,
		TrackID:    sql.NullString{String: trackA, Valid: true},
		TrackName:  sql.NullString{String: "Yellow", Valid: true},
		ArtistName: sql.NullString{String: "Coldplay", Valid: true},
		Genre:      sql.NullString{String: genre, Valid: genre != ""},
		Embedding:  "[0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8,0.9,1]",
	}
}

func similarRow(id int32, genre string) sqlc.RecommendSimilarTracksRow {
	return sqlc.RecommendSimilarTracksRow{
		ID:           id,
		TrackName:    sql.NullString{String: "Similar", Valid: true},
		ArtistName:   sql.NullString{String: "Artista", Valid: true},
		Genre:        sql.NullString{String: genre, Valid: genre != ""},
		HasEmbedding: true,
	}
}

// --- feed de Inicio ---

func TestHomeConGeneroFiltraPorGenero(t *testing.T) {
	store := &fakeRecommendationStore{
		seed: seedRow("rock"),
		similar: func(arg sqlc.RecommendSimilarTracksParams) []sqlc.RecommendSimilarTracksRow {
			return []sqlc.RecommendSimilarTracksRow{similarRow(1, arg.Genre.String), similarRow(2, arg.Genre.String)}
		},
	}
	userID := uuid.New()
	feed, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), userID, 0)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}

	if len(store.similarCalls) != 1 {
		t.Fatalf("esperaba una consulta de similares, hubo %d", len(store.similarCalls))
	}
	arg := store.similarCalls[0]
	if !arg.Genre.Valid || arg.Genre.String != "rock" || arg.SeedID != 10 || arg.UserID != userID || arg.MaxResults != DefaultHomeLimit {
		t.Fatalf("parámetros inesperados: %+v", arg)
	}
	if arg.SeedTrackName != "Yellow" || arg.SeedArtistName != "Coldplay" || arg.Embedding != seedRow("").Embedding {
		t.Fatalf("la semilla no llegó completa a la consulta: %+v", arg)
	}

	if len(feed.Sections) != 1 {
		t.Fatalf("esperaba una sección, hubo %d", len(feed.Sections))
	}
	s := feed.Sections[0]
	if s.ID != domain.HomeSectionMadeForYou || s.Strategy != domain.StrategySimilarToLast {
		t.Fatalf("sección inesperada: %+v", s)
	}
	if s.Seed == nil || s.Seed.ID != 10 || s.Seed.SpotifyID != trackA || s.Seed.Genre == nil || *s.Seed.Genre != "rock" {
		t.Fatalf("seed inesperada: %+v", s.Seed)
	}
	if len(s.Items) != 2 || s.Items[0].Type != domain.HomeItemTrack || s.Items[0].ID != 1 || *s.Items[0].Genre != "rock" {
		t.Fatalf("items inesperados: %+v", s.Items)
	}
	if len(store.popularCalls) != 0 {
		t.Fatal("no debía consultar populares")
	}
}

func TestHomeSinGeneroUsaLaFuncionSinGenero(t *testing.T) {
	store := &fakeRecommendationStore{
		seed: seedRow(""),
		similar: func(sqlc.RecommendSimilarTracksParams) []sqlc.RecommendSimilarTracksRow {
			return []sqlc.RecommendSimilarTracksRow{similarRow(1, "pop")}
		},
	}
	feed, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), uuid.New(), 5)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(store.similarCalls) != 1 || store.similarCalls[0].Genre.Valid || store.similarCalls[0].MaxResults != 5 {
		t.Fatalf("esperaba una consulta sin género con limit 5: %+v", store.similarCalls)
	}
	if s := feed.Sections[0]; s.Strategy != domain.StrategySimilarToLast || s.Seed == nil || s.Seed.Genre != nil {
		t.Fatalf("sección inesperada: %+v", s)
	}
}

func TestHomeGeneroSinResultadosReintentaSinGenero(t *testing.T) {
	store := &fakeRecommendationStore{
		seed: seedRow("rock"),
		similar: func(arg sqlc.RecommendSimilarTracksParams) []sqlc.RecommendSimilarTracksRow {
			if arg.Genre.Valid {
				return nil
			}
			return []sqlc.RecommendSimilarTracksRow{similarRow(1, "pop")}
		},
	}
	feed, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(store.similarCalls) != 2 || !store.similarCalls[0].Genre.Valid || store.similarCalls[1].Genre.Valid {
		t.Fatalf("esperaba una consulta con género y otra sin: %+v", store.similarCalls)
	}
	if s := feed.Sections[0]; s.Strategy != domain.StrategySimilarToLast || len(s.Items) != 1 {
		t.Fatalf("sección inesperada: %+v", s)
	}
}

func TestHomeSinSemillaUsaPopulares(t *testing.T) {
	store := &fakeRecommendationStore{
		popular: []sqlc.ListPopularTracksRow{sqlc.ListPopularTracksRow(similarRow(7, "pop"))},
	}
	userID := uuid.New()
	feed, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), userID, 0)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(store.similarCalls) != 0 {
		t.Fatal("sin semilla no debía consultar similares")
	}
	if len(store.popularCalls) != 1 || store.popularCalls[0].UserID != userID || store.popularCalls[0].MaxResults != DefaultHomeLimit {
		t.Fatalf("consulta de populares inesperada: %+v", store.popularCalls)
	}
	s := feed.Sections[0]
	if s.Strategy != domain.StrategyPopular || s.Seed != nil || len(s.Items) != 1 || s.Items[0].ID != 7 {
		t.Fatalf("sección inesperada: %+v", s)
	}
}

func TestHomeSimilaresVaciosUsaPopulares(t *testing.T) {
	store := &fakeRecommendationStore{seed: seedRow("rock")}
	feed, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(store.similarCalls) != 2 || len(store.popularCalls) != 1 {
		t.Fatalf("esperaba dos intentos de similares y luego populares: similares=%d populares=%d", len(store.similarCalls), len(store.popularCalls))
	}
	s := feed.Sections[0]
	if s.Strategy != domain.StrategyPopular || s.Seed != nil || s.Items == nil {
		t.Fatalf("sección inesperada (items debe ser [] y no null): %+v", s)
	}
}

func TestHomeErrorDeBDSePropaga(t *testing.T) {
	store := &fakeRecommendationStore{seedErr: errors.New("conexión perdida")}
	if _, err := NewRecommendationService(store, SearchEnrichment{}).Home(context.Background(), uuid.New(), 0); err == nil {
		t.Fatal("esperaba error")
	}
}

func TestHomeValidaLimit(t *testing.T) {
	svc := NewRecommendationService(&fakeRecommendationStore{}, SearchEnrichment{})
	for _, limit := range []int{-1, MaxHomeLimit + 1} {
		_, err := svc.Home(context.Background(), uuid.New(), limit)
		var v *domain.ValidationError
		if !errors.As(err, &v) {
			t.Errorf("limit %d: esperaba ValidationError, obtuve %v", limit, err)
		}
	}
}

func TestHomeCompletaImagenesComoLaBusqueda(t *testing.T) {
	f := newMusicFixture(true)
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}
	albumID := uuid.New()
	row := similarRow(1, "rock")
	row.TrackID = sql.NullString{String: trackA, Valid: true}
	row.AlbumID = &albumID
	row.AlbumImageUrl = sql.NullString{String: "cover.jpg", Valid: true}
	row.ArtistSpotifyID = sql.NullString{String: artistA, Valid: true}
	store := &fakeRecommendationStore{
		seed: seedRow("rock"),
		similar: func(sqlc.RecommendSimilarTracksParams) []sqlc.RecommendSimilarTracksRow {
			return []sqlc.RecommendSimilarTracksRow{row}
		},
	}
	svc := NewRecommendationService(store, SearchEnrichment{
		Enricher: NewCatalogEnricher(f.metadata, f.store, f.importer, f.queue),
		Fallback: f.fallback,
	})

	feed, err := svc.Home(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	item := feed.Sections[0].Items[0]
	if item.AlbumImageURL == nil || *item.AlbumImageURL != "cover.jpg" || item.ArtistImageURL == nil || *item.ArtistImageURL != "artist.jpg" {
		t.Fatalf("imágenes inesperadas: album=%v artist=%v", item.AlbumImageURL, item.ArtistImageURL)
	}
	if len(f.fallback.jobs) != 0 {
		t.Fatalf("nada debía quedar pendiente: %v", f.fallback.jobs)
	}
}
