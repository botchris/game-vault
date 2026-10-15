package transfer_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/transfer"
	"gamevault/internal/domain/event"
)

// recorder is an events.Publisher that keeps what it is given.
type recorder struct {
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) { r.events = append(r.events, e) }

func TestImport_publishesNewGames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an empty catalog", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		rec := &recorder{}
		svc := transfer.NewService(sqlite.NewGameRepository(db), catalog.NewService(sqlite.NewGameRepository(db), db),
			sqlite.NewSettingsRepository(db), csvfile.Codec{}, transfer.WithEvents(rec))

		t.Run("WHEN a file with two games is imported", func(t *testing.T) {
			_, err := svc.Import(ctx, []byte("title,platform,kind\nCopper Tides,Steam,library\nMoss Runner,PS3,physical\n"))
			require.NoError(t, err)

			t.Run("THEN one collection.added from an import names both", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.KindCollectionAdded, rec.events[0].Kind)

				a, ok := rec.events[0].Data.(event.CollectionAdded)
				require.True(t, ok)
				assert.Equal(t, event.OriginImport, a.Origin)
				require.Len(t, a.Games, 2)
				assert.Equal(t, "Copper Tides", a.Games[0].Title)
				assert.Equal(t, []string{"PC"}, a.Games[0].Systems)
				assert.Equal(t, "Moss Runner", a.Games[1].Title)
				assert.Zero(t, a.CopiesAdded)
			})
		})

		t.Run("WHEN the same file is imported again", func(t *testing.T) {
			rec.events = nil

			_, err := svc.Import(ctx, []byte("title,platform,kind\nCopper Tides,Steam,library\nMoss Runner,PS3,physical\n"))
			require.NoError(t, err)

			t.Run("THEN nothing is published", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})
	})
}
