package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gamevault/internal/domain/game"
)

// GameRepository implements game.Repository. Each game is one JSON document, copies included.
type GameRepository struct{ db *DB }

// NewGameRepository returns the repository backed by db.
func NewGameRepository(db *DB) *GameRepository { return &GameRepository{db: db} }

// List returns every game with its copies, by title.
func (r *GameRepository) List(ctx context.Context) ([]*game.Game, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM games ORDER BY json_extract(doc, '$.title') COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*game.Game

	for rows.Next() {
		var (
			id  game.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		g, err := decodeGame(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading game %s: %w", id, err)
		}

		games = append(games, g)
	}

	return games, rows.Err()
}

// Get returns a game with its copies.
func (r *GameRepository) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT doc FROM games WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, game.ErrGameNotFound
	}

	if err != nil {
		return nil, err
	}

	g, err := decodeGame(id, raw)
	if err != nil {
		return nil, fmt.Errorf("reading game %s: %w", id, err)
	}

	return g, nil
}

// Save writes the whole game, copies included. A copy moved to another game is part of that
// game's document from now on: saving both games in one transaction moves it.
func (r *GameRepository) Save(ctx context.Context, g *game.Game) error {
	raw, err := encodeGame(g)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO games (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, g.ID(), raw)

	return err
}

// Delete removes a game and its copies (its cached details go with it).
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
