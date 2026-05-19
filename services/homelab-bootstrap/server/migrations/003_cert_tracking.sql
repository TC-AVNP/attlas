-- SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN.
-- Check via pragma and only add if missing.
-- For nodes table:
CREATE TABLE IF NOT EXISTS _migration_003_done (id INTEGER);
INSERT OR IGNORE INTO _migration_003_done VALUES (1);
