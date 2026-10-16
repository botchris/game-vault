package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// The fixtures in testdata are documents as Game Vault wrote them on 2026-10-13 (game documents
// v3, sources and providers v1, the details cache before it had a stored-form type), with every
// field filled in and invented values. Stored rows outlive the code that wrote them, so reading a
// fixture and writing it again must give back the same bytes: a renamed or retyped field fails here.

func readFixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return string(b)
}

func TestStoredFormats_game(t *testing.T) {
	t.Run("GIVEN a version-3 game document with every field", func(t *testing.T) {
		raw := readFixture(t, "game.v3.json")

		t.Run("WHEN it is decoded", func(t *testing.T) {
			g, err := decodeGame("g1", raw)
			require.NoError(t, err)

			t.Run("THEN the game has its editions and its links' origins", func(t *testing.T) {
				assert.Equal(t, "Console Quest", g.Title())
				assert.Equal(t, "PS5", g.MainSystem())
				assert.Len(t, g.Covers(), 2)
				assert.Equal(t, []string{game.LinkSteam}, g.AutoLinks())
				assert.Equal(t, []game.LinkRef{{
					Key: "gog",
					ID:  "200",
				}}, g.RejectedLinks())
				require.Len(t, g.Copies(), 2)
				assert.Equal(t, "humble:A:quest:0", g.Copies()[0].ExternalID)
			})

			t.Run("AND encoding it again writes the same bytes", func(t *testing.T) {
				again, err := encodeGame(g)
				require.NoError(t, err)
				assert.Equal(t, raw, again)
			})
		})
	})
}

func TestStoredFormats_sourceAndProvider(t *testing.T) {
	t.Run("GIVEN a version-1 source document with every field", func(t *testing.T) {
		raw := readFixture(t, "source.v1.json")

		t.Run("WHEN it is decoded", func(t *testing.T) {
			s, err := decodeSource("src1", raw)
			require.NoError(t, err)

			t.Run("THEN its times are not swapped", func(t *testing.T) {
				assert.True(t, s.CreatedAt().Before(s.UpdatedAt()))
				assert.Equal(t, 24*time.Hour, s.SyncInterval())
			})

			t.Run("AND encoding it again writes the same bytes", func(t *testing.T) {
				again, err := encodeSource(s)
				require.NoError(t, err)
				assert.Equal(t, raw, again)
			})
		})
	})

	t.Run("GIVEN a version-1 provider document with every field", func(t *testing.T) {
		raw := readFixture(t, "provider.v1.json")

		t.Run("WHEN it is decoded and encoded again", func(t *testing.T) {
			p, err := decodeProvider("thegamesdb", raw)
			require.NoError(t, err)

			again, err := encodeProvider(p)
			require.NoError(t, err)

			t.Run("THEN the bytes are the same", func(t *testing.T) {
				assert.Equal(t, provider.KindCover, p.Kind())
				assert.Equal(t, raw, again)
			})
		})
	})
}

func TestStoredFormats_details(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a details row in the format of 2026-10-13", func(t *testing.T) {
		db := openTest(t)
		store := NewDetailsStore(db)
		raw := readFixture(t, "details.json")

		g, err := decodeGame("g1", readFixture(t, "game.v3.json"))
		require.NoError(t, err)
		require.NoError(t, NewGameRepository(db).Save(ctx, g)) // details belong to a game

		_, err = db.conn(ctx).ExecContext(ctx, `INSERT INTO game_details (game_id, language, data, fetched_at) VALUES (?, ?, ?, ?)`,
			"g1", "en", raw, formatTime(docTime))
		require.NoError(t, err)

		t.Run("WHEN it is read", func(t *testing.T) {
			d, ok, err := store.Get(ctx, "g1", "en")
			require.NoError(t, err)
			require.True(t, ok)

			t.Run("THEN every field is there", func(t *testing.T) {
				assert.Equal(t, "Wikipedia", d.SummarySource.Name)
				assert.Equal(t, []string{"Action", "Adventure"}, d.Genres)
				assert.Equal(t, 88, d.Metacritic)
				require.Len(t, d.Videos, 1)
				assert.Equal(t, "abc", d.Videos[0].YouTubeID)
				require.Len(t, d.Screenshots, 1)
				assert.Equal(t, "shot-0.jpg", d.Screenshots[0].FullAsset)
				assert.Equal(t, []provider.ID{"igdb"}, d.NotFoundBy)
				assert.True(t, d.FallbackSkipped)
				assert.True(t, d.FetchedAt.Equal(docTime))
			})

			t.Run("AND the summaries see its genres and date", func(t *testing.T) {
				sums, err := store.Summaries(ctx, "en")
				require.NoError(t, err)
				assert.Equal(t, media.Summary{
					Genres:      []string{"Action", "Adventure"},
					ReleaseDate: "2020-03-01",
					FetchedAt:   sums["g1"].FetchedAt,
				}, sums["g1"])
			})

			t.Run("AND writing it again stores the same bytes", func(t *testing.T) {
				require.NoError(t, store.Put(ctx, "g1", d))

				var again string
				require.NoError(t, db.conn(ctx).QueryRowContext(ctx, `SELECT data FROM game_details WHERE game_id = 'g1'`).Scan(&again))
				assert.Equal(t, raw, again)
			})
		})
	})
}

func TestStoredFormats_connection(t *testing.T) {
	t.Run("GIVEN a version-1 connection document with every field", func(t *testing.T) {
		raw := readFixture(t, "connection.v1.json")

		t.Run("WHEN it is decoded", func(t *testing.T) {
			c, err := decodeConnection("conn1", raw)
			require.NoError(t, err)

			t.Run("THEN its times are not swapped and it is paused", func(t *testing.T) {
				assert.True(t, c.CreatedAt().Before(c.UpdatedAt()))
				assert.True(t, c.Paused())
				assert.Equal(t, "es", c.Language())
			})

			t.Run("AND encoding it again writes the same bytes", func(t *testing.T) {
				again, err := encodeConnection(c)
				require.NoError(t, err)
				assert.Equal(t, raw, again)
			})
		})
	})
}
