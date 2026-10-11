package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoLinks(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	newGame := func(t *testing.T, links Links) *Game {
		t.Helper()

		g, err := New("Console Quest", t0)
		require.NoError(t, err)
		_, err = g.AddCopy(on(KindPhysical, "PS4"), t0)
		require.NoError(t, err)

		info := g.Info()
		info.Links = links
		_, err = g.UpdateInfo(info, t0)
		require.NoError(t, err)

		return g
	}

	t.Run("GIVEN a PS4 game linked to the PlayStation store", func(t *testing.T) {
		g := newGame(t, Links{"psn": "PPSA0001"})

		t.Run("WHEN links are found automatically", func(t *testing.T) {
			added := g.AddAutoLinks(Links{LinkSteam: "100", "psn": "OTHER", "gog": "200"}, t1)

			t.Run("THEN only the missing ones are added, marked automatic", func(t *testing.T) {
				assert.Equal(t, Links{LinkSteam: "100", "gog": "200"}, added)
				assert.Equal(t, Links{"psn": "PPSA0001", LinkSteam: "100", "gog": "200"}, g.Links())
				assert.Equal(t, []string{"gog", LinkSteam}, g.AutoLinks())
				assert.Equal(t, t1, g.UpdatedAt(), "covers and the sheet change with the links")
			})

			t.Run("AND they are not used to recognize the game in imports", func(t *testing.T) {
				assert.Equal(t, Links{"psn": "PPSA0001"}, g.consolidationLinks())
			})
		})

		t.Run("WHEN the user keeps the links while editing something else", func(t *testing.T) {
			info := g.Info()
			info.Notes = "a note"
			_, err := g.UpdateInfo(info, t1)
			require.NoError(t, err)

			t.Run("THEN they stay automatic", func(t *testing.T) {
				assert.Equal(t, []string{"gog", LinkSteam}, g.AutoLinks())
			})
		})

		t.Run("WHEN the user removes the Steam link and changes the GOG one", func(t *testing.T) {
			info := g.Info()
			info.Links = Links{"psn": "PPSA0001", "gog": "201"}
			_, err := g.UpdateInfo(info, t1)
			require.NoError(t, err)

			t.Run("THEN both automatic links are remembered as rejected, and the user's GOG link is theirs", func(t *testing.T) {
				assert.Equal(t, []LinkRef{{"gog", "200"}, {LinkSteam, "100"}}, g.RejectedLinks())
				assert.Empty(t, g.AutoLinks())
			})

			t.Run("AND a later automatic search does not add the rejected Steam link again", func(t *testing.T) {
				added := g.AddAutoLinks(Links{LinkSteam: "100"}, t1)
				assert.Empty(t, added)
				assert.NotContains(t, g.Links(), LinkSteam)
			})

			t.Run("AND another Steam id may still be found", func(t *testing.T) {
				added := g.AddAutoLinks(Links{LinkSteam: "101"}, t1)
				assert.Equal(t, Links{LinkSteam: "101"}, added)
			})
		})

		t.Run("WHEN the user adds the rejected link by hand", func(t *testing.T) {
			info := g.Info()
			info.Links = Links{"psn": "PPSA0001", "gog": "201", LinkSteam: "100"}
			_, err := g.UpdateInfo(info, t1)
			require.NoError(t, err)

			t.Run("THEN it is theirs, and no longer rejected (the automatic one it replaced is)", func(t *testing.T) {
				assert.Equal(t, []LinkRef{{"gog", "200"}, {LinkSteam, "101"}}, g.RejectedLinks())
				assert.NotContains(t, g.AutoLinks(), LinkSteam)
				assert.Equal(t, "100", g.Links()[LinkSteam])
			})
		})
	})

	t.Run("GIVEN an automatic Steam link, WHEN a source imports the game with its own Steam id", func(t *testing.T) {
		g := newGame(t, nil)
		g.AddAutoLinks(Links{LinkSteam: "100"}, t1)

		added := g.importLinks(Links{LinkSteam: "999", "gog": "200"})

		t.Run("THEN the source's links win and are no longer automatic", func(t *testing.T) {
			assert.Equal(t, Links{LinkSteam: "999", "gog": "200"}, added)
			assert.Equal(t, "999", g.Links()[LinkSteam])
			assert.Empty(t, g.AutoLinks())
		})
	})

	t.Run("GIVEN a game, WHEN an automatic search is recorded", func(t *testing.T) {
		g := newGame(t, nil)
		g.MarkAutoLinkTried(t1)

		t.Run("THEN the time is kept without counting as a change of the game", func(t *testing.T) {
			assert.Equal(t, t1, g.AutoLinkTriedAt())
			assert.Equal(t, t0, g.UpdatedAt())
		})
	})

	t.Run("GIVEN stored origins, WHEN the game is rebuilt", func(t *testing.T) {
		g := Rehydrate(NewID(), Info{
			Title: "Console Quest",
			Links: Links{LinkSteam: "100", "psn": "1"},
		}, nil, t0, t0)
		g.RestoreLinkOrigins([]string{LinkSteam, "gone"}, []LinkRef{{"gog", "200"}}, t1)

		t.Run("THEN they are back, minus marks of links no longer there", func(t *testing.T) {
			assert.Equal(t, []string{LinkSteam}, g.AutoLinks())
			assert.Equal(t, []LinkRef{{"gog", "200"}}, g.RejectedLinks())
			assert.Equal(t, t1, g.AutoLinkTriedAt())
		})
	})
}

func TestConsolidator_ignoresAutoLinks(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a disc game with an automatic Steam link", func(t *testing.T) {
		disc, err := New("Console Quest", t0)
		require.NoError(t, err)
		_, err = disc.AddCopy(on(KindPhysical, "PS4"), t0)
		require.NoError(t, err)
		disc.AddAutoLinks(Links{LinkSteam: "100"}, t0)

		t.Run("WHEN a Steam library brings a differently titled game with that AppID", func(t *testing.T) {
			res := NewConsolidator([]*Game{disc}).Apply("steam-source", []ImportedCopy{{
				ExternalID: "steam:100",
				Title:      "Console Quest: Remastered Collection",
				Links:      Links{LinkSteam: "100"},
				Details: CopyDetails{
					Kind:     KindLibrary,
					Platform: "Steam",
				},
			}}, t0)

			t.Run("THEN it is not merged into the disc's game on the strength of a guessed link", func(t *testing.T) {
				assert.Equal(t, 1, res.GamesCreated)
				assert.Len(t, disc.Copies(), 1)
			})
		})
	})
}

func TestConsolidator_importsAutoLinksAsAutomatic(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN an exported row with an automatic Steam link, imported into an empty catalog", func(t *testing.T) {
		res := NewConsolidator(nil).Apply("", []ImportedCopy{{
			ExternalID: "csv:1",
			Title:      "Console Quest",
			Links:      Links{"psn": "PPSA0001"},
			AutoLinks:  Links{LinkSteam: "100"},
			Details: CopyDetails{
				Kind:     KindPhysical,
				Platform: "PS4",
			},
		}}, t0)

		t.Run("THEN the link comes back automatic, not as a link imports may match by", func(t *testing.T) {
			require.Len(t, res.Changed, 1)
			g := res.Changed[0]
			assert.Equal(t, Links{"psn": "PPSA0001", LinkSteam: "100"}, g.Links())
			assert.Equal(t, []string{LinkSteam}, g.AutoLinks())
			assert.Equal(t, Links{"psn": "PPSA0001"}, g.consolidationLinks())
		})
	})
}

func TestAbsorb_prefersALinkThatIsNotAutomatic(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a game with an automatic Steam link and another with the user's", func(t *testing.T) {
		auto, err := New("Console Quest", t0)
		require.NoError(t, err)
		auto.AddAutoLinks(Links{LinkSteam: "100"}, t0)

		mine, err := New("Console Quest", t0)
		require.NoError(t, err)

		info := mine.Info()
		info.Links = Links{LinkSteam: "200"}
		_, err = mine.UpdateInfo(info, t0)
		require.NoError(t, err)

		t.Run("WHEN they are merged into the first", func(t *testing.T) {
			auto.Absorb(mine, t0)

			t.Run("THEN the user's link wins, and is not automatic", func(t *testing.T) {
				assert.Equal(t, "200", auto.Links()[LinkSteam])
				assert.Empty(t, auto.AutoLinks())
			})
		})
	})
}
