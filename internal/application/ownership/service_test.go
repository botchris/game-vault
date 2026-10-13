package ownership_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/ownership"
	"gamevault/internal/domain/game"
)

// catalog is an in-memory ownership.Catalog. It has no way to save anything, which is the point:
// the lookup only reads.
type catalog []*game.Game

func (c catalog) List(context.Context) ([]*game.Game, error) { return c, nil }

var now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

// newGame builds a game with its links and copies. Every title is invented.
func newGame(t *testing.T, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, now)
	require.NoError(t, err)

	if links != nil {
		_, err = g.UpdateInfo(game.Info{
			Title: title,
			Links: links,
		}, now)
		require.NoError(t, err)
	}

	for _, d := range copies {
		_, err := g.AddCopy(d, now)
		require.NoError(t, err)
	}

	return g
}

var (
	steamLibrary = game.CopyDetails{
		Kind:     game.KindLibrary,
		Platform: "Steam",
	}
	steamKey = game.CopyDetails{
		Kind:     game.KindKey,
		Platform: "Steam",
	} // unrevealed: pending
	ps4Disc = game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}
	switchCart = game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "Switch",
	}
	soldDisc = game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
		Status:   game.StatusSold,
	}
	giftedKey = game.CopyDetails{
		Kind:     game.KindKey,
		Platform: "Steam",
		Status:   game.StatusGifted,
	}
)

func TestCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	lantern := newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary, soldDisc)
	echo := newGame(t, "Echo Valley", nil, steamKey)
	echo.AddAutoLinks(game.Links{"gog": "1207664663"}, now)

	harborDisc := newGame(t, "Harbor Lights", nil, ps4Disc)
	harborKey := newGame(t, "Harbor Lights", nil, steamKey)
	copper := newGame(t, "Copper Coast", nil, switchCart)
	gone := newGame(t, "Sold Out Story", game.Links{game.LinkSteam: "7770"}, soldDisc, giftedKey)
	empty := newGame(t, "Wishlist Only", nil)
	svc := ownership.NewService(catalog{lantern, echo, harborDisc, harborKey, copper, gone, empty})

	check := func(t *testing.T, items ...ownership.Item) []ownership.Result {
		t.Helper()

		got, err := svc.Check(ctx, items)
		require.NoError(t, err)
		require.Len(t, got, len(items))

		return got
	}

	t.Run("GIVEN a store id the game is linked to", func(t *testing.T) {
		got := check(t, ownership.Item{
			Store: "steam",
			ID:    "4410",
		})[0]

		t.Run("THEN it is owned, with its current copies only", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, lantern.ID(), got.Game.ID)
			assert.Equal(t, "Lantern Keep", got.Game.Title)
			assert.Equal(t, []ownership.Copy{{
				Platform: "Steam",
				System:   "PC",
				Kind:     game.KindLibrary,
			}}, got.Game.Copies)
			assert.Empty(t, got.Candidates)
		})
	})

	t.Run("GIVEN a store id Game Vault found automatically, written loosely", func(t *testing.T) {
		got := check(t, ownership.Item{
			Store: "GOG",
			ID:    " 1207664663 ",
		})[0]

		t.Run("THEN it is owned, and the key reads as pending", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, echo.ID(), got.Game.ID)
			assert.True(t, got.Game.Copies[0].PendingKey)
			assert.Equal(t, game.KindKey, got.Game.Copies[0].Kind)
		})
	})

	t.Run("GIVEN an unknown store id with a known title", func(t *testing.T) {
		got := check(t, ownership.Item{
			Store: "steam",
			ID:    "9999",
			Title: "LANTERN KEEP™",
		})[0]

		t.Run("THEN the exact title decides", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, lantern.ID(), got.Game.ID)
		})
	})

	t.Run("GIVEN a title two games share", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Harbor Lights"})[0]

		t.Run("THEN it is possible, with both as candidates", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got.Status)
			assert.Nil(t, got.Game)
			require.Len(t, got.Candidates, 2)
			assert.ElementsMatch(t, []game.ID{harborDisc.ID(), harborKey.ID()}, []game.ID{got.Candidates[0].ID, got.Candidates[1].ID})
		})
	})

	t.Run("GIVEN a title that matches only once its edition tag is ignored", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Copper Coast - Deluxe Edition"})[0]

		t.Run("THEN it is only possible, never owned", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got.Status)
			require.Len(t, got.Candidates, 1)
			assert.Equal(t, copper.ID(), got.Candidates[0].ID)
		})
	})

	t.Run("GIVEN games whose copies are all gone or that have none, and titles no game has", func(t *testing.T) {
		got := check(t,
			ownership.Item{
				Store: "steam",
				ID:    "7770",
			},
			ownership.Item{Title: "Sold Out Story"},
			ownership.Item{Title: "Wishlist Only"},
			ownership.Item{Title: "Copper Coast 2"},
			ownership.Item{},
		)

		t.Run("THEN none is owned", func(t *testing.T) {
			for i, r := range got {
				assert.Equal(t, ownership.StatusNotOwned, r.Status, "item %d", i)
				assert.Nil(t, r.Game, "item %d", i)
			}
		})
	})

	t.Run("GIVEN items in a mixed order", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Nobody Owns This"}, ownership.Item{
			Store: "steam",
			ID:    "4410",
		}, ownership.Item{Title: "Harbor Lights"})

		t.Run("THEN each result answers its own item", func(t *testing.T) {
			assert.Equal(t,
				[]ownership.Status{ownership.StatusNotOwned, ownership.StatusOwned, ownership.StatusPossible},
				[]ownership.Status{got[0].Status, got[1].Status, got[2].Status})
		})
	})
}

func TestCheck_limits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	games := make(catalog, 0, 7)
	for i := range 7 {
		games = append(games, newGame(t, "Mirror Run", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: fmt.Sprintf("Console %d", i),
		}))
	}

	svc := ownership.NewService(games)

	t.Run("GIVEN seven games with the same title", func(t *testing.T) {
		got, err := svc.Check(ctx, []ownership.Item{{Title: "Mirror Run"}})
		require.NoError(t, err)

		t.Run("THEN five candidates are listed", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got[0].Status)
			assert.Len(t, got[0].Candidates, 5)
		})
	})

	t.Run("GIVEN 200 items", func(t *testing.T) {
		got, err := svc.Check(ctx, make([]ownership.Item, ownership.MaxItems))

		t.Run("THEN every one is answered", func(t *testing.T) {
			require.NoError(t, err)
			assert.Len(t, got, ownership.MaxItems)
		})
	})

	t.Run("GIVEN 201 items", func(t *testing.T) {
		_, err := svc.Check(ctx, make([]ownership.Item, ownership.MaxItems+1))

		t.Run("THEN the check is refused, naming the limit", func(t *testing.T) {
			assert.ErrorIs(t, err, ownership.ErrTooManyItems)
			assert.Contains(t, err.Error(), strconv.Itoa(ownership.MaxItems))
		})
	})

	t.Run("GIVEN no items", func(t *testing.T) {
		got, err := svc.Check(ctx, nil)

		t.Run("THEN the answer is empty", func(t *testing.T) {
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})
}
