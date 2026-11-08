package store

import (
	"context"
	"database/sql"
)

// Task is an item on a board.
type Task struct {
	ID        string  `json:"id"`
	BoardID   string  `json:"board_id"`
	Text      string  `json:"text"`
	CreatedBy string  `json:"created_by,omitempty"`
	DoneAt    *string `json:"done_at"`
	CreatedAt string  `json:"created_at"`
}

const taskColumns = `id, board_id, text, COALESCE(created_by, ''), done_at, created_at`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var done sql.NullString
	if err := row.Scan(&t.ID, &t.BoardID, &t.Text, &t.CreatedBy, &done, &t.CreatedAt); err != nil {
		return Task{}, err
	}
	if done.Valid {
		t.DoneAt = &done.String
	}
	return t, nil
}

// AddTask inserts a task at the top of the board.
func (s *Store) AddTask(ctx context.Context, boardID, userID, text string) (Task, error) {
	t := Task{
		ID:        randomID(),
		BoardID:   boardID,
		Text:      text,
		CreatedBy: userID,
		CreatedAt: now(),
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO tasks (id, board_id, text, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.BoardID, t.Text, t.CreatedBy, t.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

// TasksForBoard returns active tasks newest first and completed tasks most
// recently done first.
func (s *Store) TasksForBoard(ctx context.Context, boardID string) (active, done []Task, err error) {
	active, err = s.queryTasks(ctx,
		`SELECT `+taskColumns+` FROM tasks
		  WHERE board_id = ? AND done_at IS NULL
		  ORDER BY created_at DESC`, boardID)
	if err != nil {
		return nil, nil, err
	}

	done, err = s.queryTasks(ctx,
		`SELECT `+taskColumns+` FROM tasks
		  WHERE board_id = ? AND done_at IS NOT NULL
		  ORDER BY done_at DESC, created_at DESC`, boardID)
	if err != nil {
		return nil, nil, err
	}

	return active, done, nil
}

func (s *Store) queryTasks(ctx context.Context, query, boardID string) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx, query, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// DeleteTask removes a task scoped to its board. It reports whether a row
// matched.
func (s *Store) DeleteTask(ctx context.Context, boardID, taskID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND board_id = ?`, taskID, boardID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RestoreTask clears done_at, moving a task back to the active list.
func (s *Store) RestoreTask(ctx context.Context, boardID, taskID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE tasks SET done_at = NULL WHERE id = ? AND board_id = ?`, taskID, boardID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SpinTask marks one random active task done and returns it. It reports false
// when the board has no active tasks. The select-and-update is a single
// statement so concurrent spins can never return the same task.
func (s *Store) SpinTask(ctx context.Context, boardID string) (Task, bool, error) {
	row := s.DB.QueryRowContext(ctx,
		`UPDATE tasks SET done_at = ?
		  WHERE id = (
		        SELECT id FROM tasks
		         WHERE board_id = ? AND done_at IS NULL
		         ORDER BY RANDOM() LIMIT 1
		  )
		 RETURNING `+taskColumns,
		now(), boardID)

	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return t, true, nil
}
