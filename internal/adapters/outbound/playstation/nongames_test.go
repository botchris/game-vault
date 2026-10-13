package playstation

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// playedFixture is one played title of the fake gamelist API.
type playedFixture struct {
	titleID       string
	name          string
	category      string
	conceptID     int
	conceptTitles []string
}

// fakeGameListPage is how many titles the fake answers per page, whatever limit is asked, so a
// handful of titles already spans several pages.
const fakeGameListPage = 2

// gameList mimics GET /api/gamelist/v2/users/me/titles. The shape follows psn-api's
// UserPlayedGamesResponse (getUserPlayedGames); the 401 body is the one the real endpoint gives a
// bogus token (probed 2026-10-12). Values are invented.
func (f *fakeSony) gameList(w http.ResponseWriter, r *http.Request) {
	categories := r.URL.Query().Get("categories")

	if f.gameListCalls == nil {
		f.gameListCalls = map[string]int{}
	}

	f.gameListCalls[categories]++

	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") || f.gameListStatus == http.StatusUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"referenceId":"00000000-0000-0000-0000-000000000000","code":2241801,"message":"Invalid token","reason":"invalidToken","source":"token"}}`)

		return
	}

	if f.gameListRaw != "" {
		fmt.Fprint(w, f.gameListRaw)
		return
	}

	if f.gameListStatus != 0 {
		w.WriteHeader(f.gameListStatus)
		fmt.Fprint(w, `{"error":{"code":2240513,"message":"Internal server error"}}`)

		return
	}

	var titles []playedFixture

	for _, t := range f.played {
		if categories == "" || slices.Contains(strings.Split(categories, ","), t.category) {
			titles = append(titles, t)
		}
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	end := min(offset+fakeGameListPage, len(titles))

	var items []string

	for _, t := range titles[min(offset, end):end] {
		ids := `"` + strings.Join(t.conceptTitles, `","`) + `"`
		if len(t.conceptTitles) == 0 {
			ids = ""
		}

		items = append(items, fmt.Sprintf(`{"titleId":%q,"name":%q,"localizedName":%q,"imageUrl":"https://image.api.playstation.com/x.png",
			"category":%q,"service":"none_purchased","playCount":3,"concept":{"id":%d,"titleIds":[%s],"name":%q,"genres":[]},
			"firstPlayedDateTime":"2025-01-02T10:00:00.000Z","lastPlayedDateTime":"2025-02-03T10:00:00.000Z","playDuration":"PT2H10M"}`,
			t.titleID, t.name, t.name, t.category, t.conceptID, ids, t.name))
	}

	next := ""
	if end < len(titles) {
		next = fmt.Sprintf(`"nextOffset":%d,`, end)
	}

	fmt.Fprintf(w, `{"titles":[%s],%s"previousOffset":%d,"totalItemCount":%d}`, strings.Join(items, ","), next, max(offset-fakeGameListPage, 0), len(titles))
}

// libraryWithNonGames is a purchased list mixing games with an app, a video and a demo, all as
// PS4 / PS5 titles, the way the library website lists them. Titles are invented.
var libraryWithNonGames = []string{
	`{"name":"Moonlit Harbor","platform":"PS4","entitlementId":"EP0001-CUSA10001_00-MOONLIT","titleId":"CUSA10001_00","conceptId":"1001","membership":"NONE","isActive":true}`,
	`{"name":"StreamBox","platform":"PS4","entitlementId":"EP0002-CUSA20002_00-STREAMBOX","titleId":"CUSA20002_00","conceptId":"2002","membership":"NONE","isActive":true}`,
	`{"name":"Glass Orchard","platform":"PS5","entitlementId":"EP0003-PPSA30003_00-ORCHARD","titleId":"PPSA30003_00","conceptId":"3003","membership":"NONE","isActive":true}`,
	`{"name":"Tunebox","platform":"PS5","entitlementId":"EP0004-PPSA40004_00-TUNEBOX","titleId":"PPSA40004_00","conceptId":"4004","membership":"NONE","isActive":true}`,
	`{"name":"Clockwork Tales Extras","platform":"PS4","entitlementId":"EP0005-CUSA50005_00-EXTRAS","titleId":"CUSA50005_00","conceptId":"","membership":"NONE","isActive":true}`,
	`{"name":"Paper Kites Demo","platform":"PS4","entitlementId":"EP0006-CUSA60006_00-KITESDEMO","titleId":"CUSA60006_00","conceptId":"6006","membership":"NONE","isActive":true}`,
	`{"name":"Iron Valley: The Making of","platform":"PS4","entitlementId":"EP0007-CUSA70007_00-MAKING","titleId":"CUSA70007_00","conceptId":"7007","membership":"NONE","isActive":true}`,
}

// playedWithNonGames are the played titles of that account: the game and the apps.
var playedWithNonGames = []playedFixture{
	{
		titleID:       "CUSA10001_00",
		name:          "Moonlit Harbor",
		category:      "ps4_game",
		conceptID:     1001,
		conceptTitles: []string{"CUSA10001_00"},
	},
	{
		titleID:       "CUSA20002_00",
		name:          "StreamBox",
		category:      "ps4_videoservice_web_app",
		conceptID:     2002,
		conceptTitles: []string{"CUSA20002_00"},
	},
	// Played under the PS5 version's title id: only the concept says it is the same app.
	{
		titleID:   "PPSA40099_00",
		name:      "Tunebox",
		category:  "ps5_web_based_media_app",
		conceptID: 4004,
	},
	// The purchase names no concept, but the concept lists its title id.
	{
		titleID:       "CUSA50099_00",
		name:          "Clockwork Tales Extras",
		category:      "unknown_media_app",
		conceptID:     5005,
		conceptTitles: []string{"CUSA50099_00", "CUSA50005_00"},
	},
	{
		titleID:       "PPSA80008_00",
		name:          "Starfall Drift",
		category:      "ps5_native_game",
		conceptID:     8008,
		conceptTitles: []string{"PPSA80008_00"},
	},
}

func byExternalID(copies []game.ImportedCopy) map[string]game.ImportedCopy {
	out := map[string]game.ImportedCopy{}
	for _, c := range copies {
		out[c.ExternalID] = c
	}

	return out
}

func TestFetch_nonGames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a library with games, apps, a demo and a video, and the titles played on the account", func(t *testing.T) {
		p, f := newTest(t)
		f.library = libraryWithNonGames
		f.played = playedWithNonGames

		t.Run("WHEN it is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, schema.Settings{settingNPSSO: "good-npsso"})
			require.NoError(t, err)
			assert.Empty(t, warnings, "what is not a game goes to the log, not to a warning on every scan")

			got := byExternalID(copies)
			require.Len(t, got, len(libraryWithNonGames))

			t.Run("THEN an app played under a non-game category is withdrawn", func(t *testing.T) {
				c := got["psn:EP0002-CUSA20002_00-STREAMBOX"]
				assert.True(t, c.Withdrawn)
				assert.Equal(t, "StreamBox", c.Title)
			})

			t.Run("AND an app matched by its concept is withdrawn", func(t *testing.T) {
				assert.True(t, got["psn:EP0004-PPSA40004_00-TUNEBOX"].Withdrawn)
			})

			t.Run("AND an app whose concept lists the purchased title id is withdrawn", func(t *testing.T) {
				assert.True(t, got["psn:EP0005-CUSA50005_00-EXTRAS"].Withdrawn)
			})

			t.Run("AND titles whose names mark a demo or a making-of are withdrawn", func(t *testing.T) {
				assert.True(t, got["psn:EP0006-CUSA60006_00-KITESDEMO"].Withdrawn)
				assert.True(t, got["psn:EP0007-CUSA70007_00-MAKING"].Withdrawn)
			})

			t.Run("AND a played game stays", func(t *testing.T) {
				c := got["psn:EP0001-CUSA10001_00-MOONLIT"]
				assert.False(t, c.Withdrawn)
				assert.Equal(t, "PS4", c.Details.Platform)
			})

			t.Run("AND a title never played stays", func(t *testing.T) {
				assert.False(t, got["psn:EP0003-PPSA30003_00-ORCHARD"].Withdrawn)
			})

			t.Run("AND both played-titles lists were read to the end, page by page", func(t *testing.T) {
				assert.Equal(t, 3, f.gameListCalls[""])
				assert.Equal(t, 1, f.gameListCalls[gameCategories])
			})
		})
	})

	t.Run("GIVEN a played-titles list that only ever lists games, whatever is asked", func(t *testing.T) {
		p, f := newTest(t)
		f.library = libraryWithNonGames
		f.played = []playedFixture{playedWithNonGames[0], playedWithNonGames[4]}

		t.Run("WHEN it is scanned", func(t *testing.T) {
			copies, _, err := p.Fetch(ctx, schema.Settings{settingNPSSO: "good-npsso"})
			require.NoError(t, err)

			t.Run("THEN only the titles whose names mark them are withdrawn", func(t *testing.T) {
				var withdrawn []string

				for _, c := range copies {
					if c.Withdrawn {
						withdrawn = append(withdrawn, c.Title)
					}
				}

				assert.ElementsMatch(t, []string{"Paper Kites Demo", "Iron Valley: The Making of"}, withdrawn)
			})
		})
	})

	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("GIVEN a played-titles list that answers HTTP %d", status), func(t *testing.T) {
			p, f := newTest(t)
			f.library = libraryWithNonGames
			f.played = playedWithNonGames
			f.gameListStatus = status

			t.Run("WHEN it is scanned", func(t *testing.T) {
				copies, warnings, err := p.Fetch(ctx, schema.Settings{settingNPSSO: "good-npsso"})

				t.Run("THEN the scan succeeds and only the name markers exclude titles", func(t *testing.T) {
					require.NoError(t, err)
					assert.Empty(t, warnings)

					got := byExternalID(copies)
					assert.False(t, got["psn:EP0002-CUSA20002_00-STREAMBOX"].Withdrawn)
					assert.False(t, got["psn:EP0004-PPSA40004_00-TUNEBOX"].Withdrawn)
					assert.True(t, got["psn:EP0006-CUSA60006_00-KITESDEMO"].Withdrawn)
				})
			})
		})
	}

	t.Run("GIVEN a played-titles list that answers something that is not JSON", func(t *testing.T) {
		p, f := newTest(t)
		f.library = libraryWithNonGames
		f.played = playedWithNonGames
		f.gameListRaw = "<html><body>Service unavailable</body></html>"

		t.Run("WHEN it is scanned", func(t *testing.T) {
			copies, _, err := p.Fetch(ctx, schema.Settings{settingNPSSO: "good-npsso"})

			t.Run("THEN the scan succeeds with no category exclusions", func(t *testing.T) {
				require.NoError(t, err)
				assert.False(t, byExternalID(copies)["psn:EP0002-CUSA20002_00-STREAMBOX"].Withdrawn)
			})
		})
	})
}

func TestNonGameName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Paper Kites Demo", true},
		{"Paper Kites - Demo", true},
		{"Paper Kites (Demo)", true},
		{"PAPER KITES DEMO", true},
		{"Paper Kites: Trial Version", true},
		{"Paper Kites Free Trial", true},
		{"Paper Kites Trial", true},
		{"Paper Kites [Trial]", true},
		{"Paper Kites Open Beta", true},
		{"Paper Kites Closed Beta", true},
		{"Paper Kites – Beta", true},
		{"Paper Kites Original Soundtrack", true},
		{"Paper Kites Soundtrack", true},
		{"Paper Kites OST", true},
		{"Paper Kites Dynamic Theme", true},
		{"Paper Kites Theme", true},
		{"Paper Kites Avatar", true},
		{"Paper Kites Avatars", true},
		{"Paper Kites Digital Art Book", true},
		{"Paper Kites Artbook", true},
		{"Paper Kites Art Book", true},
		{"Paper Kites: Versión de prueba", true},
		{"Paper Kites Prueba gratuita", true},
		{"Paper Kites Banda sonora original", true},
		{"Paper Kites Banda sonora", true},
		{"Paper Kites Tema dinámico", true},
		{"Paper Kites Tema", true},
		{"Paper Kites Avatares", true}, //nolint:misspell // Spanish plural of avatar
		{"Paper Kites: Behind the Scenes", true},
		{"Behind the Scenes of Paper Kites", true},
		{"The Making of Paper Kites", true},
		{"Beta Protocol", false},
		{"Theme Park Rush", false},
		{"Demolition Crew", false},
		{"Avatar Legends of Glass", false},
		{"Trial of the Lantern", false},
		{"Paper Kites", false},
		{"Paper Kites Deluxe Edition", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, nonGameName(c.name), c.name)
	}
}
