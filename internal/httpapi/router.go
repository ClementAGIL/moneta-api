// Package httpapi wires the HTTP routes (Go 1.22+ method patterns on the standard mux) and middleware.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/forecast"
	"github.com/ClementAGIL/moneta-api/internal/imports"
	"github.com/ClementAGIL/moneta-api/internal/insights"
	"github.com/ClementAGIL/moneta-api/internal/ml"
	"github.com/ClementAGIL/moneta-api/internal/recurring"
	"github.com/ClementAGIL/moneta-api/internal/stats"
	"github.com/ClementAGIL/moneta-api/internal/transactions"
	"github.com/ClementAGIL/moneta-api/internal/transfers"
)

// Deps are the shared services handlers need.
type Deps struct {
	Logger       *slog.Logger
	DB           *pgxpool.Pool
	Auth         *auth.Service
	Imports      *imports.Service
	Transactions *transactions.Service
	Categorize   *categorize.Service
	Stats        *stats.Service
	Transfers    *transfers.Service
	Recurring    *recurring.Service
	Insights     *insights.Service
	Model        *ml.Service
	Forecast     *forecast.Service
	// Analyze queues the background analysis (transfers, recurring, insights) of a user.
	Analyze      func(ctx context.Context, userID uuid.UUID) error
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
	s.resourceRoutes(mux)
	s.importRoutes(mux)
	s.transactionRoutes(mux)
	s.statsRoutes(mux)
	s.recurringRoutes(mux)
	s.budgetRoutes(mux)
	s.modelRoutes(mux)
	mux.HandleFunc("/api/", apiNotFound)
	if deps.WebDir != "" {
		mux.Handle("/", webApp(deps.WebDir))
	}
	return logRequests(deps.Logger, s.recoverPanics(limitBody(mux)))
}

func (s *server) authRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/auth/register", s.limited(s.registerLimit, s.handle(s.register)))
	mux.Handle("POST /api/v1/auth/login", s.limited(s.loginLimit, s.handle(s.login)))
	mux.Handle("POST /api/v1/auth/refresh", s.handle(s.refresh))
	mux.Handle("POST /api/v1/auth/logout", s.handle(s.logout))
	mux.Handle("GET /api/v1/me", s.authed(s.me))
}
