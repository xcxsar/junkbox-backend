package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

type fakeSuggestions struct {
	rows        []sqlc.ListSuggestionsForUserRow
	popular     []sqlc.ListPopularUsersRow
	artists     map[uuid.UUID]sqlc.ListArtistsByIDsRow
	lastList    sqlc.ListSuggestionsForUserParams
	lastPopular *sqlc.ListPopularUsersParams
	refreshArgs []float64
}

func (f *fakeSuggestions) ListSuggestionsForUser(_ context.Context, arg sqlc.ListSuggestionsForUserParams) ([]sqlc.ListSuggestionsForUserRow, error) {
	f.lastList = arg
	return f.rows, nil
}

func (f *fakeSuggestions) ListPopularUsers(_ context.Context, arg sqlc.ListPopularUsersParams) ([]sqlc.ListPopularUsersRow, error) {
	f.lastPopular = &arg
	return f.popular[:min(len(f.popular), int(arg.MaxResults))], nil
}

func (f *fakeSuggestions) ListArtistsByIDs(_ context.Context, ids []uuid.UUID) ([]sqlc.ListArtistsByIDsRow, error) {
	var out []sqlc.ListArtistsByIDsRow
	for _, id := range ids {
		if a, ok := f.artists[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeSuggestions) RefreshSuggestions(_ context.Context, minScore float64, maxPerUser int32) (int64, int64, error) {
	f.refreshArgs = append(f.refreshArgs, minScore, float64(maxPerUser))
	return 3, 6, nil
}

func newFakeSuggestions() *fakeSuggestions {
	return &fakeSuggestions{artists: map[uuid.UUID]sqlc.ListArtistsByIDsRow{}}
}

func (f *fakeSuggestions) artist(name string) uuid.UUID {
	id := uuid.New()
	f.artists[id] = sqlc.ListArtistsByIDsRow{ID: id, Name: name, ImageUrl: sql.NullString{String: name + ".jpg", Valid: true}}
	return id
}

func suggestionRow(kind string, common int32, artists []uuid.UUID, similarity float64) sqlc.ListSuggestionsForUserRow {
	return sqlc.ListSuggestionsForUserRow{
		SuggestedID: uuid.New(), Kind: kind, CommonCount: common, CommonArtistIds: artists,
		Similarity: sql.NullFloat64{Float64: similarity, Valid: similarity > 0},
		ComputedAt: time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC), Username: "usuario",
	}
}

func TestSuggestionsTextosDelMotivo(t *testing.T) {
	store := newFakeSuggestions()
	hoke, feid, bb := store.artist("Hoke"), store.artist("Feid"), store.artist("Bad Bunny")
	store.rows = []sqlc.ListSuggestionsForUserRow{
		suggestionRow(domain.SuggestionCommonArtists, 5, []uuid.UUID{hoke, feid, bb}, 0.98),
		suggestionRow(domain.SuggestionCommonArtists, 2, []uuid.UUID{hoke, feid}, 0),
		suggestionRow(domain.SuggestionCommonArtists, 1, []uuid.UUID{hoke}, 0),
		suggestionRow(domain.SuggestionSimilarTaste, 0, nil, 0.97),
	}
	page, err := NewSuggestionService(store, store).Suggestions(context.Background(), uuid.New(), 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"También escucha Hoke, Feid y 3 más",
		"También escucha Hoke y Feid",
		"También escucha Hoke",
		"Gustos parecidos a los tuyos",
	}
	for i, w := range want {
		if got := page.Users[i].Reason.Text; got != w {
			t.Errorf("reason[%d] = %q, esperaba %q", i, got, w)
		}
	}
	first := page.Users[0].Reason
	if first.Type != domain.SuggestionCommonArtists || len(first.Artists) != 3 || first.Artists[0].Name != "Hoke" || *first.Artists[0].ImageURL != "Hoke.jpg" || first.Similarity == nil {
		t.Fatalf("motivo inesperado: %+v", first)
	}
	if page.Users[3].Reason.Type != domain.SuggestionSimilarTaste || page.Users[3].Reason.Artists == nil {
		t.Fatalf("motivo acústico inesperado: %+v", page.Users[3].Reason)
	}
	if store.lastPopular != nil {
		t.Fatal("con la lista completa no debía consultar populares")
	}
	if page.ComputedAt == nil {
		t.Fatal("debía informar cuándo se calcularon")
	}
}

func TestSuggestionsCompletaConPopulares(t *testing.T) {
	store := newFakeSuggestions()
	store.rows = []sqlc.ListSuggestionsForUserRow{suggestionRow(domain.SuggestionSimilarTaste, 0, nil, 0.9)}
	store.popular = []sqlc.ListPopularUsersRow{{ID: uuid.New(), Username: "popular1"}, {ID: uuid.New(), Username: "popular2"}, {ID: uuid.New(), Username: "popular3"}}
	viewer := uuid.New()

	page, err := NewSuggestionService(store, store).Suggestions(context.Background(), viewer, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 3 || page.Users[1].Reason.Type != domain.SuggestionPopular || page.Users[1].Reason.Text != "Popular en JunkBox" {
		t.Fatalf("página inesperada: %+v", page.Users)
	}
	p := store.lastPopular
	if p == nil || p.MaxResults != 2 || p.ViewerID != viewer || len(p.ExcludeIds) != 1 || p.ExcludeIds[0] != store.rows[0].SuggestedID {
		t.Fatalf("los populares debían pedirse para completar y sin repetir: %+v", p)
	}
}

func TestSuggestionsUsuarioSinCalculo(t *testing.T) {
	store := newFakeSuggestions()
	store.popular = []sqlc.ListPopularUsersRow{{ID: uuid.New(), Username: "popular1"}}
	page, err := NewSuggestionService(store, store).Suggestions(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.ComputedAt != nil || page.Limit != DefaultSuggestionLimit || len(page.Users) != 1 || store.lastPopular.MaxResults != DefaultSuggestionLimit {
		t.Fatalf("página inesperada: %+v", page)
	}
}

func TestSuggestionsValidaLimit(t *testing.T) {
	svc := NewSuggestionService(newFakeSuggestions(), nil)
	for _, limit := range []int{-1, MaxSuggestionLimit + 1} {
		_, err := svc.Suggestions(context.Background(), uuid.New(), limit)
		var v *domain.ValidationError
		if !errors.As(err, &v) {
			t.Errorf("limit %d: esperaba ValidationError, obtuve %v", limit, err)
		}
	}
}

func TestSuggestionsRefreshUsaElUmbral(t *testing.T) {
	store := newFakeSuggestions()
	profiles, suggestions, err := NewSuggestionService(store, store).Refresh(context.Background())
	if err != nil || profiles != 3 || suggestions != 6 {
		t.Fatalf("refresh: %d %d %v", profiles, suggestions, err)
	}
	if store.refreshArgs[0] != SuggestionMinScore || store.refreshArgs[1] != suggestionsPerUser {
		t.Fatalf("parámetros del cálculo: %v", store.refreshArgs)
	}
}
