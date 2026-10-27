// Banks, accounts and categories endpoints (plain CRUD over their stores).
package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/accounts"
	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/categories"
)

type nameRequest struct {
	Name string `json:"name"`
}

func (s *server) resourceRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/banks", s.authed(s.listBanks))
	mux.Handle("POST /api/v1/banks", s.authed(s.createBank))
	mux.Handle("PATCH /api/v1/banks/{id}", s.authed(s.renameBank))
	mux.Handle("DELETE /api/v1/banks/{id}", s.authed(s.deleteBank))
	mux.Handle("GET /api/v1/accounts", s.authed(s.listAccounts))
	mux.Handle("POST /api/v1/accounts", s.authed(s.createAccount))
	mux.Handle("PATCH /api/v1/accounts/{id}", s.authed(s.updateAccount))
	mux.Handle("DELETE /api/v1/accounts/{id}", s.authed(s.deleteAccount))
	mux.Handle("GET /api/v1/categories", s.authed(s.listCategories))
	mux.Handle("POST /api/v1/categories", s.authed(s.createCategory))
	mux.Handle("PATCH /api/v1/categories/{id}", s.authed(s.updateCategory))
	mux.Handle("DELETE /api/v1/categories/{id}", s.authed(s.deleteCategory))
}

// pathID parses the {id} path segment; a malformed id is just "not found".
func pathID(r *http.Request, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound(what)
	}
	return id, nil
}

func (s *server) listBanks(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(accounts.NewStore(s.DB).Banks(r.Context(), userID(r)))
}

func (s *server) createBank(w http.ResponseWriter, r *http.Request) error {
	var req nameRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	return respondCreated(w)(accounts.NewStore(s.DB).CreateBank(r.Context(), userID(r), req.Name))
}

func (s *server) renameBank(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bank")
	if err != nil {
		return err
	}
	var req nameRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	return noContent(w, accounts.NewStore(s.DB).RenameBank(r.Context(), userID(r), id, req.Name))
}

func (s *server) deleteBank(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bank")
	if err != nil {
		return err
	}
	return noContent(w, accounts.NewStore(s.DB).DeleteBank(r.Context(), userID(r), id))
}

func (s *server) listAccounts(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(accounts.NewStore(s.DB).Accounts(r.Context(), userID(r)))
}

func (s *server) createAccount(w http.ResponseWriter, r *http.Request) error {
	var in accounts.AccountInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	return respondCreated(w)(accounts.NewStore(s.DB).CreateAccount(r.Context(), userID(r), in))
}

func (s *server) updateAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "account")
	if err != nil {
		return err
	}
	var in accounts.AccountInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	return respond(w)(accounts.NewStore(s.DB).UpdateAccount(r.Context(), userID(r), id, in))
}

func (s *server) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "account")
	if err != nil {
		return err
	}
	return noContent(w, accounts.NewStore(s.DB).DeleteAccount(r.Context(), userID(r), id))
}

func (s *server) listCategories(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(categories.NewStore(s.DB).List(r.Context(), userID(r)))
}

func (s *server) createCategory(w http.ResponseWriter, r *http.Request) error {
	var in categories.Input
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	return respondCreated(w)(categories.NewStore(s.DB).Create(r.Context(), userID(r), in))
}

func (s *server) updateCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "category")
	if err != nil {
		return err
	}
	var in categories.Input
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	return respond(w)(categories.NewStore(s.DB).Update(r.Context(), userID(r), id, in))
}

func (s *server) deleteCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "category")
	if err != nil {
		return err
	}
	return noContent(w, categories.NewStore(s.DB).Delete(r.Context(), userID(r), id))
}
