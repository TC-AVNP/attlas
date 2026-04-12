-- 0004_git_repos: link local git repos to projects for auto effort tracking.

CREATE TABLE git_repos (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id       INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_path        TEXT    NOT NULL,
    author_filter    TEXT,
    session_gap_min  INTEGER NOT NULL DEFAULT 120,
    first_commit_min INTEGER NOT NULL DEFAULT 30,
    last_synced_sha  TEXT,
    last_synced_at   INTEGER,
    created_at       INTEGER NOT NULL,
    UNIQUE(project_id, repo_path)
);
