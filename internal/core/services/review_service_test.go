package services

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

// fakeResolver resuelve cualquier id al objetivo dado, o devuelve err.
type fakeResolver struct {
	target domain.Target
	err    error
}

func (f *fakeResolver) ResolveTarget(_ context.Context, targetType, _ string) (domain.Target, error) {
	if f.err != nil {
		return domain.Target{}, f.err
	}
	t := f.target
	t.Type = targetType
	return t, nil
}

// fakeRatings guarda los votos en memoria con la misma unicidad que la BD
// (usuario + objetivo).
type fakeRatings struct {
	rows map[uuid.UUID]sqlc.Rating
}

func newFakeRatings() *fakeRatings { return &fakeRatings{rows: map[uuid.UUID]sqlc.Rating{}} }

func (f *fakeRatings) upsert(r sqlc.Rating) sqlc.UpsertSongRatingRow {
	for id, old := range f.rows {
		if old.UserID == r.UserID && old.SongID == r.SongID && eqUUID(old.AlbumID, r.AlbumID) && eqUUID(old.ArtistID, r.ArtistID) {
			old.Score = r.Score
			f.rows[id] = old
			return toUpsertRow(old, false)
		}
	}
	r.ID = uuid.New()
	f.rows[r.ID] = r
	return toUpsertRow(r, true)
}

func (f *fakeRatings) UpsertSongRating(_ context.Context, arg sqlc.UpsertSongRatingParams) (sqlc.UpsertSongRatingRow, error) {
	return f.upsert(sqlc.Rating{UserID: arg.UserID, SongID: sql.NullInt32{Int32: arg.SongID, Valid: true}, Score: arg.Score}), nil
}

func (f *fakeRatings) UpsertAlbumRating(_ context.Context, arg sqlc.UpsertAlbumRatingParams) (sqlc.UpsertAlbumRatingRow, error) {
	return sqlc.UpsertAlbumRatingRow(f.upsert(sqlc.Rating{UserID: arg.UserID, AlbumID: &arg.AlbumID, Score: arg.Score})), nil
}

func (f *fakeRatings) UpsertArtistRating(_ context.Context, arg sqlc.UpsertArtistRatingParams) (sqlc.UpsertArtistRatingRow, error) {
	return sqlc.UpsertArtistRatingRow(f.upsert(sqlc.Rating{UserID: arg.UserID, ArtistID: &arg.ArtistID, Score: arg.Score})), nil
}

func (f *fakeRatings) GetUserRating(_ context.Context, arg sqlc.GetUserRatingParams) (sqlc.Rating, error) {
	for _, r := range f.rows {
		if r.UserID == arg.UserID && r.SongID == arg.SongID && eqUUID(r.AlbumID, arg.AlbumID) && eqUUID(r.ArtistID, arg.ArtistID) {
			return r, nil
		}
	}
	return sqlc.Rating{}, sql.ErrNoRows
}

func (f *fakeRatings) GetRatingByID(_ context.Context, id uuid.UUID) (sqlc.Rating, error) {
	r, ok := f.rows[id]
	if !ok {
		return sqlc.Rating{}, sql.ErrNoRows
	}
	return r, nil
}

func (f *fakeRatings) DeleteRating(_ context.Context, id uuid.UUID) (int64, error) {
	if _, ok := f.rows[id]; !ok {
		return 0, nil
	}
	delete(f.rows, id)
	return 1, nil
}

func toUpsertRow(r sqlc.Rating, inserted bool) sqlc.UpsertSongRatingRow {
	return sqlc.UpsertSongRatingRow{
		ID: r.ID, UserID: r.UserID, SongID: r.SongID, AlbumID: r.AlbumID, ArtistID: r.ArtistID,
		Score: r.Score, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Inserted: inserted,
	}
}

func eqUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// fakeReviews guarda las reseñas en memoria. createErr fuerza un error en
// CreateReview (p. ej. 23505).
type fakeReviews struct {
	rows      map[uuid.UUID]sqlc.GetReviewByIDRow
	createErr error
	lastList  sqlc.ListReviewsByTargetParams
	total     int64
}

func newFakeReviews() *fakeReviews {
	return &fakeReviews{rows: map[uuid.UUID]sqlc.GetReviewByIDRow{}}
}

func (f *fakeReviews) CreateReview(_ context.Context, arg sqlc.CreateReviewParams) (uuid.UUID, error) {
	if f.createErr != nil {
		return uuid.Nil, f.createErr
	}
	id := uuid.New()
	f.rows[id] = sqlc.GetReviewByIDRow{
		ID: id, UserID: arg.UserID, SongID: arg.SongID, AlbumID: arg.AlbumID, ArtistID: arg.ArtistID,
		Title: arg.Title, Content: arg.Content, AuthorUsername: "autor",
	}
	return id, nil
}

func (f *fakeReviews) GetReviewByID(_ context.Context, id uuid.UUID) (sqlc.GetReviewByIDRow, error) {
	r, ok := f.rows[id]
	if !ok {
		return sqlc.GetReviewByIDRow{}, sql.ErrNoRows
	}
	return r, nil
}

func (f *fakeReviews) ListReviewsByTarget(_ context.Context, arg sqlc.ListReviewsByTargetParams) ([]sqlc.ListReviewsByTargetRow, error) {
	f.lastList = arg
	var out []sqlc.ListReviewsByTargetRow
	for _, r := range f.rows {
		out = append(out, sqlc.ListReviewsByTargetRow(r))
	}
	return out, nil
}

func (f *fakeReviews) CountReviewsByTarget(_ context.Context, _ sqlc.CountReviewsByTargetParams) (int64, error) {
	return f.total, nil
}

func (f *fakeReviews) UpdateReview(_ context.Context, arg sqlc.UpdateReviewParams) (int64, error) {
	r, ok := f.rows[arg.ID]
	if !ok {
		return 0, nil
	}
	r.Title, r.Content = arg.Title, arg.Content
	f.rows[arg.ID] = r
	return 1, nil
}

func (f *fakeReviews) DeleteReview(_ context.Context, id uuid.UUID) (int64, error) {
	if _, ok := f.rows[id]; !ok {
		return 0, nil
	}
	delete(f.rows, id)
	return 1, nil
}

// --- RatingService ---

func TestRateValidaScore(t *testing.T) {
	svc := NewRatingService(newFakeRatings(), &fakeResolver{target: domain.Target{SongID: 10924}})
	cases := map[string]*float64{
		"ausente":       nil,
		"cero":          ptr(0.0),
		"negativo":      ptr(-1.0),
		"paso inválido": ptr(4.3),
		"cuarto":        ptr(4.25),
		"mayor que 5":   ptr(5.5),
	}
	for name, score := range cases {
		_, _, err := svc.Rate(context.Background(), uuid.New(), domain.TargetSong, "10924", score)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("%s: esperaba ValidationError, obtuve %v", name, err)
		}
	}
	for _, ok := range []float64{0.5, 1, 4.5, 5} {
		if _, _, err := svc.Rate(context.Background(), uuid.New(), domain.TargetSong, "10924", ptr(ok)); err != nil {
			t.Errorf("score %v: error inesperado %v", ok, err)
		}
	}
}

func TestRateUpsertDevuelveCreated(t *testing.T) {
	store := newFakeRatings()
	albumID := uuid.New()
	svc := NewRatingService(store, &fakeResolver{target: domain.Target{AlbumID: albumID}})
	user := uuid.New()

	first, created, err := svc.Rate(context.Background(), user, domain.TargetAlbum, albumID.String(), ptr(4.5))
	if err != nil || !created {
		t.Fatalf("primer voto: created=%v err=%v", created, err)
	}
	second, created, err := svc.Rate(context.Background(), user, domain.TargetAlbum, albumID.String(), ptr(5.0))
	if err != nil || created {
		t.Fatalf("segundo voto: created=%v err=%v", created, err)
	}
	if first.ID != second.ID || second.Score != 5 {
		t.Errorf("el segundo voto debe reemplazar al primero: %+v vs %+v", first, second)
	}
	if second.Target != (domain.TargetRef{Type: domain.TargetAlbum, ID: albumID.String()}) {
		t.Errorf("target = %+v", second.Target)
	}
	if len(store.rows) != 1 {
		t.Errorf("filas = %d, esperaba 1", len(store.rows))
	}
}

func TestRatePropagaErrorDelResolver(t *testing.T) {
	svc := NewRatingService(newFakeRatings(), &fakeResolver{err: domain.ErrNotFound})
	if _, _, err := svc.Rate(context.Background(), uuid.New(), domain.TargetSong, "1", ptr(3.0)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("esperaba ErrNotFound, obtuve %v", err)
	}
}

func TestGetMineSinVoto(t *testing.T) {
	svc := NewRatingService(newFakeRatings(), &fakeResolver{target: domain.Target{SongID: 7}})
	if _, err := svc.GetMine(context.Background(), uuid.New(), domain.TargetSong, "7"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("esperaba ErrNotFound, obtuve %v", err)
	}
}

func TestDeleteRatingAutorizacion(t *testing.T) {
	store := newFakeRatings()
	svc := NewRatingService(store, &fakeResolver{target: domain.Target{SongID: 7}})
	owner := uuid.New()
	rating, _, err := svc.Rate(context.Background(), owner, domain.TargetSong, "7", ptr(3.5))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), uuid.New(), rating.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("otro usuario: esperaba ErrForbidden, obtuve %v", err)
	}
	if err := svc.Delete(context.Background(), owner, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("id inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
	if err := svc.Delete(context.Background(), owner, rating.ID); err != nil {
		t.Errorf("autor: error inesperado %v", err)
	}
	if len(store.rows) != 0 {
		t.Error("el voto debía borrarse")
	}
}

// --- ReviewService ---

func TestCreateReviewNormaliza(t *testing.T) {
	store := newFakeReviews()
	svc := NewReviewService(store, &fakeResolver{target: domain.Target{SongID: 10924}})

	review, err := svc.Create(context.Background(), uuid.New(), domain.TargetSong, "10924", ReviewInput{
		Title:   ptr("   "),
		Content: "  Gran producción y letra.  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Title != nil {
		t.Errorf("un título vacío debe guardarse como NULL, obtuve %q", *review.Title)
	}
	if review.Content != "Gran producción y letra." {
		t.Errorf("content = %q", review.Content)
	}
	if review.Target != (domain.TargetRef{Type: domain.TargetSong, ID: "10924"}) {
		t.Errorf("target = %+v", review.Target)
	}
}

func TestCreateReviewValidaLongitudes(t *testing.T) {
	svc := NewReviewService(newFakeReviews(), &fakeResolver{target: domain.Target{SongID: 1}})
	cases := map[string]ReviewInput{
		"content corto":           {Content: " abcd "},
		"content largo":           {Content: strings.Repeat("a", MaxReviewContentLen+1)},
		"title largo":             {Title: ptr(strings.Repeat("ñ", MaxReviewTitleLen+1)), Content: "válido"},
		"content solo de espacio": {Content: "          "},
	}
	for name, in := range cases {
		_, err := svc.Create(context.Background(), uuid.New(), domain.TargetSong, "1", in)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("%s: esperaba ValidationError, obtuve %v", name, err)
		}
	}
	// Los límites se cuentan en caracteres, no en bytes.
	if _, err := svc.Create(context.Background(), uuid.New(), domain.TargetSong, "1", ReviewInput{
		Title: ptr(strings.Repeat("ñ", MaxReviewTitleLen)), Content: "ñññññ",
	}); err != nil {
		t.Errorf("límites exactos en runas: error inesperado %v", err)
	}
}

func TestCreateReviewDuplicada(t *testing.T) {
	store := newFakeReviews()
	store.createErr = uniqueViolation{}
	svc := NewReviewService(store, &fakeResolver{target: domain.Target{SongID: 1}})
	_, err := svc.Create(context.Background(), uuid.New(), domain.TargetSong, "1", ReviewInput{Content: "otra reseña"})
	if !errors.Is(err, domain.ErrReviewAlreadyExists) {
		t.Fatalf("esperaba ErrReviewAlreadyExists, obtuve %v", err)
	}
}

func TestUpdateDeleteReviewAutorizacion(t *testing.T) {
	store := newFakeReviews()
	svc := NewReviewService(store, &fakeResolver{target: domain.Target{SongID: 1}})
	owner, other := uuid.New(), uuid.New()
	review, err := svc.Create(context.Background(), owner, domain.TargetSong, "1", ReviewInput{Title: ptr("Título"), Content: "Primera versión"})
	if err != nil {
		t.Fatal(err)
	}

	edit := ReviewInput{Content: "Segunda versión"}
	if _, err := svc.Update(context.Background(), other, review.ID, edit); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("editar ajena: esperaba ErrForbidden, obtuve %v", err)
	}
	if _, err := svc.Update(context.Background(), owner, uuid.New(), edit); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editar inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
	updated, err := svc.Update(context.Background(), owner, review.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "Segunda versión" || updated.Title != nil {
		t.Errorf("PUT reemplaza título y contenido: %+v", updated)
	}

	if err := svc.Delete(context.Background(), other, review.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("borrar ajena: esperaba ErrForbidden, obtuve %v", err)
	}
	if err := svc.Delete(context.Background(), owner, review.ID); err != nil {
		t.Errorf("borrar propia: %v", err)
	}
	if _, err := svc.Get(context.Background(), review.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("tras borrar: esperaba ErrNotFound, obtuve %v", err)
	}
}

func TestListReviewsPaginacion(t *testing.T) {
	store := newFakeReviews()
	store.total = 45
	albumID := uuid.New()
	svc := NewReviewService(store, &fakeResolver{target: domain.Target{AlbumID: albumID}})

	page, err := svc.List(context.Background(), domain.TargetAlbum, albumID.String(), 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if store.lastList.MaxResults != DefaultReviewLimit || store.lastList.Skip != 2*DefaultReviewLimit {
		t.Errorf("limit/offset = %d/%d", store.lastList.MaxResults, store.lastList.Skip)
	}
	if store.lastList.AlbumID == nil || *store.lastList.AlbumID != albumID || store.lastList.SongID.Valid || store.lastList.ArtistID != nil {
		t.Errorf("filtro por objetivo incorrecto: %+v", store.lastList)
	}
	if page.Total != 45 || page.Page != 3 || page.Limit != DefaultReviewLimit || page.Reviews == nil {
		t.Errorf("página = %+v", page)
	}

	for _, bad := range [][2]int{{51, 1}, {-1, 1}, {10, -1}, {50, 1 << 30}} {
		_, err := svc.List(context.Background(), domain.TargetAlbum, albumID.String(), bad[0], bad[1])
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("limit=%d page=%d: esperaba ValidationError, obtuve %v", bad[0], bad[1], err)
		}
	}
}

// --- MusicService.ResolveTarget ---

func TestResolveTarget(t *testing.T) {
	catalog := &fakeCatalog{track: &sqlc.GetTrackRow{ID: 10924}}
	svc := NewMusicService(catalog, nil, nil, nil, 0)

	target, err := svc.ResolveTarget(context.Background(), domain.TargetSong, "4uLU6hMCjMI75M1A2tKUQC")
	if err != nil {
		t.Fatal(err)
	}
	if target.SongID != 10924 || catalog.lastTrackArg.SpotifyID != "4uLU6hMCjMI75M1A2tKUQC" {
		t.Errorf("un ID de Spotify debe resolverse al id local: %+v / %+v", target, catalog.lastTrackArg)
	}

	if _, err := svc.ResolveTarget(context.Background(), domain.TargetArtist, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("artista inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
	var validation *domain.ValidationError
	if _, err := svc.ResolveTarget(context.Background(), "playlist", "1"); !errors.As(err, &validation) {
		t.Errorf("tipo inválido: esperaba ValidationError, obtuve %v", err)
	}
	if _, err := svc.ResolveTarget(context.Background(), domain.TargetAlbum, "no-es-id"); !errors.As(err, &validation) {
		t.Errorf("id inválido: esperaba ValidationError, obtuve %v", err)
	}
}
