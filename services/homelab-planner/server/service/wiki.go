package service

import (
	"database/sql"
	"fmt"
	"strings"
)

// Page is a wiki article.
type Page struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Position  int64  `json:"position"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// PageSummary is returned in list views (no body).
type PageSummary struct {
	ID       int64  `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Position int64  `json:"position"`
}

// JournalEntry is a dated blog post.
type JournalEntry struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// JournalSummary is returned in list views (no body).
type JournalSummary struct {
	ID    int64  `json:"id"`
	Date  string `json:"date"`
	Title string `json:"title"`
}

// --- Pages -------------------------------------------------------------------

func (s *Service) ListPages() ([]PageSummary, error) {
	rows, err := s.DB.Query(`
		SELECT id, slug, title, position FROM pages ORDER BY position ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []PageSummary
	for rows.Next() {
		var p PageSummary
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Position); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func (s *Service) GetPage(slug string) (*Page, error) {
	var p Page
	err := s.DB.QueryRow(`
		SELECT id, slug, title, body, position, created_at, updated_at
		FROM pages WHERE slug = ?
	`, slug).Scan(&p.ID, &p.Slug, &p.Title, &p.Body, &p.Position, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("page %q: %w", slug, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

type CreatePageInput struct {
	Slug  string
	Title string
	Body  string
}

func (s *Service) CreatePage(in CreatePageInput) (*Page, error) {
	slug := strings.TrimSpace(in.Slug)
	title := strings.TrimSpace(in.Title)
	if slug == "" {
		return nil, fmt.Errorf("slug is required: %w", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("title is required: %w", ErrInvalidInput)
	}

	var maxPos int64
	s.DB.QueryRow(`SELECT COALESCE(MAX(position), -1) FROM pages`).Scan(&maxPos)

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO pages (slug, title, body, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, slug, title, in.Body, maxPos+1, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Page{
		ID: id, Slug: slug, Title: title, Body: in.Body,
		Position: maxPos + 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

type UpdatePageInput struct {
	Title *string
	Body  *string
}

func (s *Service) UpdatePage(slug string, in UpdatePageInput) (*Page, error) {
	p, err := s.GetPage(slug)
	if err != nil {
		return nil, err
	}

	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, fmt.Errorf("title cannot be empty: %w", ErrInvalidInput)
		}
		p.Title = t
	}
	if in.Body != nil {
		p.Body = *in.Body
	}

	now := s.now()
	_, err = s.DB.Exec(`
		UPDATE pages SET title = ?, body = ?, updated_at = ? WHERE slug = ?
	`, p.Title, p.Body, now, slug)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = now
	return p, nil
}

// --- Journal -----------------------------------------------------------------

func (s *Service) ListJournal() ([]JournalSummary, error) {
	rows, err := s.DB.Query(`
		SELECT id, date, title FROM journal_entries ORDER BY date DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []JournalSummary
	for rows.Next() {
		var e JournalSummary
		if err := rows.Scan(&e.ID, &e.Date, &e.Title); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Service) GetJournalEntry(id int64) (*JournalEntry, error) {
	var e JournalEntry
	err := s.DB.QueryRow(`
		SELECT id, date, title, body, created_at, updated_at
		FROM journal_entries WHERE id = ?
	`, id).Scan(&e.ID, &e.Date, &e.Title, &e.Body, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("journal entry %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

type CreateJournalInput struct {
	Date  string
	Title string
	Body  string
}

func (s *Service) CreateJournalEntry(in CreateJournalInput) (*JournalEntry, error) {
	date := strings.TrimSpace(in.Date)
	title := strings.TrimSpace(in.Title)
	if date == "" {
		return nil, fmt.Errorf("date is required: %w", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("title is required: %w", ErrInvalidInput)
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO journal_entries (date, title, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, date, title, in.Body, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &JournalEntry{
		ID: id, Date: date, Title: title, Body: in.Body,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

type UpdateJournalInput struct {
	Date  *string
	Title *string
	Body  *string
}

func (s *Service) UpdateJournalEntry(id int64, in UpdateJournalInput) (*JournalEntry, error) {
	e, err := s.GetJournalEntry(id)
	if err != nil {
		return nil, err
	}

	if in.Date != nil {
		d := strings.TrimSpace(*in.Date)
		if d == "" {
			return nil, fmt.Errorf("date cannot be empty: %w", ErrInvalidInput)
		}
		e.Date = d
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, fmt.Errorf("title cannot be empty: %w", ErrInvalidInput)
		}
		e.Title = t
	}
	if in.Body != nil {
		e.Body = *in.Body
	}

	now := s.now()
	_, err = s.DB.Exec(`
		UPDATE journal_entries SET date = ?, title = ?, body = ?, updated_at = ? WHERE id = ?
	`, e.Date, e.Title, e.Body, now, id)
	if err != nil {
		return nil, err
	}
	e.UpdatedAt = now
	return e, nil
}

func (s *Service) DeleteJournalEntry(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM journal_entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("journal entry %d: %w", id, ErrNotFound)
	}
	return nil
}
