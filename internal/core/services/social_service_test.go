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

// fakeSocial guarda seguimientos y Me gusta en memoria.
type fakeSocial struct {
	users   map[uuid.UUID]string // id → username
	follows map[[2]uuid.UUID]bool
	likes   map[[2]uuid.UUID]bool
	reviews map[uuid.UUID]bool
	lastArg sqlc.GetPublicProfileParams
}

func newFakeSocial() *fakeSocial {
	return &fakeSocial{users: map[uuid.UUID]string{}, follows: map[[2]uuid.UUID]bool{}, likes: map[[2]uuid.UUID]bool{}, reviews: map[uuid.UUID]bool{}}
}

func (f *fakeSocial) GetPublicProfile(_ context.Context, arg sqlc.GetPublicProfileParams) (sqlc.GetPublicProfileRow, error) {
	f.lastArg = arg
	for id, name := range f.users {
		if id == arg.ID || name == arg.Username {
			var followers int64
			for k := range f.follows {
				if k[1] == id {
					followers++
				}
			}
			return sqlc.GetPublicProfileRow{ID: id, Username: name, FollowersCount: followers, FollowedByMe: f.follows[[2]uuid.UUID{arg.ViewerID, id}]}, nil
		}
	}
	return sqlc.GetPublicProfileRow{}, sql.ErrNoRows
}

func (f *fakeSocial) FollowUser(_ context.Context, arg sqlc.FollowUserParams) error {
	f.follows[[2]uuid.UUID{arg.FollowerID, arg.FolloweeID}] = true
	return nil
}

func (f *fakeSocial) UnfollowUser(_ context.Context, arg sqlc.UnfollowUserParams) error {
	delete(f.follows, [2]uuid.UUID{arg.FollowerID, arg.FolloweeID})
	return nil
}

func (f *fakeSocial) ListFollowers(_ context.Context, arg sqlc.ListFollowersParams) ([]sqlc.ListFollowersRow, error) {
	var out []sqlc.ListFollowersRow
	for k := range f.follows {
		if k[1] == arg.UserID {
			out = append(out, sqlc.ListFollowersRow{ID: k[0], Username: f.users[k[0]]})
		}
	}
	return out, nil
}

func (f *fakeSocial) ListFollowing(_ context.Context, _ sqlc.ListFollowingParams) ([]sqlc.ListFollowingRow, error) {
	return nil, nil
}

func (f *fakeSocial) LikeReview(_ context.Context, arg sqlc.LikeReviewParams) error {
	f.likes[[2]uuid.UUID{arg.ReviewID, arg.UserID}] = true
	return nil
}

func (f *fakeSocial) UnlikeReview(_ context.Context, arg sqlc.UnlikeReviewParams) error {
	delete(f.likes, [2]uuid.UUID{arg.ReviewID, arg.UserID})
	return nil
}

func (f *fakeSocial) GetReviewLikesCount(_ context.Context, id uuid.UUID) (int32, error) {
	if !f.reviews[id] {
		return 0, sql.ErrNoRows
	}
	var n int32
	for k := range f.likes {
		if k[0] == id {
			n++
		}
	}
	return n, nil
}

func TestFollowPorUsernameEsIdempotente(t *testing.T) {
	store := newFakeSocial()
	me, other := uuid.New(), uuid.New()
	store.users[me], store.users[other] = "yo_mismo", "cesar_beltran"
	svc := NewSocialService(store)

	for range 2 {
		profile, err := svc.Follow(context.Background(), me, "cesar_beltran")
		if err != nil {
			t.Fatal(err)
		}
		if !profile.FollowedByMe || profile.FollowersCount != 1 || profile.IsMe {
			t.Fatalf("perfil tras seguir: %+v", profile)
		}
	}
	page, err := svc.Followers(context.Background(), me, other.String(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 1 || page.Users[0].ID != me || page.NextCursor != nil {
		t.Fatalf("seguidores: %+v", page)
	}
	profile, err := svc.Unfollow(context.Background(), me, other.String())
	if err != nil {
		t.Fatal(err)
	}
	if profile.FollowedByMe || profile.FollowersCount != 0 {
		t.Fatalf("perfil tras dejar de seguir: %+v", profile)
	}
}

func TestFollowValidaciones(t *testing.T) {
	store := newFakeSocial()
	me := uuid.New()
	store.users[me] = "yo_mismo"
	svc := NewSocialService(store)

	var v *domain.ValidationError
	if _, err := svc.Follow(context.Background(), me, me.String()); !errors.As(err, &v) {
		t.Errorf("seguirse a sí mismo: esperaba ValidationError, obtuve %v", err)
	}
	if _, err := svc.Follow(context.Background(), me, "No Válido!"); !errors.As(err, &v) {
		t.Errorf("id inválido: esperaba ValidationError, obtuve %v", err)
	}
	if _, err := svc.Follow(context.Background(), me, "nadie"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("usuario inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
	if p, err := svc.Profile(context.Background(), me, "yo_mismo"); err != nil || !p.IsMe || store.lastArg.Username != "yo_mismo" {
		t.Errorf("perfil propio: %+v %v", p, err)
	}
}

func TestLikeEsIdempotenteYDevuelveElConteo(t *testing.T) {
	store := newFakeSocial()
	review, me := uuid.New(), uuid.New()
	store.reviews[review] = true
	svc := NewSocialService(store)

	for range 2 {
		res, err := svc.Like(context.Background(), me, review)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Liked || res.LikesCount != 1 {
			t.Fatalf("like: %+v", res)
		}
	}
	res, err := svc.Unlike(context.Background(), me, review)
	if err != nil || res.Liked || res.LikesCount != 0 {
		t.Fatalf("unlike: %+v %v", res, err)
	}
	if _, err := svc.Like(context.Background(), me, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reseña inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
}
