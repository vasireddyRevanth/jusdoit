package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"jusdoit/internal/auth"
	"jusdoit/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = iota

// authedHandler is a handler that receives the authenticated user.
type authedHandler func(http.ResponseWriter, *http.Request, store.User)

// boardHandler is a handler that also receives the board and caller role.
type boardHandler func(http.ResponseWriter, *http.Request, store.User, store.Board, string)

// sessionUser resolves the caller from the session cookie.
func (s *Server) sessionUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	user, err := s.Store.SessionUser(r.Context(), auth.HashToken(c.Value))
	if err != nil {
		s.clearSessionCookie(w)
		return store.User{}, false
	}
	return user, true
}

// requireAuth rejects requests without a live session.
func (s *Server) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.sessionUser(w, r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}

		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx), user)
	}
}

// requireBoard loads the board and enforces a minimum role. Boards the caller
// cannot see return 404 so their existence is not leaked.
func (s *Server) requireBoard(minRole string, next boardHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.sessionUser(w, r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}

		boardID := r.PathValue("id")
		role, err := s.Store.BoardRole(r.Context(), boardID, user.ID)
		if err != nil {
			writeError(w, http.StatusNotFound, "board not found")
			return
		}
		if !roleAtLeast(role, minRole) {
			writeError(w, http.StatusForbidden, "insufficient permission")
			return
		}

		board, err := s.Store.BoardByID(r.Context(), boardID)
		if err != nil {
			writeError(w, http.StatusNotFound, "board not found")
			return
		}
		board.Role = role

		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx), user, board, role)
	}
}

var roleRank = map[string]int{store.RoleViewer: 1, store.RoleEditor: 2, store.RoleOwner: 3}

func roleAtLeast(have, need string) bool {
	return roleRank[have] >= roleRank[need]
}

// userFrom returns the authenticated user attached by requireAuth.
func userFrom(r *http.Request) store.User {
	u, _ := r.Context().Value(userCtxKey).(store.User)
	return u
}

// csrf rejects unsafe methods whose Origin does not match this host.
// SameSite=Lax cookies provide the primary defence; this is a second check.
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					writeError(w, http.StatusForbidden, "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the caller address, preferring nginx's forwarded header.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// rateLimiter is a small in-memory sliding window counter.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	// Bound memory if many distinct clients appear.
	if len(l.hits) > 10000 {
		l.hits = make(map[string][]time.Time)
	}

	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}

	l.hits[key] = append(recent, now)
	return true
}
