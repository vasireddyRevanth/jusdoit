package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store owns the SQLite connection pool.
type Store struct {
	DB *sql.DB
}

// Open opens the database at path and applies the schema.
func Open(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite serialises writes; one connection avoids SQLITE_BUSY under load.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &Store{DB: db}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	return s.DB.Close()
}

// Healthy verifies the database answers a trivial query.
func (s *Store) Healthy(ctx context.Context) error {
	var one int
	return s.DB.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}
