// Package apperr defines the errors domain packages return so the HTTP layer can map them to statuses without
// knowing each package (not found → 404, invalid → 422, conflict → 409).
package apperr

import "fmt"

// Kind classifies an Error.
type Kind int

const (
	KindNotFound Kind = iota
	KindInvalid
	KindConflict
)

// Error is a client-facing domain error.
type Error struct {
	Kind    Kind
	Field   string // set for KindInvalid
	Message string
}

func (e *Error) Error() string { return e.Message }

func NotFound(what string) error {
	return &Error{Kind: KindNotFound, Message: what + " not found"}
}

func Invalid(field, format string, args ...any) error {
	return &Error{Kind: KindInvalid, Field: field, Message: fmt.Sprintf(format, args...)}
}

func Conflict(format string, args ...any) error {
	return &Error{Kind: KindConflict, Message: fmt.Sprintf(format, args...)}
}
