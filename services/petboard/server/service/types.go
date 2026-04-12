// Package service is the internal business layer that both the REST
// handlers and (later) the MCP tools delegate to. Keeping it separate
// from transport means there's only one place that knows how a project
// gets created, how a status transition updates timestamps, etc.
package service

import "errors"

// Priority is the canonical set of project priority levels.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// Status is the canonical set of feature statuses. Transitions between
// them auto-set the matching timestamp on the feature row.
type Status string

const (
	StatusBacklog    Status = "backlog"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusDropped    Status = "dropped"
)

// Project is the petboard row with computed aggregates the UI wants on
// the universe view.
type Project struct {
	ID          int64    `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Problem     string   `json:"problem"`
	Description *string  `json:"description,omitempty"`
	Priority    Priority `json:"priority"`
	Color       string   `json:"color"`
	CreatedAt   int64    `json:"created_at"`
	ArchivedAt  *int64   `json:"archived_at,omitempty"`
	RepoPath    *string  `json:"repo_path,omitempty"`
	CanvasX     *float64 `json:"canvas_x,omitempty"`
	CanvasY     *float64 `json:"canvas_y,omitempty"`

	// Aggregates — only populated on list/get responses.
	FeatureCounts map[Status]int `json:"feature_counts,omitempty"`
	TotalMinutes  int64          `json:"total_minutes"`
}

// Feature is a single backlog item under a project.
type Feature struct {
	ID          int64   `json:"id"`
	ProjectID   int64   `json:"project_id"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Status      Status  `json:"status"`
	CreatedAt   int64   `json:"created_at"`
	StartedAt   *int64  `json:"started_at,omitempty"`
	CompletedAt *int64  `json:"completed_at,omitempty"`
	DroppedAt   *int64  `json:"dropped_at,omitempty"`
}

// EffortLog records a chunk of work against a project (optionally tied
// to a specific feature).
type EffortLog struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"project_id"`
	FeatureID *int64  `json:"feature_id,omitempty"`
	Minutes   int64   `json:"minutes"`
	Note      *string `json:"note,omitempty"`
	LoggedAt  int64   `json:"logged_at"`
}

// GitRepo links a local git repository to a project for auto effort
// derivation from commit history.
type GitRepo struct {
	ID             int64   `json:"id"`
	ProjectID      int64   `json:"project_id"`
	RepoPath       string  `json:"repo_path"`
	AuthorFilter   *string `json:"author_filter,omitempty"`
	SessionGapMin  int64   `json:"session_gap_min"`
	FirstCommitMin int64   `json:"first_commit_min"`
	LastSyncedSHA  *string `json:"last_synced_sha,omitempty"`
	LastSyncedAt   *int64  `json:"last_synced_at,omitempty"`
	CreatedAt      int64   `json:"created_at"`
}

// ProjectDetail bundles a project with its features and recent effort
// log — what GET /api/projects/:slug returns and what MCP get_project
// returns.
type ProjectDetail struct {
	Project
	Features []Feature   `json:"features"`
	Effort   []EffortLog `json:"effort"`
	GitRepos []GitRepo   `json:"git_repos,omitempty"`
}

// CreateProjectInput captures the fields required to create a project.
// `Problem` is mandatory — validation is enforced in the service layer.
type CreateProjectInput struct {
	Name        string
	Problem     string
	Priority    Priority
	Description *string
	Color       *string // optional override; derived from slug if empty
	RepoPath    *string
}

// UpdateProjectInput holds the nullable fields accepted by PATCH. Only
// non-nil fields are applied.
type UpdateProjectInput struct {
	Name        *string
	Problem     *string
	Description *string
	Priority    *Priority
	Color       *string
	RepoPath    *string
	CanvasX     *float64
	CanvasY     *float64
	Archived    *bool
}

// CreateFeatureInput captures the fields required to create a feature.
type CreateFeatureInput struct {
	Title       string
	Description *string
}

// UpdateFeatureInput is what PATCH /api/features/:id accepts.
type UpdateFeatureInput struct {
	Title       *string
	Description *string
	Status      *Status
}

// LogEffortInput is the payload for POST .../effort.
type LogEffortInput struct {
	Minutes   int64
	Note      *string
	FeatureID *int64
}

// LinkRepoInput is the payload for linking a git repo to a project.
type LinkRepoInput struct {
	RepoPath       string
	AuthorFilter   *string
	SessionGapMin  *int64
	FirstCommitMin *int64
}

// GitSyncResult summarizes what a sync run produced.
type GitSyncResult struct {
	RepoPath       string      `json:"repo_path"`
	CommitsScanned int         `json:"commits_scanned"`
	SessionsFound  int         `json:"sessions_found"`
	MinutesLogged  int64       `json:"minutes_logged"`
	EffortLogs     []EffortLog `json:"effort_logs"`
}

// Sentinel errors for common failure modes so handlers can map them to
// the right HTTP status without string-matching.
var (
	ErrNotFound         = errors.New("not found")
	ErrInvalidInput     = errors.New("invalid input")
	ErrDuplicateSlug    = errors.New("duplicate slug")
	ErrInvalidTransition = errors.New("invalid status transition")
)

// ValidPriority reports whether p is a known priority value.
func ValidPriority(p Priority) bool {
	switch p {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// ValidStatus reports whether s is a known feature status.
func ValidStatus(s Status) bool {
	switch s {
	case StatusBacklog, StatusInProgress, StatusDone, StatusDropped:
		return true
	}
	return false
}
