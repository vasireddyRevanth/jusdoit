package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"jusdoit/internal/store"
)

// Server holds dependencies shared by all handlers.
type Server struct {
	Store         *store.Store
	Version       string
	SecureCookies bool

	loginLimiter    *rateLimiter
	registerLimiter *rateLimiter
}

// New builds a Server with rate limiters configured.
func New(st *store.Store, version string, secureCookies bool) *Server {
	return &Server{
		Store:           st,
		Version:         version,
		SecureCookies:   secureCookies,
		loginLimiter:    newRateLimiter(10, time.Minute),
		registerLimiter: newRateLimiter(5, time.Hour),
	}
}

// Handler builds the HTTP router with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", s.handleIndex)
	mux.HandleFunc("GET /api/health", s.handleHealth)

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))

	mux.HandleFunc("GET /api/boards", s.requireAuth(s.handleListBoards))
	mux.HandleFunc("POST /api/boards", s.requireAuth(s.handleCreateBoard))
	mux.HandleFunc("GET /api/boards/{id}", s.requireBoard(store.RoleViewer, s.handleGetBoard))
	mux.HandleFunc("PATCH /api/boards/{id}", s.requireBoard(store.RoleOwner, s.handleRenameBoard))
	mux.HandleFunc("DELETE /api/boards/{id}", s.requireBoard(store.RoleOwner, s.handleDeleteBoard))
	mux.HandleFunc("POST /api/boards/{id}/tasks", s.requireBoard(store.RoleEditor, s.handleAddTask))
	mux.HandleFunc("DELETE /api/boards/{id}/tasks/{taskId}", s.requireBoard(store.RoleEditor, s.handleDeleteTask))
	mux.HandleFunc("POST /api/boards/{id}/tasks/{taskId}/restore", s.requireBoard(store.RoleEditor, s.handleRestoreTask))
	mux.HandleFunc("POST /api/boards/{id}/spin", s.requireBoard(store.RoleEditor, s.handleSpin))

	return recoverer(logger(csrf(mux)))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "jusdoit",
		"version": s.Version,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	overall, dbStatus, status := "ok", "ok", http.StatusOK
	if err := s.Store.Healthy(ctx); err != nil {
		overall, dbStatus, status = "degraded", "error", http.StatusServiceUnavailable
		slog.Error("health check failed", "err", err)
	}

	writeJSON(w, status, map[string]any{
		"status":  overall,
		"db":      dbStatus,
		"version": s.Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// writeJSON encodes body and sets a no-store cache header.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError returns a JSON error envelope.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a bounded JSON body into dst.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing data")
	}
	return nil
}

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start).String(),
			"remote", clientIP(r),
		)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
