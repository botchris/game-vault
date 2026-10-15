package catalog_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// recorder is an events.Publisher that keeps what it is given.
type recorder struct {
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) { r.events = append(r.events, e) }

// added returns the payload of the only event recorded, which must be a collection.added.
func (r *recorder) added(t *testing.T) event.CollectionAdded {
	t.Helper()

	require.Len(t, r.events, 1)
	require.Equal(t, event.KindCollectionAdded, r.events[0].Kind)

	a, ok := r.events[0].Data.(event.CollectionAdded)
	require.True(t, ok)

	return a
}

func TestCatalog_publishesGamesAddedByHand(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*catalog.Service, *recorder) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		rec := &recorder{}

		return catalog.NewService(sqlite.NewGameRepository(db), db, catalog.WithEvents(rec)), rec
	}

	t.Run("GIVEN a catalog", func(t *testing.T) {
		svc, rec := setup(t)

		t.Run("WHEN a game is created with two copies", func(t *testing.T) {
			g, err := svc.CreateGame(ctx, game.Info{Title: "Copper Tides"}, []game.CopyDetails{
				physical("PS4", "5030934110075"),
				{
					Kind:     game.KindLibrary,
					Platform: "Steam",
				},
			})
			require.NoError(t, err)

			t.Run("THEN one collection.added by hand names it with its systems", func(t *testing.T) {
				a := rec.added(t)
				assert.Equal(t, event.OriginManual, a.Origin)
				assert.Empty(t, a.OriginName)
				assert.Equal(t, []event.GameRef{{
					ID:      g.ID(),
					Title:   "Copper Tides",
					Systems: []string{"PC", "PS4"},
				}}, a.Games)
				assert.Zero(t, a.CopiesAdded)
			})
		})
	})

	t.Run("GIVEN a catalog with a game", func(t *testing.T) {
		svc, rec := setup(t)

		keep, err := svc.CreateGame(ctx, game.Info{Title: "Lantern Keep"}, nil)
		require.NoError(t, err)

		rec.events = nil

		t.Run("WHEN a scanning session adds a copy to it and two copies of a new game", func(t *testing.T) {
			results, _, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
				{
					Ref:     "a",
					GameID:  keep.ID(),
					Details: physical("PS4", "5030934110075"),
				},
				{
					Ref:     "b",
					Title:   "Moss Runner",
					Details: physical("PS4", "5030941110075"),
				},
				{
					Ref:     "c",
					Title:   "Moss Runner",
					Details: physical("PS5", "0711719541028"),
				},
			})
			require.NoError(t, err)

			t.Run("THEN one collection.added names the new game, and counts the other copy", func(t *testing.T) {
				a := rec.added(t)
				assert.Equal(t, event.OriginManual, a.Origin)
				assert.Equal(t, []event.GameRef{{
					ID:      results[1].GameID,
					Title:   "Moss Runner",
					Systems: []string{"PS4", "PS5"},
				}}, a.Games)
				assert.Equal(t, 1, a.CopiesAdded)
			})
		})

		t.Run("WHEN a scanning session only adds copies to games already there", func(t *testing.T) {
			rec.events = nil

			_, _, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{{
				Ref:     "d",
				GameID:  keep.ID(),
				Details: physical("PS4", "5030934110075"),
			}})
			require.NoError(t, err)

			t.Run("THEN nothing is published: no game entered the collection", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})
	})
}
