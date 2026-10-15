package igdb

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// releaseNow is the clock of the release tests: 2026-10-15 00:00 UTC (Unix 1792022400).
var releaseNow = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

// Upcoming games as a games query with the release fields returns them (invented values).
const (
	harborLights = `{"id": 3001, "name": "Harbor Lights", "first_release_date": 1796083200,
  "url": "https://www.igdb.com/games/harbor-lights", "cover": {"id": 1, "image_id": "co-hl"},
  "platforms": [6, 167, 999], "franchises": [], "collections": []}`

	lanternKeepII = `{"id": 2001, "name": "Lantern Keep II", "first_release_date": 1795132800,
  "url": "https://www.igdb.com/games/lantern-keep-ii", "cover": {"id": 2, "image_id": "co-lk2"},
  "platforms": [6], "franchise": 40, "franchises": [40], "collections": [70]}`

	echoValleyRemake = `{"id": 2002, "name": "Echo Valley Remake", "first_release_date": 1796860800,
  "url": "https://www.igdb.com/games/echo-valley-remake", "platforms": [48, 167], "franchises": [41], "collections": []}`
)

func TestReleases_anticipated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN IGDB knows one anticipated game", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.anticipated = "[" + harborLights + "]"
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the releases of the next 60 days are asked for", func(t *testing.T) {
			got, err := r.Anticipated(ctx, goodSettings, 60*24*time.Hour)
			require.NoError(t, err)

			t.Run("THEN one query asks for hyped main games in the window, most hyped first", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "where first_release_date >= 1792022400 & first_release_date < 1797206400")
				assert.Contains(t, sent[0], "hypes > 0")
				assert.Contains(t, sent[0], "version_parent = null & parent_game = null")
				assert.Contains(t, sent[0], "sort hypes desc; limit 40;")
			})

			t.Run("AND the game comes with its date, known systems, cover and page", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, game.Release{
					ProviderGameID: "3001",
					Title:          "Harbor Lights",
					Date:           "2026-12-01",
					Platforms:      []string{game.SystemPC, game.SystemPS5},
					CoverURL:       "https://images.igdb.com/igdb/image/upload/t_cover_big/co-hl.jpg",
					URL:            "https://www.igdb.com/games/harbor-lights",
				}, got[0])
			})

			t.Run("AND the cover's host is declared, so the page loads it through the image proxy", func(t *testing.T) {
				u, err := url.Parse(got[0].CoverURL)
				require.NoError(t, err)
				assert.Contains(t, r.ImageHosts(), u.Hostname())
			})
		})
	})

	t.Run("GIVEN no credentials", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the anticipated releases are asked for", func(t *testing.T) {
			_, err := r.Anticipated(ctx, schema.Settings{}, 60*24*time.Hour)

			t.Run("THEN the settings are missing, without a request", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Empty(t, f.sent("games"))
			})
		})
	})
}

func TestReleases_sequels(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	owned := []home.OwnedRef{
		{
			Game:  "g1",
			Title: "Lantern Keep",
			Links: game.Links{game.LinkSteam: "4410"},
		},
		{
			Game:  "g2",
			Title: "Echo Valley",
			Links: game.Links{game.LinkSteam: "4420", game.LinkGOG: "1207658924"},
		},
		{
			Game:  "g3",
			Title: "Copper Coast",
		}, // a disc: no store link
	}

	t.Run("GIVEN two owned games IGDB knows by their Steam apps, in a collection and a franchise", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[
  {"id": 5, "uid": "4410", "game": {"id": 1001, "franchise": 40, "franchises": [40], "collections": [70]}},
  {"id": 6, "uid": "4420", "game": {"id": 1002, "franchises": [41], "collections": []}}
]`
		// IGDB also lists an owned game itself as upcoming in its series when it is re-released:
		// 1001 must not come back.
		f.series = "[" + lanternKeepII + "," + echoValleyRemake + `,{"id": 1001, "name": "Lantern Keep", "first_release_date": 1794000000}]`
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels of the owned games are asked for", func(t *testing.T) {
			got, err := r.Sequels(ctx, goodSettings, owned)
			require.NoError(t, err)

			t.Run("THEN the games are found by their Steam apps only, in one query", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], `where external_game_source = 1 & uid = ("4410","4420");`)
				assert.Contains(t, sent[0], "game.franchise,game.franchises,game.collections")
			})

			t.Run("AND one query asks for upcoming main games of their series, soonest first", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "collections = (70) | franchises = (40,41) | franchise = (40,41)")
				assert.Contains(t, sent[0], "first_release_date > 1792022400")
				assert.Contains(t, sent[0], "sort first_release_date asc;")
			})

			t.Run("AND each release names the owned game it continues, the collection's before the franchise's", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, "Lantern Keep II", got[0].Title)
				assert.Equal(t, game.ID("g1"), got[0].Because)
				assert.Equal(t, "Lantern Keep", got[0].BecauseTitle)
				assert.Equal(t, "Echo Valley Remake", got[1].Title)
				assert.Equal(t, game.ID("g2"), got[1].Because)
				assert.Equal(t, []string{game.SystemPS4, game.SystemPS5}, got[1].Platforms)
				assert.Empty(t, got[1].CoverURL)
			})
		})
	})

	t.Run("GIVEN no owned game has a Steam link", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for, for an empty collection too", func(t *testing.T) {
			got, err := r.Sequels(ctx, goodSettings, owned[2:])
			require.NoError(t, err)
			none, err := r.Sequels(ctx, goodSettings, nil)
			require.NoError(t, err)

			t.Run("THEN IGDB is not asked at all", func(t *testing.T) {
				assert.Empty(t, got)
				assert.Empty(t, none)
				assert.Empty(t, f.sent("external_games"))
				assert.Empty(t, f.sent("games"))
			})
		})
	})

	t.Run("GIVEN 501 owned games on Steam", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		many := make([]home.OwnedRef, 501)
		for i := range many {
			many[i] = home.OwnedRef{
				Game:  game.ID(fmt.Sprintf("g%d", i)),
				Links: game.Links{game.LinkSteam: fmt.Sprint(10000 + i)},
			}
		}

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			_, err := r.Sequels(ctx, goodSettings, many)
			require.NoError(t, err)

			t.Run("THEN they are looked up in batches of IGDB's largest limit", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 2)
				assert.Equal(t, 500, strings.Count(sent[0], `"`)/2)
				assert.Equal(t, 1, strings.Count(sent[1], `"`)/2)
			})
		})
	})

	t.Run("GIVEN a batch of Steam apps listed under more IGDB games than the limit", func(t *testing.T) {
		f := newFakeIGDB(t)

		many := make([]home.OwnedRef, maxBatch)
		rows := make([]string, maxBatch)

		for i := range many {
			app := fmt.Sprint(10000 + i)
			many[i] = home.OwnedRef{
				Game:  game.ID(fmt.Sprintf("g%d", i)),
				Links: game.Links{game.LinkSteam: app},
			}
			rows[i] = fmt.Sprintf(`{"id": %d, "uid": %q, "game": {"id": %d, "franchises": [], "collections": []}}`, i+1, app, 1000+i)
		}

		// The last app is also listed under a second game, the one in a collection: its row only
		// comes on the second page.
		f.externalPages = []string{
			"[" + strings.Join(rows, ",") + "]",
			`[{"id": 9999, "uid": "10499", "game": {"id": 9001, "franchises": [], "collections": [70]}}]`,
		}
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			_, err := r.Sequels(ctx, goodSettings, many)
			require.NoError(t, err)

			t.Run("THEN the full page is followed by the next one, in a stable order", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 2)
				assert.Contains(t, sent[0], "sort id asc; limit 500; offset 0;")
				assert.Contains(t, sent[1], "sort id asc; limit 500; offset 500;")
			})

			t.Run("AND the rows of the second page count", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "collections = (70)")
			})
		})
	})

	t.Run("GIVEN two owned games of the same franchise, which IGDB lists in the reverse order", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[
  {"id": 6, "uid": "4420", "game": {"id": 1002, "franchises": [40], "collections": []}},
  {"id": 5, "uid": "4410", "game": {"id": 1001, "franchises": [40], "collections": []}}
]`
		f.series = "[" + lanternKeepII + "]"
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			got, err := r.Sequels(ctx, goodSettings, owned[:2])
			require.NoError(t, err)

			t.Run("THEN the sequel names the first owned game, whatever IGDB's order", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, game.ID("g1"), got[0].Because)
			})
		})
	})

	t.Run("GIVEN IGDB asks to slow down", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.setBusy(true)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			_, err := r.Sequels(ctx, goodSettings, owned)

			t.Run("THEN the provider is rate-limited", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

func TestReleases_test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a working Twitch application", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := r.Test(ctx, goodSettings)

			t.Run("THEN it works, with the anticipated query a refresh sends", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 1, f.tokens())

				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "where first_release_date >= 1792022400 & first_release_date < 1797206400")
				assert.Contains(t, sent[0], "sort hypes desc; limit 40;")
			})
		})
	})

	t.Run("GIVEN credentials Twitch rejects", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN they are tested", func(t *testing.T) {
			err := r.Test(ctx, schema.Settings{settingClientID: "bad-id", settingClientSecret: "good-secret"})

			t.Run("THEN the settings are wrong, without a query", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Empty(t, f.sent("games"))
			})
		})
	})
}
