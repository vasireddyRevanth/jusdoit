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

// requireAuth rejects requests without a live session.
func (s *Server) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}

		user, err := s.Store.SessionUser(r.Context(), auth.HashToken(c.Value))
		if err != nil {
			s.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}

		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx), user)
	}
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
