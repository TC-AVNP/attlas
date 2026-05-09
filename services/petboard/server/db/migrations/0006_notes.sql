-- Add notes column to projects and features for storing technical
-- implementation details, research findings, and ideation context.
ALTER TABLE projects ADD COLUMN notes TEXT;
ALTER TABLE features ADD COLUMN notes TEXT;
