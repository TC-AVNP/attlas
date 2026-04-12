package service

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Service {
	return &Service{DB: db, Now: time.Now}
}

func (s *Service) now() int64 { return s.Now().Unix() }

// --- Steps -----------------------------------------------------------------

func (s *Service) ListSteps() ([]Step, error) {
	rows, err := s.DB.Query(`
		SELECT s.id, s.title, s.description, s.position, s.category, s.total_budget_cents, s.created_at, s.completed_at,
		       COALESCE((SELECT COUNT(*) FROM checklist_items WHERE step_id = s.id), 0),
		       COALESCE((SELECT COUNT(*) FROM checklist_items WHERE step_id = s.id AND status = 'arrived'), 0),
		       COALESCE((SELECT SUM(COALESCE(budget_cents, 0)) FROM checklist_items WHERE step_id = s.id), 0),
		       COALESCE((SELECT SUM(COALESCE(actual_cost_cents, 0)) FROM checklist_items WHERE step_id = s.id), 0)
		FROM steps s
		ORDER BY s.position ASC, s.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []Step
	for rows.Next() {
		var st Step
		if err := rows.Scan(
			&st.ID, &st.Title, &st.Description, &st.Position, &st.Category, &st.TotalBudgetCents, &st.CreatedAt, &st.CompletedAt,
			&st.ItemCount, &st.ArrivedCount, &st.BudgetCents, &st.ActualCents,
		); err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	return steps, rows.Err()
}

func (s *Service) GetStep(id int64) (*StepDetail, error) {
	var st Step
	err := s.DB.QueryRow(`
		SELECT s.id, s.title, s.description, s.position, s.category, s.total_budget_cents, s.created_at, s.completed_at,
		       COALESCE((SELECT COUNT(*) FROM checklist_items WHERE step_id = s.id), 0),
		       COALESCE((SELECT COUNT(*) FROM checklist_items WHERE step_id = s.id AND status = 'arrived'), 0),
		       COALESCE((SELECT SUM(COALESCE(budget_cents, 0)) FROM checklist_items WHERE step_id = s.id), 0),
		       COALESCE((SELECT SUM(COALESCE(actual_cost_cents, 0)) FROM checklist_items WHERE step_id = s.id), 0)
		FROM steps s WHERE s.id = ?
	`, id).Scan(
		&st.ID, &st.Title, &st.Description, &st.Position, &st.TotalBudgetCents, &st.CreatedAt, &st.CompletedAt,
		&st.ItemCount, &st.ArrivedCount, &st.BudgetCents, &st.ActualCents,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("step %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	detail := &StepDetail{Step: st}

	// Load checklist items with their options
	items, err := s.listItems(id)
	if err != nil {
		return nil, err
	}
	detail.Items = items

	// Load build log
	logRows, err := s.DB.Query(`
		SELECT id, step_id, body, created_at
		FROM build_log_entries WHERE step_id = ?
		ORDER BY created_at DESC
	`, id)
	if err != nil {
		return nil, err
	}
	defer logRows.Close()

	for logRows.Next() {
		var entry BuildLogEntry
		if err := logRows.Scan(&entry.ID, &entry.StepID, &entry.Body, &entry.CreatedAt); err != nil {
			return nil, err
		}
		detail.BuildLog = append(detail.BuildLog, entry)
	}
	if err := logRows.Err(); err != nil {
		return nil, err
	}

	return detail, nil
}

func (s *Service) listItems(stepID int64) ([]ChecklistItem, error) {
	rows, err := s.DB.Query(`
		SELECT id, step_id, name, group_name, budget_cents, actual_cost_cents, status, selected_option_id, created_at
		FROM checklist_items WHERE step_id = ?
		ORDER BY group_name ASC, created_at ASC
	`, stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ChecklistItem
	for rows.Next() {
		var it ChecklistItem
		if err := rows.Scan(
			&it.ID, &it.StepID, &it.Name, &it.GroupName, &it.BudgetCents, &it.ActualCostCents,
			&it.Status, &it.SelectedOptionID, &it.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load options for each item
	for i := range items {
		opts, err := s.listOptions(items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Options = opts
	}
	return items, nil
}

func (s *Service) listOptions(itemID int64) ([]ItemOption, error) {
	rows, err := s.DB.Query(`
		SELECT id, item_id, name, url, price_cents, notes, created_at
		FROM item_options WHERE item_id = ?
		ORDER BY created_at ASC
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var opts []ItemOption
	for rows.Next() {
		var o ItemOption
		if err := rows.Scan(&o.ID, &o.ItemID, &o.Name, &o.URL, &o.PriceCents, &o.Notes, &o.CreatedAt); err != nil {
			return nil, err
		}
		opts = append(opts, o)
	}
	return opts, rows.Err()
}

func (s *Service) CreateStep(in CreateStepInput) (*Step, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required: %w", ErrInvalidInput)
	}

	// Position: put it at the end
	var maxPos int64
	s.DB.QueryRow(`SELECT COALESCE(MAX(position), -1) FROM steps`).Scan(&maxPos)

	now := s.now()
	cat := in.Category
	if cat == "" {
		cat = CategoryExecuting
	}
	if !ValidStepCategory(cat) {
		return nil, fmt.Errorf("invalid category %q: %w", cat, ErrInvalidInput)
	}

	res, err := s.DB.Exec(`
		INSERT INTO steps (title, description, position, category, total_budget_cents, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, title, in.Description, maxPos+1, cat, in.TotalBudgetCents, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Step{
		ID: id, Title: title, Description: in.Description,
		Position: maxPos + 1, Category: cat, TotalBudgetCents: in.TotalBudgetCents, CreatedAt: now,
	}, nil
}

func (s *Service) UpdateStep(id int64, in UpdateStepInput) (*Step, error) {
	st, err := s.getStepRow(id)
	if err != nil {
		return nil, err
	}

	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, fmt.Errorf("title cannot be empty: %w", ErrInvalidInput)
		}
		st.Title = t
	}
	if in.Description != nil {
		st.Description = *in.Description
	}
	if in.Position != nil {
		st.Position = *in.Position
	}
	if in.Category != nil {
		if !ValidStepCategory(*in.Category) {
			return nil, fmt.Errorf("invalid category %q: %w", *in.Category, ErrInvalidInput)
		}
		st.Category = *in.Category
	}
	if in.TotalBudgetCents != nil {
		st.TotalBudgetCents = in.TotalBudgetCents
	}
	if in.Completed != nil {
		if *in.Completed {
			now := s.now()
			st.CompletedAt = &now
		} else {
			st.CompletedAt = nil
		}
	}

	_, err = s.DB.Exec(`
		UPDATE steps SET title = ?, description = ?, position = ?, category = ?, total_budget_cents = ?, completed_at = ?
		WHERE id = ?
	`, st.Title, st.Description, st.Position, st.Category, st.TotalBudgetCents, st.CompletedAt, id)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Service) DeleteStep(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM steps WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("step %d: %w", id, ErrNotFound)
	}
	return nil
}

func (s *Service) getStepRow(id int64) (*Step, error) {
	var st Step
	err := s.DB.QueryRow(`
		SELECT id, title, description, position, category, total_budget_cents, created_at, completed_at
		FROM steps WHERE id = ?
	`, id).Scan(&st.ID, &st.Title, &st.Description, &st.Position, &st.Category, &st.TotalBudgetCents, &st.CreatedAt, &st.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("step %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// --- Checklist Items -------------------------------------------------------

func (s *Service) CreateItem(stepID int64, in CreateItemInput) (*ChecklistItem, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required: %w", ErrInvalidInput)
	}
	// Verify step exists
	if _, err := s.getStepRow(stepID); err != nil {
		return nil, err
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO checklist_items (step_id, name, group_name, budget_cents, status, created_at)
		VALUES (?, ?, ?, ?, 'researching', ?)
	`, stepID, name, in.GroupName, in.BudgetCents, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ChecklistItem{
		ID: id, StepID: stepID, Name: name, GroupName: in.GroupName,
		BudgetCents: in.BudgetCents, Status: StatusResearching, CreatedAt: now,
	}, nil
}

func (s *Service) UpdateItem(id int64, in UpdateItemInput) (*ChecklistItem, error) {
	var it ChecklistItem
	err := s.DB.QueryRow(`
		SELECT id, step_id, name, group_name, budget_cents, actual_cost_cents, status, selected_option_id, created_at
		FROM checklist_items WHERE id = ?
	`, id).Scan(&it.ID, &it.StepID, &it.Name, &it.GroupName, &it.BudgetCents, &it.ActualCostCents,
		&it.Status, &it.SelectedOptionID, &it.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, fmt.Errorf("name cannot be empty: %w", ErrInvalidInput)
		}
		it.Name = n
	}
	if in.GroupName != nil {
		it.GroupName = *in.GroupName
	}
	if in.BudgetCents != nil {
		it.BudgetCents = in.BudgetCents
	}
	if in.ActualCostCents != nil {
		it.ActualCostCents = in.ActualCostCents
	}
	if in.Status != nil {
		if !ValidItemStatus(*in.Status) {
			return nil, fmt.Errorf("invalid status %q: %w", *in.Status, ErrInvalidInput)
		}
		it.Status = *in.Status
	}
	if in.SelectedOptionID != nil {
		it.SelectedOptionID = in.SelectedOptionID
	}

	_, err = s.DB.Exec(`
		UPDATE checklist_items
		SET name = ?, group_name = ?, budget_cents = ?, actual_cost_cents = ?, status = ?, selected_option_id = ?
		WHERE id = ?
	`, it.Name, it.GroupName, it.BudgetCents, it.ActualCostCents, it.Status, it.SelectedOptionID, id)
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (s *Service) DeleteItem(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM checklist_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("item %d: %w", id, ErrNotFound)
	}
	return nil
}

// --- Item Options ----------------------------------------------------------

func (s *Service) CreateOption(itemID int64, in CreateOptionInput) (*ItemOption, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required: %w", ErrInvalidInput)
	}
	// Verify item exists
	var exists bool
	s.DB.QueryRow(`SELECT 1 FROM checklist_items WHERE id = ?`, itemID).Scan(&exists)
	if !exists {
		return nil, fmt.Errorf("item %d: %w", itemID, ErrNotFound)
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO item_options (item_id, name, url, price_cents, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, itemID, name, in.URL, in.PriceCents, in.Notes, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ItemOption{
		ID: id, ItemID: itemID, Name: name, URL: in.URL,
		PriceCents: in.PriceCents, Notes: in.Notes, CreatedAt: now,
	}, nil
}

func (s *Service) UpdateOption(id int64, in UpdateOptionInput) (*ItemOption, error) {
	var o ItemOption
	err := s.DB.QueryRow(`
		SELECT id, item_id, name, url, price_cents, notes, created_at
		FROM item_options WHERE id = ?
	`, id).Scan(&o.ID, &o.ItemID, &o.Name, &o.URL, &o.PriceCents, &o.Notes, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("option %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		o.Name = strings.TrimSpace(*in.Name)
	}
	if in.URL != nil {
		o.URL = *in.URL
	}
	if in.PriceCents != nil {
		o.PriceCents = in.PriceCents
	}
	if in.Notes != nil {
		o.Notes = *in.Notes
	}

	_, err = s.DB.Exec(`
		UPDATE item_options SET name = ?, url = ?, price_cents = ?, notes = ?
		WHERE id = ?
	`, o.Name, o.URL, o.PriceCents, o.Notes, id)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Service) DeleteOption(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM item_options WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("option %d: %w", id, ErrNotFound)
	}
	return nil
}

// --- Build Log -------------------------------------------------------------

func (s *Service) CreateLogEntry(stepID int64, in CreateLogEntryInput) (*BuildLogEntry, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, fmt.Errorf("body is required: %w", ErrInvalidInput)
	}
	if _, err := s.getStepRow(stepID); err != nil {
		return nil, err
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO build_log_entries (step_id, body, created_at) VALUES (?, ?, ?)
	`, stepID, body, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &BuildLogEntry{ID: id, StepID: stepID, Body: body, CreatedAt: now}, nil
}

func (s *Service) UpdateLogEntry(id int64, in UpdateLogEntryInput) (*BuildLogEntry, error) {
	var entry BuildLogEntry
	err := s.DB.QueryRow(`
		SELECT id, step_id, body, created_at FROM build_log_entries WHERE id = ?
	`, id).Scan(&entry.ID, &entry.StepID, &entry.Body, &entry.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("log entry %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	if in.Body != nil {
		entry.Body = strings.TrimSpace(*in.Body)
	}

	_, err = s.DB.Exec(`UPDATE build_log_entries SET body = ? WHERE id = ?`, entry.Body, id)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *Service) DeleteLogEntry(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM build_log_entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("log entry %d: %w", id, ErrNotFound)
	}
	return nil
}
