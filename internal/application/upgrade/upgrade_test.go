package upgrade_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/upgrade"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
)

// The documents below are written in the shapes older versions stored (invented values): version
// 1 had the copies' free-text condition, version 2 the game's single cover (coverUrl, or coverPhoto
// naming a photo of one of its copies).
const (
	photo = "abababababababababababababababababababababababababababababababab"

	// v1Game has one PS3 copy, a condition and no cover.
	v1Game = `{"v":1,"title":"Old Racer","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z",` +
		`"copies":[{"id":"c1","kind":"physical","platform":"PS3","status":"owned","condition":"Sealed",` +
		`"createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`

	// v2URLGame has copies on two systems and a chosen cover URL.
	v2URLGame = `{"v":2,"title":"Twin Quest","coverUrl":"https://example.test/twin.jpg",` +
		`"createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z","copies":[` +
		`{"id":"c1","kind":"physical","platform":"PS3","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"},` +
		`{"id":"c2","kind":"physical","platform":"Xbox 360","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`

	// v2PhotoGame has copies on two systems and a photo of the Xbox 360 copy as its cover.
	v2PhotoGame = `{"v":2,"title":"Snap Shot","coverPhoto":"` + photo + `",` +
		`"createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z","copies":[` +
		`{"id":"c1","kind":"physical","platform":"PS3","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"},` +
		`{"id":"c2","kind":"physical","platform":"Xbox 360","status":"owned",` +
		`"photos":[{"id":"` + photo + `","addedAt":"2026-01-01T10:00:00Z"}],` +
		`"createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`

	// v2BrokenGame cannot be read: its copy's contents are unknown.
	v2BrokenGame = `{"v":2,"title":"Broken","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z",` +
		`"copies":[{"id":"c1","kind":"physical","platform":"PS3","status":"owned","contents":["bogus"],` +
		`"createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`

	// v3Game is current: copies on two systems and no chosen cover.
	v3Game = `{"v":3,"title":"Fresh Start","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z","copies":[` +
		`{"id":"c1","kind":"physical","platform":"PS3","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"},` +
		`{"id":"c2","kind":"physical","platform":"Xbox 360","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`

	// v3Plain is current, with one PS3 copy.
	v3Plain = `{"v":3,"title":"Plain","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-02T10:00:00Z","copies":[` +
		`{"id":"c1","kind":"physical","platform":"PS3","status":"owned","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T10:00:00Z"}]}`
)

var jpeg = media.Image{
	Data:        []byte("\xff\xd8\xff fake jpeg"),
	ContentType: "image/jpeg",
}

// env is a server's storage as an older version left it: the database file (also open raw, to
// write and read the stored rows as they are), the game-data folder and the upgrade over them.
type env struct {
	raw     *sql.DB
	games   *sqlite.GameRepository
	assets  *gamedata.Store
	upgrade *upgrade.Service
	logs    *bytes.Buffer
}

func newEnv(t *testing.T, ctx context.Context, rows map[string]string, authRow string, legacy map[string]string) *env {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "gamevault.db")

	db, err := sqlite.Open(ctx, path, "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { raw.Close() })

	for id, doc := range rows {
		_, err := raw.ExecContext(ctx, `INSERT INTO games (id, doc) VALUES (?, ?)`, id, doc)
		require.NoError(t, err)
	}

	if authRow != "" {
		_, err := raw.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('auth', ?, '2026-01-01T10:00:00Z')`, authRow)
		require.NoError(t, err)
	}

	// The legacy files sit in folders named as the game-data store names them: "<title> [<id>]".
	root := filepath.Join(dir, "game-data")
	for folder, file := range legacy {
		require.NoError(t, os.MkdirAll(filepath.Join(root, folder), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, folder, file), jpeg.Data, 0o644))
	}

	assets, err := gamedata.Open(root)
	require.NoError(t, err)

	games := sqlite.NewGameRepository(db)
	settings := sqlite.NewSettingsRepository(db)
	reg := providers.NewRegistry(sqlite.NewProviderRepository(db), nil)
	covers := media.New(games, reg, assets, noFetch{}).Covers

	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))

	return &env{
		raw:     raw,
		games:   games,
		assets:  assets,
		upgrade: upgrade.NewService(games, db, settings, covers, upgrade.WithLogger(log)),
		logs:    logs,
	}
}

// stored returns the stored document of a game, as it is in the database.
func (e *env) stored(t *testing.T, ctx context.Context, id string) string {
	t.Helper()

	var doc string
	require.NoError(t, e.raw.QueryRowContext(ctx, `SELECT doc FROM games WHERE id = ?`, id).Scan(&doc))

	return doc
}

func (e *env) storedAuth(t *testing.T, ctx context.Context) string {
	t.Helper()

	var value string
	require.NoError(t, e.raw.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'auth'`).Scan(&value))

	return value
}

type noFetch struct{}

func (noFetch) Fetch(context.Context, string) (media.Image, error) {
	return media.Image{}, errors.New("no downloads in this test")
}

func TestService_Run(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	t.Run("GIVEN games stored by older versions, and covers cached before editions", func(t *testing.T) {
		e := newEnv(t, ctx, map[string]string{
			"0001": v1Game,
			"0002": v2URLGame,
			"0003": v2PhotoGame,
			"0004": v2BrokenGame,
			"0005": v3Game,
			"0006": v3Plain,
		}, "", map[string]string{
			"Old Racer [0001]":   "cover.jpg",     // one system: its edition adopts it
			"Twin Quest [0002]":  "cover.jpg",     // two systems, a chosen cover: the main edition adopts it
			"Fresh Start [0005]": "cover.jpg",     // two systems, nothing chosen: it may show the other box, so it goes
			"Plain [0006]":       "cover.missing", // only the "no cover found" marker, which nothing reads
			"Deleted [0099]":     "cover.jpg",     // the game is gone
		})
		current := e.stored(t, ctx, "0005")

		t.Run("WHEN the upgrade runs", func(t *testing.T) {
			e.upgrade.Run(ctx)

			t.Run("THEN every readable game is stored in the current version, and only those", func(t *testing.T) {
				for _, id := range []string{"0001", "0002", "0003"} {
					doc := e.stored(t, ctx, id)
					assert.Contains(t, doc, `"v":3`, id)
					assert.NotContains(t, doc, `"coverUrl"`, id)
					assert.NotContains(t, doc, `"coverPhoto"`, id)
					assert.NotContains(t, doc, `"condition"`, id)
					assert.Contains(t, doc, `"updatedAt":"2026-01-02T10:00:00Z"`, id, "only re-encoded, never touched")
				}

				assert.Equal(t, current, e.stored(t, ctx, "0005"), "a current document is not rewritten")
			})

			t.Run("AND the version-1 condition became a grade and contents", func(t *testing.T) {
				g, err := e.games.Get(ctx, "0001")
				require.NoError(t, err)
				assert.Equal(t, game.GradeSealed, g.Copies()[0].Grade)
				assert.Contains(t, e.stored(t, ctx, "0001"), `"grade":"sealed"`)
			})

			t.Run("AND the chosen cover URL is on the main edition", func(t *testing.T) {
				doc := e.stored(t, ctx, "0002")
				assert.Contains(t, doc, `"covers":{"PS3":{"url":"https://example.test/twin.jpg"}}`)
				assert.Contains(t, doc, `"mainSystem":"PS3"`)
			})

			t.Run("AND the chosen photo is on its copy's edition, which became the main one", func(t *testing.T) {
				doc := e.stored(t, ctx, "0003")
				assert.Contains(t, doc, `"covers":{"Xbox 360":{"photo":"`+photo+`"}}`)
				assert.Contains(t, doc, `"mainSystem":"Xbox 360"`)
			})

			t.Run("AND a game that cannot be read is left as it was, with a warning naming it", func(t *testing.T) {
				assert.Equal(t, v2BrokenGame, e.stored(t, ctx, "0004"))
				assert.Contains(t, e.logs.String(), "level=WARN")
				assert.Contains(t, e.logs.String(), "game=0004")
			})

			t.Run("AND the covers cached before editions are adopted by the main edition or removed", func(t *testing.T) {
				_, ok, err := e.assets.GetCover("0001", "PS3")
				require.NoError(t, err)
				assert.True(t, ok, "the only edition adopts it")

				_, ok, err = e.assets.GetCover("0002", "PS3")
				require.NoError(t, err)
				assert.True(t, ok, "the main edition, which has the chosen cover, adopts it")

				_, ok, err = e.assets.GetCover("0005", "PS3")
				require.NoError(t, err)
				assert.False(t, ok, "a game on two systems without a chosen cover does not adopt it")

				assert.Empty(t, e.assets.LegacyCoverIDs())
			})

			t.Run("AND one line per part says what was done", func(t *testing.T) {
				out := e.logs.String()
				assert.Contains(t, out, "games rewritten in the current format")
				assert.Contains(t, out, "count=3")
				assert.Contains(t, out, "adopted=2 removed=3")
				assert.NotContains(t, out, "security settings", "there were none to rewrite")
			})
		})

		t.Run("WHEN the upgrade runs again (the next start)", func(t *testing.T) {
			e.logs.Reset()

			before := map[string]string{}

			for _, id := range []string{"0001", "0002", "0003", "0005", "0006"} {
				before[id] = e.stored(t, ctx, id)
			}

			e.upgrade.Run(ctx)

			t.Run("THEN nothing changes and only the unreadable game is reported again", func(t *testing.T) {
				for id, doc := range before {
					assert.Equal(t, doc, e.stored(t, ctx, id), id)
				}

				out := e.logs.String()
				assert.NotContains(t, out, "rewritten")
				assert.NotContains(t, out, "adopted")
				assert.Contains(t, out, "game=0004")
			})
		})
	})
}

func TestService_Run_authSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	for _, c := range []struct {
		row  string
		want auth.Authentication
	}{
		{`{"localBypass":true,"trustTailscale":true,"tailscaleLogins":[]}`, auth.AuthTrustedNetworks},
		{`{"localBypass":false,"trustTailscale":true,"tailscaleLogins":["you@example.com"]}`, auth.AuthRequired},
	} {
		t.Run("GIVEN security settings saved as "+c.row, func(t *testing.T) {
			e := newEnv(t, ctx, nil, c.row, nil)

			t.Run("WHEN the upgrade runs", func(t *testing.T) {
				e.upgrade.Run(ctx)

				t.Run("THEN they are stored in the current shape, with the same meaning", func(t *testing.T) {
					value := e.storedAuth(t, ctx)
					assert.Contains(t, value, `"authentication":"`+string(c.want)+`"`)
					assert.Contains(t, value, `"trustedNetworks":["127.0.0.0/8","::1/128"]`)
					assert.NotContains(t, value, "localBypass")
					assert.NotContains(t, value, "tailscale")
				})

				t.Run("AND one line says so, without the values", func(t *testing.T) {
					out := e.logs.String()
					assert.Contains(t, out, "security settings rewritten in the current format")
					assert.NotContains(t, out, string(c.want))
				})
			})

			t.Run("WHEN the upgrade runs again", func(t *testing.T) {
				e.logs.Reset()
				before := e.storedAuth(t, ctx)

				e.upgrade.Run(ctx)

				t.Run("THEN nothing is rewritten or logged", func(t *testing.T) {
					assert.Equal(t, before, e.storedAuth(t, ctx))
					assert.Empty(t, strings.TrimSpace(e.logs.String()))
				})
			})
		})
	}
}
