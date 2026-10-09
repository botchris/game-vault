package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"gamevault/internal/domain/game"
)

// GameRepository implements game.Repository.
type GameRepository struct{ db *DB }

// NewGameRepository returns the repository backed by db.
func NewGameRepository(db *DB) *GameRepository { return &GameRepository{db: db} }

const copyCols = `id, game_id, kind, platform, status, cd_key, redeem_by, origin, acquired_on, edition,
	condition, location, barcode, notes, COALESCE(source_id, ''), external_id, created_at, updated_at`

// List returns every game with its copies.
func (r *GameRepository) List(ctx context.Context) ([]*game.Game, error) {
	q := r.db.conn(ctx)

	copies, err := r.copies(ctx, q, `SELECT `+copyCols+` FROM copies ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}

	rows, err := q.QueryContext(ctx, `SELECT id, title, links, notes, cover_url, created_at, updated_at FROM games ORDER BY title COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*game.Game

	for rows.Next() {
		g, err := scanGame(rows, copies)
		if err != nil {
			return nil, err
		}

		games = append(games, g)
	}

	return games, rows.Err()
}

// Get returns a game with its copies.
func (r *GameRepository) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	q := r.db.conn(ctx)

	copies, err := r.copies(ctx, q, `SELECT `+copyCols+` FROM copies WHERE game_id = ? ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}

	row := q.QueryRowContext(ctx, `SELECT id, title, links, notes, cover_url, created_at, updated_at FROM games WHERE id = ?`, id)

	g, err := scanGame(row, copies)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, game.ErrGameNotFound
	}

	return g, err
}

// Save upserts the game and makes its stored copies match the aggregate exactly. Copies that
// moved from another game are re-parented thanks to the upsert on the copy id.
func (r *GameRepository) Save(ctx context.Context, g *game.Game) error {
	return r.db.WithinTx(ctx, func(ctx context.Context) error {
		q := r.db.conn(ctx)

		_, err := q.ExecContext(ctx, `INSERT INTO games (id, title, links, notes, cover_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET title = excluded.title, links = excluded.links,
			  notes = excluded.notes, cover_url = excluded.cover_url, updated_at = excluded.updated_at`,
			g.ID(), g.Title(), encodeLinks(g.Links()), g.Notes(), g.CoverURL(), formatTime(g.CreatedAt()), formatTime(g.UpdatedAt()))
		if err != nil {
			return err
		}

		ids := []string{}
		for _, c := range g.Copies() {
			ids = append(ids, string(c.ID))

			var sourceID any
			if c.SourceID != "" {
				sourceID = c.SourceID
			}

			_, err := q.ExecContext(ctx, `INSERT INTO copies (id, game_id, kind, platform, status, cd_key, redeem_by, origin,
				acquired_on, edition, condition, location, barcode, notes, source_id, external_id, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET game_id = excluded.game_id, kind = excluded.kind, platform = excluded.platform,
				  status = excluded.status, cd_key = excluded.cd_key, redeem_by = excluded.redeem_by, origin = excluded.origin,
				  acquired_on = excluded.acquired_on, edition = excluded.edition, condition = excluded.condition,
				  location = excluded.location, barcode = excluded.barcode, notes = excluded.notes, source_id = excluded.source_id,
				  external_id = excluded.external_id, updated_at = excluded.updated_at`,
				c.ID, g.ID(), c.Kind, c.Platform, c.Status, c.Key, c.RedeemBy, c.Origin, c.AcquiredOn, c.Edition,
				c.Condition, c.Location, c.Barcode, c.Notes, sourceID, c.ExternalID, formatTime(c.CreatedAt), formatTime(c.UpdatedAt))
			if err != nil {
				return err
			}
		}

		idsJSON, _ := json.Marshal(ids)
		_, err = q.ExecContext(ctx, `DELETE FROM copies WHERE game_id = ? AND id NOT IN (SELECT value FROM json_each(?))`, g.ID(), string(idsJSON))

		return err
	})
}

// Delete removes a game and its copies.
func (r *GameRepository) Delete(ctx context.Context, id game.ID) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM games WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return game.ErrGameNotFound
	}

	return nil
}

func (r *GameRepository) copies(ctx context.Context, q querier, query string, args ...any) (map[game.ID][]game.Copy, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[game.ID][]game.Copy{}

	for rows.Next() {
		var (
			c                game.Copy
			gameID           game.ID
			created, updated string
		)
		if err := rows.Scan(&c.ID, &gameID, &c.Kind, &c.Platform, &c.Status, &c.Key, &c.RedeemBy, &c.Origin,
			&c.AcquiredOn, &c.Edition, &c.Condition, &c.Location, &c.Barcode, &c.Notes, &c.SourceID, &c.ExternalID, &created, &updated); err != nil {
			return nil, err
		}

		c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
		out[gameID] = append(out[gameID], c)
	}

	return out, rows.Err()
}

func scanGame(sc interface{ Scan(...any) error }, copies map[game.ID][]game.Copy) (*game.Game, error) {
	var (
		id               game.ID
		info             game.Info
		links            string
		created, updated string
	)
	if err := sc.Scan(&id, &info.Title, &links, &info.Notes, &info.CoverURL, &created, &updated); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(links), &info.Links); err != nil {
		return nil, fmt.Errorf("game %s: reading links: %w", id, err)
	}

	return game.Rehydrate(id, info, copies[id], parseTime(created), parseTime(updated)), nil
}

// encodeLinks stores links as a JSON object; nil links are "{}" (the column's default).
func encodeLinks(l game.Links) string {
	if len(l) == 0 {
		return "{}"
	}

	b, _ := json.Marshal(l) // a map of strings always marshals

	return string(b)
}
