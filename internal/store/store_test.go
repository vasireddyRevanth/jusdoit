package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "alice", "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected generated id")
	}

	if _, err := st.CreateUser(ctx, "ALICE", "hash"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username error = %v, want ErrUsernameTaken", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "bob", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := st.CreateSession(ctx, "token-hash", u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := st.SessionUser(ctx, "token-hash")
	if err != nil {
		t.Fatalf("session user: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("session user id = %q, want %q", got.ID, u.ID)
	}

	if err := st.DeleteSession(ctx, "token-hash"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := st.SessionUser(ctx, "token-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete = %v, want ErrNotFound", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u, _ := st.CreateUser(ctx, "carol", "hash")
	if err := st.CreateSession(ctx, "expired", u.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := st.SessionUser(ctx, "expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session = %v, want ErrNotFound", err)
	}

	n, err := st.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned = %d, want 1", n)
	}
}
