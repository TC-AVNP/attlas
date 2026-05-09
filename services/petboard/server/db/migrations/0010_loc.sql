-- Lines of code tracking. Stored as a JSON object with per-language breakdown.
-- Example: {"go": 15940, "ts": 6992, "sql": 629, "total": 51432}
ALTER TABLE projects ADD COLUMN loc TEXT;
