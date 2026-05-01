-- Add lifecycle stage and interest level to projects.
ALTER TABLE projects ADD COLUMN stage TEXT NOT NULL DEFAULT 'idea' CHECK (stage IN ('idea', 'live', 'completed'));
ALTER TABLE projects ADD COLUMN interest TEXT NOT NULL DEFAULT 'meh' CHECK (interest IN ('excited', 'meh', 'bored'));
