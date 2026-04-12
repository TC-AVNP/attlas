package service

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// gitCommit is a parsed line from `git log --format=...`.
type gitCommit struct {
	SHA       string
	Author    string
	Timestamp int64
	Subject   string
}

// --- repo linking ---------------------------------------------------------

// LinkRepo associates a local git repository with a project.
func (s *Service) LinkRepo(projectSlug string, in LinkRepoInput) (*GitRepo, error) {
	in.RepoPath = strings.TrimSpace(in.RepoPath)
	if in.RepoPath == "" {
		return nil, fmt.Errorf("%w: repo_path is required", ErrInvalidInput)
	}

	// Validate the path exists and is a git repo.
	if _, err := os.Stat(in.RepoPath); err != nil {
		return nil, fmt.Errorf("%w: path does not exist: %s", ErrInvalidInput, in.RepoPath)
	}
	cmd := exec.Command("git", "-C", in.RepoPath, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: path is not a git repository: %s", ErrInvalidInput, in.RepoPath)
	}

	var pid int64
	err := s.DB.QueryRow(`SELECT id FROM projects WHERE slug = ?`, projectSlug).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	gapMin := int64(120)
	if in.SessionGapMin != nil && *in.SessionGapMin > 0 {
		gapMin = *in.SessionGapMin
	}
	firstMin := int64(30)
	if in.FirstCommitMin != nil && *in.FirstCommitMin > 0 {
		firstMin = *in.FirstCommitMin
	}

	now := s.now()
	res, err := s.DB.Exec(`
		INSERT INTO git_repos (project_id, repo_path, author_filter, session_gap_min, first_commit_min, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, pid, in.RepoPath, in.AuthorFilter, gapMin, firstMin, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: repo already linked to this project", ErrInvalidInput)
		}
		return nil, err
	}

	id, _ := res.LastInsertId()
	return &GitRepo{
		ID:             id,
		ProjectID:      pid,
		RepoPath:       in.RepoPath,
		AuthorFilter:   in.AuthorFilter,
		SessionGapMin:  gapMin,
		FirstCommitMin: firstMin,
		CreatedAt:      now,
	}, nil
}

// UpdateRepoSHA advances the sync cursor without creating effort logs.
func (s *Service) UpdateRepoSHA(repoID int64, sha string) error {
	res, err := s.DB.Exec(`
		UPDATE git_repos SET last_synced_sha = ?, last_synced_at = ? WHERE id = ?
	`, sha, s.now(), repoID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UnlinkRepo removes a git repo link. Existing effort logs are kept.
func (s *Service) UnlinkRepo(repoID int64) error {
	res, err := s.DB.Exec(`DELETE FROM git_repos WHERE id = ?`, repoID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// gitReposFor fetches all git repos linked to a project.
func (s *Service) gitReposFor(projectID int64) ([]GitRepo, error) {
	rows, err := s.DB.Query(`
		SELECT id, project_id, repo_path, author_filter,
		       session_gap_min, first_commit_min,
		       last_synced_sha, last_synced_at, created_at
		FROM git_repos
		WHERE project_id = ?
		ORDER BY created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitRepo
	for rows.Next() {
		var r GitRepo
		if err := rows.Scan(
			&r.ID, &r.ProjectID, &r.RepoPath, &r.AuthorFilter,
			&r.SessionGapMin, &r.FirstCommitMin,
			&r.LastSyncedSHA, &r.LastSyncedAt, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- sync -----------------------------------------------------------------

// SyncProjectRepos syncs all git repos linked to a project.
func (s *Service) SyncProjectRepos(projectSlug string) ([]GitSyncResult, error) {
	var pid int64
	err := s.DB.QueryRow(`SELECT id FROM projects WHERE slug = ?`, projectSlug).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	repos, err := s.gitReposFor(pid)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("%w: no git repos linked to project", ErrInvalidInput)
	}

	var results []GitSyncResult
	for _, repo := range repos {
		result, err := s.syncRepo(repo)
		if err != nil {
			return nil, fmt.Errorf("sync %s: %w", repo.RepoPath, err)
		}
		results = append(results, *result)
	}
	return results, nil
}

// syncRepo is the core sync logic for a single git repo.
func (s *Service) syncRepo(repo GitRepo) (*GitSyncResult, error) {
	commits, err := gitLog(repo.RepoPath, repo.LastSyncedSHA, repo.AuthorFilter)
	if err != nil {
		return nil, err
	}

	if len(commits) == 0 {
		return &GitSyncResult{
			RepoPath: repo.RepoPath,
		}, nil
	}

	// Group commits into work sessions.
	sessions := groupSessions(commits, repo.SessionGapMin*60, repo.FirstCommitMin)

	// Insert effort logs in a transaction.
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var totalMinutes int64
	var logs []EffortLog
	for _, sess := range sessions {
		note := "[git] " + sess.summary
		res, err := tx.Exec(`
			INSERT INTO effort_logs (project_id, feature_id, minutes, note, logged_at)
			VALUES (?, NULL, ?, ?, ?)
		`, repo.ProjectID, sess.minutes, note, sess.endTime)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		logs = append(logs, EffortLog{
			ID:        id,
			ProjectID: repo.ProjectID,
			Minutes:   sess.minutes,
			Note:      &note,
			LoggedAt:  sess.endTime,
		})
		totalMinutes += sess.minutes
	}

	// Update sync cursor.
	lastSHA := commits[len(commits)-1].SHA
	now := s.now()
	if _, err := tx.Exec(`
		UPDATE git_repos SET last_synced_sha = ?, last_synced_at = ? WHERE id = ?
	`, lastSHA, now, repo.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &GitSyncResult{
		RepoPath:       repo.RepoPath,
		CommitsScanned: len(commits),
		SessionsFound:  len(sessions),
		MinutesLogged:  totalMinutes,
		EffortLogs:     logs,
	}, nil
}

// --- git helpers -----------------------------------------------------------

func gitLog(repoPath string, sinceSHA *string, authorFilter *string) ([]gitCommit, error) {
	args := []string{"-C", repoPath, "log", "--format=%H|%ae|%at|%s", "--reverse"}
	if authorFilter != nil && *authorFilter != "" {
		args = append(args, "--author="+*authorFilter)
	}
	if sinceSHA != nil && *sinceSHA != "" {
		args = append(args, *sinceSHA+"..HEAD")
	}

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		// If sinceSHA no longer exists (force push), the command fails.
		return nil, fmt.Errorf("git log failed (force-pushed?): %w", err)
	}

	var commits []gitCommit
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		commits = append(commits, gitCommit{
			SHA:       parts[0],
			Author:    parts[1],
			Timestamp: ts,
			Subject:   parts[3],
		})
	}
	return commits, nil
}

type session struct {
	endTime int64
	minutes int64
	summary string
}

func groupSessions(commits []gitCommit, gapSec int64, firstCommitMin int64) []session {
	if len(commits) == 0 {
		return nil
	}

	var sessions []session
	sessStart := commits[0].Timestamp
	sessEnd := commits[0].Timestamp
	var subjects []string
	subjects = append(subjects, commits[0].Subject)

	flush := func() {
		dur := (sessEnd - sessStart) / 60
		dur += firstCommitMin
		if dur < 1 {
			dur = 1
		}
		summary := strings.Join(subjects, "; ")
		if len(summary) > 500 {
			summary = summary[:497] + "..."
		}
		sessions = append(sessions, session{
			endTime: sessEnd,
			minutes: dur,
			summary: summary,
		})
	}

	for i := 1; i < len(commits); i++ {
		c := commits[i]
		if c.Timestamp-sessEnd > gapSec {
			flush()
			sessStart = c.Timestamp
			sessEnd = c.Timestamp
			subjects = subjects[:0]
		} else {
			sessEnd = c.Timestamp
		}
		subjects = append(subjects, c.Subject)
	}
	flush()

	return sessions
}
