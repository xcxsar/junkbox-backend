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
	// Ratings y Reviews exponen calificaciones y reseñas. Si son nil, esas
	// rutas no se montan.
	Ratings *RatingHandler
	Reviews *ReviewHandler
	// Home expone el feed de Inicio (recomendaciones). Si es nil, la ruta
	// no se monta.
	Home *HomeHandler
	// Social expone el feed de reseñas, perfiles, seguidores, Me gusta y
	// comentarios. Si es nil, esas rutas no se montan.
	Social *SocialHandler
	JWT    chimw.JWTValidator
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
			if opts.Ratings != nil {
				r.Post("/ratings", opts.Ratings.Rate)
				r.Get("/ratings/my", opts.Ratings.Mine)
				r.Delete("/ratings/{id}", opts.Ratings.Delete)
			}
			if opts.Reviews != nil {
				r.Post("/reviews", opts.Reviews.Create)
				r.Get("/reviews", opts.Reviews.List)
				r.Get("/reviews/{id}", opts.Reviews.Get)
				r.Put("/reviews/{id}", opts.Reviews.Update)
				r.Delete("/reviews/{id}", opts.Reviews.Delete)
			}
			if opts.Home != nil {
				r.Get("/home", opts.Home.Home)
			}
			if opts.Social != nil {
				r.Get("/feed", opts.Social.Feed)
				// Ruta fija antes que /users/{id}: chi la prefiere, así que el
				// username "suggestions" solo es accesible por UUID.
				r.Get("/users/suggestions", opts.Social.Suggestions)
				r.Get("/users/{id}", opts.Social.Profile)
				r.Post("/users/{id}/follow", opts.Social.Follow)
				r.Delete("/users/{id}/follow", opts.Social.Unfollow)
				r.Get("/users/{id}/followers", opts.Social.Followers)
				r.Get("/users/{id}/following", opts.Social.Following)
				r.Post("/reviews/{id}/like", opts.Social.Like)
				r.Delete("/reviews/{id}/like", opts.Social.Unlike)
				r.Get("/reviews/{id}/comments", opts.Social.ListComments)
				r.Post("/reviews/{id}/comments", opts.Social.CreateComment)
				r.Get("/comments/{id}/replies", opts.Social.ListReplies)
				r.Put("/comments/{id}", opts.Social.UpdateComment)
				r.Delete("/comments/{id}", opts.Social.DeleteComment)
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
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         300,
	}
	// go-chi/cors interpreta una lista vacía como "todos los orígenes".
	if len(origins) == 0 {
		opts.AllowOriginFunc = func(*http.Request, string) bool { return false }
	}
	return cors.Handler(opts)
}
