package store

import (
	"context"
	"database/sql"
	"errors"
)

// Board roles, highest privilege first.
const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Board is a task list owned by one user and shared with members.
type Board struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerID   string `json:"owner_id"`
	Role      string `json:"role,omitempty"`
	CreatedAt string `json:"created_at"`
}

// CreateBoard inserts a board and its owner membership atomically.
func (s *Store) CreateBoard(ctx context.Context, ownerID, name string) (Board, error) {
	b := Board{
		ID:        randomID(),
		Name:      name,
		OwnerID:   ownerID,
		Role:      RoleOwner,
		CreatedAt: now(),
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO boards (id, name, owner_id, created_at) VALUES (?, ?, ?, ?)`,
		b.ID, b.Name, b.OwnerID, b.CreatedAt); err != nil {
		return Board{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		b.ID, ownerID, RoleOwner, b.CreatedAt); err != nil {
		return Board{}, err
	}

	if err := tx.Commit(); err != nil {
		return Board{}, err
	}
	return b, nil
}

// BoardsForUser lists boards the user belongs to, with their role.
func (s *Store) BoardsForUser(ctx context.Context, userID string) ([]Board, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT b.id, b.name, b.owner_id, b.created_at, m.role
		   FROM boards b JOIN board_members m ON m.board_id = b.id
		  WHERE m.user_id = ?
		  ORDER BY b.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	boards := []Board{}
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.OwnerID, &b.CreatedAt, &b.Role); err != nil {
			return nil, err
		}
		boards = append(boards, b)
	}
	return boards, rows.Err()
}

// BoardByID returns a board record without membership information.
func (s *Store) BoardByID(ctx context.Context, boardID string) (Board, error) {
	var b Board
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, owner_id, created_at FROM boards WHERE id = ?`, boardID).
		Scan(&b.ID, &b.Name, &b.OwnerID, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Board{}, ErrNotFound
	}
	return b, err
}

// BoardRole returns the user's role on a board.
func (s *Store) BoardRole(ctx context.Context, boardID, userID string) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx,
		`SELECT role FROM board_members WHERE board_id = ? AND user_id = ?`,
		boardID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// RenameBoard updates a board name.
func (s *Store) RenameBoard(ctx context.Context, boardID, name string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE boards SET name = ? WHERE id = ?`, name, boardID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBoard removes a board and everything attached to it.
func (s *Store) DeleteBoard(ctx context.Context, boardID string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, boardID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
