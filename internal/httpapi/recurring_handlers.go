// Recurring series and manual transfer pairing endpoints.
package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/recurring"
)

func (s *server) recurringRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/recurring", s.authed(s.listRecurring))
	mux.Handle("PATCH /api/v1/recurring/{id}", s.authed(s.updateRecurring))
	mux.Handle("POST /api/v1/transactions/{id}/transfer/{other}", s.authed(s.pairTransfer))
	mux.Handle("DELETE /api/v1/transactions/{id}/transfer", s.authed(s.unpairTransfer))
}

func (s *server) listRecurring(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(s.Recurring.List(r.Context(), userID(r)))
}

func (s *server) updateRecurring(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "recurring series")
	if err != nil {
		return err
	}
	var u recurring.Update
	if err := decodeJSON(r, &u); err != nil {
		return err
	}
	return noContent(w, s.Recurring.Set(r.Context(), userID(r), id, u))
}

func (s *server) pairTransfer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "transaction")
	if err != nil {
		return err
	}
	other, err := uuid.Parse(r.PathValue("other"))
	if err != nil {
		return apperr.NotFound("transaction")
	}
	if err := s.Transfers.Pair(r.Context(), userID(r), id, other); err != nil {
		return err
	}
	return noContent(w, s.Analyze(r.Context(), userID(r)))
}

func (s *server) unpairTransfer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "transaction")
	if err != nil {
		return err
	}
	if err := s.Transfers.Unpair(r.Context(), userID(r), id); err != nil {
		return err
	}
	if err := s.Categorize.Recategorize(r.Context(), userID(r), nil); err != nil {
		return err
	}
	return noContent(w, s.Analyze(r.Context(), userID(r)))
}
