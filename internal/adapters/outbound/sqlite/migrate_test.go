package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

func TestOpen_backupBeforeMigrating(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a new database", func(t *testing.T) {
		dir := t.TempDir()
		backups := filepath.Join(dir, "backups")

		t.Run("WHEN it is opened", func(t *testing.T) {
			db, err := Open(ctx, filepath.Join(dir, "new.db"), backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN nothing is backed up: there is nothing to lose", func(t *testing.T) {
				files, _ := filepath.Glob(filepath.Join(backups, "*.db"))
				assert.Empty(t, files)
			})
		})
	})

	t.Run("GIVEN a database migrated up to 0007", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0007")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN it is opened with the pending migrations", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN a copy named after the first pending migration was written first", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0008.db"))
			})
		})

		t.Run("WHEN that copy is restored and opened again", func(t *testing.T) {
			restored := filepath.Join(dir, "restored.db")
			data, err := os.ReadFile(filepath.Join(backups, "pre-migration-0008.db"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(restored, data, 0o600))

			db, err := Open(ctx, restored, backups)

			t.Run("THEN it migrates, keeping the first copy and writing a timestamped one", func(t *testing.T) {
				require.NoError(t, err)
				db.Close()

				files, _ := filepath.Glob(filepath.Join(backups, "pre-migration-0008*.db"))
				assert.Len(t, files, 2)
			})
		})
	})
}

func TestMigration0009(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database as version 0008 left it, with rows of every shape", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "old.db")

		old, err := openUpTo(ctx, path, "", "0008")
		require.NoError(t, err)

		_, err = old.sql.ExecContext(ctx, `
			INSERT INTO sources (id, type, name, enabled, sync_interval_seconds, settings, last_sync, created_at, updated_at) VALUES
			  ('s1', 'humble', 'Humble Bundle', 0, 86400, '{"session":"secret"}',
			   '{"startedAt":"2026-10-08T10:00:00Z","finishedAt":"2026-10-08T10:01:00Z","fetched":2,"copiesAdded":1,"copiesUpdated":0,"copiesUnchanged":1,"gamesCreated":1,"warnings":["w"]}',
			   '2026-10-01T10:00:00Z', '2026-10-08T10:01:00Z'),
			  ('s2', 'steam', 'Steam', 1, 0, '{}', NULL, '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO games (id, title, notes, cover_url, links, created_at, updated_at) VALUES
			  ('g1', 'Hades', 'GOTY', '', '{"steam":"1145360"}', '2026-10-01T10:00:00Z', '2026-10-02T10:00:00Z'),
			  ('g2', 'Empty', '', '', '{}', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO copies (id, game_id, kind, platform, status, cd_key, redeem_by, origin, acquired_on, edition,
			                    condition, location, barcode, notes, source_id, external_id, created_at, updated_at) VALUES
			  ('c2', 'g1', 'physical', 'PS4', 'owned', '', '', 'GAME', '2021-07-11', 'Collector''s', 'Complete', 'Shelf',
			   '2001234567893', 'signed', NULL, '', '2026-10-02T10:00:00Z', '2026-10-02T10:00:00Z'),
			  ('c1', 'g1', 'key', 'Steam', 'revealed', 'AAAA', '2027-01-01', 'Humble', '', '', '', '', '', '', 's1',
			   'humble:A:hades:0', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO providers (id, kind, enabled, priority, settings, updated_at) VALUES
			  ('thegamesdb', 'cover', 0, 0, '{"api_key":"k"}', '2026-10-01T10:00:00Z'),
			  ('steam', 'cover', 1, 1, '{}', '2026-10-01T10:00:00Z');
			INSERT INTO game_details (game_id, language, data, fetched_at) VALUES ('g1', 'en', '{}', '2026-10-01T10:00:00Z');`)
		require.NoError(t, err)
		old.Close()

		t.Run("WHEN it is opened by this version", func(t *testing.T) {
			db, err := Open(ctx, path, "")
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })

			// The migration writes version-1 game documents, which this version no longer reads (the
			// releases that converted them are gone), so the stored documents are checked as JSON.
			t.Run("THEN games keep their fields and their copies, in their order", func(t *testing.T) {
				docs := map[string]gameDoc{}
				rows, err := db.sql.QueryContext(ctx, `SELECT id, doc FROM games`)
				require.NoError(t, err)

				for rows.Next() {
					var (
						id  string
						raw string
						doc gameDoc
					)

					require.NoError(t, rows.Scan(&id, &raw))
					require.NoError(t, json.Unmarshal([]byte(raw), &doc))
					docs[id] = doc
				}

				require.NoError(t, rows.Err())
				rows.Close()

				require.Len(t, docs, 2)
				assert.Equal(t, 1, docs["g1"].V)
				assert.Equal(t, "Empty", docs["g2"].Title)
				assert.Empty(t, docs["g2"].Copies)

				hades := docs["g1"]
				assert.Equal(t, game.Links{"steam": "1145360"}, hades.Links)
				assert.Equal(t, "GOTY", hades.Notes)
				require.Len(t, hades.Copies, 2)
				assert.Equal(t, "c1", hades.Copies[0].ID, "copies come in creation order")
				assert.Equal(t, "s1", hades.Copies[0].SourceID)
				assert.Equal(t, "AAAA", hades.Copies[0].Key)
				assert.Empty(t, hades.Copies[1].SourceID, "a manual copy has no source")
				assert.Equal(t, "2001234567893", hades.Copies[1].Barcode)
				assert.Equal(t, "Shelf", hades.Copies[1].Location)
			})

			t.Run("AND this version refuses to read those games, saying their version is no longer supported", func(t *testing.T) {
				_, err := NewGameRepository(db).List(ctx)
				assert.ErrorIs(t, err, errOlderDocument)
			})

			t.Run("AND sources keep their state, disabled ones disabled", func(t *testing.T) {
				srcs, err := NewSourceRepository(db).List(ctx)
				require.NoError(t, err)
				require.Len(t, srcs, 2)
				assert.Equal(t, "Humble Bundle", srcs[0].Name())
				assert.False(t, srcs[0].Enabled())
				assert.Equal(t, 24*time.Hour, srcs[0].SyncInterval())
				assert.Equal(t, "secret", srcs[0].Settings()["session"])
				require.NotNil(t, srcs[0].LastSync())
				assert.Equal(t, []string{"w"}, srcs[0].LastSync().Warnings)
				assert.True(t, srcs[1].Enabled())
				assert.Nil(t, srcs[1].LastSync(), "a source never scanned has no report")
			})

			t.Run("AND providers keep their order and state", func(t *testing.T) {
				ps, err := NewProviderRepository(db).List(ctx, provider.KindCover)
				require.NoError(t, err)
				require.Len(t, ps, 2)
				assert.Equal(t, provider.ID("thegamesdb"), ps[0].ID())
				assert.False(t, ps[0].Enabled())
				assert.Equal(t, "k", ps[0].Settings()["api_key"])
				assert.True(t, ps[1].Enabled())
			})

			t.Run("AND the cached details survive", func(t *testing.T) {
				var n int
				require.NoError(t, db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_details WHERE game_id = 'g1'`).Scan(&n))
				assert.Equal(t, 1, n)
			})
		})
	})
}

func TestMigration0010(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database migrated up to 0009", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0009")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN this version opens it", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN it was copied before game documents start being written as version 3", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0010.db"))
			})
		})
	})
}

func TestMigration0011(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database migrated up to 0010", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0010")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN this version opens it", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)

			defer db.Close()

			t.Run("THEN it was copied first, and access tokens can be stored", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0011.db"))
				assert.NoError(t, NewAuthRepository(db).SaveAccessToken(ctx, auth.AccessToken{
					ID:        "t1",
					Name:      "Extension, laptop",
					Hash:      auth.HashToken("gvt_one"),
					CreatedAt: time.Now(),
				}))
			})
		})
	})
}

func TestMigration0012(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database migrated up to 0011", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0011")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN this version opens it", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)

			defer db.Close()

			t.Run("THEN it was copied first, and home snapshots can be stored", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0012.db"))
				assert.NoError(t, NewHomeSnapshots(db).SaveSnapshot(ctx, home.Snapshot{Block: home.BlockDeals}))
			})
		})
	})
}
