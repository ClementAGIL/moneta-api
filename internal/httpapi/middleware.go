// Middleware: access log, body size limit, error-returning handlers, authentication, rate limits.
package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxBodyBytes = 6 << 20 // imports are up to 5 MB, plus multipart overhead

type ctxKey int

const userKey ctxKey = 0

// handlerFunc is a handler that returns its error instead of writing it.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (s *server) handle(h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

// authed requires a valid "Authorization: Bearer <access token>" and puts the user id in the context.
func (s *server) authed(h handlerFunc) http.Handler {
	return s.handle(func(w http.ResponseWriter, r *http.Request) error {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return errUnauthorized("missing access token")
		}
		id, err := s.Auth.Verify(token)
		if err != nil {
			return errUnauthorized("invalid or expired access token")
		}
		return h(w, r.WithContext(context.WithValue(r.Context(), userKey, id)))
	})
}

// userID returns the authenticated user; only valid inside authed handlers.
func userID(r *http.Request) uuid.UUID {
	id, _ := r.Context().Value(userKey).(uuid.UUID)
	return id
}

func (s *server) limited(l *limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			s.writeError(w, r, errTooMany)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the TCP peer: the app is reached directly, not through a proxy, so headers are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
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
