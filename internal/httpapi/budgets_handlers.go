// Budgets (with the month's progress) and insights endpoints.
package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/budgets"
)

type budgetRequest struct {
	CategoryID uuid.UUID `json:"categoryId"`
	Amount     int64     `json:"amountCents"` // 0 removes the budget
}

func (s *server) budgetRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/budgets", s.authed(s.listBudgets))
	mux.Handle("PUT /api/v1/budgets", s.authed(s.setBudget))
	mux.Handle("GET /api/v1/insights", s.authed(s.listInsights))
	mux.Handle("POST /api/v1/insights/{id}/dismiss", s.authed(s.dismissInsight))
}

// listBudgets: ?month=2026-10 (default current month).
func (s *server) listBudgets(w http.ResponseWriter, r *http.Request) error {
	month, err := monthParam(r, "month", thisMonth())
	if err != nil {
		return err
	}
	return respond(w)(budgets.NewStore(s.DB).Progress(r.Context(), userID(r), month))
}

func (s *server) setBudget(w http.ResponseWriter, r *http.Request) error {
	var req budgetRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if err := budgets.NewStore(s.DB).Set(r.Context(), userID(r), req.CategoryID, req.Amount); err != nil {
		return err
	}
	return noContent(w, s.Insights.Generate(r.Context(), userID(r), thisMonth()))
}

// listInsights: ?month=2026-10 (default: the current and the previous month).
func (s *server) listInsights(w http.ResponseWriter, r *http.Request) error {
	months := []time.Time{thisMonth(), thisMonth().AddDate(0, -1, 0)}
	if r.URL.Query().Get("month") != "" {
		month, err := monthParam(r, "month", thisMonth())
		if err != nil {
			return err
		}
		months = []time.Time{month}
	}
	return respond(w)(s.Insights.List(r.Context(), userID(r), months))
}

func (s *server) dismissInsight(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "insight")
	if err != nil {
		return err
	}
	return noContent(w, s.Insights.Dismiss(r.Context(), userID(r), id))
}
