// Package httpapi wires the HTTP routes (Go 1.22+ method patterns on the standard mux) and middleware.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/auth"
)

// Deps are the shared services handlers need.
type Deps struct {
	Logger       *slog.Logger
	DB           *pgxpool.Pool
	Auth         *auth.Service
	WebDir       string // non-empty: also serve the built web app on /
	CookieSecure bool   // refresh cookie only over HTTPS
}

type server struct {
	Deps
	loginLimit    *limiter
	registerLimit *limiter
}

// NewRouter returns the API handler with logging applied to every route.
func NewRouter(deps Deps) http.Handler {
	s := &server{Deps: deps, loginLimit: newLimiter(loginPerMinute, loginBurst),
		registerLimit: newLimiter(registerPerMinute, registerBurst)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", health)
	mux.Handle("GET /api/ready", ready(deps.DB))
	s.authRoutes(mux)
	mux.HandleFunc("/api/", apiNotFound)
	if deps.WebDir != "" {
		mux.Handle("/", webApp(deps.WebDir))
	}
	return logRequests(deps.Logger, limitBody(mux))
}

func (s *server) authRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/auth/register", s.limited(s.registerLimit, s.handle(s.register)))
	mux.Handle("POST /api/v1/auth/login", s.limited(s.loginLimit, s.handle(s.login)))
	mux.Handle("POST /api/v1/auth/refresh", s.handle(s.refresh))
	mux.Handle("POST /api/v1/auth/logout", s.handle(s.logout))
	mux.Handle("GET /api/v1/me", s.authed(s.me))
}
