package sqlite

import (
	"context"

	"gamevault/internal/domain/game"
)

// GameRepository implements game.Repository. Each game is one JSON document, copies included.
type GameRepository struct {
	docs docTable[game.ID, *game.Game]
}

// NewGameRepository returns the repository backed by db.
func NewGameRepository(db *DB) *GameRepository {
	return &GameRepository{docs: docTable[game.ID, *game.Game]{
		db:       db,
		table:    "games",
		noun:     "game",
		encode:   encodeGame,
		decode:   decodeGame,
		notFound: game.ErrGameNotFound,
	}}
}

// List returns every game with its copies, by title.
func (r *GameRepository) List(ctx context.Context) ([]*game.Game, error) {
	return r.docs.list(ctx, `ORDER BY json_extract(doc, '$.title') COLLATE NOCASE, id`)
}

// Get returns a game with its copies.
func (r *GameRepository) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	return r.docs.get(ctx, id)
}

// Save writes the whole game, copies included. A copy moved to another game is part of that
// game's document from now on: saving both games in one transaction moves it.
func (r *GameRepository) Save(ctx context.Context, g *game.Game) error {
	return r.docs.save(ctx, g.ID(), g)
}

// Delete removes a game and its copies (its cached details go with it).
func (r *GameRepository) Delete(ctx context.Context, id game.ID) error {
	return r.docs.remove(ctx, id)
}
