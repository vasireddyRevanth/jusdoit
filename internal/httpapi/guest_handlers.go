package httpapi

import (
	"errors"
	"net/http"

	"jusdoit/internal/store"
)

func (s *Server) handleCreateGuestLink(w http.ResponseWriter, r *http.Request, user store.User, board store.Board, _ string) {
	var p rolePayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, ok := memberRole(p.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}

	token, link, err := s.Store.CreateGuestLink(r.Context(), board.ID, role, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create guest link")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"link": link, "token": token})
}

func (s *Server) handleListGuestLinks(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	links, err := s.Store.GuestLinksForBoard(r.Context(), board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load guest links")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleDeleteGuestLink(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	found, err := s.Store.DeleteGuestLink(r.Context(), board.ID, r.PathValue("linkId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke guest link")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "guest link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGuestPreview(w http.ResponseWriter, r *http.Request) {
	target, err := s.Store.GuestLinkByToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "guest link is invalid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load guest link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invite": target})
}

func (s *Server) handleGuestJoin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	// A signed-in account joins as itself, keeping its role and history.
	if user, ok := s.sessionUser(w, r); ok {
		target, err := s.Store.JoinLinkAsUser(r.Context(), token, user.ID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "guest link is invalid")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not join board")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"board_id": target.BoardID,
			"role":     target.Role,
			"guest":    false,
		})
		return
	}

	// Otherwise mint a throwaway account for this browser.
	if !s.guestLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many guest joins, try again later")
		return
	}
	target, guest, err := s.Store.JoinGuestLink(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "guest link is invalid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not join board")
		return
	}
	if err := s.startSession(w, r, guest.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"board_id": target.BoardID,
		"role":     target.Role,
		"guest":    true,
		"username": guest.Username,
	})
}
