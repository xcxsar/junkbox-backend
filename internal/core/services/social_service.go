package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites de la búsqueda de usuarios (SEARCH-04).
const (
	DefaultUserSearchLimit = 20
	MaxUserSearchLimit     = 50
	maxUserQueryLen        = 100
)

// SocialService implementa el perfil público, los seguidores y los Me gusta
// de reseñas (HOME-04), y la búsqueda de usuarios (SEARCH-04).
type SocialService struct {
	store ports.SocialStore
}

// NewSocialService crea el servicio.
func NewSocialService(store ports.SocialStore) *SocialService {
	return &SocialService{store: store}
}

// SearchUsers busca usuarios por username o nombre visible, sin tildes ni
// mayúsculas, sin incluir a viewerID. El orden y la coincidencia están en
// la consulta SearchUsers. limit = 0 usa DefaultUserSearchLimit.
func (s *SocialService) SearchUsers(ctx context.Context, viewerID uuid.UUID, query string, limit int) (*domain.UserSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, domain.NewValidationError("q es requerido")
	}
	if utf8.RuneCountInString(query) > maxUserQueryLen {
		return nil, domain.NewValidationError(fmt.Sprintf("q no puede exceder %d caracteres", maxUserQueryLen))
	}
	if limit == 0 {
		limit = DefaultUserSearchLimit
	}
	if limit < 1 || limit > MaxUserSearchLimit {
		return nil, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxUserSearchLimit))
	}

	rows, err := s.store.SearchUsers(ctx, sqlc.SearchUsersParams{ViewerID: viewerID, Query: query, MaxResults: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("social: buscar usuarios: %w", err)
	}
	out := &domain.UserSearchResult{Query: query, Count: len(rows), Results: make([]domain.UserSearchEntry, len(rows))}
	for i, r := range rows {
		out.Results[i] = domain.UserSearchEntry{
			ReviewAuthor: domain.ReviewAuthor{ID: r.ID, Username: r.Username, DisplayName: stringPtr(r.DisplayName), AvatarURL: stringPtr(r.AvatarUrl)},
			FollowedByMe: r.FollowedByMe,
		}
	}
	return out, nil
}

// Profile devuelve el perfil público de un usuario (UUID o username) visto
// por viewerID.
func (s *SocialService) Profile(ctx context.Context, viewerID uuid.UUID, user string) (*domain.PublicProfile, error) {
	arg := sqlc.GetPublicProfileParams{ViewerID: viewerID}
	if id, err := uuid.Parse(user); err == nil {
		arg.ID = id
	} else if usernamePattern.MatchString(user) {
		arg.Username = user
	} else {
		return nil, domain.NewValidationError("id debe ser un UUID o un username")
	}
	r, err := s.store.GetPublicProfile(ctx, arg)
	if err != nil {
		return nil, notFoundOr(err, "social: obtener perfil")
	}
	return &domain.PublicProfile{
		ID:             r.ID,
		Username:       r.Username,
		DisplayName:    stringPtr(r.DisplayName),
		AvatarURL:      stringPtr(r.AvatarUrl),
		CreatedAt:      r.CreatedAt,
		FollowersCount: r.FollowersCount,
		FollowingCount: r.FollowingCount,
		ReviewsCount:   r.ReviewsCount,
		FollowedByMe:   r.FollowedByMe,
		IsMe:           r.ID == viewerID,
	}, nil
}

// Follow hace que viewerID siga al usuario. Es idempotente y devuelve el
// perfil actualizado. Seguirse a uno mismo es un 400.
func (s *SocialService) Follow(ctx context.Context, viewerID uuid.UUID, user string) (*domain.PublicProfile, error) {
	return s.setFollow(ctx, viewerID, user, true)
}

// Unfollow deja de seguir al usuario. Es idempotente.
func (s *SocialService) Unfollow(ctx context.Context, viewerID uuid.UUID, user string) (*domain.PublicProfile, error) {
	return s.setFollow(ctx, viewerID, user, false)
}

func (s *SocialService) setFollow(ctx context.Context, viewerID uuid.UUID, user string, follow bool) (*domain.PublicProfile, error) {
	target, err := s.Profile(ctx, viewerID, user)
	if err != nil {
		return nil, err
	}
	if target.IsMe {
		return nil, domain.NewValidationError("no puedes seguirte a ti mismo")
	}
	if follow {
		err = s.store.FollowUser(ctx, sqlc.FollowUserParams{FollowerID: viewerID, FolloweeID: target.ID})
	} else {
		err = s.store.UnfollowUser(ctx, sqlc.UnfollowUserParams{FollowerID: viewerID, FolloweeID: target.ID})
	}
	switch {
	case isForeignKeyViolation(err):
		// El usuario se borró entre la lectura y la escritura.
		return nil, domain.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("social: seguir: %w", err)
	}
	return s.Profile(ctx, viewerID, target.ID.String())
}

// Followers lista quién sigue al usuario, del seguimiento más reciente al
// más antiguo.
func (s *SocialService) Followers(ctx context.Context, viewerID uuid.UUID, user, cursor string, limit int) (*domain.FollowPage, error) {
	return s.follows(ctx, viewerID, user, cursor, limit, true)
}

// Following lista a quién sigue el usuario.
func (s *SocialService) Following(ctx context.Context, viewerID uuid.UUID, user, cursor string, limit int) (*domain.FollowPage, error) {
	return s.follows(ctx, viewerID, user, cursor, limit, false)
}

func (s *SocialService) follows(ctx context.Context, viewerID uuid.UUID, user, rawCursor string, limit int, followers bool) (*domain.FollowPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	profile, err := s.Profile(ctx, viewerID, user)
	if err != nil {
		return nil, err
	}
	kind := "following:"
	if followers {
		kind = "followers:"
	}
	scope := kind + profile.ID.String()
	cur, err := decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	cursorID, cursorAt := cur.args()

	var rows []sqlc.ListFollowersRow
	if followers {
		rows, err = s.store.ListFollowers(ctx, sqlc.ListFollowersParams{
			ViewerID: viewerID, UserID: profile.ID, CursorID: cursorID, CursorCreatedAt: cursorAt, MaxResults: int32(limit + 1),
		})
	} else {
		var following []sqlc.ListFollowingRow
		following, err = s.store.ListFollowing(ctx, sqlc.ListFollowingParams{
			ViewerID: viewerID, UserID: profile.ID, CursorID: cursorID, CursorCreatedAt: cursorAt, MaxResults: int32(limit + 1),
		})
		for _, r := range following {
			rows = append(rows, sqlc.ListFollowersRow(r))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("social: listar seguidores: %w", err)
	}

	page := &domain.FollowPage{UserID: profile.ID, Limit: limit, Users: []domain.FollowEntry{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = keysetCursor{Scope: scope, CreatedAt: last.FollowedAt, ID: last.ID}.encode()
	}
	for _, r := range rows {
		page.Users = append(page.Users, domain.FollowEntry{
			ReviewAuthor: domain.ReviewAuthor{ID: r.ID, Username: r.Username, DisplayName: stringPtr(r.DisplayName), AvatarURL: stringPtr(r.AvatarUrl)},
			FollowedAt:   r.FollowedAt,
			FollowedByMe: r.FollowedByMe,
		})
	}
	return page, nil
}

// Like da Me gusta a una reseña. Es idempotente. ErrNotFound si la reseña
// no existe.
func (s *SocialService) Like(ctx context.Context, viewerID, reviewID uuid.UUID) (*domain.LikeResult, error) {
	return s.setLike(ctx, viewerID, reviewID, true)
}

// Unlike quita el Me gusta. Es idempotente.
func (s *SocialService) Unlike(ctx context.Context, viewerID, reviewID uuid.UUID) (*domain.LikeResult, error) {
	return s.setLike(ctx, viewerID, reviewID, false)
}

func (s *SocialService) setLike(ctx context.Context, viewerID, reviewID uuid.UUID, like bool) (*domain.LikeResult, error) {
	if _, err := s.store.GetReviewLikesCount(ctx, reviewID); err != nil {
		return nil, notFoundOr(err, "social: obtener reseña")
	}
	var err error
	if like {
		err = s.store.LikeReview(ctx, sqlc.LikeReviewParams{ReviewID: reviewID, UserID: viewerID})
	} else {
		err = s.store.UnlikeReview(ctx, sqlc.UnlikeReviewParams{ReviewID: reviewID, UserID: viewerID})
	}
	switch {
	case isForeignKeyViolation(err):
		return nil, domain.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("social: me gusta: %w", err)
	}
	// El trigger ya actualizó likes_count en la misma sentencia.
	count, err := s.store.GetReviewLikesCount(ctx, reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("social: contar me gusta: %w", err)
	}
	return &domain.LikeResult{ReviewID: reviewID, Liked: like, LikesCount: count}, nil
}

// isForeignKeyViolation detecta el SQLSTATE 23503 (foreign_key_violation),
// como isUniqueViolation.
func isForeignKeyViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23503"
}
