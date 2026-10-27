// Auth endpoints. The web app gets the refresh token as an HttpOnly cookie; the mobile app (client: "mobile") gets it
// in the JSON body and sends it back in the body.
package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/ClementAGIL/moneta-api/internal/auth"
)

const refreshCookie = "moneta_refresh"

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Client   string `json:"client,omitempty"` // "mobile" or empty (web)
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken,omitempty"` // mobile only
	Client       string `json:"client,omitempty"`
}

type tokenResponse struct {
	auth.Tokens
	RefreshToken string `json:"refreshToken,omitempty"`
}

func (s *server) register(w http.ResponseWriter, r *http.Request) error {
	var req credentials
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	_, err := s.Auth.Register(r.Context(), req.Email, req.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		return errValidation("email", err.Error())
	case errors.Is(err, auth.ErrWeakPassword):
		return errValidation("password", err.Error())
	case errors.Is(err, auth.ErrEmailTaken):
		return errConflict(err.Error())
	case err != nil:
		return err
	}
	return s.startSession(w, r, req)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) error {
	var req credentials
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	return s.startSession(w, r, req)
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request, req credentials) error {
	tokens, err := s.Auth.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return errUnauthorized(err.Error())
	}
	if err != nil {
		return err
	}
	s.sendTokens(w, tokens, req.Client)
	return nil
}

func (s *server) refresh(w http.ResponseWriter, r *http.Request) error {
	req, token := s.readRefresh(r)
	tokens, err := s.Auth.Refresh(r.Context(), token, r.UserAgent())
	if errors.Is(err, auth.ErrInvalidToken) {
		s.clearCookie(w)
		return errUnauthorized("session expired, please log in again")
	}
	if err != nil {
		return err
	}
	s.sendTokens(w, tokens, req.Client)
	return nil
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) error {
	_, token := s.readRefresh(r)
	s.clearCookie(w)
	if token != "" {
		if err := s.Auth.Logout(r.Context(), token); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) me(w http.ResponseWriter, r *http.Request) error {
	user, err := s.Auth.Me(r.Context(), userID(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, user)
	return nil
}

// readRefresh takes the token from the body (mobile) or the cookie (web). An empty body is fine.
func (s *server) readRefresh(r *http.Request) (refreshRequest, string) {
	var req refreshRequest
	if r.ContentLength != 0 {
		_ = decodeJSON(r, &req)
	}
	if req.RefreshToken != "" {
		return req, req.RefreshToken
	}
	if c, err := r.Cookie(refreshCookie); err == nil {
		return req, c.Value
	}
	return req, ""
}

func (s *server) sendTokens(w http.ResponseWriter, t auth.Tokens, client string) {
	resp := tokenResponse{Tokens: t}
	if client == "mobile" {
		resp.RefreshToken = t.Refresh
	} else {
		s.setCookie(w, t.Refresh, t.RefreshExpires)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) setCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: value, Path: "/api/v1/auth", Expires: expires,
		HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (s *server) clearCookie(w http.ResponseWriter) {
	s.setCookie(w, "", time.Unix(0, 0))
}
