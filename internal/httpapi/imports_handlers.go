// Import endpoints: multipart upload, mapping, review/commit, cancel/undo, history and saved profiles.
package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/db"
	"github.com/ClementAGIL/moneta-api/internal/imports"
)

const maxUploadBytes = 5 << 20

type commitRequest struct {
	Keep []int `json:"keep"` // review rows to import anyway
}

type profile struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	LastAccountID *uuid.UUID `json:"lastAccountId"`
}

func (s *server) importRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/imports", s.authed(s.uploadImport))
	mux.Handle("GET /api/v1/imports", s.authed(s.listImports))
	mux.Handle("GET /api/v1/imports/{id}", s.authed(s.getImport))
	mux.Handle("PUT /api/v1/imports/{id}/mapping", s.authed(s.mapImport))
	mux.Handle("POST /api/v1/imports/{id}/commit", s.authed(s.commitImport))
	mux.Handle("DELETE /api/v1/imports/{id}", s.authed(s.deleteImport))
	mux.Handle("GET /api/v1/import-profiles", s.authed(s.listProfiles))
	mux.Handle("DELETE /api/v1/import-profiles/{id}", s.authed(s.deleteProfile))
}

// uploadImport reads the "file" multipart field fully in memory; nothing touches the disk.
func (s *server) uploadImport(w http.ResponseWriter, r *http.Request) error {
	if !s.importLimit.allow(userID(r).String()) {
		return errTooMany
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return errBadRequest(`send the bank file as multipart field "file"`)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxUploadBytes {
		return errValidation("file", "the file is larger than 5 MB")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	return respondCreated(w)(s.Imports.Upload(r.Context(), userID(r), header.Filename, data))
}

func (s *server) listImports(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(s.Imports.List(r.Context(), userID(r)))
}

func (s *server) getImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "import")
	if err != nil {
		return err
	}
	return respond(w)(s.Imports.Get(r.Context(), userID(r), id))
}

func (s *server) mapImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "import")
	if err != nil {
		return err
	}
	var req imports.MappingRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	return respond(w)(s.Imports.SetMapping(r.Context(), userID(r), id, req))
}

func (s *server) commitImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "import")
	if err != nil {
		return err
	}
	var req commitRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	if req.Keep == nil {
		req.Keep = []int{}
	}
	return respond(w)(s.Imports.Commit(r.Context(), userID(r), id, req.Keep))
}

func (s *server) deleteImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "import")
	if err != nil {
		return err
	}
	return noContent(w, s.Imports.Delete(r.Context(), userID(r), id))
}

func (s *server) listProfiles(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT id, name, last_account_id FROM import_profiles WHERE user_id = $1
		ORDER BY updated_at DESC`, userID(r))
	if err != nil {
		return err
	}
	defer rows.Close()
	profiles := []profile{}
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.ID, &p.Name, &p.LastAccountID); err != nil {
			return err
		}
		profiles = append(profiles, p)
	}
	return respond(w)(profiles, rows.Err())
}

func (s *server) deleteProfile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "profile")
	if err != nil {
		return err
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM import_profiles WHERE id = $1 AND user_id = $2`, id, userID(r))
	return noContent(w, db.Affected(tag, err, "profile"))
}
