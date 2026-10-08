-- Cached game details (summary, genres, companies, trailers...) per game and UI language,
-- merged from the metadata providers. Re-downloadable: safe to clear.
CREATE TABLE game_details (
  game_id    TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  language   TEXT NOT NULL,
  data       TEXT NOT NULL,
  fetched_at TEXT NOT NULL,
  PRIMARY KEY (game_id, language)
);
