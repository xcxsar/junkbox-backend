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

// hokeArtist es un artista de Spotify que nunca se sincronizó.
func hokeArtist() *sqlc.GetArtistRow {
	return &sqlc.GetArtistRow{ID: uuid.New(), Name: "Hoke", SpotifyID: sql.NullString{String: artistA, Valid: true}, Enriched: true}
}

func artistTrackRow(id int32, name string) sqlc.ListTracksByArtistRow {
	return sqlc.ListTracksByArtistRow{ID: id, TrackName: sql.NullString{String: name, Valid: true}, ArtistName: sql.NullString{String: "Hoke", Valid: true}}
}

// albumTrack es una pista de Spotify del artista (o de otro, si artist
// difiere).
func albumTrack(id, name, artist string) domain.ImportedTrack {
	return domain.ImportedTrack{SpotifyID: id, Name: name, Artist: domain.ImportedArtist{SpotifyID: artist, Name: "Hoke"}}
}

// --- álbumes del artista ---

func TestArtistAlbumsSincronizaUnaVez(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artistAlbums = []sqlc.ListAlbumsByArtistRow{
		{ID: uuid.New(), Title: "BBO", ArtistName: "Hoke", AlbumType: "album", ImageUrl: sql.NullString{String: "bbo.jpg", Valid: true}, TotalTracks: 11},
	}

	got, err := f.svc.ArtistAlbums(context.Background(), artistA, 0)
	if err != nil {
		t.Fatalf("ArtistAlbums: %v", err)
	}
	if len(got) != 1 || got[0].Title != "BBO" || got[0].ImageURL == nil || *got[0].ImageURL != "bbo.jpg" {
		t.Fatalf("álbumes inesperados: %+v", got)
	}
	if f.provider.albumsCalls != 1 || len(f.catalog.albumsSynced) != 1 || f.catalog.albumsSynced[0] != f.catalog.artist.ID {
		t.Fatalf("debía sincronizar y marcar (calls=%d, marcas=%v)", f.provider.albumsCalls, f.catalog.albumsSynced)
	}

	f.catalog.artist.AlbumsSyncedAt = sql.NullTime{Time: time.Now(), Valid: true}
	if _, err := f.svc.ArtistAlbums(context.Background(), artistA, 0); err != nil {
		t.Fatalf("ArtistAlbums: %v", err)
	}
	if f.provider.albumsCalls != 1 {
		t.Fatalf("ya sincronizado: no debía consultar Spotify (calls=%d)", f.provider.albumsCalls)
	}
}

func TestArtistAlbumsTTLVencidoResincroniza(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artist.AlbumsSyncedAt = sql.NullTime{Time: time.Now().Add(-DiscographyTTL - time.Hour), Valid: true}

	if _, err := f.svc.ArtistAlbums(context.Background(), artistA, 0); err != nil {
		t.Fatalf("ArtistAlbums: %v", err)
	}
	if f.provider.albumsCalls != 1 {
		t.Fatalf("con el TTL vencido debía consultar Spotify (calls=%d)", f.provider.albumsCalls)
	}
}

func TestArtistAlbumsSpotifyFallaRespondeLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artistAlbums = []sqlc.ListAlbumsByArtistRow{{ID: uuid.New(), Title: "BBO", AlbumType: "album"}}
	f.provider.discErr = errors.New("spotify caído")

	got, err := f.svc.ArtistAlbums(context.Background(), artistA, 0)
	if err != nil {
		t.Fatalf("ArtistAlbums: %v", err)
	}
	if len(got) != 1 || len(f.catalog.albumsSynced) != 0 {
		t.Fatalf("debía responder los locales sin marcar: %+v (marcas=%v)", got, f.catalog.albumsSynced)
	}
}

func TestArtistAlbumsSinSpotifyIDNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = &sqlc.GetArtistRow{ID: uuid.New(), Name: "Del CSV"}

	if _, err := f.svc.ArtistAlbums(context.Background(), f.catalog.artist.ID.String(), 0); err != nil {
		t.Fatalf("ArtistAlbums: %v", err)
	}
	if f.provider.albumsCalls != 0 {
		t.Fatalf("sin spotify_id no debía consultar Spotify (calls=%d)", f.provider.albumsCalls)
	}
}

func TestDiscografiaArtistaInexistenteYLimitInvalido(t *testing.T) {
	f := newMusicFixture(true)

	if _, err := f.svc.ArtistAlbums(context.Background(), artistA, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("álbumes: err = %v, se esperaba ErrNotFound", err)
	}
	if _, err := f.svc.ArtistTracks(context.Background(), artistA, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("canciones: err = %v, se esperaba ErrNotFound", err)
	}
	var v *domain.ValidationError
	if _, err := f.svc.ArtistTracks(context.Background(), artistA, MaxSearchLimit+1); !errors.As(err, &v) {
		t.Fatalf("err = %v, se esperaba ValidationError", err)
	}
	if _, err := f.svc.ArtistAlbums(context.Background(), "no-es-un-id", 0); !errors.As(err, &v) {
		t.Fatalf("err = %v, se esperaba ValidationError", err)
	}
}

// --- canciones del artista ---

func TestArtistTracksCompletaConPistasDeAlbumes(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artist.AlbumsSyncedAt = sql.NullTime{Time: time.Now(), Valid: true}
	f.catalog.artistTracks = []sqlc.ListTracksByArtistRow{artistTrackRow(1, "Medallones")}
	f.catalog.artistTracksAfter = []sqlc.ListTracksByArtistRow{artistTrackRow(1, "Medallones"), artistTrackRow(2, "Olympique"), artistTrackRow(3, "Jjjj")}
	albumBBO, albumSingle, albumOld := uuid.New(), uuid.New(), uuid.New()
	f.catalog.albumsToFill = []sqlc.ListAlbumsToFillByArtistRow{
		{ID: albumBBO, SpotifyID: sql.NullString{String: "bbo", Valid: true}},
		{ID: albumSingle, SpotifyID: sql.NullString{String: "single", Valid: true}},
		{ID: albumOld, SpotifyID: sql.NullString{String: "old", Valid: true}},
	}
	f.provider.albumTracks = map[string][]domain.ImportedTrack{
		// Una pista es de otro artista: no cuenta para completar.
		"bbo":    {albumTrack("t1", "Olympique", artistA), albumTrack("t2", "Feat", "otro")},
		"single": {albumTrack("t3", "Jjjj", artistA)},
		"old":    {albumTrack("t4", "Vieja", artistA)},
	}

	got, err := f.svc.ArtistTracks(context.Background(), artistA, 3)
	if err != nil {
		t.Fatalf("ArtistTracks: %v", err)
	}
	if len(got) != 3 || got[2].TrackName != "Jjjj" {
		t.Fatalf("canciones inesperadas: %+v", got)
	}
	// Faltaban 2: bbo aporta 1 del artista y single la otra; old no se pide.
	if f.provider.albumCalls != 2 || f.provider.albumsCalls != 0 {
		t.Fatalf("llamadas: álbum=%d discografía=%d", f.provider.albumCalls, f.provider.albumsCalls)
	}
	if f.catalog.lastFillArg.MaxResults != maxAlbumsToFill {
		t.Fatalf("debía pedir hasta %d álbumes: %+v", maxAlbumsToFill, f.catalog.lastFillArg)
	}
	if len(f.catalog.albumTracksSynced) != 2 || len(f.catalog.tracksSynced) != 1 {
		t.Fatalf("marcas: álbumes=%v artista=%v", f.catalog.albumTracksSynced, f.catalog.tracksSynced)
	}
	if len(f.queue.jobs) != 3 {
		t.Fatalf("debía encolar las 3 pistas nuevas para embedding: %+v", f.queue.jobs)
	}
}

func TestArtistTracksSincronizaAlbumesSiHaceFalta(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()

	if _, err := f.svc.ArtistTracks(context.Background(), artistA, 0); err != nil {
		t.Fatalf("ArtistTracks: %v", err)
	}
	if f.provider.albumsCalls != 1 || len(f.catalog.albumsSynced) != 1 || len(f.catalog.tracksSynced) != 1 {
		t.Fatalf("debía sincronizar álbumes y marcar (calls=%d, marcas=%v/%v)", f.provider.albumsCalls, f.catalog.albumsSynced, f.catalog.tracksSynced)
	}
}

func TestArtistTracksSpotifyFallaNoMarcaYRespondeLocales(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artistTracks = []sqlc.ListTracksByArtistRow{artistTrackRow(1, "Medallones")}
	f.provider.discErr = errors.New("spotify caído")

	got, err := f.svc.ArtistTracks(context.Background(), artistA, 0)
	if err != nil {
		t.Fatalf("ArtistTracks: %v", err)
	}
	if len(got) != 1 || len(f.catalog.tracksSynced) != 0 {
		t.Fatalf("debía responder las locales sin marcar: %+v (marcas=%v)", got, f.catalog.tracksSynced)
	}
}

func TestArtistTracksSuficientesLocalesNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artistTracks = []sqlc.ListTracksByArtistRow{artistTrackRow(1, "A"), artistTrackRow(2, "B")}

	got, err := f.svc.ArtistTracks(context.Background(), artistA, 2)
	if err != nil {
		t.Fatalf("ArtistTracks: %v", err)
	}
	if len(got) != 2 || f.provider.albumsCalls+f.provider.albumCalls != 0 {
		t.Fatalf("no debía consultar Spotify: %+v (calls=%d/%d)", got, f.provider.albumsCalls, f.provider.albumCalls)
	}
}

func TestArtistTracksYaSincronizadoNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.artist = hokeArtist()
	f.catalog.artist.TracksSyncedAt = sql.NullTime{Time: time.Now(), Valid: true}

	if _, err := f.svc.ArtistTracks(context.Background(), artistA, 0); err != nil {
		t.Fatalf("ArtistTracks: %v", err)
	}
	if f.provider.albumsCalls+f.provider.albumCalls != 0 {
		t.Fatalf("no debía consultar Spotify (calls=%d/%d)", f.provider.albumsCalls, f.provider.albumCalls)
	}
}

// --- pistas del álbum ---

func bboAlbum() *sqlc.GetAlbumRow {
	return &sqlc.GetAlbumRow{ID: uuid.New(), SpotifyID: sql.NullString{String: "bbo", Valid: true}, Title: "BBO", ArtistName: "Hoke", AlbumType: "album", TotalTracks: 3}
}

func TestGetAlbumCompletaPistasFaltantes(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.album = bboAlbum()
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{{ID: 1}}
	f.catalog.albumTracksAfter = []sqlc.ListTracksByAlbumRow{{ID: 2}, {ID: 1}, {ID: 3}}
	f.provider.albumTracks = map[string][]domain.ImportedTrack{"bbo": {albumTrack("t1", "Uno", artistA), albumTrack("t2", "Dos", artistA), albumTrack("t3", "Tres", artistA)}}

	got, err := f.svc.GetAlbum(context.Background(), f.catalog.album.ID.String())
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if len(got.Tracks) != 3 || got.Tracks[0].ID != 2 || f.provider.albumCalls != 1 || len(f.catalog.albumTracksSynced) != 1 {
		t.Fatalf("debía completar las pistas: %+v (calls=%d, marcas=%v)", got.Tracks, f.provider.albumCalls, f.catalog.albumTracksSynced)
	}
}

func TestGetAlbumYaSincronizadoNoConsultaSpotify(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.album = bboAlbum()
	f.catalog.album.TracksSyncedAt = sql.NullTime{Time: time.Now(), Valid: true}
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{{ID: 1}}

	got, err := f.svc.GetAlbum(context.Background(), f.catalog.album.ID.String())
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if len(got.Tracks) != 1 || f.provider.albumCalls != 0 {
		t.Fatalf("no debía consultar Spotify: %+v (calls=%d)", got.Tracks, f.provider.albumCalls)
	}
}

func TestGetAlbum404EnSpotifyMarcaYNoReintenta(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.album = bboAlbum()
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{{ID: 1}}
	f.provider.albumTracks = map[string][]domain.ImportedTrack{} // "bbo" no existe

	got, err := f.svc.GetAlbum(context.Background(), f.catalog.album.ID.String())
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if len(got.Tracks) != 1 || len(f.catalog.albumTracksSynced) != 1 {
		t.Fatalf("debía responder lo local y marcar el álbum: %+v (marcas=%v)", got.Tracks, f.catalog.albumTracksSynced)
	}
}

func TestGetAlbumSpotifyFallaNoMarca(t *testing.T) {
	f := newMusicFixture(true)
	f.catalog.album = bboAlbum()
	f.catalog.albumTracks = []sqlc.ListTracksByAlbumRow{{ID: 1}}
	f.provider.discErr = errors.New("spotify caído")

	got, err := f.svc.GetAlbum(context.Background(), f.catalog.album.ID.String())
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if len(got.Tracks) != 1 || len(f.catalog.albumTracksSynced) != 0 {
		t.Fatalf("debía responder lo local sin marcar: %+v (marcas=%v)", got.Tracks, f.catalog.albumTracksSynced)
	}
}
