package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// User is a public account record.
type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

var (
	// ErrUsernameTaken means the username is already registered.
	ErrUsernameTaken = errors.New("username taken")
	// ErrNotFound means the requested record does not exist.
	ErrNotFound = errors.New("not found")
)

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// CreateUser inserts a user and returns the public record.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	u := User{ID: randomID(), Username: username, CreatedAt: now()}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, passwordHash, u.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	return u, nil
}

// UserByUsername returns the public user and password hash.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, string, error) {
	var u User
	var hash string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`,
		username).Scan(&u.ID, &u.Username, &hash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	return u, hash, err
}

// UserByID returns a public user record.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}
