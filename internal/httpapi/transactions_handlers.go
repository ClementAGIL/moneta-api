// Transactions (list/search, edit) and rules (CRUD, preview) endpoints.
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/transactions"
)

const previewLimit = 20

type rulePreview struct {
	Total int                        `json:"total"`
	Items []transactions.Transaction `json:"items"`
}

func (s *server) transactionRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/transactions", s.authed(s.listTransactions))
	mux.Handle("GET /api/v1/transactions/{id}", s.authed(s.getTransaction))
	mux.Handle("PATCH /api/v1/transactions/{id}", s.authed(s.editTransaction))
	mux.Handle("GET /api/v1/rules", s.authed(s.listRules))
	mux.Handle("POST /api/v1/rules", s.authed(s.createRule))
	mux.Handle("PATCH /api/v1/rules/{id}", s.authed(s.updateRule))
	mux.Handle("DELETE /api/v1/rules/{id}", s.authed(s.deleteRule))
	mux.Handle("POST /api/v1/rules/preview", s.authed(s.previewRule))
}

func (s *server) listTransactions(w http.ResponseWriter, r *http.Request) error {
	f, err := parseFilter(r)
	if err != nil {
		return err
	}
	return respond(w)(s.Transactions.List(r.Context(), userID(r), f))
}

func (s *server) getTransaction(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "transaction")
	if err != nil {
		return err
	}
	return respond(w)(s.Transactions.Get(r.Context(), userID(r), id))
}

func (s *server) editTransaction(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "transaction")
	if err != nil {
		return err
	}
	var u transactions.Update
	if err := decodeJSON(r, &u); err != nil {
		return err
	}
	return respond(w)(s.Transactions.Edit(r.Context(), userID(r), id, u, s.Categorize.CorrectMerchant))
}

func (s *server) listRules(w http.ResponseWriter, r *http.Request) error {
	keys, err := s.Auth.Keys(r.Context(), userID(r))
	if err != nil {
		return err
	}
	return respond(w)(s.Categorize.Rules(r.Context(), userID(r), keys))
}

func (s *server) createRule(w http.ResponseWriter, r *http.Request) error {
	return s.saveRule(w, r, nil)
}

func (s *server) updateRule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "rule")
	if err != nil {
		return err
	}
	return s.saveRule(w, r, &id)
}

func (s *server) saveRule(w http.ResponseWriter, r *http.Request, id *uuid.UUID) error {
	var in categorize.RuleInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	savedID, err := s.Categorize.SaveRule(r.Context(), userID(r), id, in, "manual")
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]uuid.UUID{"id": savedID})
	return nil
}

func (s *server) deleteRule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "rule")
	if err != nil {
		return err
	}
	return noContent(w, s.Categorize.DeleteRule(r.Context(), userID(r), id))
}

func (s *server) previewRule(w http.ResponseWriter, r *http.Request) error {
	var in categorize.RuleInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	ids, total, err := s.Categorize.Preview(r.Context(), userID(r), in, previewLimit)
	if err != nil {
		return err
	}
	preview := rulePreview{Total: total, Items: []transactions.Transaction{}}
	for _, id := range ids {
		t, err := s.Transactions.Get(r.Context(), userID(r), id)
		if err != nil {
			return err
		}
		preview.Items = append(preview.Items, t)
	}
	writeJSON(w, http.StatusOK, preview)
	return nil
}

// parseFilter reads ?account=&category=&uncategorized=true&from=&to=&q=&limit=&cursor=.
func parseFilter(r *http.Request) (transactions.Filter, error) {
	q := r.URL.Query()
	f := transactions.Filter{Query: q.Get("q"), Cursor: q.Get("cursor"), Uncategorized: q.Get("uncategorized") == "true"}
	var err error
	if f.AccountID, err = optionalID(q.Get("account"), "account"); err != nil {
		return f, err
	}
	if f.CategoryID, err = optionalID(q.Get("category"), "category"); err != nil {
		return f, err
	}
	if f.From, err = optionalDate(q.Get("from"), "from"); err != nil {
		return f, err
	}
	if f.To, err = optionalDate(q.Get("to"), "to"); err != nil {
		return f, err
	}
	if v := q.Get("limit"); v != "" {
		if f.Limit, err = strconv.Atoi(v); err != nil {
			return f, apperr.Invalid("limit", "must be a number")
		}
	}
	return f, nil
}

func optionalID(v, field string) (*uuid.UUID, error) {
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, apperr.Invalid(field, "invalid id")
	}
	return &id, nil
}

func optionalDate(v, field string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, apperr.Invalid(field, "must be a date like 2026-01-31")
	}
	return &t, nil
}
