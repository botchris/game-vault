package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateInfo_links(t *testing.T) {
	t.Run("GIVEN a game linked to Steam", func(t *testing.T) {
		g, _ := New("Hades", t0)
		_, err := g.UpdateInfo(Info{Title: "Hades", Links: Links{"steam": "1145360"}}, t0)
		require.NoError(t, err)

		t.Run("WHEN a GOG link is added, with spaces and an empty Steam id", func(t *testing.T) {
			changed, err := g.UpdateInfo(Info{Title: "Hades", Links: Links{" GOG ": " 1207658924 ", "steam": ""}}, t0)
			require.NoError(t, err)

			t.Run("THEN the ids are trimmed and the empty one unlinks Steam", func(t *testing.T) {
				assert.Equal(t, Links{"gog": "1207658924"}, g.Links())
			})

			t.Run("AND the cover and details are refreshed", func(t *testing.T) {
				assert.True(t, changed)
			})
		})

		t.Run("WHEN a link has a malformed store or an id with spaces", func(t *testing.T) {
			for _, bad := range []Links{{"Steam Store": "1"}, {"steam": "11 45"}} {
				_, err := g.UpdateInfo(Info{Title: "Hades", Links: bad}, t0)

				t.Run("THEN it is refused", func(t *testing.T) {
					assert.Error(t, err)
				})
			}
		})

		t.Run("WHEN the caller changes the map it got back", func(t *testing.T) {
			g.Links()["gog"] = "1"

			t.Run("THEN the game is not changed", func(t *testing.T) {
				assert.Equal(t, "1207658924", g.Links()["gog"])
			})
		})
	})
}

func TestConsolidator_links(t *testing.T) {
	t.Run("GIVEN a game linked to GOG by hand under another title", func(t *testing.T) {
		g, _ := New("The Witcher 3", t0)
		_, err := g.UpdateInfo(Info{Title: "The Witcher 3", Links: Links{"gog": "1207664663", "steam": "292030"}}, t0)
		require.NoError(t, err)

		t.Run("WHEN a source imports a copy linked to the same GOG id and another Steam AppID", func(t *testing.T) {
			r := NewConsolidator([]*Game{g}).Apply("src", []ImportedCopy{{
				ExternalID: "gog:1207664663", Title: "The Witcher 3: Wild Hunt - Game of the Year Edition",
				Links:   Links{"gog": "1207664663", "steam": "499450", "ea": "1"},
				Details: CopyDetails{Kind: KindLibrary, Platform: "GOG"},
			}}, t0)

			t.Run("THEN the copy joins that game whatever its title", func(t *testing.T) {
				assert.Zero(t, r.GamesCreated)
				require.Len(t, g.Copies(), 1)
			})

			t.Run("AND only the store the game had no link to is added", func(t *testing.T) {
				assert.Equal(t, Links{"gog": "1207664663", "steam": "292030", "ea": "1"}, g.Links())
			})
		})
	})

	t.Run("GIVEN an import whose link is malformed", func(t *testing.T) {
		r := NewConsolidator(nil).Apply("src", []ImportedCopy{{
			ExternalID: "x:1", Title: "Hades", Links: Links{"steam": "11 45"},
			Details: CopyDetails{Kind: KindLibrary, Platform: "Steam"},
		}}, t0)

		t.Run("THEN the copy is imported without links and a warning says why", func(t *testing.T) {
			require.Len(t, r.Changed, 1)
			assert.Empty(t, r.Changed[0].Links())
			require.Len(t, r.Warnings, 1)
			assert.Contains(t, r.Warnings[0], "steam link")
		})
	})
}
