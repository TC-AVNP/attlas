-- Project lifecycle timestamps, auto-set when stage changes.
ALTER TABLE projects ADD COLUMN started_at INTEGER;
ALTER TABLE projects ADD COLUMN live_at INTEGER;
ALTER TABLE projects ADD COLUMN completed_at INTEGER;
