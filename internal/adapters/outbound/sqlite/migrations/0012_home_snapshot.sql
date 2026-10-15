-- The home page's external blocks (deals, releases): one row per block with the last good data as a
-- JSON document, when it was gathered, and the last failed refresh ('' when the last one worked).
-- The data is a cache of public information: losing it only means gathering it again.
CREATE TABLE home_snapshots (
  block      TEXT PRIMARY KEY,
  doc        TEXT NOT NULL,
  fetched_at TEXT NOT NULL DEFAULT '',
  error      TEXT NOT NULL DEFAULT '',
  error_kind TEXT NOT NULL DEFAULT '',
  failed_at  TEXT NOT NULL DEFAULT ''
);
