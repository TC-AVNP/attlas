package service

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Service is the business layer. It owns the *sql.DB and exposes
// methods that REST and MCP handlers both use. No transport concerns
// live here; the error sentinels in types.go let callers map failures
// to their preferred shape.
type Service struct {
	DB  *sql.DB
	Now func() time.Time // overridable for tests
}

// New constructs a Service that uses time.Now for timestamps.
func New(db *sql.DB) *Service {
	return &Service{DB: db, Now: time.Now}
}

func (s *Service) now() int64 { return s.Now().Unix() }

// --- Projects ----------------------------------------------------------

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a human name into a URL-safe slug. We keep it small:
// lowercase, ascii letters and digits, collapse everything else to a
// single hyphen, trim hyphens from the ends. Collisions are handled by
// appending -2, -3, ... in CreateProject.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "project"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// deriveColor produces a stable hex color from a slug. Hash the slug,
// take the first three bytes as RGB, and clamp the brightness so threads
// pop against the dark canvas background.
func deriveColor(slug string) string {
	h := sha1.Sum([]byte(slug))
	r := int(h[0])
	g := int(h[1])
	b := int(h[2])
	// Boost dark channels so every color reads clearly on #0a0e1a.
	boost := func(c int) int {
		if c < 80 {
			c += 80
		}
		if c > 220 {
			c -= 20
		}
		return c
	}
	return "#" + hex.EncodeToString([]byte{
		byte(boost(r)), byte(boost(g)), byte(boost(b)),
	})
}

// ListProjects returns every project (optionally including archived
// ones) with its feature counts and total effort minutes.
func (s *Service) ListProjects(includeArchived bool) ([]Project, error) {
	where := "WHERE p.archived_at IS NULL"
	if includeArchived {
		where = ""
	}
	rows, err := s.DB.Query(`
		SELECT p.id, p.slug, p.name, p.problem, p.description, p.priority,
		       p.stage, p.interest, p.color, p.created_at, p.archived_at,
		       p.repo_path, p.canvas_x, p.canvas_y
		FROM projects p
		` + where + `
		ORDER BY p.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.Name, &p.Problem, &p.Description, &p.Priority,
			&p.Stage, &p.Interest, &p.Color, &p.CreatedAt, &p.ArchivedAt,
			&p.RepoPath, &p.CanvasX, &p.CanvasY,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch aggregates for every project we loaded. One pass each over
	// features + effort_logs is fine for the scale this service targets
	// (tens of projects, hundreds of features).
	counts, err := s.featureCounts()
	if err != nil {
		return nil, err
	}
	effort, err := s.effortTotals()
	if err != nil {
		return nil, err
	}
	for i := range projects {
		projects[i].FeatureCounts = counts[projects[i].ID]
		projects[i].TotalMinutes = effort[projects[i].ID]
	}
	return projects, nil
}

// featureCounts returns { project_id: { status: count } } for every
// project that has at least one feature. Missing projects implicitly
// have zero features.
func (s *Service) featureCounts() (map[int64]map[Status]int, error) {
	rows, err := s.DB.Query(`
		SELECT project_id, status, COUNT(*)
		FROM features
		GROUP BY project_id, status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]map[Status]int)
	for rows.Next() {
		var pid int64
		var status Status
		var n int
		if err := rows.Scan(&pid, &status, &n); err != nil {
			return nil, err
		}
		m, ok := out[pid]
		if !ok {
			m = make(map[Status]int)
			out[pid] = m
		}
		m[status] = n
	}
	return out, rows.Err()
}

// effortTotals returns { project_id: total_minutes } for every project
// that has at least one effort log entry.
func (s *Service) effortTotals() (map[int64]int64, error) {
	rows, err := s.DB.Query(`
		SELECT project_id, COALESCE(SUM(minutes), 0)
		FROM effort_logs
		GROUP BY project_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]int64)
	for rows.Next() {
		var pid, mins int64
		if err := rows.Scan(&pid, &mins); err != nil {
			return nil, err
		}
		out[pid] = mins
	}
	return out, rows.Err()
}

// GetProject fetches a project by slug plus its features and effort log.
func (s *Service) GetProject(slug string) (*ProjectDetail, error) {
	var p Project
	err := s.DB.QueryRow(`
		SELECT id, slug, name, problem, description, priority, stage, interest,
		       color, created_at, archived_at, repo_path, canvas_x, canvas_y
		FROM projects
		WHERE slug = ?
	`, slug).Scan(
		&p.ID, &p.Slug, &p.Name, &p.Problem, &p.Description, &p.Priority,
		&p.Stage, &p.Interest, &p.Color, &p.CreatedAt, &p.ArchivedAt,
		&p.RepoPath, &p.CanvasX, &p.CanvasY,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	features, err := s.featuresFor(p.ID)
	if err != nil {
		return nil, err
	}
	effort, err := s.effortFor(p.ID)
	if err != nil {
		return nil, err
	}

	// Compute aggregates inline so the detail response carries the same
	// shape as list responses.
	counts := make(map[Status]int)
	for _, f := range features {
		counts[f.Status]++
	}
	var total int64
	for _, e := range effort {
		total += e.Minutes
	}
	p.FeatureCounts = counts
	p.TotalMinutes = total

	repos, err := s.gitReposFor(p.ID)
	if err != nil {
		return nil, err
	}

	return &ProjectDetail{Project: p, Features: features, Effort: effort, GitRepos: repos}, nil
}

func (s *Service) featuresFor(projectID int64) ([]Feature, error) {
	rows, err := s.DB.Query(`
		SELECT id, project_id, title, description, status,
		       created_at, started_at, completed_at, dropped_at
		FROM features
		WHERE project_id = ?
		ORDER BY created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feature
	for rows.Next() {
		var f Feature
		if err := rows.Scan(
			&f.ID, &f.ProjectID, &f.Title, &f.Description, &f.Status,
			&f.CreatedAt, &f.StartedAt, &f.CompletedAt, &f.DroppedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) effortFor(projectID int64) ([]EffortLog, error) {
	rows, err := s.DB.Query(`
		SELECT id, project_id, feature_id, minutes, note, logged_at
		FROM effort_logs
		WHERE project_id = ?
		ORDER BY logged_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffortLog
	for rows.Next() {
		var e EffortLog
		if err := rows.Scan(
			&e.ID, &e.ProjectID, &e.FeatureID, &e.Minutes, &e.Note, &e.LoggedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateProject validates input, generates a unique slug, derives a
// color if not supplied, and inserts. Rejects empty problem statements.
func (s *Service) CreateProject(in CreateProjectInput) (*Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Problem = strings.TrimSpace(in.Problem)
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if in.Problem == "" {
		return nil, fmt.Errorf("%w: problem is required", ErrInvalidInput)
	}
	if !ValidPriority(in.Priority) {
		return nil, fmt.Errorf("%w: priority must be high/medium/low", ErrInvalidInput)
	}

	base := slugify(in.Name)
	slug := base
	for i := 2; ; i++ {
		var exists int
		err := s.DB.QueryRow(
			`SELECT COUNT(*) FROM projects WHERE slug = ?`, slug,
		).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if exists == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}

	color := deriveColor(slug)
	if in.Color != nil && *in.Color != "" {
		color = *in.Color
	}

	stage := StageIdea
	if in.Stage != nil && ValidStage(*in.Stage) {
		stage = *in.Stage
	}
	interest := InterestMeh
	if in.Interest != nil && ValidInterest(*in.Interest) {
		interest = *in.Interest
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO projects (slug, name, problem, description, priority, stage, interest, color, repo_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, slug, in.Name, in.Problem, in.Description, in.Priority, stage, interest, color, in.RepoPath, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Project{
		ID:          id,
		Slug:        slug,
		Name:        in.Name,
		Problem:     in.Problem,
		Description: in.Description,
		Priority:    in.Priority,
		Stage:       stage,
		Interest:    interest,
		Color:       color,
		RepoPath:    in.RepoPath,
		CreatedAt:   now,
	}, nil
}

// UpdateProject applies only the non-nil fields of `in`. Returns the
// updated project (via a subsequent GetProject) so callers can respond
// with a full, consistent row.
func (s *Service) UpdateProject(slug string, in UpdateProjectInput) (*ProjectDetail, error) {
	// Build SET clause dynamically from non-nil fields.
	sets := make([]string, 0, 8)
	args := make([]any, 0, 8)

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name cannot be blank", ErrInvalidInput)
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if in.Problem != nil {
		problem := strings.TrimSpace(*in.Problem)
		if problem == "" {
			return nil, fmt.Errorf("%w: problem cannot be blank", ErrInvalidInput)
		}
		sets = append(sets, "problem = ?")
		args = append(args, problem)
	}
	if in.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *in.Description)
	}
	if in.Priority != nil {
		if !ValidPriority(*in.Priority) {
			return nil, fmt.Errorf("%w: priority must be high/medium/low", ErrInvalidInput)
		}
		sets = append(sets, "priority = ?")
		args = append(args, *in.Priority)
	}
	if in.Stage != nil {
		if !ValidStage(*in.Stage) {
			return nil, fmt.Errorf("%w: stage must be idea/live/completed", ErrInvalidInput)
		}
		sets = append(sets, "stage = ?")
		args = append(args, *in.Stage)
	}
	if in.Interest != nil {
		if !ValidInterest(*in.Interest) {
			return nil, fmt.Errorf("%w: interest must be excited/meh/bored", ErrInvalidInput)
		}
		sets = append(sets, "interest = ?")
		args = append(args, *in.Interest)
	}
	if in.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *in.Color)
	}
	if in.RepoPath != nil {
		sets = append(sets, "repo_path = ?")
		args = append(args, *in.RepoPath)
	}
	if in.CanvasX != nil {
		sets = append(sets, "canvas_x = ?")
		args = append(args, *in.CanvasX)
	}
	if in.CanvasY != nil {
		sets = append(sets, "canvas_y = ?")
		args = append(args, *in.CanvasY)
	}
	if in.Archived != nil {
		if *in.Archived {
			sets = append(sets, "archived_at = ?")
			args = append(args, s.now())
		} else {
			sets = append(sets, "archived_at = NULL")
		}
	}

	if len(sets) == 0 {
		// Nothing to update — return the current state.
		return s.GetProject(slug)
	}

	args = append(args, slug)
	query := fmt.Sprintf(
		`UPDATE projects SET %s WHERE slug = ?`,
		strings.Join(sets, ", "),
	)
	res, err := s.DB.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetProject(slug)
}

// DeleteProject either soft-deletes (archived_at = now) or hard-deletes
// depending on hard. Hard delete cascades to features and effort_logs.
func (s *Service) DeleteProject(slug string, hard bool) error {
	if hard {
		res, err := s.DB.Exec(`DELETE FROM projects WHERE slug = ?`, slug)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}
	res, err := s.DB.Exec(
		`UPDATE projects SET archived_at = ? WHERE slug = ? AND archived_at IS NULL`,
		s.now(), slug,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Either the project doesn't exist or was already archived.
		// Differentiate so callers can treat archived as idempotent.
		var exists int
		s.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE slug = ?`, slug).Scan(&exists)
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// --- Features ----------------------------------------------------------

// CreateFeature adds a new feature in the backlog state under the
// specified project (looked up by slug).
func (s *Service) CreateFeature(projectSlug string, in CreateFeatureInput) (*Feature, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	var pid int64
	err := s.DB.QueryRow(
		`SELECT id FROM projects WHERE slug = ?`, projectSlug,
	).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO features (project_id, title, description, status, created_at)
		VALUES (?, ?, ?, 'backlog', ?)
	`, pid, in.Title, in.Description, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Feature{
		ID:          id,
		ProjectID:   pid,
		Title:       in.Title,
		Description: in.Description,
		Status:      StatusBacklog,
		CreatedAt:   now,
	}, nil
}

// UpdateFeature applies non-nil fields. Status transitions auto-set the
// matching timestamp column (started_at, completed_at, dropped_at).
// Transitioning back to backlog from a terminal state clears those.
func (s *Service) UpdateFeature(id int64, in UpdateFeatureInput) (*Feature, error) {
	// Load the current feature so we can reason about the transition.
	var f Feature
	err := s.DB.QueryRow(`
		SELECT id, project_id, title, description, status,
		       created_at, started_at, completed_at, dropped_at
		FROM features WHERE id = ?
	`, id).Scan(
		&f.ID, &f.ProjectID, &f.Title, &f.Description, &f.Status,
		&f.CreatedAt, &f.StartedAt, &f.CompletedAt, &f.DroppedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 6)
	args := make([]any, 0, 6)

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: title cannot be blank", ErrInvalidInput)
		}
		sets = append(sets, "title = ?")
		args = append(args, title)
	}
	if in.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *in.Description)
	}
	if in.Status != nil {
		next := *in.Status
		if !ValidStatus(next) {
			return nil, fmt.Errorf("%w: status must be backlog/in_progress/done/dropped", ErrInvalidInput)
		}
		if next != f.Status {
			sets = append(sets, "status = ?")
			args = append(args, next)
			now := s.now()
			switch next {
			case StatusInProgress:
				// Record the first time this feature started, but leave
				// it alone on subsequent re-entries so cycle time stays
				// honest to the original start.
				if f.StartedAt == nil {
					sets = append(sets, "started_at = ?")
					args = append(args, now)
				}
				// Clear terminal timestamps in case we're re-opening.
				sets = append(sets, "completed_at = NULL", "dropped_at = NULL")
			case StatusDone:
				if f.StartedAt == nil {
					sets = append(sets, "started_at = ?")
					args = append(args, now)
				}
				sets = append(sets, "completed_at = ?", "dropped_at = NULL")
				args = append(args, now)
			case StatusDropped:
				sets = append(sets, "dropped_at = ?", "completed_at = NULL")
				args = append(args, now)
			case StatusBacklog:
				sets = append(sets, "started_at = NULL", "completed_at = NULL", "dropped_at = NULL")
			}
		}
	}

	if len(sets) == 0 {
		return &f, nil
	}

	args = append(args, id)
	query := fmt.Sprintf(
		`UPDATE features SET %s WHERE id = ?`,
		strings.Join(sets, ", "),
	)
	if _, err := s.DB.Exec(query, args...); err != nil {
		return nil, err
	}

	// Return the fresh row so callers always see canonical state.
	err = s.DB.QueryRow(`
		SELECT id, project_id, title, description, status,
		       created_at, started_at, completed_at, dropped_at
		FROM features WHERE id = ?
	`, id).Scan(
		&f.ID, &f.ProjectID, &f.Title, &f.Description, &f.Status,
		&f.CreatedAt, &f.StartedAt, &f.CompletedAt, &f.DroppedAt,
	)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFeature removes a feature outright. Effort logs referencing it
// have their feature_id set to NULL via ON DELETE SET NULL.
func (s *Service) DeleteFeature(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM features WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Effort ------------------------------------------------------------

// LogEffort records a chunk of work against a project, optionally tied
// to a specific feature. Minutes must be positive.
func (s *Service) LogEffort(projectSlug string, in LogEffortInput) (*EffortLog, error) {
	if in.Minutes <= 0 {
		return nil, fmt.Errorf("%w: minutes must be positive", ErrInvalidInput)
	}
	var pid int64
	err := s.DB.QueryRow(
		`SELECT id FROM projects WHERE slug = ?`, projectSlug,
	).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// If a feature_id was provided, ensure it belongs to the same project.
	if in.FeatureID != nil {
		var owner int64
		err := s.DB.QueryRow(
			`SELECT project_id FROM features WHERE id = ?`, *in.FeatureID,
		).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: feature %d not found", ErrInvalidInput, *in.FeatureID)
		}
		if err != nil {
			return nil, err
		}
		if owner != pid {
			return nil, fmt.Errorf("%w: feature does not belong to project", ErrInvalidInput)
		}
	}
	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO effort_logs (project_id, feature_id, minutes, note, logged_at)
		VALUES (?, ?, ?, ?, ?)
	`, pid, in.FeatureID, in.Minutes, in.Note, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &EffortLog{
		ID:        id,
		ProjectID: pid,
		FeatureID: in.FeatureID,
		Minutes:   in.Minutes,
		Note:      in.Note,
		LoggedAt:  now,
	}, nil
}
