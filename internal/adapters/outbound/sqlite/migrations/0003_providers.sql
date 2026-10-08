-- Metadata provider configuration (Plex-agent style chains): one row per provider implementation.
CREATE TABLE providers (
  id         TEXT PRIMARY KEY,
  kind       TEXT    NOT NULL,
  enabled    INTEGER NOT NULL,
  priority   INTEGER NOT NULL,
  settings   TEXT    NOT NULL DEFAULT '{}',
  updated_at TEXT    NOT NULL
);
CREATE INDEX providers_kind ON providers(kind, priority);
