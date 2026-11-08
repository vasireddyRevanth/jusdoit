package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"jusdoit/internal/store"
)

const inviteTTL = 7 * 24 * time.Hour

type memberPayload struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type rolePayload struct {
	Role string `json:"role"`
}

type invitePayload struct {
	Role string `json:"role"`
}

func memberRole(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case store.RoleEditor:
		return store.RoleEditor, true
	case store.RoleViewer:
		return store.RoleViewer, true
	}
	return "", false
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	members, err := s.Store.MembersForBoard(r.Context(), board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	var p memberPayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, ok := memberRole(p.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}
	username := strings.TrimSpace(p.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "enter a username")
		return
	}

	member, err := s.Store.MemberByUsername(r.Context(), board.ID, username, role)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no user with that name")
		return
	case errors.Is(err, store.ErrOwnerImmutable):
		writeError(w, http.StatusBadRequest, "the board owner's role cannot be changed")
		return
	case errors.Is(err, store.ErrAlreadyMember):
		writeError(w, http.StatusConflict, "user is already a member")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not add member")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"member": member})
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
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

	found, err := s.Store.SetMemberRole(r.Context(), board.ID, r.PathValue("userId"), role)
	if errors.Is(err, store.ErrOwnerImmutable) {
		writeError(w, http.StatusBadRequest, "the board owner's role cannot be changed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update member")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	found, err := s.Store.RemoveMember(r.Context(), board.ID, r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove member")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request, user store.User, board store.Board, _ string) {
	var p invitePayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, ok := memberRole(p.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}

	token, invite, err := s.Store.CreateInvite(r.Context(), board.ID, role, user.ID, inviteTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create invite")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": invite, "token": token})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	invites, err := s.Store.InvitesForBoard(r.Context(), board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load invites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func (s *Server) handleDeleteInvite(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	found, err := s.Store.DeleteInvite(r.Context(), board.ID, r.PathValue("inviteId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke invite")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "invite not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInvitePreview(w http.ResponseWriter, r *http.Request, _ store.User) {
	target, err := s.Store.InviteByToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invite is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load invite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invite": target})
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request, user store.User) {
	target, err := s.Store.AcceptInvite(r.Context(), r.PathValue("token"), user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invite is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not join board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board_id": target.BoardID, "role": target.Role})
}
