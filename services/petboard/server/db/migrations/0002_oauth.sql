-- 0002_oauth: tables for the MCP OAuth 2.1 flow.
--
-- Three tables:
--
--  oauth_clients         — RFC 7591 dynamic client registrations
--  oauth_auth_codes      — short-lived authorization codes (PKCE)
--  oauth_access_tokens   — long-lived bearer tokens (30 days)
--
-- We never store raw tokens or codes — only their SHA-256 hashes — so a
-- DB leak doesn't immediately compromise sessions. The plain values
-- live only in HTTP responses to the legitimate caller.

CREATE TABLE oauth_clients (
    id            TEXT PRIMARY KEY,             -- client_id, opaque
    name          TEXT NOT NULL,
    redirect_uris TEXT NOT NULL,                -- JSON array of strings
    created_at    INTEGER NOT NULL              -- unix seconds
);

CREATE TABLE oauth_auth_codes (
    code_hash             TEXT PRIMARY KEY,      -- sha256(code)
    client_id             TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL DEFAULT 'S256',
    redirect_uri          TEXT NOT NULL,
    scope                 TEXT NOT NULL DEFAULT '',
    user_email            TEXT NOT NULL,         -- captured from session at /authorize time
    expires_at            INTEGER NOT NULL,      -- unix seconds, ~10 minutes from issuance
    used                  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_oauth_auth_codes_expires ON oauth_auth_codes(expires_at);

CREATE TABLE oauth_access_tokens (
    token_hash    TEXT PRIMARY KEY,              -- sha256(token)
    client_id     TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    scope         TEXT NOT NULL DEFAULT '',
    user_email    TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,              -- unix seconds, ~30 days
    last_used_at  INTEGER
);

CREATE INDEX idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at);
