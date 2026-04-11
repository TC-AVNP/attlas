-- 0003_todos: standalone todos that aren't tied to any project.
--
-- Project features cover the "what's on this project's backlog" use
-- case. Standalone todos cover the "I should remember to do X someday
-- but it isn't a project on its own" use case — typical examples are
-- cross-cutting refactors, infra chores, things you want to think
-- about later.
--
-- Deliberately tiny schema. No status enum (just done / not-done), no
-- priority (use the title to convey urgency if you must), no due
-- dates. If a todo grows enough to need those, promote it to a real
-- project with create_project.

CREATE TABLE todos (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    text         TEXT NOT NULL CHECK (length(trim(text)) > 0),
    created_at   INTEGER NOT NULL,
    completed_at INTEGER
);

CREATE INDEX idx_todos_open ON todos(completed_at) WHERE completed_at IS NULL;
