-- Tags on projects. Stored as a JSON array of strings, e.g. ["business_case"].
ALTER TABLE projects ADD COLUMN tags TEXT;
