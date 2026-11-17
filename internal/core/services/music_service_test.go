package services

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

type fakeCatalog struct {
	tracks  []sqlc.SearchTracksTrigramRow
	artists []sqlc.SearchArtistsTrigramRow
	albums  []sqlc.SearchAlbumsTrigramRow
	track   *sqlc.GetTrackRow
	// relinked es lo que devuelve GetTrack desde la segunda lectura (la
	// fila ya enlazada con su álbum). nil: siempre devuelve track.
	relinked    *sqlc.GetTrackRow
	trackReads  int
	artist      *sqlc.GetArtistRow
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
	f.trackReads++
	if f.relinked != nil && f.trackReads > 1 {
		return *f.relinked, nil
	}
	if f.track == nil {
		return sqlc.GetTrackRow{}, sql.ErrNoRows
	}
	return *f.track, nil
}

func (f *fakeCatalog) GetArtist(_ context.Context, arg sqlc.GetArtistParams) (sqlc.GetArtistRow, error) {
	f.lastArtistArg = arg
	if f.artist == nil {
		return sqlc.GetArtistRow{}, sql.ErrNoRows
	}
	return *f.artist, nil
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
// pendientes de embedding. Las imágenes son las que llegan de Spotify.
type fakeImporter struct {
	mu       sync.Mutex
	err      error
	imported []domain.ImportedTrack
}

func (f *fakeImporter) ImportTracks(_ context.Context, tracks []domain.ImportedTrack) ([]domain.ImportedSong, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	f.imported = append(f.imported, tracks...)
	f.mu.Unlock()
	out := make([]domain.ImportedSong, len(tracks))
	for i, t := range tracks {
		title := t.Album.Title
		out[i] = domain.ImportedSong{
			Track: domain.Track{
				ID: int32(1000 + i), SpotifyID: t.SpotifyID, TrackName: t.Name, ArtistName: t.Artist.Name, PendingEmbedding: true,
				AlbumTitle: &title, AlbumImageURL: t.Album.ImageURL, ArtistImageURL: t.Artist.ImageURL,
			},
			ISRC:            t.ISRC,
			ArtistSpotifyID: t.Artist.SpotifyID,
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

type fakeQueue struct {
	mu   sync.Mutex
	jobs []domain.EmbeddingJob
}

func (f *fakeQueue) Enqueue(jobs ...domain.EmbeddingJob) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, jobs...)
}

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
	metadata *fakeMetadata
	store    *fakeEnrichStore
	fallback *fakeEnrichQueue
	svc      *MusicService
}

// newMusicFixture arma el servicio. Con withProvider, Spotify respalda la
// búsqueda y completa imágenes con un presupuesto de 500 ms.
func newMusicFixture(withProvider bool) *musicFixture {
	f := &musicFixture{
		catalog:  &fakeCatalog{},
		provider: &fakeProvider{tracks: []domain.ImportedTrack{spotifyTrack()}},
		importer: &fakeImporter{},
		queue:    &fakeQueue{},
		metadata: newFakeMetadata(),
		store:    newFakeEnrichStore(),
		fallback: &fakeEnrichQueue{},
	}
	f.build(withProvider, 500*time.Millisecond, 0)
	return f
}

func (f *musicFixture) build(withProvider bool, budget time.Duration, concurrency int) {
	if !withProvider {
		f.svc = NewMusicService(f.catalog, f.importer, nil, f.queue, 0.8, SearchEnrichment{})
		return
	}
	f.svc = NewMusicService(f.catalog, f.importer, f.provider, f.queue, 0.8, SearchEnrichment{
		Enricher:    NewCatalogEnricher(f.metadata, f.store, f.importer, f.queue),
		Fallback:    f.fallback,
		Budget:      budget,
		Concurrency: concurrency,
	})
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

// --- imágenes en la búsqueda (enriquecimiento síncrono) ---

// linkedTrack es una canción con álbum cuyo artista tiene spotify_id pero
// aún no tiene imagen.
func linkedTrack(id int32, trackID, artistID string) sqlc.SearchTracksTrigramRow {
	albumID := uuid.New()
	r := localTrack("Espresso", 1.0)
	r.ID = id
	r.TrackID = sql.NullString{String: trackID, Valid: true}
	r.AlbumID = &albumID
	r.AlbumTitle = sql.NullString{String: "Short n' Sweet", Valid: true}
	r.AlbumImageUrl = sql.NullString{String: "cover.jpg", Valid: true}
	r.ArtistSpotifyID = sql.NullString{String: artistID, Valid: true}
	return r
}

func TestSearchCompletaImagenDelArtistaEnLaMismaRespuesta(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{linkedTrack(1, trackA, artistA)}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}

	res, err := f.svc.Search(context.Background(), "Espresso", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	tr := res.Results.([]domain.Track)[0]
	if tr.AlbumImageURL == nil || *tr.AlbumImageURL != "cover.jpg" || tr.ArtistImageURL == nil || *tr.ArtistImageURL != "artist.jpg" {
		t.Fatalf("imágenes inesperadas: album=%v artist=%v", tr.AlbumImageURL, tr.ArtistImageURL)
	}
	if f.store.enriched[artistA].String != "artist.jpg" {
		t.Fatalf("debía guardar la imagen: %+v", f.store.enriched)
	}
	if len(f.fallback.jobs) != 0 || f.provider.calls != 0 {
		t.Fatalf("nada debía quedar pendiente ni consultar la búsqueda de Spotify: jobs=%v calls=%d", f.fallback.jobs, f.provider.calls)
	}
}

func TestSearchEnlazaCancionDelCSVEnLaMismaRespuesta(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Grain", 1.0)} // sin álbum ni artista de Spotify
	f.metadata.tracks[trackA] = domain.ImportedTrack{
		SpotifyID: trackA, Name: "Grain",
		Artist: domain.ImportedArtist{SpotifyID: artistB, Name: "Dan Telfer"},
		Album:  domain.ImportedAlbum{Title: "Grain", ImageURL: strp("cover.jpg")},
	}
	f.metadata.artists[artistB] = domain.ImportedArtist{SpotifyID: artistB, ImageURL: strp("artist.jpg")}

	res, err := f.svc.Search(context.Background(), "Grain", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	tr := res.Results.([]domain.Track)[0]
	if tr.AlbumImageURL == nil || *tr.AlbumImageURL != "cover.jpg" || tr.AlbumTitle == nil || *tr.AlbumTitle != "Grain" {
		t.Fatalf("debía traer la portada del álbum enlazado: %+v", tr)
	}
	if tr.ArtistImageURL == nil || *tr.ArtistImageURL != "artist.jpg" {
		t.Fatalf("debía encadenar la imagen del artista: %v", tr.ArtistImageURL)
	}
	if a, tc := f.metadata.calls(); a != 1 || tc != 1 {
		t.Fatalf("llamadas = artistas %d, canciones %d", a, tc)
	}
}

func TestSearchDeduplicaArtistas(t *testing.T) {
	f := newMusicFixture(true)
	ids := []string{"0000000000000000000001", "0000000000000000000002", "0000000000000000000003", "0000000000000000000004", "0000000000000000000005"}
	for i, id := range ids {
		f.catalog.tracks = append(f.catalog.tracks, linkedTrack(int32(i+1), id, artistA))
	}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}

	res, err := f.svc.Search(context.Background(), "Espresso", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if f.metadata.artistCalls[artistA] != 1 {
		t.Fatalf("calls = %d, se esperaba 1", f.metadata.artistCalls[artistA])
	}
	for _, tr := range res.Results.([]domain.Track) {
		if tr.ArtistImageURL == nil || *tr.ArtistImageURL != "artist.jpg" {
			t.Fatalf("todas las canciones debían recibir la imagen: %+v", tr)
		}
	}
}

func TestSearchRespetaElPresupuestoYDelegaAlWorker(t *testing.T) {
	f := newMusicFixture(true)
	f.build(true, 50*time.Millisecond, 0)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{linkedTrack(1, trackA, artistA)}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}
	f.metadata.delay = time.Second

	start := time.Now()
	res, err := f.svc.Search(context.Background(), "Espresso", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("la búsqueda tardó %s; el presupuesto es 50ms", elapsed)
	}
	if tr := res.Results.([]domain.Track)[0]; tr.ArtistImageURL != nil {
		t.Fatalf("no debía alcanzar a traer la imagen: %v", *tr.ArtistImageURL)
	}
	want := domain.EnrichmentJob{Kind: domain.EnrichArtist, SpotifyID: artistA}
	if len(f.fallback.jobs) != 1 || f.fallback.jobs[0] != want {
		t.Fatalf("debía delegar al worker: %+v", f.fallback.jobs)
	}
	if len(f.store.artistFails) != 0 {
		t.Fatalf("el corte por presupuesto no debe consumir intentos: %v", f.store.artistFails)
	}
}

func TestSearchSeDetieneAnteRateLimit(t *testing.T) {
	f := newMusicFixture(true)
	f.build(true, time.Second, 1)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{
		linkedTrack(1, "0000000000000000000001", artistA),
		linkedTrack(2, "0000000000000000000002", artistB),
		linkedTrack(3, "0000000000000000000003", artistC),
	}
	f.metadata.err = rateLimitErr{wait: 30 * time.Second}

	if _, err := f.svc.Search(context.Background(), "Espresso", "track", 0); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if a, _ := f.metadata.calls(); a != 1 {
		t.Fatalf("debía detenerse tras el primer 429 (calls=%d)", a)
	}
	if len(f.fallback.jobs) != 3 {
		t.Fatalf("los tres artistas debían quedar para el worker: %+v", f.fallback.jobs)
	}
}

func TestSearchNoConsultaArtistasYaEnriquecidos(t *testing.T) {
	f := newMusicFixture(true)
	enriched := linkedTrack(1, trackA, artistA)
	enriched.ArtistEnriched = true // Spotify no tiene imagen para él
	exhausted := linkedTrack(2, "0000000000000000000002", artistB)
	exhausted.ArtistEnrichAttempts = MaxEnrichAttempts
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{enriched, exhausted}

	if _, err := f.svc.Search(context.Background(), "Espresso", "track", 0); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if a, tc := f.metadata.calls(); a != 0 || tc != 0 {
		t.Fatalf("no debía llamar a Spotify (artistas %d, canciones %d)", a, tc)
	}
}

func TestSearchCompletaImagenDeArtistasLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artists = []sqlc.SearchArtistsTrigramRow{{
		ID: uuid.New(), Name: "Sabrina Carpenter", Score: 1.0,
		SpotifyID: sql.NullString{String: artistA, Valid: true},
	}}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}

	res, err := f.svc.Search(context.Background(), "Sabrina Carpenter", "artist", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if a := res.Results.([]domain.Artist)[0]; a.ImageURL == nil || *a.ImageURL != "artist.jpg" {
		t.Fatalf("debía traer la imagen: %+v", a)
	}
}

func TestSearchCancionInexistenteEnSpotifyNoVaAlWorker(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.tracks = []sqlc.SearchTracksTrigramRow{localTrack("Grain", 1.0)}

	res, err := f.svc.Search(context.Background(), "Grain", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if tr := res.Results.([]domain.Track)[0]; tr.AlbumImageURL != nil {
		t.Fatalf("no hay portada: %v", *tr.AlbumImageURL)
	}
	if len(f.store.songFails) != 1 || !f.store.songFails[0].GiveUp {
		t.Fatalf("debía rendirse con la canción: %+v", f.store.songFails)
	}
	if len(f.fallback.jobs) != 0 {
		t.Fatalf("no debía delegar al worker: %+v", f.fallback.jobs)
	}
}

func TestSearchSpotifyFallbackTraeImagenes(t *testing.T) {
	f := newMusicFixture(true)
	track := spotifyTrack()
	track.Album = domain.ImportedAlbum{Title: "Short n' Sweet", ImageURL: strp("cover.jpg")}
	f.provider.tracks = []domain.ImportedTrack{track}
	f.metadata.artists[track.Artist.SpotifyID] = domain.ImportedArtist{SpotifyID: track.Artist.SpotifyID, ImageURL: strp("artist.jpg")}

	res, err := f.svc.Search(context.Background(), "Espresso Sabrina Carpenter", "track", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	tr := res.Results.([]domain.Track)[0]
	if res.Source != domain.SourceSpotifyFallback || tr.AlbumImageURL == nil || *tr.AlbumImageURL != "cover.jpg" || tr.ArtistImageURL == nil || *tr.ArtistImageURL != "artist.jpg" {
		t.Fatalf("resultado inesperado: source=%s track=%+v", res.Source, tr)
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

// --- detalle con carátula (HOME-02) ---

// csvTrackRow es una canción del CSV sin álbum de Spotify vinculado.
func csvTrackRow() *sqlc.GetTrackRow {
	return &sqlc.GetTrackRow{
		ID:        7,
		TrackID:   sql.NullString{String: trackA, Valid: true},
		TrackName: sql.NullString{String: "Grain", Valid: true},
	}
}

func TestGetTrackEnlazaCancionDelCSVConSuAlbum(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.track = csvTrackRow()
	albumID, artistID := uuid.New(), uuid.New()
	linked := *csvTrackRow()
	linked.AlbumID = &albumID
	linked.ArtistID = &artistID
	linked.AlbumTitle = sql.NullString{String: "Grain", Valid: true}
	linked.AlbumImageUrl = sql.NullString{String: "cover.jpg", Valid: true}
	linked.ArtistSpotifyID = sql.NullString{String: artistB, Valid: true}
	f.catalog.relinked = &linked
	f.metadata.tracks[trackA] = domain.ImportedTrack{
		SpotifyID: trackA, Name: "Grain",
		Artist: domain.ImportedArtist{SpotifyID: artistB, Name: "Dan Telfer"},
		Album:  domain.ImportedAlbum{Title: "Grain", ImageURL: strp("cover.jpg")},
	}
	f.metadata.artists[artistB] = domain.ImportedArtist{SpotifyID: artistB, ImageURL: strp("artist.jpg")}

	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if got.AlbumImageURL == nil || *got.AlbumImageURL != "cover.jpg" || got.AlbumTitle == nil || *got.AlbumTitle != "Grain" {
		t.Fatalf("debía traer la portada del álbum enlazado: %+v", got.Track)
	}
	if got.AlbumID == nil || *got.AlbumID != albumID || got.ArtistID == nil || *got.ArtistID != artistID {
		t.Fatalf("debía devolver album_id y artist_id de la fila releída: album=%v artist=%v", got.AlbumID, got.ArtistID)
	}
	if got.ArtistImageURL == nil || *got.ArtistImageURL != "artist.jpg" {
		t.Fatalf("debía encadenar la imagen del artista: %v", got.ArtistImageURL)
	}
	if a, tc := f.metadata.calls(); a != 1 || tc != 1 {
		t.Fatalf("llamadas = artistas %d, canciones %d", a, tc)
	}
}

func TestGetTrackYaEnlazadaNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	albumID := uuid.New()
	row := csvTrackRow()
	row.AlbumID = &albumID
	row.AlbumImageUrl = sql.NullString{String: "cover.jpg", Valid: true}
	row.ArtistImageUrl = sql.NullString{String: "artist.jpg", Valid: true}
	row.ArtistSpotifyID = sql.NullString{String: artistA, Valid: true}
	f.catalog.track = row

	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if a, tc := f.metadata.calls(); a != 0 || tc != 0 || f.catalog.trackReads != 1 {
		t.Fatalf("no debía consultar Spotify ni releer: artistas %d, canciones %d, lecturas %d", a, tc, f.catalog.trackReads)
	}
	if *got.AlbumImageURL != "cover.jpg" || *got.ArtistImageURL != "artist.jpg" {
		t.Fatalf("imágenes inesperadas: %+v", got.Track)
	}
}

func TestGetTrackSinSpotifyRespondeLoDelCatalogo(t *testing.T) {
	f := newMusicFixture(false)
	f.catalog.track = csvTrackRow()

	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if got.AlbumImageURL != nil || got.AlbumID != nil {
		t.Fatalf("sin Spotify no hay portada: %+v", got)
	}
}

func TestGetTrackRespetaElPresupuestoYDelegaAlWorker(t *testing.T) {
	f := newMusicFixture(true)
	f.build(true, 50*time.Millisecond, 0)
	f.catalog.track = csvTrackRow()
	f.metadata.delay = time.Second

	start := time.Now()
	got, err := f.svc.GetTrack(context.Background(), "7")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("el detalle tardó %s; el presupuesto es 50ms", elapsed)
	}
	if got.AlbumImageURL != nil {
		t.Fatalf("no debía alcanzar a traer la portada: %v", *got.AlbumImageURL)
	}
	want := domain.EnrichmentJob{Kind: domain.EnrichTrack, SpotifyID: trackA}
	if len(f.fallback.jobs) != 1 || f.fallback.jobs[0] != want {
		t.Fatalf("debía delegar al worker: %+v", f.fallback.jobs)
	}
}

func TestResolveTargetNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.track = csvTrackRow()

	if _, err := f.svc.ResolveTarget(context.Background(), domain.TargetSong, "7"); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if a, tc := f.metadata.calls(); a != 0 || tc != 0 {
		t.Fatalf("calificar no debe llamar a Spotify: artistas %d, canciones %d", a, tc)
	}
}

// --- resumen del objetivo de una reseña ---

func TestSummarizeTargetCancionCompletaCaratula(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.track = csvTrackRow()
	f.metadata.tracks[trackA] = domain.ImportedTrack{
		SpotifyID: trackA, Name: "Grain",
		Artist: domain.ImportedArtist{SpotifyID: artistB, Name: "Dan Telfer"},
		Album:  domain.ImportedAlbum{Title: "Grain", ImageURL: strp("cover.jpg")},
	}

	got, err := f.svc.SummarizeTarget(context.Background(), domain.Target{Type: domain.TargetSong, SongID: 7})
	if err != nil {
		t.Fatalf("SummarizeTarget: %v", err)
	}
	track, ok := got.(*domain.Track)
	if !ok {
		t.Fatalf("esperaba *domain.Track, obtuve %T", got)
	}
	if track.ID != 7 || track.AlbumImageURL == nil || *track.AlbumImageURL != "cover.jpg" {
		t.Fatalf("resumen inesperado: %+v", track)
	}
	if f.catalog.lastTrackArg != (sqlc.GetTrackParams{ID: 7}) {
		t.Fatalf("debía buscar por id local: %+v", f.catalog.lastTrackArg)
	}
}

func TestSummarizeTargetAlbumSinPistasNiEstadisticas(t *testing.T) {
	f := newMusicFixture(true)
	albumID := uuid.New()
	f.catalog.album = &sqlc.GetAlbumRow{ID: albumID, Title: "Short n' Sweet", ArtistName: "Sabrina Carpenter",
		ImageUrl: sql.NullString{String: "cover.jpg", Valid: true}, RatingsCount: 3}
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{{ID: 1}}

	got, err := f.svc.SummarizeTarget(context.Background(), domain.Target{Type: domain.TargetAlbum, AlbumID: albumID})
	if err != nil {
		t.Fatalf("SummarizeTarget: %v", err)
	}
	album, ok := got.(*domain.Album)
	if !ok {
		t.Fatalf("esperaba *domain.Album, obtuve %T", got)
	}
	if album.ID != albumID || *album.ImageURL != "cover.jpg" || album.Tracks != nil || album.Ratings != nil {
		t.Fatalf("resumen inesperado: %+v", album)
	}
}

func TestSummarizeTargetArtistaCompletaImagen(t *testing.T) {
	f := newMusicFixture(true)
	artistID := uuid.New()
	f.catalog.artist = &sqlc.GetArtistRow{ID: artistID, Name: "Coldplay", SpotifyID: sql.NullString{String: artistA, Valid: true}}
	f.metadata.artists[artistA] = domain.ImportedArtist{SpotifyID: artistA, ImageURL: strp("artist.jpg")}

	got, err := f.svc.SummarizeTarget(context.Background(), domain.Target{Type: domain.TargetArtist, ArtistID: artistID})
	if err != nil {
		t.Fatalf("SummarizeTarget: %v", err)
	}
	artist, ok := got.(*domain.Artist)
	if !ok {
		t.Fatalf("esperaba *domain.Artist, obtuve %T", got)
	}
	if artist.ImageURL == nil || *artist.ImageURL != "artist.jpg" || artist.Ratings != nil {
		t.Fatalf("resumen inesperado: %+v", artist)
	}
	if f.catalog.lastArtistArg != (sqlc.GetArtistParams{ID: artistID}) {
		t.Fatalf("arg = %+v", f.catalog.lastArtistArg)
	}
}

func TestGetArtistYaEnriquecidoNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = &sqlc.GetArtistRow{ID: uuid.New(), Name: "Sin foto", SpotifyID: sql.NullString{String: artistA, Valid: true}, Enriched: true}

	got, err := f.svc.GetArtist(context.Background(), artistA)
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if a, _ := f.metadata.calls(); a != 0 || got.ImageURL != nil || got.Ratings == nil {
		t.Fatalf("no debía consultar Spotify (llamadas %d) y debía conservar ratings: %+v", a, got)
	}
}

func TestSummarizeTargetNoEncontrado(t *testing.T) {
	f := newMusicFixture(false)
	if _, err := f.svc.SummarizeTarget(context.Background(), domain.Target{Type: domain.TargetSong, SongID: 99}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, se esperaba ErrNotFound", err)
	}
}
