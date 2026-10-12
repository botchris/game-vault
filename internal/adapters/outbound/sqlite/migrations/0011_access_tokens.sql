-- Access tokens for programs that may only look up what the user owns (the browser extension).
-- Only a SHA-256 of each token is stored; last_used_at is '' until the token is first used.
CREATE TABLE access_tokens (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
