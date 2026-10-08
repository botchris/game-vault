-- Custom cover image URL per game (empty = use the Steam cover).
ALTER TABLE games ADD COLUMN cover_url TEXT NOT NULL DEFAULT '';

-- Runtime settings edited from the UI, stored as JSON per key.
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
