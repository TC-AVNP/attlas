-- Add category to steps: 'refining' (decide/research) or 'executing' (build/do)
ALTER TABLE steps ADD COLUMN category TEXT NOT NULL DEFAULT 'executing'
    CHECK(category IN ('refining', 'executing'));
