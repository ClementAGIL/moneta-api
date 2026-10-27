// Package httpapi wires the HTTP routes (Go 1.22+ method patterns on the standard mux) and middleware.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps are the shared services handlers need.
type Deps struct {
	Logger *slog.Logger
	DB     *pgxpool.Pool
	WebDir string // non-empty: also serve the built web app on /
}

// NewRouter returns the API handler with logging applied to every route.
func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", health)
	mux.Handle("GET /api/ready", ready(deps.DB))
	mux.HandleFunc("/api/", apiNotFound)
	if deps.WebDir != "" {
		mux.Handle("/", webApp(deps.WebDir))
	}
	return logRequests(deps.Logger, mux)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// statusRecorder keeps the response status so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}
