package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	chimw "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
)

// RouterOptions es lo que el router necesita para montarse.
type RouterOptions struct {
	Auth *AuthHandler
	JWT  chimw.JWTValidator
	// Revocation comprueba la denylist y el corte global en cada petición
	// autenticada. Si es nil, se omite la comprobación (tests).
	Revocation chimw.TokenRevocation
}

// NewRouter construye el árbol de rutas Chi.
func NewRouter(opts RouterOptions) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Endpoints públicos
		r.Post("/auth/register", opts.Auth.Register)
		r.Post("/auth/login", opts.Auth.Login)
		r.Post("/auth/refresh", opts.Auth.Refresh)

		// Endpoints protegidos (logout necesita el access token para
		// revocarlo en la denylist).
		r.Group(func(r chi.Router) {
			r.Use(chimw.AuthMiddleware(opts.JWT, opts.Revocation))
			r.Get("/auth/me", opts.Auth.Me)
			r.Post("/auth/logout", opts.Auth.Logout)
		})
	})

	return r
}
