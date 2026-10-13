package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
)

func TestGameRepository_outdatedIDs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN games stored in every document version", func(t *testing.T) {
		db := openTest(t)
		repo := NewGameRepository(db)

		for id, raw := range map[string]string{
			"g-v1": `{"v":1,"title":"Old","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z"}`,
			"g-v2": `{"v":2,"title":"Older","coverUrl":"https://example.test/c.jpg","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z"}`,
			"g-v3": `{"v":3,"title":"Current","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z"}`,
		} {
			_, err := db.conn(ctx).ExecContext(ctx, `INSERT INTO games (id, doc) VALUES (?, ?)`, id, raw)
			require.NoError(t, err)
		}

		t.Run("WHEN the outdated ones are asked for", func(t *testing.T) {
			ids, err := repo.OutdatedIDs(ctx)
			require.NoError(t, err)

			t.Run("THEN only the games stored before the current version are listed", func(t *testing.T) {
				assert.Equal(t, []game.ID{"g-v1", "g-v2"}, ids)
			})
		})

		t.Run("WHEN an outdated game is read and saved again", func(t *testing.T) {
			g, err := repo.Get(ctx, "g-v2")
			require.NoError(t, err)
			require.NoError(t, repo.Save(ctx, g))

			ids, err := repo.OutdatedIDs(ctx)
			require.NoError(t, err)

			t.Run("THEN it is no longer outdated", func(t *testing.T) {
				assert.Equal(t, []game.ID{"g-v1"}, ids)
			})
		})
	})
}

func TestSettingsRepository_authOutdated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a new database", func(t *testing.T) {
		db := openTest(t)
		r := NewSettingsRepository(db)

		t.Run("WHEN nothing has been saved THEN the settings are not outdated", func(t *testing.T) {
			outdated, err := r.AuthOutdated(ctx)
			require.NoError(t, err)
			assert.False(t, outdated)
		})

		t.Run("WHEN a version that trusted Tailscale identities saved them", func(t *testing.T) {
			_, err := db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`,
				keyAuth, `{"localBypass":true,"trustTailscale":true,"tailscaleLogins":[]}`, "2026-10-01T10:00:00Z")
			require.NoError(t, err)

			outdated, err := r.AuthOutdated(ctx)
			require.NoError(t, err)

			t.Run("THEN they are outdated", func(t *testing.T) {
				assert.True(t, outdated)
			})
		})

		t.Run("WHEN they are saved in the current shape", func(t *testing.T) {
			require.NoError(t, r.SaveAuth(ctx, auth.DefaultSettings()))

			outdated, err := r.AuthOutdated(ctx)
			require.NoError(t, err)

			t.Run("THEN they are no longer outdated", func(t *testing.T) {
				assert.False(t, outdated)
			})
		})
	})
}
