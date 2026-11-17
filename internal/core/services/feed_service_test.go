package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// fakeFeed devuelve rows tal cual (la BD ordena y filtra) y guarda los
// parámetros recibidos.
type fakeFeed struct {
	popular       []sqlc.ListPopularFeedRow
	following     []sqlc.ListFollowingFeedRow
	lastPopular   sqlc.ListPopularFeedParams
	lastFollowing sqlc.ListFollowingFeedParams
}

func (f *fakeFeed) ListPopularFeed(_ context.Context, arg sqlc.ListPopularFeedParams) ([]sqlc.ListPopularFeedRow, error) {
	f.lastPopular = arg
	return f.popular, nil
}

func (f *fakeFeed) ListFollowingFeed(_ context.Context, arg sqlc.ListFollowingFeedParams) ([]sqlc.ListFollowingFeedRow, error) {
	f.lastFollowing = arg
	return f.following, nil
}

func feedRow(createdAt time.Time, score int32, songID int32) sqlc.ListPopularFeedRow {
	r := sqlc.ListPopularFeedRow{ID: uuid.New(), UserID: uuid.New(), Content: "Muy buena.", CreatedAt: createdAt, AuthorUsername: "autor"}
	r.SongID.Int32, r.SongID.Valid = songID, true
	r.PopularityScore.Int32, r.PopularityScore.Valid = score, true
	return r
}

func TestFeedPopularPaginaConCursor(t *testing.T) {
	// 23:30 del 16 en Mazatlán (UTC-7) es el 17 en UTC: el día del cursor
	// debe salir de la zona pedida.
	late := time.Date(2026, 11, 17, 6, 30, 0, 0, time.UTC)
	store := &fakeFeed{popular: []sqlc.ListPopularFeedRow{feedRow(late, 5, 1), feedRow(late.Add(-time.Hour), 3, 2), feedRow(late.Add(-2*time.Hour), 1, 3)}}
	summaries := &fakeSummarizer{}
	svc := NewFeedService(store, summaries, "")
	viewer := uuid.New()

	page, err := svc.Feed(context.Background(), viewer, domain.FeedTabPopular, "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if store.lastPopular.Tz != DefaultFeedTimezone || store.lastPopular.MaxResults != 3 || store.lastPopular.ViewerID != viewer || store.lastPopular.CursorID != nil {
		t.Fatalf("parámetros de la primera página: %+v", store.lastPopular)
	}
	if len(page.Reviews) != 2 || page.NextCursor == nil || page.Timezone != DefaultFeedTimezone {
		t.Fatalf("página inesperada: %+v", page)
	}
	if page.Reviews[0].Target.Summary == nil || len(summaries.calls) != 2 {
		t.Fatalf("cada reseña debía traer su resumen: %d llamadas", len(summaries.calls))
	}

	if _, err := svc.Feed(context.Background(), viewer, domain.FeedTabPopular, "", *page.NextCursor, 2); err != nil {
		t.Fatal(err)
	}
	arg := store.lastPopular
	second := store.popular[1]
	if arg.CursorID == nil || *arg.CursorID != second.ID || !arg.CursorCreatedAt.Time.Equal(second.CreatedAt) || arg.CursorScore.Int32 != 3 {
		t.Fatalf("el cursor debía apuntar a la última reseña entregada: %+v", arg)
	}
	if arg.CursorDay.String != "2026-11-16" {
		t.Fatalf("día del cursor = %q, esperaba el día local 2026-11-16", arg.CursorDay.String)
	}
}

func TestFeedUltimaPaginaSinCursor(t *testing.T) {
	store := &fakeFeed{popular: []sqlc.ListPopularFeedRow{feedRow(time.Now(), 0, 1)}}
	page, err := NewFeedService(store, nil, "").Feed(context.Background(), uuid.New(), domain.FeedTabPopular, "UTC", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != nil || len(page.Reviews) != 1 || page.Reviews[0].Target.Summary != nil {
		t.Fatalf("página inesperada: %+v", page)
	}
}

func TestFeedResumeCadaObjetivoUnaVez(t *testing.T) {
	now := time.Now()
	store := &fakeFeed{popular: []sqlc.ListPopularFeedRow{feedRow(now, 2, 7), feedRow(now, 1, 7), feedRow(now, 0, 8)}}
	summaries := &fakeSummarizer{}
	if _, err := NewFeedService(store, summaries, "").Feed(context.Background(), uuid.New(), domain.FeedTabPopular, "", "", 0); err != nil {
		t.Fatal(err)
	}
	if len(summaries.calls) != 2 {
		t.Fatalf("dos objetivos distintos, hubo %d resúmenes", len(summaries.calls))
	}
}

func TestFeedFollowing(t *testing.T) {
	r := sqlc.ListFollowingFeedRow(feedRow(time.Now(), 0, 1))
	r.FollowingAuthor = true
	store := &fakeFeed{following: []sqlc.ListFollowingFeedRow{r, sqlc.ListFollowingFeedRow(feedRow(time.Now().Add(-time.Minute), 0, 2))}}
	svc := NewFeedService(store, nil, "")

	page, err := svc.Feed(context.Background(), uuid.New(), domain.FeedTabFollowing, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Tab != domain.FeedTabFollowing || page.Timezone != "" || len(page.Reviews) != 1 || !page.Reviews[0].FollowingAuthor || page.NextCursor == nil {
		t.Fatalf("página inesperada: %+v", page)
	}
	if _, err := svc.Feed(context.Background(), uuid.New(), domain.FeedTabFollowing, "", *page.NextCursor, 1); err != nil {
		t.Fatal(err)
	}
	if store.lastFollowing.CursorID == nil || *store.lastFollowing.CursorID != r.ID {
		t.Fatalf("cursor inesperado: %+v", store.lastFollowing)
	}
}

func TestFeedValidaciones(t *testing.T) {
	store := &fakeFeed{popular: []sqlc.ListPopularFeedRow{feedRow(time.Now(), 0, 1), feedRow(time.Now(), 0, 2)}}
	svc := NewFeedService(store, nil, "")
	ctx := context.Background()
	first, err := svc.Feed(ctx, uuid.New(), domain.FeedTabPopular, "America/Mexico_City", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][4]string{ // tab, tz, cursor, descripción
		"tab vacío":              {"", "", ""},
		"tab desconocido":        {"trending", "", ""},
		"tz inválida":            {domain.FeedTabPopular, "Mars/Base", ""},
		"tz Local":               {domain.FeedTabPopular, "Local", ""},
		"cursor basura":          {domain.FeedTabPopular, "", "no-es-un-cursor"},
		"cursor de otra tz":      {domain.FeedTabPopular, "UTC", *first.NextCursor},
		"cursor de otra pestaña": {domain.FeedTabFollowing, "", *first.NextCursor},
	}
	for name, c := range cases {
		_, err := svc.Feed(ctx, uuid.New(), c[0], c[1], c[2], 0)
		var v *domain.ValidationError
		if !errors.As(err, &v) {
			t.Errorf("%s: esperaba ValidationError, obtuve %v", name, err)
		}
	}
	for _, limit := range []int{-1, MaxPageLimit + 1} {
		_, err := svc.Feed(ctx, uuid.New(), domain.FeedTabPopular, "", "", limit)
		var v *domain.ValidationError
		if !errors.As(err, &v) {
			t.Errorf("limit %d: esperaba ValidationError, obtuve %v", limit, err)
		}
	}
}
