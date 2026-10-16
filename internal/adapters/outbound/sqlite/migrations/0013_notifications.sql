-- Notifications (docs/superpowers/specs/2026-10-15-notifications-design.md): the configured
-- connectors as JSON documents, the queue of deliveries, and what was already told, so a restart
-- neither repeats nor loses a notification.
CREATE TABLE connections (
  id  TEXT PRIMARY KEY,
  doc TEXT NOT NULL
);
CREATE INDEX connections_name ON connections(json_extract(doc, '$.name') COLLATE NOCASE);

-- One row per event and connection. state: queued, sent, failed (given up), dropped (too old).
CREATE TABLE notification_outbox (
  id            TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL,
  event         TEXT NOT NULL,
  state         TEXT NOT NULL,
  attempts      INTEGER NOT NULL DEFAULT 0,
  next_at       TEXT NOT NULL,
  last_error    TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX notification_outbox_due ON notification_outbox(state, next_at);
CREATE INDEX notification_outbox_connection ON notification_outbox(connection_id, created_at);

-- Health issues already notified (the watcher's memory), by Health's key.
CREATE TABLE notified_issues (
  key   TEXT PRIMARY KEY,
  issue TEXT NOT NULL,
  since TEXT NOT NULL
);

-- Deadline thresholds already notified per key copy (30, 7, 1 days).
CREATE TABLE notified_keys (
  copy_id   TEXT NOT NULL,
  threshold INTEGER NOT NULL,
  at        TEXT NOT NULL,
  PRIMARY KEY (copy_id, threshold)
);
