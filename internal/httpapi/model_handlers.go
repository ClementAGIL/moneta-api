// Classifier status, retraining, and "why this category" for a transaction.
package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/jobs"
	"github.com/ClementAGIL/moneta-api/internal/ml"
)

type explanation struct {
	Source     *string    `json:"source"`
	CategoryID *uuid.UUID `json:"categoryId"`
	Reasons    []string   `json:"reasons"`
}

func (s *server) modelRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/model", s.authed(s.modelStatus))
	mux.Handle("POST /api/v1/model/retrain", s.authed(s.retrainModel))
	mux.Handle("GET /api/v1/transactions/{id}/explain", s.authed(s.explainTransaction))
}

func (s *server) modelStatus(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(s.Model.Status(r.Context(), userID(r)))
}

func (s *server) retrainModel(w http.ResponseWriter, r *http.Request) error {
	id := userID(r)
	return noContent(w, jobs.Enqueue(r.Context(), s.DB, ml.JobKind, &id, struct{}{}, time.Time{}))
}

// explainTransaction re-runs the automatic pipeline on the transaction; categories set by hand or as transfers
// come back with their source and no reasons.
func (s *server) explainTransaction(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "transaction")
	if err != nil {
		return err
	}
	t, err := s.Transactions.Get(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	e := explanation{Source: t.CategorySource, CategoryID: t.CategoryID, Reasons: []string{}}
	if t.CategorySource != nil && (*t.CategorySource == "user" || *t.CategorySource == "transfer") {
		writeJSON(w, http.StatusOK, e)
		return nil
	}
	d, reasons, err := s.Categorize.Explain(r.Context(), userID(r), t.Label, t.Amount)
	if err != nil {
		return err
	}
	e.Source, e.CategoryID = d.Source, d.CategoryID
	if reasons != nil {
		e.Reasons = reasons
	}
	writeJSON(w, http.StatusOK, e)
	return nil
}
