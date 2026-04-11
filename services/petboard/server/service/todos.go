package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Todo is a standalone reminder that isn't tied to any project. Used
// for cross-cutting chores like "refactor the foo package" that don't
// deserve their own project but shouldn't be lost either.
type Todo struct {
	ID          int64  `json:"id"`
	Text        string `json:"text"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt *int64 `json:"completed_at,omitempty"`
}

// CreateTodoInput is the body for POST /api/todos.
type CreateTodoInput struct {
	Text string
}

// UpdateTodoInput is the body for PATCH /api/todos/{id}.
// Either field may be nil-or-zero; only set fields are applied.
type UpdateTodoInput struct {
	Text      *string
	Completed *bool
}

// ListTodos returns all todos. If includeCompleted is false, only
// open (not completed) todos are returned. Newest first.
func (s *Service) ListTodos(includeCompleted bool) ([]Todo, error) {
	q := `SELECT id, text, created_at, completed_at FROM todos`
	if !includeCompleted {
		q += ` WHERE completed_at IS NULL`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.ID, &t.Text, &t.CreatedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) CreateTodo(in CreateTodoInput) (*Todo, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, fmt.Errorf("%w: text is required", ErrInvalidInput)
	}
	now := s.now()
	res, err := s.DB.Exec(
		`INSERT INTO todos(text, created_at) VALUES (?, ?)`,
		text, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Todo{ID: id, Text: text, CreatedAt: now}, nil
}

func (s *Service) UpdateTodo(id int64, in UpdateTodoInput) (*Todo, error) {
	// Load current row first so we can compute the right completed_at
	// transition (only set on the false→true edge, only clear on
	// true→false). This also gives us a clean ErrNotFound path.
	row := s.DB.QueryRow(
		`SELECT id, text, created_at, completed_at FROM todos WHERE id = ?`, id,
	)
	var t Todo
	if err := row.Scan(&t.ID, &t.Text, &t.CreatedAt, &t.CompletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if in.Text != nil {
		text := strings.TrimSpace(*in.Text)
		if text == "" {
			return nil, fmt.Errorf("%w: text is required", ErrInvalidInput)
		}
		t.Text = text
	}
	if in.Completed != nil {
		if *in.Completed {
			if t.CompletedAt == nil {
				now := s.now()
				t.CompletedAt = &now
			}
		} else {
			t.CompletedAt = nil
		}
	}

	if _, err := s.DB.Exec(
		`UPDATE todos SET text = ?, completed_at = ? WHERE id = ?`,
		t.Text, t.CompletedAt, id,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Service) DeleteTodo(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM todos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
