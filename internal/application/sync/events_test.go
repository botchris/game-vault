package sync_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// recorder is an events.Publisher that keeps what it is given.
type recorder struct {
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) { r.events = append(r.events, e) }

// scripted is a source that answers its items, or fails with err.
type scripted struct {
	items []game.ImportedCopy
	err   error
}

func (p *scripted) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "sc",
		Name: "Scripted",
	}
}

func (p *scripted) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	return p.items, nil, p.err
}

func (p *scripted) Test(context.Context, schema.Settings) error { return nil }

func TestSync_publishesNewGames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a catalog with one game and a source that has it and two new ones", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		cat := catalog.NewService(games, db)

		_, err = cat.CreateGame(ctx, game.Info{Title: "Lantern Keep"}, nil)
		require.NoError(t, err)

		p := &scripted{items: []game.ImportedCopy{
			item("sc:1", "Copper Tides"),
			item("sc:2", "Lantern Keep"),
			item("sc:3", "Moss Runner"),
		}}
		rec := &recorder{}
		svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, cat, []sync.Provider{p}, sync.WithEvents(rec))

		v, err := svc.Create(ctx, "sc", source.Config{
			Name:    "My shelf",
			Enabled: true,
		})
		require.NoError(t, err)

		t.Run("WHEN it is scanned", func(t *testing.T) {
			_, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			t.Run("THEN one collection.added names the source and the two new games", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.KindCollectionAdded, rec.events[0].Kind)

				added, ok := rec.events[0].Data.(event.CollectionAdded)
				require.True(t, ok)
				assert.Equal(t, event.OriginSource, added.Origin)
				assert.Equal(t, "My shelf", added.OriginName)
				require.Len(t, added.Games, 2)
				assert.Equal(t, "Copper Tides", added.Games[0].Title)
				assert.Equal(t, "Moss Runner", added.Games[1].Title)
				assert.NotEmpty(t, added.Games[0].ID)
				assert.Equal(t, []string{"PS4"}, added.Games[0].Systems)

				t.Run("AND the copy added to the game already there is counted", func(t *testing.T) {
					assert.Equal(t, 1, added.CopiesAdded)
				})
			})
		})

		t.Run("WHEN it is scanned again and creates nothing", func(t *testing.T) {
			rec.events = nil

			_, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			t.Run("THEN nothing is published", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN a scan with a new game fails", func(t *testing.T) {
			rec.events = nil

			p.items = append(p.items, item("sc:4", "Quiet Orbit"))
			p.err = errors.New("the store is down")

			_, err := svc.Sync(ctx, v.ID())
			require.Error(t, err)

			t.Run("THEN nothing is published", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})
	})
}
