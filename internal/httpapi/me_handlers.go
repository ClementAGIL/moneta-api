// Account endpoints: settings, data export, deletion.
package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/export"
)

type deleteRequest struct {
	Password string `json:"password"`
}

func (s *server) meRoutes(mux *http.ServeMux) {
	mux.Handle("PATCH /api/v1/me", s.authed(s.updateMe))
	mux.Handle("GET /api/v1/me/export", s.authed(s.exportMe))
	mux.Handle("DELETE /api/v1/me", s.authed(s.deleteMe))
}

func (s *server) updateMe(w http.ResponseWriter, r *http.Request) error {
	var in auth.Settings
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	if err := accountError(s.Auth.Update(r.Context(), userID(r), in)); err != nil {
		return err
	}
	return s.me(w, r)
}

// exportMe streams a ZIP; headers go out first, so a failure midway can only be logged.
func (s *server) exportMe(w http.ResponseWriter, r *http.Request) error {
	name := fmt.Sprintf("moneta-export-%s.zip", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	src := export.Sources{DB: s.DB, Auth: s.Auth, Categorize: s.Categorize, Recurring: s.Recurring, Imports: s.Imports,
		Transactions: s.Transactions}
	if err := export.Write(r.Context(), src, userID(r), w); err != nil {
		s.Logger.Error("export failed", "err", err)
	}
	return nil
}

func (s *server) deleteMe(w http.ResponseWriter, r *http.Request) error {
	var req deleteRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	err := s.Auth.Delete(r.Context(), userID(r), req.Password)
	if errors.Is(err, auth.ErrWrongPassword) {
		return errValidation("password", err.Error())
	}
	if err != nil {
		return err
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func accountError(err error) error {
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		return errValidation("currentPassword", err.Error())
	case errors.Is(err, auth.ErrWeakPassword):
		return errValidation("newPassword", err.Error())
	case errors.Is(err, auth.ErrInvalidLocale):
		return errValidation("locale", err.Error())
	}
	return err
}
