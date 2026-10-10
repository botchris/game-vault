package game

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemOf(t *testing.T) {
	for platform, want := range map[string]string{
		"Steam": "PC", "Epic Games": "PC", "GOG": "PC", "EA App": "PC", "Ubisoft Connect": "PC", "Battle.net": "PC",
		"itch.io": "PC", "Amazon Games": "PC", "Rockstar": "PC", "Riot": "PC", "Battlestate (Tarkov)": "PC", "PC": "PC",
		"steam": "PC",
		"PS5":   "PS5", "PS4": "PS4", "PS3": "PS3", "PS2": "PS2", "PS1": "PS1", "PSP": "PSP", "PS Vita": "PS Vita",
		"Xbox Series": "Xbox Series", "Xbox One": "Xbox One", "Xbox 360": "Xbox 360", "Xbox": "Xbox",
		"Switch": "Switch", "Wii U": "Wii U", "Wii": "Wii", "GameCube": "GameCube", "N64": "N64", "3DS": "3DS",
		"DS": "DS", "Game Boy": "Game Boy", "xbox 360": "Xbox 360",
		"PlayStation Store": "PS4", "Nintendo eShop": "Switch", "Microsoft Store / Xbox": "PC",
		"  Amiga  ": "Amiga", "Mega Drive": "Mega Drive",
		"": "Other", "   ": "Other",
	} {
		assert.Equal(t, want, SystemOf(platform), "platform %q", platform)
	}
}

func TestCopySystem(t *testing.T) {
	t.Run("GIVEN a game with a PlayStation Store copy", func(t *testing.T) {
		g, err := New("Astro Bot", t0)
		require.NoError(t, err)
		c, err := g.AddCopy(CopyDetails{
			Kind:     KindLibrary,
			Platform: "PlayStation Store",
		}, t0)
		require.NoError(t, err)

		t.Run("THEN its system is the one the platform implies", func(t *testing.T) {
			assert.Equal(t, "PS4", g.Copies()[0].System())
		})

		t.Run("WHEN its source says PS5 THEN the source wins over the platform", func(t *testing.T) {
			g.copies[0].SourceSystem = "PS5"
			assert.Equal(t, "PS5", g.Copies()[0].System())
		})

		t.Run("WHEN the user says ps4 THEN the user wins over the source, named like SystemOf names it", func(t *testing.T) {
			d := c.CopyDetails
			d.System = "ps4"
			got, err := g.UpdateCopy(c.ID, d, t0)
			require.NoError(t, err)
			assert.Equal(t, "PS4", got.CopyDetails.System)
			assert.Equal(t, "PS4", got.System())
		})

		t.Run("WHEN the override is emptied THEN the copy goes back to automatic", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "  "
			got, err := g.UpdateCopy(c.ID, d, t0)
			require.NoError(t, err)
			assert.Empty(t, got.CopyDetails.System)
			assert.Equal(t, "PS5", got.System())
		})
	})

	t.Run("GIVEN systems typed by the user", func(t *testing.T) {
		g, err := New("Halo 3", t0)
		require.NoError(t, err)

		t.Run("THEN known names are fixed and unknown ones kept, trimmed", func(t *testing.T) {
			for typed, want := range map[string]string{"steam": "PC", "xbox 360": "Xbox 360", " Amiga 500 ": "Amiga 500", "Xbox 360/S Slim": "Xbox 360/S Slim"} {
				c, err := g.AddCopy(CopyDetails{
					Kind:   KindPhysical,
					System: typed,
				}, t0)
				require.NoError(t, err, typed)
				assert.Equal(t, want, c.CopyDetails.System, typed)
			}
		})

		t.Run("THEN names without a letter or digit, too long or with control characters are refused", func(t *testing.T) {
			for _, typed := range []string{"..", "--", strings.Repeat("x", 41), "PS\x004"} {
				_, err := g.AddCopy(CopyDetails{
					Kind:   KindPhysical,
					System: typed,
				}, t0)

				var ve *ValidationError
				assert.ErrorAs(t, err, &ve, "%q", typed)
			}
		})
	})
}

func TestConsolidator_systems(t *testing.T) {
	imported := func(system string) []ImportedCopy {
		return []ImportedCopy{{
			ExternalID: "psn:1",
			Title:      "Astro Bot",
			System:     system,
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "PlayStation Store",
				Status:   StatusOwned,
			},
		}}
	}

	t.Run("GIVEN a scan that says the copy is for PS5", func(t *testing.T) {
		res := NewConsolidator(nil).Apply("src", imported("PS5"), t0)
		require.Len(t, res.Changed, 1)
		g := res.Changed[0]

		t.Run("THEN the copy keeps the source's system, which wins over the platform's", func(t *testing.T) {
			assert.Equal(t, "PS5", g.Copies()[0].SourceSystem)
			assert.Equal(t, "PS5", g.Copies()[0].System())
		})

		t.Run("WHEN the user overrides it and the next scan says PS5 again", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "PS4"
			_, err := g.UpdateCopy(g.Copies()[0].ID, d, t0)
			require.NoError(t, err)

			again := NewConsolidator([]*Game{g}).Apply("src", imported("PS5"), t0)

			t.Run("THEN the override survives and the copy counts as unchanged", func(t *testing.T) {
				assert.Equal(t, 1, again.CopiesUnchanged)
				assert.Equal(t, "PS4", g.Copies()[0].System())
				assert.Equal(t, "PS5", g.Copies()[0].SourceSystem)
			})
		})

		t.Run("WHEN a later scan no longer says the system", func(t *testing.T) {
			again := NewConsolidator([]*Game{g}).Apply("src", imported(""), t0)

			t.Run("THEN the source's system is forgotten and the copy counts as updated", func(t *testing.T) {
				assert.Equal(t, 1, again.CopiesUpdated)
				assert.Empty(t, g.Copies()[0].SourceSystem)
			})
		})
	})

	t.Run("GIVEN an import that sets the copy's own system (a CSV row)", func(t *testing.T) {
		in := imported("")
		in[0].Details.System = "PS5"
		res := NewConsolidator(nil).Apply("", in, t0)
		require.Len(t, res.Changed, 1)
		g := res.Changed[0]

		t.Run("THEN it is stored as the copy's override", func(t *testing.T) {
			assert.Equal(t, "PS5", g.Copies()[0].CopyDetails.System)
		})

		t.Run("AND an import without one keeps it", func(t *testing.T) {
			NewConsolidator([]*Game{g}).Apply("", imported(""), t0)
			assert.Equal(t, "PS5", g.Copies()[0].CopyDetails.System)
		})
	})

	t.Run("GIVEN a source system that is not a valid name", func(t *testing.T) {
		res := NewConsolidator(nil).Apply("src", imported("--"), t0)

		t.Run("THEN the copy is imported without it and the scan warns", func(t *testing.T) {
			require.Len(t, res.Changed, 1)
			assert.Empty(t, res.Changed[0].Copies()[0].SourceSystem)
			assert.Len(t, res.Warnings, 1)
		})
	})
}
