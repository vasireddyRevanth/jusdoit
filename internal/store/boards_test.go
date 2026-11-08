package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBoardCreateListAndRole(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, err := st.CreateUser(ctx, "owner", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	board, err := st.CreateBoard(ctx, user.ID, "home")
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	if board.Role != RoleOwner {
		t.Fatalf("role = %q, want owner", board.Role)
	}

	boards, err := st.BoardsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list boards: %v", err)
	}
	if len(boards) != 1 || boards[0].ID != board.ID {
		t.Fatalf("boards = %+v, want the created board", boards)
	}

	role, err := st.BoardRole(ctx, board.ID, user.ID)
	if err != nil {
		t.Fatalf("board role: %v", err)
	}
	if role != RoleOwner {
		t.Fatalf("board role = %q, want owner", role)
	}

	other, _ := st.CreateUser(ctx, "stranger", "hash")
	if _, err := st.BoardRole(ctx, board.ID, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger role err = %v, want ErrNotFound", err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, _ := st.CreateUser(ctx, "owner", "hash")
	board, _ := st.CreateBoard(ctx, user.ID, "home")

	first, err := st.AddTask(ctx, board.ID, user.ID, "first")
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	if _, err := st.AddTask(ctx, board.ID, user.ID, "second"); err != nil {
		t.Fatalf("add task: %v", err)
	}

	active, done, err := st.TasksForBoard(ctx, board.ID)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if len(active) != 2 || len(done) != 0 {
		t.Fatalf("active=%d done=%d, want 2/0", len(active), len(done))
	}

	winner, found, err := st.SpinTask(ctx, board.ID)
	if err != nil || !found {
		t.Fatalf("spin: found=%v err=%v", found, err)
	}
	if winner.DoneAt == nil {
		t.Fatal("winner should be marked done")
	}

	active, done, _ = st.TasksForBoard(ctx, board.ID)
	if len(active) != 1 || len(done) != 1 {
		t.Fatalf("after spin active=%d done=%d, want 1/1", len(active), len(done))
	}

	if ok, err := st.RestoreTask(ctx, board.ID, first.ID); err != nil || !ok {
		t.Fatalf("restore: ok=%v err=%v", ok, err)
	}
	active, done, _ = st.TasksForBoard(ctx, board.ID)
	if len(active) != 2 || len(done) != 0 {
		t.Fatalf("after restore active=%d done=%d, want 2/0", len(active), len(done))
	}

	if ok, err := st.DeleteTask(ctx, board.ID, first.ID); err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
	if ok, _ := st.DeleteTask(ctx, "wrong-board", first.ID); ok {
		t.Fatal("delete should be scoped to the board")
	}
}

func TestSpinIsAtomicUnderConcurrency(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, _ := st.CreateUser(ctx, "owner", "hash")
	board, _ := st.CreateBoard(ctx, user.ID, "home")

	const taskCount = 5
	for i := 0; i < taskCount; i++ {
		if _, err := st.AddTask(ctx, board.ID, user.ID, "task"); err != nil {
			t.Fatalf("add task: %v", err)
		}
	}

	type result struct {
		task  Task
		found bool
		err   error
	}
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, found, err := st.SpinTask(ctx, board.ID)
			results <- result{task, found, err}
		}()
	}
	wg.Wait()
	close(results)

	seen := map[string]bool{}
	successes := 0
	for res := range results {
		if res.err != nil {
			t.Fatalf("spin error: %v", res.err)
		}
		if !res.found {
			continue
		}
		successes++
		if seen[res.task.ID] {
			t.Fatalf("task %s returned twice", res.task.ID)
		}
		seen[res.task.ID] = true
	}

	if successes != taskCount {
		t.Fatalf("successful spins = %d, want %d", successes, taskCount)
	}

	active, done, _ := st.TasksForBoard(ctx, board.ID)
	if len(active) != 0 || len(done) != taskCount {
		t.Fatalf("active=%d done=%d, want 0/%d", len(active), len(done), taskCount)
	}
}
