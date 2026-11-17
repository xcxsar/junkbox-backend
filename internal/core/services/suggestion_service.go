package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Parámetros de las sugerencias de usuarios.
const (
	DefaultSuggestionLimit = 10
	MaxSuggestionLimit     = 50
	// SuggestionMinScore es la calificación mínima para que algo "le guste"
	// a un usuario.
	SuggestionMinScore = 3.5
	// suggestionsPerUser es cuántas sugerencias se guardan por usuario; al
	// leer se filtran las que ya sigue, así que se guardan de más.
	suggestionsPerUser = 100
	// DefaultSuggestionRefresh es cada cuánto recalcula el worker.
	DefaultSuggestionRefresh = time.Hour
)

// SuggestionService sugiere usuarios con gustos parecidos ("Conecta por lo
// que suena", HOME-05). Las sugerencias personalizadas se precalculan por
// lotes (Refresh); al leer se quitan los que ya sigues y se completa con
// los usuarios más populares.
type SuggestionService struct {
	store     ports.SuggestionStore
	refresher ports.SuggestionRefresher
}

// NewSuggestionService crea el servicio.
func NewSuggestionService(store ports.SuggestionStore, refresher ports.SuggestionRefresher) *SuggestionService {
	return &SuggestionService{store: store, refresher: refresher}
}

// Suggestions devuelve hasta limit usuarios sugeridos para viewerID.
// limit = 0 usa DefaultSuggestionLimit.
func (s *SuggestionService) Suggestions(ctx context.Context, viewerID uuid.UUID, limit int) (*domain.SuggestionPage, error) {
	if limit == 0 {
		limit = DefaultSuggestionLimit
	}
	if limit < 1 || limit > MaxSuggestionLimit {
		return nil, domain.NewValidationError(fmt.Sprintf("limit debe estar entre 1 y %d", MaxSuggestionLimit))
	}

	rows, err := s.store.ListSuggestionsForUser(ctx, sqlc.ListSuggestionsForUserParams{ViewerID: viewerID, MaxResults: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("sugerencias: listar: %w", err)
	}
	artists, err := s.artists(ctx, rows)
	if err != nil {
		return nil, err
	}

	page := &domain.SuggestionPage{Limit: limit, Users: make([]domain.UserSuggestion, 0, limit)}
	exclude := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if page.ComputedAt == nil {
			page.ComputedAt = &r.ComputedAt
		}
		exclude = append(exclude, r.SuggestedID)
		page.Users = append(page.Users, domain.UserSuggestion{
			ReviewAuthor: domain.ReviewAuthor{ID: r.SuggestedID, Username: r.Username, DisplayName: stringPtr(r.DisplayName), AvatarURL: stringPtr(r.AvatarUrl)},
			Reason:       personalizedReason(r, artists),
		})
	}

	if missing := limit - len(page.Users); missing > 0 {
		popular, err := s.store.ListPopularUsers(ctx, sqlc.ListPopularUsersParams{ViewerID: viewerID, ExcludeIds: exclude, MaxResults: int32(missing)})
		if err != nil {
			return nil, fmt.Errorf("sugerencias: listar populares: %w", err)
		}
		for _, r := range popular {
			page.Users = append(page.Users, domain.UserSuggestion{
				ReviewAuthor: domain.ReviewAuthor{ID: r.ID, Username: r.Username, DisplayName: stringPtr(r.DisplayName), AvatarURL: stringPtr(r.AvatarUrl)},
				Reason:       domain.SuggestionReason{Type: domain.SuggestionPopular, Text: "Popular en JunkBox", Artists: []domain.SuggestionArtist{}},
			})
		}
	}
	return page, nil
}

// Refresh recalcula los perfiles acústicos y las sugerencias de todos los
// usuarios.
func (s *SuggestionService) Refresh(ctx context.Context) (profiles, suggestions int64, err error) {
	return s.refresher.RefreshSuggestions(ctx, SuggestionMinScore, suggestionsPerUser)
}

// artists carga los artistas en común de todas las filas en una consulta.
func (s *SuggestionService) artists(ctx context.Context, rows []sqlc.ListSuggestionsForUserRow) (map[uuid.UUID]domain.SuggestionArtist, error) {
	var ids []uuid.UUID
	for _, r := range rows {
		ids = append(ids, r.CommonArtistIds...)
	}
	out := map[uuid.UUID]domain.SuggestionArtist{}
	if len(ids) == 0 {
		return out, nil
	}
	found, err := s.store.ListArtistsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sugerencias: obtener artistas: %w", err)
	}
	for _, a := range found {
		out[a.ID] = domain.SuggestionArtist{ID: a.ID, Name: a.Name, ImageURL: stringPtr(a.ImageUrl)}
	}
	return out, nil
}

func personalizedReason(r sqlc.ListSuggestionsForUserRow, artists map[uuid.UUID]domain.SuggestionArtist) domain.SuggestionReason {
	reason := domain.SuggestionReason{
		Type:        r.Kind,
		CommonCount: r.CommonCount,
		Artists:     []domain.SuggestionArtist{},
		Similarity:  float64Ptr(r.Similarity),
	}
	for _, id := range r.CommonArtistIds {
		// Un artista borrado después del cálculo simplemente no aparece.
		if a, ok := artists[id]; ok {
			reason.Artists = append(reason.Artists, a)
		}
	}
	if r.Kind == domain.SuggestionCommonArtists && len(reason.Artists) > 0 {
		reason.Text = commonArtistsText(reason.Artists, int(r.CommonCount))
	} else {
		reason.Type = domain.SuggestionSimilarTaste
		reason.Text = "Gustos parecidos a los tuyos"
	}
	return reason
}

// commonArtistsText: "También escucha Hoke", "También escucha Hoke y
// Feid", "También escucha Hoke, Feid y 3 más".
func commonArtistsText(artists []domain.SuggestionArtist, total int) string {
	switch {
	case total <= 1 || len(artists) == 1:
		return "También escucha " + artists[0].Name
	case total == 2:
		return fmt.Sprintf("También escucha %s y %s", artists[0].Name, artists[1].Name)
	default:
		return fmt.Sprintf("También escucha %s, %s y %d más", artists[0].Name, artists[1].Name, total-2)
	}
}

// SuggestionWorker recalcula las sugerencias al arrancar y cada interval.
type SuggestionWorker struct {
	svc      *SuggestionService
	interval time.Duration
}

// NewSuggestionWorker crea el worker. interval <= 0 usa
// DefaultSuggestionRefresh. Run debe lanzarse en una goroutine.
func NewSuggestionWorker(svc *SuggestionService, interval time.Duration) *SuggestionWorker {
	if interval <= 0 {
		interval = DefaultSuggestionRefresh
	}
	return &SuggestionWorker{svc: svc, interval: interval}
}

// Run recalcula hasta que ctx se cancela. Un error solo se registra: la
// siguiente ronda lo vuelve a intentar y mientras tanto se sirven las
// sugerencias anteriores.
func (w *SuggestionWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *SuggestionWorker) refresh(ctx context.Context) {
	start := time.Now()
	profiles, suggestions, err := w.svc.Refresh(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("worker de sugerencias: %v", err)
		}
		return
	}
	log.Printf("worker de sugerencias: %d perfiles acústicos y %d sugerencias en %s", profiles, suggestions, time.Since(start).Round(time.Millisecond))
}
