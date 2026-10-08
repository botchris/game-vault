-- Games are the aggregate root; copies are owned instances of a game.
CREATE TABLE games (
  id           TEXT PRIMARY KEY,
  title        TEXT    NOT NULL,
  steam_app_id INTEGER NOT NULL DEFAULT 0,
  notes        TEXT    NOT NULL DEFAULT '',
  created_at   TEXT    NOT NULL,
  updated_at   TEXT    NOT NULL
);
CREATE INDEX games_title ON games(title COLLATE NOCASE);

-- Sources are scanned accounts (Humble Bundle, Steam...). Settings and the last sync report are JSON.
CREATE TABLE sources (
  id                    TEXT PRIMARY KEY,
  type                  TEXT    NOT NULL,
  name                  TEXT    NOT NULL,
  enabled               INTEGER NOT NULL DEFAULT 1,
  sync_interval_seconds INTEGER NOT NULL DEFAULT 0,
  settings              TEXT    NOT NULL DEFAULT '{}',
  last_sync             TEXT,
  created_at            TEXT    NOT NULL,
  updated_at            TEXT    NOT NULL
);

CREATE TABLE copies (
  id          TEXT PRIMARY KEY,
  game_id     TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  platform    TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,
  cd_key      TEXT NOT NULL DEFAULT '',
  redeem_by   TEXT NOT NULL DEFAULT '',
  origin      TEXT NOT NULL DEFAULT '',
  acquired_on TEXT NOT NULL DEFAULT '',
  edition     TEXT NOT NULL DEFAULT '',
  condition   TEXT NOT NULL DEFAULT '',
  location    TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  source_id   TEXT REFERENCES sources(id) ON DELETE SET NULL,
  external_id TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX copies_game ON copies(game_id);
CREATE INDEX copies_source ON copies(source_id);
CREATE UNIQUE INDEX copies_external_id ON copies(external_id) WHERE external_id <> '';
