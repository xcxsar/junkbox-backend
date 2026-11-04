package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	chimw "github.com/xcxsar/junkbox-backend/internal/adapters/primary/http/middleware"
)

// RouterOptions es lo que el router necesita para montarse.
type RouterOptions struct {
	Auth  *AuthHandler
	Music *MusicHandler
	// Account expone recuperación de contraseña y verificación de correo.
	// Si es nil, esas rutas no se montan.
	Account *AccountHandler
	JWT     chimw.JWTValidator
	// Revocation comprueba la denylist y el corte global en cada petición
	// autenticada. Si es nil, se omite la comprobación (tests).
	Revocation chimw.TokenRevocation
	// AllowedOrigins son los orígenes del frontend admitidos por CORS. Si
	// está vacía, se rechaza cualquier origen.
	AllowedOrigins []string
}

// NewRouter construye el árbol de rutas Chi.
func NewRouter(opts RouterOptions) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsHandler(opts.AllowedOrigins))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Endpoints públicos
		r.Post("/auth/register", opts.Auth.Register)
		r.Post("/auth/login", opts.Auth.Login)
		r.Post("/auth/refresh", opts.Auth.Refresh)
		if opts.Account != nil {
			// Sin Bearer: quien olvidó su contraseña no tiene sesión. reset y
			// verify se autentican con el token de un solo uso del correo.
			r.Post("/auth/password/forgot", opts.Account.ForgotPassword)
			r.Post("/auth/password/reset", opts.Account.ResetPassword)
			r.Post("/auth/email/verify", opts.Account.VerifyEmail)
		}

		// Endpoints protegidos (logout necesita el access token para
		// revocarlo en la denylist).
		r.Group(func(r chi.Router) {
			r.Use(chimw.AuthMiddleware(opts.JWT, opts.Revocation))
			r.Get("/auth/me", opts.Auth.Me)
			r.Post("/auth/logout", opts.Auth.Logout)
			if opts.Account != nil {
				r.Post("/auth/email/verify/resend", opts.Account.ResendVerification)
			}

			if opts.Music != nil {
				r.Get("/music/search", opts.Music.Search)
				r.Get("/music/tracks/{id}", opts.Music.Track)
				r.Get("/music/artists/{id}", opts.Music.Artist)
				r.Get("/music/albums/{id}", opts.Music.Album)
			}
		})
	})

	return r
}

// corsHandler permite llamadas desde el navegador solo a los orígenes dados.
// La auth viaja en Authorization y en el cuerpo JSON (sin cookies), así que
// no se habilitan credenciales.
func corsHandler(origins []string) func(http.Handler) http.Handler {
	opts := cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         300,
	}
	// go-chi/cors interpreta una lista vacía como "todos los orígenes".
	if len(origins) == 0 {
		opts.AllowOriginFunc = func(*http.Request, string) bool { return false }
	}
	return cors.Handler(opts)
}
