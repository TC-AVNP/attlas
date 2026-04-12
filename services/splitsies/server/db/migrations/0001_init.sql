-- Splitsies schema: users, groups, expenses, settlements.
-- All monetary amounts are stored as INTEGER cents to avoid floating-point.
-- All timestamps are unix seconds.

-- Users are whitelisted by email. Google OAuth populates name/picture on first login.
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    picture TEXT NOT NULL DEFAULT '',
    is_admin INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    last_login_at INTEGER
);

-- Sessions for cookie-based auth after Google OAuth.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- Groups of users who split expenses together.
CREATE TABLE groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    photo_url TEXT NOT NULL DEFAULT '',
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL
);

-- Group membership is permanent — no leaving once added.
CREATE TABLE group_members (
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    added_at INTEGER NOT NULL,
    PRIMARY KEY (group_id, user_id)
);

-- Predefined + user-created expense categories.
CREATE TABLE categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_by INTEGER REFERENCES users(id)
);

INSERT INTO categories (name, is_default) VALUES
    ('vacation', 1),
    ('house', 1),
    ('bills', 1),
    ('dog', 1),
    ('insurance', 1),
    ('gas', 1),
    ('food', 1);

-- Expenses within a group.
CREATE TABLE expenses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    paid_by INTEGER NOT NULL REFERENCES users(id),
    amount INTEGER NOT NULL,
    description TEXT NOT NULL,
    category_id INTEGER REFERENCES categories(id),
    split_type TEXT NOT NULL CHECK(split_type IN ('even', 'custom', 'percentage')),
    created_at INTEGER NOT NULL,
    deleted_at INTEGER
);
CREATE INDEX idx_expenses_group ON expenses(group_id);
CREATE INDEX idx_expenses_paid_by ON expenses(paid_by);
CREATE INDEX idx_expenses_created ON expenses(created_at);

-- How each expense is split among members.
CREATE TABLE expense_splits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    expense_id INTEGER NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    amount INTEGER NOT NULL,
    UNIQUE(expense_id, user_id)
);

-- Settlements: real-world payments recorded in the app.
CREATE TABLE settlements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    from_user INTEGER NOT NULL REFERENCES users(id),
    to_user INTEGER NOT NULL REFERENCES users(id),
    amount INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    deleted_at INTEGER
);
CREATE INDEX idx_settlements_group ON settlements(group_id);
CREATE INDEX idx_settlements_created ON settlements(created_at);
