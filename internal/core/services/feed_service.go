package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

const (
	// DefaultFeedTimezone es la zona con la que se define "el día" del feed
	// "popular" si el cliente no envía tz.
	DefaultFeedTimezone = "America/Mazatlan"
	// summaryConcurrency es cuántos objetivos se resumen a la vez por página:
	// cada uno puede esperar a Spotify (con su propio presupuesto).
	summaryConcurrency = 8
)

// FeedService arma los feeds de reseñas de Inicio (HOME-04).
type FeedService struct {
	store     ports.FeedStore
	summaries ports.TargetSummarizer // nil: reseñas sin target.summary
	defaultTZ string
}

// NewFeedService crea el servicio. defaultTZ vacío usa DefaultFeedTimezone;
// debe ser una zona IANA válida (la valida config).
func NewFeedService(store ports.FeedStore, summaries ports.TargetSummarizer, defaultTZ string) *FeedService {
	if defaultTZ == "" {
		defaultTZ = DefaultFeedTimezone
	}
	return &FeedService{store: store, summaries: summaries, defaultTZ: defaultTZ}
}

// Feed devuelve una página del feed tab ("popular" o "following") para
// viewerID. tz solo aplica a "popular" (vacío usa la zona por defecto).
// cursor es el next_cursor de la página anterior (vacío: primera página) y
// limit = 0 usa DefaultPageLimit.
func (s *FeedService) Feed(ctx context.Context, viewerID uuid.UUID, tab, tz, cursor string, limit int) (*domain.FeedPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	switch tab {
	case domain.FeedTabPopular:
		return s.popular(ctx, viewerID, tz, cursor, limit)
	case domain.FeedTabFollowing:
		return s.following(ctx, viewerID, cursor, limit)
	default:
		return nil, domain.NewValidationError("tab debe ser popular o following")
	}
}

func (s *FeedService) popular(ctx context.Context, viewerID uuid.UUID, tz, rawCursor string, limit int) (*domain.FeedPage, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = s.defaultTZ
	}
	loc, err := loadTimezone(tz)
	if err != nil {
		return nil, err
	}
	scope := "feed:" + domain.FeedTabPopular
	cur, err := decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	// El día local se calcula en Postgres; el cursor solo es válido con la
	// misma zona con la que se generó.
	if cur != nil && cur.TZ != tz {
		return nil, domain.NewValidationError("el cursor se generó con otra zona horaria (tz)")
	}

	arg := sqlc.ListPopularFeedParams{ViewerID: viewerID, Tz: tz, MaxResults: int32(limit + 1)}
	if cur != nil {
		arg.CursorID, arg.CursorCreatedAt = cur.args()
		arg.CursorDay = sql.NullString{String: cur.Day, Valid: true}
		arg.CursorScore = sql.NullInt32{Int32: cur.Score, Valid: true}
	}
	rows, err := s.store.ListPopularFeed(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("feed: listar populares: %w", err)
	}
	page := make([]sqlc.ListReviewsByTargetRow, len(rows))
	for i, r := range rows {
		page[i] = sqlc.ListReviewsByTargetRow(r)
	}

	out := &domain.FeedPage{Tab: domain.FeedTabPopular, Timezone: tz, Limit: limit}
	page, last := trimPage(page, limit)
	if last != nil {
		out.NextCursor = keysetCursor{
			Scope:     scope,
			TZ:        tz,
			Day:       last.CreatedAt.In(loc).Format(time.DateOnly),
			Score:     last.PopularityScore.Int32,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}.encode()
	}
	if out.Reviews, err = s.reviews(ctx, page); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *FeedService) following(ctx context.Context, viewerID uuid.UUID, rawCursor string, limit int) (*domain.FeedPage, error) {
	scope := "feed:" + domain.FeedTabFollowing
	cur, err := decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	arg := sqlc.ListFollowingFeedParams{ViewerID: viewerID, MaxResults: int32(limit + 1)}
	arg.CursorID, arg.CursorCreatedAt = cur.args()
	rows, err := s.store.ListFollowingFeed(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("feed: listar seguidos: %w", err)
	}
	page := make([]sqlc.ListReviewsByTargetRow, len(rows))
	for i, r := range rows {
		page[i] = sqlc.ListReviewsByTargetRow(r)
	}

	out := &domain.FeedPage{Tab: domain.FeedTabFollowing, Limit: limit}
	page, last := trimPage(page, limit)
	if last != nil {
		out.NextCursor = keysetCursor{Scope: scope, CreatedAt: last.CreatedAt, ID: last.ID}.encode()
	}
	if out.Reviews, err = s.reviews(ctx, page); err != nil {
		return nil, err
	}
	return out, nil
}

// trimPage recorta la fila de más que se pidió para saber si hay otra
// página. last es la última fila entregada si la hay (nil: no hay más).
func trimPage(rows []sqlc.ListReviewsByTargetRow, limit int) ([]sqlc.ListReviewsByTargetRow, *sqlc.ListReviewsByTargetRow) {
	if len(rows) <= limit {
		return rows, nil
	}
	rows = rows[:limit]
	return rows, &rows[limit-1]
}

// reviews convierte las filas y adjunta el resumen de cada objetivo. Los
// objetivos se resumen una vez cada uno y en paralelo (como máximo
// summaryConcurrency a la vez).
func (s *FeedService) reviews(ctx context.Context, rows []sqlc.ListReviewsByTargetRow) ([]domain.Review, error) {
	out := make([]domain.Review, len(rows))
	targets := make([]domain.Target, len(rows))
	for i, r := range rows {
		out[i] = reviewFromRow(r)
		targets[i] = targetFromColumns(r.SongID, r.AlbumID, r.ArtistID)
	}
	summaries, err := summarizeTargets(ctx, s.summaries, targets)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Target.Summary = summaries[targets[i]]
	}
	return out, nil
}

// summarizeTargets resume cada objetivo distinto una sola vez.
func summarizeTargets(ctx context.Context, summaries ports.TargetSummarizer, targets []domain.Target) (map[domain.Target]any, error) {
	result := map[domain.Target]any{}
	if summaries == nil {
		return result, nil
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		sem      = make(chan struct{}, summaryConcurrency)
	)
	seen := map[domain.Target]bool{}
	for _, t := range targets {
		if seen[t] {
			continue
		}
		seen[t] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			summary, err := targetSummary(ctx, summaries, t)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			result[t] = summary
		}()
	}
	wg.Wait()
	return result, firstErr
}

// loadTimezone valida una zona IANA. "Local" depende del servidor y no se
// acepta.
func loadTimezone(tz string) (*time.Location, error) {
	invalid := domain.NewValidationError(fmt.Sprintf("tz inválida (%q): se espera una zona IANA, p. ej. America/Mexico_City", tz))
	if tz == "Local" || len(tz) > 64 {
		return nil, invalid
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, invalid
	}
	return loc, nil
}
