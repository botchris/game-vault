-- Accounts for username/password login (optional: local and Tailscale access need none).
CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

-- Logged-in browsers. Only a SHA-256 of the session token is stored.
CREATE TABLE sessions (
  token_hash   TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);
