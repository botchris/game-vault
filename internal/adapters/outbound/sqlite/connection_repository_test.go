package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

var _ notify.Connections = (*ConnectionRepository)(nil)

func newConnection(name string, at time.Time) *connection.Connection {
	return connection.Rehydrate(connection.Snapshot{
		ID:        connection.NewID(),
		Type:      "ntfy",
		Name:      name,
		Enabled:   true,
		Events:    []event.Kind{event.KindCollectionAdded, event.KindIssueOpened},
		Language:  "es",
		Settings:  schema.Settings{"token": "t-123"},
		CreatedAt: at,
		UpdatedAt: at,
	})
}

func TestConnectionRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := NewConnectionRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	t.Run("GIVEN a saved connection", func(t *testing.T) {
		phone := newConnection("Phone", at)
		require.NoError(t, repo.Save(ctx, phone))

		t.Run("WHEN it is read back", func(t *testing.T) {
			got, err := repo.Get(ctx, phone.ID())
			require.NoError(t, err)

			t.Run("THEN every field is kept", func(t *testing.T) {
				assert.Equal(t, phone.Snapshot(), got.Snapshot())
			})
		})

		t.Run("WHEN it is paused and saved again", func(t *testing.T) {
			phone.Pause("rejected token", at.Add(time.Hour))
			require.NoError(t, repo.Save(ctx, phone))

			got, err := repo.Get(ctx, phone.ID())
			require.NoError(t, err)

			t.Run("THEN the pause is kept", func(t *testing.T) {
				assert.True(t, got.Paused())
				assert.Equal(t, "rejected token", got.PausedWhy())
				assert.True(t, got.PausedAt().Equal(at.Add(time.Hour)))
			})
		})

		t.Run("AND another one named in lower case", func(t *testing.T) {
			require.NoError(t, repo.Save(ctx, newConnection("alpha", at)))

			t.Run("WHEN they are listed", func(t *testing.T) {
				list, err := repo.List(ctx)
				require.NoError(t, err)

				t.Run("THEN they come by name, ignoring case", func(t *testing.T) {
					require.Len(t, list, 2)
					assert.Equal(t, "alpha", list[0].Name())
					assert.Equal(t, "Phone", list[1].Name())
				})
			})
		})

		t.Run("WHEN it is deleted", func(t *testing.T) {
			require.NoError(t, repo.Delete(ctx, phone.ID()))

			t.Run("THEN it is gone, and deleting again says so", func(t *testing.T) {
				_, err := repo.Get(ctx, phone.ID())
				assert.ErrorIs(t, err, connection.ErrNotFound)
				assert.ErrorIs(t, repo.Delete(ctx, phone.ID()), connection.ErrNotFound)
			})
		})
	})
}
