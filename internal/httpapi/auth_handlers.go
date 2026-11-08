package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"jusdoit/internal/auth"
	"jusdoit/internal/store"
)

const (
	sessionCookie = "sid"
	sessionTTL    = 30 * 24 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validateCredentials(c credentials) string {
	if !usernamePattern.MatchString(c.Username) {
		return "username must be 3-32 characters: letters, numbers, _ or -"
	}
	if len(c.Password) < 8 {
		return "password must be at least 8 characters"
	}
	if len(c.Password) > 72 {
		return "password must be at most 72 characters"
	}
	return ""
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.registerLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many sign-ups, try again later")
		return
	}

	var c credentials
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateCredentials(c); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	user, err := s.Store.CreateUser(r.Context(), c.Username, hash)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	if err := s.startSession(w, r, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again shortly")
		return
	}

	var c credentials
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, hash, err := s.Store.UserByUsername(r.Context(), c.Username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			auth.WastePasswordTime(c.Password)
		}
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !auth.CheckPassword(hash, c.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	if err := s.startSession(w, r, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.Store.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, user store.User) {
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

// startSession creates a session row and sets the session cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	raw, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionTTL)
	if err := s.Store.CreateSession(r.Context(), hash, userID, expires); err != nil {
		return err
	}
	s.setSessionCookie(w, raw, expires)
	return nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SecureCookies,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SecureCookies,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}
