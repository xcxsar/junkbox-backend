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

type fakeCatalog struct {
	tracks      []sqlc.SearchTracksTrigramRow
	artists     []sqlc.SearchArtistsTrigramRow
	albums      []sqlc.SearchAlbumsTrigramRow
	track       *sqlc.GetTrackRow
	album       *sqlc.GetAlbumRow
	albumTracks []sqlc.ListTracksByAlbumRow
	searchErr   error

	lastTrackArg  sqlc.GetTrackParams
	lastArtistArg sqlc.GetArtistParams
}

func (f *fakeCatalog) SearchTracksTrigram(_ context.Context, _ sqlc.SearchTracksTrigramParams) ([]sqlc.SearchTracksTrigramRow, error) {
	return f.tracks, f.searchErr
}

func (f *fakeCatalog) SearchArtistsTrigram(_ context.Context, _ sqlc.SearchArtistsTrigramParams) ([]sqlc.SearchArtistsTrigramRow, error) {
	return f.artists, f.searchErr
}

func (f *fakeCatalog) SearchAlbumsTrigram(_ context.Context, _ sqlc.SearchAlbumsTrigramParams) ([]sqlc.SearchAlbumsTrigramRow, error) {
	return f.albums, f.searchErr
}

func (f *fakeCatalog) GetTrack(_ context.Context, arg sqlc.GetTrackParams) (sqlc.GetTrackRow, error) {
	f.lastTrackArg = arg
	if f.track == nil {
		return sqlc.GetTrackRow{}, sql.ErrNoRows
	}
	return *f.track, nil
}

func (f *fakeCatalog) GetArtist(_ context.Context, arg sqlc.GetArtistParams) (sqlc.GetArtistRow, error) {
	f.lastArtistArg = arg
	return sqlc.GetArtistRow{}, sql.ErrNoRows
}

func (f *fakeCatalog) GetAlbum(_ context.Context, _ sqlc.GetAlbumParams) (sqlc.GetAlbumRow, error) {
	if f.album == nil {
		return sqlc.GetAlbumRow{}, sql.ErrNoRows
	}
	return *f.album, nil
}

func (f *fakeCatalog) ListTracksByAlbum(_ context.Context, _ *uuid.UUID) ([]sqlc.ListTracksByAlbumRow, error) {
	return f.albumTracks, nil
}

type fakeProvider struct {
	tracks []domain.ImportedTrack
	err    error
	calls  int
}

func (f *fakeProvider) SearchTracks(_ context.Context, _ string, _ int) ([]domain.ImportedTrack, error) {
	f.calls++
	return f.tracks, f.err
}

func (f *fakeProvider) SearchArtists(_ context.Context, _ string, _ int) ([]domain.ImportedArtist, error) {
	f.calls++
	return nil, f.err
}

func (f *fakeProvider) SearchAlbums(_ context.Context, _ string, _ int) ([]domain.ImportedAlbum, error) {
	f.calls++
	return nil, f.err
}

// fakeImporter "persiste" las canciones asignándoles un id y las deja
// pendientes de embedding.
type fakeImporter struct {
	err      error
	imported []domain.ImportedTrack
}

func (f *fakeImporter) ImportTracks(_ context.Context, tracks []domain.ImportedTrack) ([]domain.ImportedSong, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.imported = append(f.imported, tracks...)
	out := make([]domain.ImportedSong, len(tracks))
	for i, t := range tracks {
		out[i] = domain.ImportedSong{
			Track: domain.Track{ID: int32(1000 + i), SpotifyID: t.SpotifyID, TrackName: t.Name, ArtistName: t.Artist.Name, PendingEmbedding: true},
			ISRC:  t.ISRC,
		}
	}
	return out, nil
}

func (f *fakeImporter) ImportArtists(_ context.Context, _ []domain.ImportedArtist) ([]domain.Artist, error) {
	return nil, f.err
}

func (f *fakeImporter) ImportAlbums(_ context.Context, _ []domain.ImportedAlbum) ([]domain.Album, error) {
	return nil, f.err
}

type fakeQueue struct{ jobs []domain.EmbeddingJob }

func (f *fakeQueue) Enqueue(jobs ...domain.EmbeddingJob) { f.jobs = append(f.jobs, jobs...) }

func localTrack(name string, score float64) sqlc.SearchTracksTrigramRow {
	return sqlc.SearchTracksTrigramRow{
		ID:           1,
		TrackID:      sql.NullString{String: "1TIHAkrLbRALyt4O4Oh28i", Valid: true},
		TrackName:    sql.NullString{String: name, Valid: true},
		ArtistName:   sql.NullString{String: "Dan Telfer", Valid: true},
		HasEmbedding: true,
		Score:        score,
	}
}

func spotifyTrack() domain.ImportedTrack {
	isrc := "USUM72401994"
	return domain.ImportedTrack{
		SpotifyID: "2qSkXiYOKEzfk3vhRpIQUR",
		Name:      "Espresso",
		Artist:    domain.ImportedArtist{SpotifyID: "74KM79TiuVKeVCqs8QtB0B", Name: "Sabrina Carpenter"},
		ISRC:      &isrc,
	}
}

type musicFixture struct {
	catalog  *fakeCatalog
	provider *fakeProvider
	importer *fakeImporter
	queue    *fakeQueue
	svc      *MusicService
}

func newMusicFixture(withProvider bool) *musicFixture {
	f := &musicFixture{
		catalog:  &fakeCatalog{},
		provider: &fakeProvider{tracks: []domain.ImportedTrack{spotifyTrack()}},
		importer: &fakeImporter{},
		queue:    &fakeQueue{},
	}
	if withProvider {
		f.svc = NewMusicService(f.catalog, f.importer, f.provider, f.queue, 0.8)
	} else {
		f.svc = NewMusicService(f.catalog, f.importer, nil, f.queue, 0.8)
	}
	return f
}

// --- búsqueda ---

func TestSearchLocalHitNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Grain", 1.0)}

	res, err := f.svc.Search(context.Background(), "  Grain ", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceLocalCache || res.Count != 1 || res.Query != "Grain" || res.Type != domain.SearchTypeTrack {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if f.provider.calls != 0 {
		t.Fatalf("no debía consultar Spotify (calls=%d)", f.provider.calls)
	}
}

func TestSearchSinResultadosLocalesUsaSpotifyYEncola(t *testing.T) {
	f := newMusicFixture(true)

	res, err := f.svc.Search(context.Background(), "Espresso Sabrina Carpenter", "track", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceSpotifyFallback || res.Count != 1 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	tracks := res.Results.([]domain.Track)
	if !tracks[0].PendingEmbedding || tracks[0].HasEmbedding {
		t.Fatalf("la canción importada debe quedar pendiente: %+v", tracks[0])
	}
	if len(f.queue.jobs) != 1 || f.queue.jobs[0].ISRC != "USUM72401994" || f.queue.jobs[0].TrackID != "2qSkXiYOKEzfk3vhRpIQUR" {
		t.Fatalf("job encolado inesperado: %+v", f.queue.jobs)
	}
}

func TestSearchPuntajeBajoElUmbralUsaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Nonsense", 0.667)}

	res, err := f.svc.Search(context.Background(), "Espresso Sabrina Carpenter", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceSpotifyFallback || f.provider.calls != 1 {
		t.Fatalf("debía consultar Spotify: %+v (calls=%d)", res, f.provider.calls)
	}
}

func TestSearchSpotifyFallaConResultadosLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Nonsense", 0.667)}
	f.provider.err = errors.New("spotify caído")

	res, err := f.svc.Search(context.Background(), "Espresso Sabrina Carpenter", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceLocalCache || res.Count != 1 {
		t.Fatalf("debía degradar a resultados locales: %+v", res)
	}
}

func TestSearchSpotifyFallaSinResultadosLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.provider.err = errors.New("spotify caído")

	if _, err := f.svc.Search(context.Background(), "nada", "track", 0); !errors.Is(err, domain.ErrMusicProviderUnavailable) {
		t.Fatalf("err = %v, se esperaba ErrMusicProviderUnavailable", err)
	}
}

func TestSearchImportFallaSinResultadosLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.importer.err = errors.New("tx falló")

	if _, err := f.svc.Search(context.Background(), "nada", "track", 0); !errors.Is(err, domain.ErrMusicProviderUnavailable) {
		t.Fatalf("err = %v, se esperaba ErrMusicProviderUnavailable", err)
	}
}

func TestSearchSinProveedorEsSoloLocal(t *testing.T) {
	f := newMusicFixture(false)

	res, err := f.svc.Search(context.Background(), "nada", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceLocalCache || res.Count != 0 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
}

func TestSearchSpotifySinResultadosConservaLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Grainy", 0.2)}
	f.provider.tracks = nil

	res, err := f.svc.Search(context.Background(), "Grain", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != domain.SourceLocalCache || res.Count != 1 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
}

func TestSearchErrorDeBDSePropaga(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.searchErr = errors.New("db caída")

	_, err := f.svc.Search(context.Background(), "Grain", "track", 0)
	var v *domain.ValidationError
	if err == nil || errors.As(err, &v) || errors.Is(err, domain.ErrMusicProviderUnavailable) {
		t.Fatalf("se esperaba error interno, err = %v", err)
	}
}

func TestSearchValidaciones(t *testing.T) {
	f := newMusicFixture(true)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name, q, typ string
		limit        int
	}{
		{"q vacío", "   ", "track", 0},
		{"q largo", string(long), "track", 0},
		{"type inválido", "x", "playlist", 0},
		{"limit negativo", "x", "track", -1},
		{"limit excesivo", "x", "track", 51},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.Search(context.Background(), c.q, c.typ, c.limit)
			var v *domain.ValidationError
			if !errors.As(err, &v) {
				t.Fatalf("err = %v, se esperaba ValidationError", err)
			}
		})
	}
}

// --- detalle ---

func TestGetTrackPorIDLocalYSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.track = &sqlc.GetTrackRow{
		ID:           7,
		TrackID:      sql.NullString{String: "2qSkXiYOKEzfk3vhRpIQUR", Valid: true},
		Energy:       sql.NullFloat64{Float64: 0.7, Valid: true},
		HasEmbedding: true,
	}

	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if f.catalog.lastTrackArg != (sqlc.GetTrackParams{ID: 7}) {
		t.Fatalf("arg = %+v", f.catalog.lastTrackArg)
	}
	if got.AudioFeatures == nil || *got.AudioFeatures.Energy != 0.7 || got.AudioFeatures.Speechiness != nil {
		t.Fatalf("audio features inesperados: %+v", got.AudioFeatures)
	}

	if _, err := f.svc.GetTrack(context.Background(), "2qSkXiYOKEzfk3vhRpIQUR"); err != nil {
		t.Fatalf("GetTrack spotify: %v", err)
	}
	if f.catalog.lastTrackArg != (sqlc.GetTrackParams{SpotifyID: "2qSkXiYOKEzfk3vhRpIQUR"}) {
		t.Fatalf("arg = %+v", f.catalog.lastTrackArg)
	}
}

func TestGetTrackSinAudioFeaturesDevuelveNull(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.track = &sqlc.GetTrackRow{ID: 7, PendingEmbedding: true}

	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if got.AudioFeatures != nil {
		t.Fatalf("audio_features debía ser null: %+v", got.AudioFeatures)
	}
}

func TestGetTrackIDInvalidoYNoEncontrado(t *testing.T) {
	f := newMusicFixture(true)
	var v *domain.ValidationError
	for _, id := range []string{"abc", "0", "-3", "2qSkXiYOKEzfk3vhRpIQU"} {
		if _, err := f.svc.GetTrack(context.Background(), id); !errors.As(err, &v) {
			t.Fatalf("id %q: err = %v, se esperaba ValidationError", id, err)
		}
	}
	if _, err := f.svc.GetTrack(context.Background(), "99"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, se esperaba ErrNotFound", err)
	}
}

func TestGetArtistResuelveUUIDOSpotify(t *testing.T) {
	f := newMusicFixture(true)
	id := uuid.New()

	if _, err := f.svc.GetArtist(context.Background(), id.String()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if f.catalog.lastArtistArg != (sqlc.GetArtistParams{ID: id}) {
		t.Fatalf("arg = %+v", f.catalog.lastArtistArg)
	}
	if _, err := f.svc.GetArtist(context.Background(), "74KM79TiuVKeVCqs8QtB0B"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if f.catalog.lastArtistArg != (sqlc.GetArtistParams{SpotifyID: "74KM79TiuVKeVCqs8QtB0B"}) {
		t.Fatalf("arg = %+v", f.catalog.lastArtistArg)
	}
	var v *domain.ValidationError
	if _, err := f.svc.GetArtist(context.Background(), "no-es-un-id"); !errors.As(err, &v) {
		t.Fatalf("err = %v, se esperaba ValidationError", err)
	}
}

func TestGetAlbumIncluyePistas(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.album = &sqlc.GetAlbumRow{ID: uuid.New(), Title: "Short n' Sweet", ArtistName: "Sabrina Carpenter", AlbumType: "album"}
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{
		{ID: 1, TrackName: sql.NullString{String: "Taste", Valid: true}},
		{ID: 2, TrackName: sql.NullString{String: "Espresso", Valid: true}},
	}

	got, err := f.svc.GetAlbum(context.Background(), f.catalog.album.ID.String())
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if len(got.Tracks) != 2 || got.Tracks[1].TrackName != "Espresso" || *got.Tracks[1].AlbumTitle != "Short n' Sweet" {
		t.Fatalf("pistas inesperadas: %+v", got.Tracks)
	}
}
