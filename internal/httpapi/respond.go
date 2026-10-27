// JSON responses and errors: handlers return an error, apiError values become {"error": {code, message, fields}}.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
)

// apiError is an error the client may see.
type apiError struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func errBadRequest(msg string) *apiError {
	return &apiError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

func errValidation(field, msg string) *apiError {
	return &apiError{Status: http.StatusUnprocessableEntity, Code: "validation", Message: msg,
		Fields: map[string]string{field: msg}}
}

func errUnauthorized(msg string) *apiError {
	return &apiError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

func errConflict(msg string) *apiError {
	return &apiError{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

var errTooMany = &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many requests"}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// respond returns a function writing a store result as 200 JSON: `return respond(w)(store.List(...))`.
func respond(w http.ResponseWriter) func(any, error) error {
	return func(v any, err error) error {
		if err == nil {
			writeJSON(w, http.StatusOK, v)
		}
		return err
	}
}

// respondCreated is respond with 201 Created.
func respondCreated(w http.ResponseWriter) func(any, error) error {
	return func(v any, err error) error {
		if err == nil {
			writeJSON(w, http.StatusCreated, v)
		}
		return err
	}
}

func noContent(w http.ResponseWriter, err error) error {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
	}
	return err
}

// fromDomain maps apperr kinds to HTTP errors.
func fromDomain(e *apperr.Error) *apiError {
	switch e.Kind {
	case apperr.KindNotFound:
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: e.Message}
	case apperr.KindInvalid:
		return errValidation(e.Field, e.Message)
	default:
		return errConflict(e.Message)
	}
}

// writeError sends client errors as they are and hides everything else behind a logged 500.
func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *apiError
	var domainErr *apperr.Error
	if errors.As(err, &domainErr) {
		apiErr = fromDomain(domainErr)
	} else if !errors.As(err, &apiErr) {
		s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		apiErr = &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error"}
	}
	writeJSON(w, apiErr.Status, map[string]*apiError{"error": apiErr})
}

// decodeJSON reads a JSON body into dst, rejecting unknown fields.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errBadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]*apiError{"error": fromDomain(apperr.NotFound("route").(*apperr.Error))})
}
