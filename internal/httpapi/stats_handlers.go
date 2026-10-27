// Stats endpoints: month summary, category series, top merchants.
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
)

const (
	defaultSeriesMonths = 12
	defaultMerchants    = 10
	maxMerchants        = 50
)

func (s *server) statsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/stats/summary", s.authed(s.statsSummary))
	mux.Handle("GET /api/v1/stats/categories", s.authed(s.statsCategories))
	mux.Handle("GET /api/v1/stats/merchants", s.authed(s.statsMerchants))
}

// statsSummary: ?month=2026-10 (default: current month).
func (s *server) statsSummary(w http.ResponseWriter, r *http.Request) error {
	month, err := monthParam(r, "month", thisMonth())
	if err != nil {
		return err
	}
	return respond(w)(s.Stats.Summary(r.Context(), userID(r), month))
}

// statsCategories: ?from=2026-01&to=2026-10 (default: the last 12 months).
func (s *server) statsCategories(w http.ResponseWriter, r *http.Request) error {
	to, err := monthParam(r, "to", thisMonth())
	if err != nil {
		return err
	}
	from, err := monthParam(r, "from", to.AddDate(0, 1-defaultSeriesMonths, 0))
	if err != nil {
		return err
	}
	return respond(w)(s.Stats.Categories(r.Context(), userID(r), from, to))
}

// statsMerchants: ?from=2026-10&to=2026-10&limit=10 (months, inclusive; default: current month).
func (s *server) statsMerchants(w http.ResponseWriter, r *http.Request) error {
	to, err := monthParam(r, "to", thisMonth())
	if err != nil {
		return err
	}
	from, err := monthParam(r, "from", to)
	if err != nil {
		return err
	}
	limit := defaultMerchants
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
			return apperr.Invalid("limit", "must be a positive number")
		}
	}
	return respond(w)(s.Stats.Merchants(r.Context(), userID(r), from, to.AddDate(0, 1, 0), min(limit, maxMerchants)))
}

func monthParam(r *http.Request, name string, fallback time.Time) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback, nil
	}
	t, err := time.Parse("2006-01", v)
	if err != nil {
		return time.Time{}, apperr.Invalid(name, "must be a month like 2026-10")
	}
	return t, nil
}

func thisMonth() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
