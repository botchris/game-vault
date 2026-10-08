package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// DetailsStore implements media.DetailsStore with one JSON row per game and language.
type DetailsStore struct{ db *DB }

// NewDetailsStore returns the repository backed by db.
func NewDetailsStore(db *DB) *DetailsStore { return &DetailsStore{db: db} }

var _ media.DetailsStore = (*DetailsStore)(nil)

// Get returns the stored details of a game in a language.
func (s *DetailsStore) Get(ctx context.Context, id game.ID, language string) (media.GameDetails, bool, error) {
	var raw string

	err := s.db.conn(ctx).QueryRowContext(ctx, `SELECT data FROM game_details WHERE game_id = ? AND language = ?`, id, language).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return media.GameDetails{}, false, nil
	}

	if err != nil {
		return media.GameDetails{}, false, err
	}

	var d media.GameDetails
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return media.GameDetails{}, false, nil // unreadable cache entry: fetch again
	}

	return d, true, nil
}

// Put stores the details of a game, replacing the previous ones for that language.
func (s *DetailsStore) Put(ctx context.Context, id game.ID, d media.GameDetails) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}

	_, err = s.db.conn(ctx).ExecContext(ctx, `INSERT INTO game_details (game_id, language, data, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(game_id, language) DO UPDATE SET data = excluded.data, fetched_at = excluded.fetched_at`,
		id, d.Language, string(raw), formatTime(d.FetchedAt))

	return err
}

// Delete removes every stored language of a game's details.
func (s *DetailsStore) Delete(ctx context.Context, id game.ID) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `DELETE FROM game_details WHERE game_id = ?`, id)
	return err
}

// Summaries returns the short summary of every game with details in a language.
func (s *DetailsStore) Summaries(ctx context.Context, language string) (map[game.ID]media.Summary, error) {
	rows, err := s.db.conn(ctx).QueryContext(ctx, `SELECT game_id, data, fetched_at FROM game_details WHERE language = ?`, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[game.ID]media.Summary{}

	for rows.Next() {
		var (
			id           game.ID
			raw, fetched string
		)
		if err := rows.Scan(&id, &raw, &fetched); err != nil {
			return nil, err
		}

		var d struct {
			Genres      []string
			ReleaseDate string
		}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}

		out[id] = media.Summary{Genres: d.Genres, ReleaseDate: d.ReleaseDate, FetchedAt: parseTime(fetched)}
	}

	return out, rows.Err()
}
