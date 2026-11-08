package httpapi

import (
	"net/http"
	"strings"

	"jusdoit/internal/store"
)

type boardPayload struct {
	Name string `json:"name"`
}

type taskPayload struct {
	Text string `json:"text"`
}

func cleanName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if len(name) < 1 || len(name) > 60 {
		return "", false
	}
	return name, true
}

func cleanTaskText(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if len(text) < 1 || len(text) > 500 {
		return "", false
	}
	return text, true
}

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request, user store.User) {
	boards, err := s.Store.BoardsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load boards")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request, user store.User) {
	var p boardPayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := cleanName(p.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "board name must be 1-60 characters")
		return
	}

	board, err := s.Store.CreateBoard(r.Context(), user.ID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create board")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"board": board})
}

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	active, done, err := s.Store.TasksForBoard(r.Context(), board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load tasks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"board":  board,
		"active": active,
		"done":   done,
	})
}

func (s *Server) handleRenameBoard(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	var p boardPayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := cleanName(p.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "board name must be 1-60 characters")
		return
	}
	if err := s.Store.RenameBoard(r.Context(), board.ID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "could not rename board")
		return
	}
	board.Name = name
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	if err := s.Store.DeleteBoard(r.Context(), board.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete board")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddTask(w http.ResponseWriter, r *http.Request, user store.User, board store.Board, _ string) {
	var p taskPayload
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	text, ok := cleanTaskText(p.Text)
	if !ok {
		writeError(w, http.StatusBadRequest, "task must be 1-500 characters")
		return
	}

	task, err := s.Store.AddTask(r.Context(), board.ID, user.ID, text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add task")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": task})
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	found, err := s.Store.DeleteTask(r.Context(), board.ID, r.PathValue("taskId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete task")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreTask(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	found, err := s.Store.RestoreTask(r.Context(), board.ID, r.PathValue("taskId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not restore task")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSpin(w http.ResponseWriter, r *http.Request, _ store.User, board store.Board, _ string) {
	task, found, err := s.Store.SpinTask(r.Context(), board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not spin")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}
