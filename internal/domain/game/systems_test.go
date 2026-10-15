package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystems_namesNeverChange pins every catalog name: names are stored in copies, editions,
// cover file names and CSV files, so a renamed system orphans what was saved under the old name.
// A new system is appended here; an existing line never changes.
func TestSystems_namesNeverChange(t *testing.T) {
	names := make([]string, 0, len(catalog))
	for _, s := range Systems() {
		names = append(names, s.Name)
	}

	assert.ElementsMatch(t, pinnedSystemNames, names)
}

func TestSystems_catalogRules(t *testing.T) {
	t.Run("GIVEN the catalog", func(t *testing.T) {
		all := Systems()

		t.Run("THEN every name and alias has a key of its own", func(t *testing.T) {
			seen := map[string]string{}

			for _, s := range all {
				for _, w := range append([]string{s.Name}, s.Aliases...) {
					k := SystemKey(w)
					require.NotEmpty(t, k, "%s: empty alias", s.Name)

					if other, ok := seen[k]; ok && other != s.Name {
						assert.Failf(t, "alias shared", "%q belongs to %s and %s", w, other, s.Name)
					}

					seen[k] = s.Name
				}
			}

			t.Run("AND no name or alias is a store's name", func(t *testing.T) {
				for _, p := range platforms {
					_, clash := seen[SystemKey(p.name)]
					assert.False(t, clash, "store %q is also a system name or alias", p.name)
				}
			})
		})

		t.Run("THEN each system has a maker, a kind, a year and a short label of at most 6 characters", func(t *testing.T) {
			for _, s := range all {
				assert.NotEmpty(t, s.Maker, s.Name)
				assert.Contains(t, []SystemKind{SystemKindHome, SystemKindHandheld, SystemKindComputer, SystemKindPC}, s.Kind, s.Name)
				assert.GreaterOrEqual(t, s.Year, 1970, s.Name)
				assert.LessOrEqual(t, len([]rune(s.Short)), 6, s.Name)
				assert.LessOrEqual(t, len([]rune(s.Name)), maxSystemLen, s.Name)
			}
		})

		t.Run("THEN makers are contiguous, PC first", func(t *testing.T) {
			assert.Equal(t, SystemPC, all[0].Name)

			var makers []string

			for _, s := range all {
				if len(makers) == 0 || makers[len(makers)-1] != s.Maker {
					assert.NotContains(t, makers, s.Maker, "maker %s appears in two places", s.Maker)
					makers = append(makers, s.Maker)
				}
			}
		})

		t.Run("THEN within a maker: home consoles, handhelds, computers, each newest first", func(t *testing.T) {
			rank := map[SystemKind]int{SystemKindPC: 0, SystemKindHome: 1, SystemKindHandheld: 2, SystemKindComputer: 3}

			for i := 1; i < len(all); i++ {
				a, b := all[i-1], all[i]
				if a.Maker != b.Maker {
					continue
				}

				if rank[a.Kind] == rank[b.Kind] {
					assert.GreaterOrEqual(t, a.Year, b.Year, "%s before %s", a.Name, b.Name)
				} else {
					assert.Less(t, rank[a.Kind], rank[b.Kind], "%s before %s", a.Name, b.Name)
				}
			}
		})

		t.Run("THEN the modern systems are the ones Steam's PC versions usually match", func(t *testing.T) {
			var modern []string

			for _, s := range all {
				if s.Modern {
					modern = append(modern, s.Name)
				}
			}

			assert.ElementsMatch(t, []string{SystemPC, SystemPS5, SystemPS4, SystemXboxSeries, SystemXboxOne, SystemSwitch, "Switch 2"}, modern)
		})

		t.Run("THEN every part of a name with a slash is an alias", func(t *testing.T) {
			for _, s := range all {
				if !strings.Contains(s.Name, " / ") {
					continue
				}

				for _, part := range strings.Split(s.Name, " / ") {
					got, ok := SystemNamed(part)
					assert.True(t, ok && got.Name == s.Name, "%q is not an alias of %s", part, s.Name)
				}
			}
		})
	})
}

// TestSystemKey_webAgrees runs the cases the web's systemKey (web/src/lib/systems.ts) is tested with
// too, so both sides match names and aliases the same way.
func TestSystemKey_webAgrees(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "tests", "systemKey.cases.json"))
	require.NoError(t, err)

	var cases [][2]string

	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases)

	for _, c := range cases {
		assert.Equal(t, c[1], SystemKey(c[0]), "key of %q", c[0])
	}
}

func TestSystemNamed(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"ps4", SystemPS4},
		{"P.S. 4", SystemPS4},
		{"PlayStation 4", SystemPS4},
		{"megadrive", "Mega Drive / Genesis"},
		{"Sega Genesis", "Mega Drive / Genesis"},
		{"Super Famicom", "SNES"},
		{"Famicom", "NES"},
		{"Steam Deck", SystemPC},
		{"game boy advance", "Game Boy Advance"},
		{"Switch 2", "Switch 2"},
		{"switch", SystemSwitch},
	} {
		got, ok := SystemNamed(tc.in)
		require.True(t, ok, tc.in)
		assert.Equal(t, tc.want, got.Name, tc.in)
	}

	for _, unknown := range []string{"", "Commodore PET 2001 prototype", "Steam"} {
		_, ok := SystemNamed(unknown)
		assert.False(t, ok, unknown)
	}

	t.Run("THEN Systems returns a copy callers cannot change", func(t *testing.T) {
		s := Systems()
		s[0].Aliases = append(s[0].Aliases[:0], "changed")
		assert.False(t, slices.Contains(Systems()[0].Aliases, "changed"))
	})
}

// pinnedSystemNames is every catalog name: see TestSystems_namesNeverChange.
var pinnedSystemNames = []string{
	"PC",
	"PS5",
	"PS4",
	"PS3",
	"PS2",
	"PS1",
	"PS Vita",
	"PSP",
	"Xbox Series",
	"Xbox One",
	"Xbox 360",
	"Xbox",
	"Switch 2",
	"Switch",
	"Wii U",
	"Wii",
	"GameCube",
	"Nintendo 64DD",
	"N64",
	"SNES",
	"Famicom Disk System",
	"NES",
	"3DS",
	"DS",
	"Game Boy Advance",
	"Pokémon Mini",
	"Game Boy Color",
	"Virtual Boy",
	"Game Boy",
	"Game & Watch",
	"Dreamcast",
	"32X",
	"Saturn",
	"Pico",
	"Mega-CD / Sega CD",
	"Mega Drive / Genesis",
	"Master System",
	"SG-1000",
	"Game Gear",
	"Atari Jaguar CD",
	"Atari Jaguar",
	"Atari 7800",
	"Atari 5200",
	"Atari 2600",
	"Atari Lynx",
	"Atari ST",
	"Atari 8-bit",
	"PC-FX",
	"SuperGrafx",
	"PC Engine CD / TurboGrafx-CD",
	"PC Engine / TurboGrafx-16",
	"PC-98",
	"PC-88",
	"Neo Geo CD",
	"Neo Geo AES",
	"Neo Geo Pocket Color",
	"Neo Geo Pocket",
	"Playdia",
	"WonderSwan Color",
	"WonderSwan",
	"Acorn Archimedes",
	"Acorn Electron",
	"BBC Micro",
	"Amstrad GX4000",
	"Amstrad CPC",
	"Apple Pippin",
	"Mac (classic)",
	"Apple II",
	"Evercade",
	"ColecoVision",
	"Amiga CD32",
	"Commodore CDTV",
	"Amiga",
	"Commodore 16 / Plus/4",
	"Commodore 64",
	"VIC-20",
	"Commodore PET",
	"Dragon 32/64",
	"Channel F",
	"FM Towns",
	"Fujitsu FM-7",
	"Vectrex",
	"MSX2",
	"MSX",
	"Odyssey 2",
	"Magnavox Odyssey",
	"HyperScan",
	"Intellivision",
	"N-Gage",
	"Ouya",
	"3DO",
	"Playdate",
	"CD-i",
	"Sharp X68000",
	"Sharp X1",
	"Sinclair QL",
	"ZX Spectrum",
	"ZX81",
	"Game.com",
	"R-Zone",
	"V.Smile",
	"Nuon",
	"Casio Loopy",
	"LaserActive",
	"Super Cassette Vision",
	"Emerson Arcadia 2001",
	"Epoch Cassette Vision",
	"Interton VC 4000",
	"Bally Astrocade",
	"Gizmondo",
	"Tapwave Zodiac",
	"Mega Duck",
	"Watara Supervision",
	"Gamate",
	"Microvision",
	"Thomson MO5",
	"Texas Instruments TI-99/4A",
	"TRS-80 Color Computer",
}
