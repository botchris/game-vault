# Systems Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Grow Game Vault from 20 systems to a catalog of about 110 (consoles, handhelds, classic
computers) with aliases, served by the API to a searchable picker, and add a region to every copy.

**Architecture:** One table in `internal/domain/game/systems.go` is the source of truth (name, maker,
kind, year, aliases, modern, short label). The domain's platform helpers, the barcode name parser,
Steam and SteamGridDB read it; IGDB, TheGamesDB and Wikidata keep their own id tables, checked
against it by `game.CheckProviderCoverage`. A new `CatalogService.ListSystems` serves it; the web
drops its hand-kept arrays and builds the picker, badges and filters from it. `CopyDetails.Region`
is a new enum-like field stored in the copy document, the CSV and the API.

**Tech Stack:** Go 1.26, SQLite JSON documents, ConnectRPC/buf, React 19 + TypeScript, Node's test
runner, i18n en/es. Everything runs in the toolchain container through Task.

**Spec:** `docs/superpowers/specs/2026-10-16-systems-catalog-design.md`

## Global Constraints

- The 20 stored system names never change: PC, PS5, PS4, PS3, PS2, PS1, PSP, PS Vita, Xbox Series,
  Xbox One, Xbox 360, Xbox, Switch, Wii U, Wii, GameCube, N64, 3DS, DS, Game Boy (and Other).
- New systems use the maker's own model name, untranslated ("NES", "SNES", "Game Boy Advance",
  "Mega Drive / Genesis", "ZX Spectrum"). System names are never translated in the UI.
- Matching a name or alias ignores case, spaces, hyphens and dots. An alias belongs to one system
  only, and never equals a store platform's name.
- Region values: `PAL`, `NTSC-U`, `NTSC-J`, `NTSC-K`, `NTSC-C`, `Asia`, `Region free`, or empty
  (not set); stored as the ids `pal`, `ntsc_u`, `ntsc_j`, `ntsc_k`, `ntsc_c`, `asia`,
  `region_free`. Region belongs to every copy kind (never dropped on a kind change).
- No stored name changes: a hand-typed alias becomes the catalog name only when that copy is saved
  again; existing copies are not rewritten in bulk.
- Everything in the repository is English; every UI string goes through `t()` with keys in
  `web/src/i18n/locales/en.json` and `es.json`. UI text never names other products.
- Go: load the `write-go` skill first. Doc comment on every exported identifier, one struct field per
  line, blank line between documented members, GIVEN/WHEN/THEN tests, time-bounded contexts.
- Commands only through Task / the toolchain container (`task lint`, `task test`, `task generate`,
  `task test-server`); never Go or Node on the host. One-off commands:
  `docker run --rm --init -v "$PWD:/src" -w /src -v gamevault-gomod:/go/pkg/mod -v gamevault-gobuild:/root/.cache/go-build gamevault-toolchain:446777a3814f <cmd>`.
- Never touch `config/` or the server on port 8080. No attribution lines in commits. No personal
  data in commits, tests or docs.
- Delivery: branch `feature/systems-catalog`, one PR to `dev` (`gh pr create --base dev`).

## Review Focus

- A retail name containing an ordinary word that is also a system alias ("Genesis Noir PS4",
  "Wii Sports Wii", "Saturn Bomberman (Saturn)") must keep the word in the title and pick the right
  system: covered by the weak-alias rule and its cases in Task 2.
- A copy whose platform was typed by hand before this change ("Mega Drive", "dreamcast") must keep
  working: it keeps its stored text until saved, then becomes the catalog name; free text not in
  the catalog stays free text. Covered in Task 1 (`normalize`) and the picker's free-text option
  (Task 8).
- Names with a slash ("Mega Drive / Genesis", "Dragon 32/64") must work as cover file names, URL
  paths and CSV values: covered by a cover round trip in Task 1 and the CSV test in Task 3.
- The vocabulary test in `cmd/gamevault` must keep catching literals of the old names in adapters
  while allowing the provider tables keyed by catalog names: covered in Task 5.
- The picker on a phone (bottom sheet, 16 px search input, keyboard open) and with only a keyboard
  (arrows, Enter, Escape): covered by the visual check in Task 11 and the pure helper tests in
  Task 7.

---

### Task 1: The catalog in the domain

**Files:**
- Create: `internal/domain/game/systems.go`, `internal/domain/game/systems_test.go`,
  `web/tests/systemKey.cases.json`
- Modify: `internal/domain/game/platforms.go`, `internal/domain/game/system.go`,
  `internal/domain/game/platforms_test.go`, `internal/domain/game/system_test.go`

**Interfaces:**
- Produces (used by every later task):
  ```go
  type SystemKind string // SystemKindHome "home", SystemKindHandheld "handheld", SystemKindComputer "computer", SystemKindPC "pc"
  type System struct { Name, Maker string; Kind SystemKind; Year int; Aliases []string; Modern bool; Short string } // one field per line in the code
  func Systems() []System                 // the catalog in display order, a deep copy
  func SystemNamed(s string) (System, bool) // by name or alias, SystemKey equality
  func SystemKey(s string) string           // lower case, without spaces, hyphens and dots
  func IsModernSystem(name string) bool     // exact catalog name with Modern set
  func Consoles() []string                  // every catalog name but PC, catalog order (unchanged signature)
  func KnownPlatforms() []string            // the 14 stores, then every catalog name (unchanged signature)
  ```

- [ ] **Step 1: Write the failing tests** in `internal/domain/game/systems_test.go`:

```go
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
```

Create `web/tests/systemKey.cases.json`:

```json
[
  ["PS4", "ps4"],
  ["P.S. 4", "ps4"],
  ["  Mega-Drive ", "megadrive"],
  ["Game Boy", "gameboy"],
  ["Xbox Series X|S", "xboxseriesx|s"],
  ["Pokémon Mini", "pokémonmini"],
  ["Mega Drive / Genesis", "megadrive/genesis"],
  ["", ""]
]
```

Add to `platforms_test.go` (replace the expectations of `TestKnownPlatforms`; keep the
`TestVocabulary_valuesNeverChange` table unchanged):

```go
		t.Run("THEN they are the stores, in a fixed order, then every catalog system", func(t *testing.T) {
			want := []string{
				"Steam", "Epic Games", "GOG", "EA App", "Battle.net", "Ubisoft Connect", "Microsoft Store / Xbox",
				"Rockstar", "Battlestate (Tarkov)", "Riot", "itch.io", "Amazon Games", "PlayStation Store", "Nintendo eShop",
			}

			for _, s := range Systems() {
				want = append(want, s.Name)
			}

			assert.Equal(t, want, KnownPlatforms())
		})

		t.Run("THEN the consoles are the catalog's systems but PC, in catalog order", func(t *testing.T) {
			var want []string

			for _, s := range Systems()[1:] {
				want = append(want, s.Name)
			}

			assert.Equal(t, want, Consoles())
		})
```

Add to `system_test.go` (new test function `TestSystems_aliasesOnSave`):

```go
func TestSystems_aliasesOnSave(t *testing.T) {
	t.Run("GIVEN copy details typed with aliases", func(t *testing.T) {
		d := CopyDetails{Kind: KindPhysical, Platform: "Sega Genesis", System: "super famicom"}

		t.Run("WHEN they are normalized", func(t *testing.T) {
			got, err := d.normalize()
			require.NoError(t, err)

			t.Run("THEN the platform and the system become the catalog's names", func(t *testing.T) {
				assert.Equal(t, "Mega Drive / Genesis", got.Platform)
				assert.Equal(t, "SNES", got.System)
			})
		})
	})

	t.Run("GIVEN a stored platform that is an alias", func(t *testing.T) {
		t.Run("THEN SystemOf keeps the stored text until the copy is saved again", func(t *testing.T) {
			assert.Equal(t, "Sega Genesis", SystemOf("Sega Genesis"))
			assert.Equal(t, "Dreamcast", SystemOf("dreamcast"))
			assert.Equal(t, "Commodore PET", SystemOf(" Commodore PET "))
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `docker run … go test ./internal/domain/game/` — Expected: FAIL to compile (`Systems`,
`SystemNamed`, `SystemKey`, `pinnedSystemNames`, `catalog` undefined).

- [ ] **Step 3: Write `internal/domain/game/systems.go`**

```go
package game

import (
	"slices"
	"strings"
	"unicode"
)

// SystemKind says what sort of machine a system is.
type SystemKind string

// Values of SystemKind.
const (
	SystemKindPC       SystemKind = "pc"       // PC, its own kind
	SystemKindHome     SystemKind = "home"     // a home console
	SystemKindHandheld SystemKind = "handheld" // a handheld console
	SystemKindComputer SystemKind = "computer" // a classic home computer
)

// System is one game system of the catalog: what a copy can be played on.
type System struct {
	// Name is the stored name: copies, editions, cover file names and CSV files use it, so it never
	// changes (TestSystems_namesNeverChange).
	Name string

	// Maker is who made it ("Nintendo", "Sega"), as the picker groups systems.
	Maker string

	// Kind says whether it is a home console, a handheld or a computer.
	Kind SystemKind

	// Year is the year of its first release anywhere.
	Year int

	// Aliases are its regional names, abbreviations and spellings ("Genesis", "Sega Mega Drive").
	Aliases []string

	// Modern reports that Steam's PC version of a game usually is the same game: PC and the
	// consoles of the PS4 generation or later that share most games with PC.
	Modern bool

	// Short is the badge label when it differs from the name ("MD", "GBA"); empty: the name.
	Short string
}

// catalog is every system Game Vault knows, in display order: PC, then by maker; within a maker,
// home consoles, then handhelds, then computers, each newest first. Adding a system is one entry
// here plus one line in pinnedSystemNames, and a decision in each provider's table (see
// CheckProviderCoverage).
var catalog = []System{
	// The entries of "Appendix: the catalog" at the end of this plan, verbatim.
}

// systemIndex maps the key of every name and alias to its system's position in catalog.
var systemIndex = func() map[string]int {
	out := map[string]int{}

	for i, s := range catalog {
		for _, w := range append([]string{s.Name}, s.Aliases...) {
			out[SystemKey(w)] = i
		}
	}

	return out
}()

// Systems returns the catalog in display order (a copy: callers cannot change it).
func Systems() []System {
	out := make([]System, len(catalog))
	for i, s := range catalog {
		s.Aliases = slices.Clone(s.Aliases)
		out[i] = s
	}

	return out
}

// SystemKey is how names and aliases are compared: lower case, without spaces, hyphens and dots
// ("P.S. 4" and "ps-4" are "ps4"). The web's systemKey does the same (web/tests/systemKey.cases.json).
func SystemKey(s string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '-' || r == '.' {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// SystemNamed returns the catalog system s names, by its name or one of its aliases.
func SystemNamed(s string) (System, bool) {
	k := SystemKey(s)
	if k == "" {
		return System{}, false
	}

	i, ok := systemIndex[k]
	if !ok {
		return System{}, false
	}

	return Systems()[i], true
}

// IsModernSystem reports whether name is a catalog system with Modern set.
func IsModernSystem(name string) bool {
	i := slices.IndexFunc(catalog, func(s System) bool { return s.Name == name })
	return i >= 0 && catalog[i].Modern
}
```

Put `pinnedSystemNames` (every catalog name, as a `[]string` literal, one per line grouped like the
catalog) at the end of `systems_test.go`.

- [ ] **Step 4: Make the platform helpers read the catalog** (`platforms.go`, `system.go`)

In `platforms.go`, remove the 20 rows under "PC and consoles" from `platforms` (the table keeps
only the 14 stores) and change:

```go
// platformNamed returns the known platform p names, ignoring case and surrounding spaces: a store,
// or a catalog system by its exact name (an alias is not a platform name; CanonicalPlatform turns
// it into one when a copy is saved).
func platformNamed(p string) (platformInfo, bool) {
	p = strings.TrimSpace(p)
	if i := slices.IndexFunc(platforms, func(k platformInfo) bool { return strings.EqualFold(k.name, p) }); i >= 0 {
		return platforms[i], true
	}

	if i := slices.IndexFunc(catalog, func(s System) bool { return strings.EqualFold(s.Name, p) }); i >= 0 {
		return platformInfo{name: catalog[i].Name, system: catalog[i].Name}, true
	}

	return platformInfo{}, false
}

// KnownPlatforms returns the stores and launchers Game Vault knows, then every catalog system (a
// copy: callers cannot change it).
func KnownPlatforms() []string {
	out := make([]string, 0, len(platforms)+len(catalog))
	for _, p := range platforms {
		out = append(out, p.name)
	}

	for _, s := range catalog {
		out = append(out, s.Name)
	}

	return out
}

// Consoles returns every catalog system but PC, in catalog order. Providers that map systems to a
// catalog's own ids check their tables against the catalog (see CheckProviderCoverage).
func Consoles() []string {
	out := make([]string, 0, len(catalog))

	for _, s := range catalog {
		if s.Name != SystemPC {
			out = append(out, s.Name)
		}
	}

	return out
}

// CanonicalPlatform fixes the casing of known platforms ("steam" → "Steam") and turns a system's
// alias into its name ("Sega Genesis" → "Mega Drive / Genesis"), so they compare equal. Unknown
// platforms are returned trimmed but otherwise untouched.
func CanonicalPlatform(p string) string {
	if k, ok := platformNamed(p); ok {
		return k.name
	}

	if s, ok := SystemNamed(p); ok {
		return s.Name
	}

	return strings.TrimSpace(p)
}
```

Update the comment on the `platforms` var ("the stores and launchers"; consoles come from the
catalog). In `system.go`, end `normalizeSystem` with:

```go
	if sys, ok := SystemNamed(s); ok {
		return sys.Name, nil
	}

	return SystemOf(s), nil
```

- [ ] **Step 5: Run the domain tests**

Run: `docker run … go test ./internal/domain/...` — Expected: PASS. Then `task test`: fix any
test elsewhere that pinned the old 20-entry `Consoles()` / `KnownPlatforms()` lists by building
the expectation from `game.Systems()` the same way.

- [ ] **Step 6: Cover file names with a slash** — add to
`internal/adapters/outbound/gamedata/editions_test.go` a case that saves and reads back a cover for
system `"Mega Drive / Genesis"` and asserts the file name has no `/` (`coverFile` already hashes; the
test pins it). Run `go test ./internal/adapters/outbound/gamedata/`. Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/game web/tests/systemKey.cases.json internal/adapters/outbound/gamedata
git commit -m "Domain: a catalog of game systems with makers, years and aliases; consoles and known platforms come from it"
```

---

### Task 2: Recognising systems in retail names

**Files:**
- Modify: `internal/domain/game/product.go`, `internal/domain/game/product_test.go`

**Interfaces:**
- Consumes: `Systems()`, `SystemKey` (Task 1).
- Produces: `ParseProductName(raw string) (title, platform, edition string)` (same signature; now
  returns catalog names for every system), and `SystemPattern() string` (a regexp source matching
  any name or alias, longest first, case-insensitive, for SteamGridDB in Task 6).

- [ ] **Step 1: Write the failing cases** — extend the table in `product_test.go` (keep the six
existing cases):

```go
		// Systems the regexes did not know, and names that are ordinary words.
		{"Nintendo Switch 2 Edition The Legend of Zelda Tears of the Kingdom", "The Legend of Zelda Tears of the Kingdom", "Switch 2", ""},
		{"Mario Kart World (Switch 2)", "Mario Kart World", "Switch 2", ""},
		{"Mario Kart 8 Deluxe Switch PAL", "Mario Kart 8 Deluxe", "Switch", ""},
		{"Crash Bandicoot PlayStation 1", "Crash Bandicoot", "PS1", ""},
		{"Metal Gear Solid PS1", "Metal Gear Solid", "PS1", ""},
		{"Pokemon Yellow Game Boy Color", "Pokemon Yellow", "Game Boy Color", ""},
		{"Pokemon Ruby Game Boy Advance PAL", "Pokemon Ruby", "Game Boy Advance", ""},
		{"Tetris Game Boy", "Tetris", "Game Boy", ""},
		{"Sonic the Hedgehog 2 Sega Mega Drive", "Sonic the Hedgehog 2", "Mega Drive / Genesis", ""},
		{"Super Mario World SNES PAL", "Super Mario World", "SNES", ""},
		{"Genesis Noir PS4", "Genesis Noir", "PS4", ""},
		{"Wii Sports Wii", "Wii Sports", "Wii", ""},
		{"Saturn Bomberman (Saturn)", "Saturn Bomberman", "Saturn", ""},
		{"Mario Kart DS", "Mario Kart", "DS", ""},
		{"Ridge Racer PSP", "Ridge Racer", "PSP", ""},
		{"Super Mario 64 N64", "Super Mario 64", "N64", ""},
		{"Halo Combat Evolved Xbox", "Halo Combat Evolved", "Xbox", ""},
		{"The Sims PC CD-ROM", "The Sims", "PC", ""},
		{"Playstation4 Games", "Games", "PS4", ""},
```

(The last one is CeX's UK category name: only its platform is used.)

- [ ] **Step 2: Run to see them fail** — `go test ./internal/domain/game/ -run TestParseProductName`.
Expected: FAIL on the new cases.

- [ ] **Step 3: Replace `platformTokens` with tokens built from the catalog**

```go
// systemToken is one name or alias of a catalog system as ParseProductName looks for it.
type systemToken struct {
	// re matches the alias as a whole word; its first group is the alias itself.
	re *regexp.Regexp

	// pattern is the alias's own regexp source (aliasPattern), without the word boundaries.
	pattern string

	// system is the catalog name the alias belongs to.
	system string

	// weak is set for an alias that is one word of letters ("Genesis", "Switch", "Wii", "DS"): an
	// ordinary word in a title, so it only counts in brackets, at the end of the name, or written in
	// capitals when it has at most four letters ("DS", "SNES").
	weak bool
}

// systemTokens are every name and alias of the catalog, longest first ("Switch 2" before "Switch",
// "Game Boy Advance" before "Game Boy"). Names holding brackets ("Mac (classic)") are left out:
// their aliases are enough.
var systemTokens = func() []systemToken {
	var out []systemToken

	for _, s := range catalog {
		for _, w := range append([]string{s.Name}, s.Aliases...) {
			if strings.ContainsAny(w, "()[]") {
				continue
			}

			p := aliasPattern(w)
			out = append(out, systemToken{
				re:      regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(` + p + `)(?:$|[^\p{L}\p{N}])`),
				pattern: p,
				system:  s.Name,
				weak:    isWeakAlias(w),
			})
		}
	}

	slices.SortStableFunc(out, func(a, b systemToken) int { return len(b.pattern) - len(a.pattern) })

	return out
}()

// reAliasRuns splits an alias into runs of letters, runs of digits and single other characters.
var reAliasRuns = regexp.MustCompile(`\p{L}+|\p{N}+|[^\s.\-\p{L}\p{N}]`)

// aliasPattern is the regexp source of an alias whose parts may be written together or apart with
// spaces, hyphens or dots ("PS4", "PS 4", "PlayStation4", "Mega-Drive").
func aliasPattern(alias string) string {
	runs := reAliasRuns.FindAllString(alias, -1)
	for i, r := range runs {
		runs[i] = regexp.QuoteMeta(r)
	}

	return strings.Join(runs, `[\s.\-]*`)
}

// isWeakAlias reports whether an alias is a single word of letters (see systemToken.weak).
func isWeakAlias(alias string) bool {
	return alias != "" && !strings.ContainsFunc(alias, func(r rune) bool { return !unicode.IsLetter(r) })
}

// SystemPattern is a regexp source matching any catalog name or alias (case-insensitive by the
// caller's flags), longest first, for adapters that strip system words from titles.
func SystemPattern() string {
	parts := make([]string, 0, len(systemTokens))
	for _, t := range systemTokens {
		parts = append(parts, t.pattern)
	}

	return strings.Join(parts, "|")
}
```

`ParseProductName` becomes:

```go
func ParseProductName(raw string) (title, platform, edition string) {
	// Barcodes and region tags go first, so "Switch PAL" ends with its system.
	s := reGTIN.ReplaceAllString(raw, " ")
	s = reRegion.ReplaceAllString(s, " ")

	for _, t := range systemTokens {
		for {
			loc := t.matchIn(s)
			if loc == nil {
				break
			}

			if platform == "" {
				platform = t.system
			}

			s = s[:loc[0]] + " " + s[loc[1]:]
		}
	}

	if m := reEdition.FindString(s); m != "" {
		edition = editionName(m)
		s = strings.Replace(s, m, " ", 1)
	}

	s = reNoise.ReplaceAllString(s, " ")
	s = reBracketChars.ReplaceAllString(s, " ")
	// … the rest as today (spaces, title case, roman numerals).
}

// matchIn returns the position of the first acceptable match of the token's alias in s, nil when
// there is none. A weak alias is acceptable inside brackets, at the end of s (nothing but
// punctuation after it), or in capitals when it has at most four letters.
func (t systemToken) matchIn(s string) []int {
	for _, m := range t.re.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[2], m[3]
		if !t.weak || inBrackets(s, start, end) || strings.TrimFunc(s[end:], isNotAlnum) == "" ||
			len(s[start:end]) <= 4 && s[start:end] == strings.ToUpper(s[start:end]) {
			return []int{start, end}
		}
	}

	return nil
}
```

with small helpers `inBrackets(s, start, end)` (the nearest non-space character before is `(` or
`[` and the nearest after is `)` or `]`) and `isNotAlnum(r rune) bool`. Delete `platformTokens`.
The old patterns' retail spellings become catalog aliases in Task 1's table: "X360", "XB360",
"XB1", "Xbone", "PSVita", "PC DVD", "PC CD", "PC CD-ROM", "PC DVD-ROM", "Windows", "Nintendo
Switch 2 Edition", "PlayStation4"-style spellings are covered by `aliasPattern`.

- [ ] **Step 4: Run** `go test ./internal/domain/game/ ./internal/adapters/outbound/...` — Expected:
PASS (CeX, UPCitemdb and EAN-Search tests use `ParseProductName`; a changed expectation there is
only acceptable when the new answer is the right system).

- [ ] **Step 5: Commit** — `git commit -m "Domain: retail names are read with the catalog's names and aliases, longest first; ordinary words count only at the end or in brackets"`

---

### Task 3: A region on every copy

**Files:**
- Create: `internal/domain/game/region.go`, `internal/domain/game/region_test.go`
- Modify: `internal/domain/game/copy.go`, `internal/adapters/outbound/sqlite/docs.go` (+ its test),
  `internal/adapters/outbound/csvfile/codec.go` (+ golden files in `testdata`),
  `proto/gamevault/v1/game.proto`, `internal/adapters/inbound/rpc/game_mapper.go` (+ test),
  `web/src/lib/model.ts` (`emptyDetails` gets `region`)

**Interfaces:**
- Produces:
  ```go
  type Region string // RegionPAL "pal", RegionNTSCU "ntsc_u", RegionNTSCJ "ntsc_j", RegionNTSCK "ntsc_k", RegionNTSCC "ntsc_c", RegionAsia "asia", RegionFree "region_free"
  func Regions() []Region
  func (r Region) Valid() bool              // "" is valid (not set)
  func ParseRegion(s string) (Region, bool) // ids and labels: "PAL", "ntsc-u", "NTSC U", "Region free"
  // CopyDetails.Region Region
  ```
  Proto: `enum CopyRegion { COPY_REGION_UNSPECIFIED = 0; COPY_REGION_PAL = 1; COPY_REGION_NTSC_U = 2; COPY_REGION_NTSC_J = 3; COPY_REGION_NTSC_K = 4; COPY_REGION_NTSC_C = 5; COPY_REGION_ASIA = 6; COPY_REGION_REGION_FREE = 7; }`
  and `CopyRegion region = 18;` in `CopyDetails`.

- [ ] **Step 1: Failing domain test** `region_test.go`:

```go
func TestRegion(t *testing.T) {
	t.Run("GIVEN the region ids and the ways people write them", func(t *testing.T) {
		for in, want := range map[string]Region{
			"pal": RegionPAL, "PAL": RegionPAL, "NTSC-U": RegionNTSCU, "ntsc u": RegionNTSCU, "NTSC-J": RegionNTSCJ,
			"ntsc_k": RegionNTSCK, "NTSC-C": RegionNTSCC, "Asia": RegionAsia, "Region free": RegionFree, "region_free": RegionFree,
		} {
			got, ok := ParseRegion(in)
			assert.True(t, ok, in)
			assert.Equal(t, want, got, in)
		}

		_, ok := ParseRegion("Europe")
		assert.False(t, ok)
	})

	t.Run("GIVEN a key copy with a region", func(t *testing.T) {
		d := CopyDetails{Kind: KindKey, Region: RegionNTSCJ}

		t.Run("WHEN it is normalized and turned into a physical copy", func(t *testing.T) {
			got, err := d.normalize()
			require.NoError(t, err)

			got.Kind = KindPhysical
			got, err = got.normalize()
			require.NoError(t, err)

			t.Run("THEN the region stays: it belongs to every kind", func(t *testing.T) {
				assert.Equal(t, RegionNTSCJ, got.Region)
			})
		})
	})

	t.Run("GIVEN an unknown region", func(t *testing.T) {
		_, err := CopyDetails{Kind: KindPhysical, Region: "mars"}.normalize()
		assert.ErrorIs(t, err, ErrInvalid)
	})
}
```

(Check the name of the domain's invalid sentinel used by `invalid(...)` in `copy.go` and use it.)

- [ ] **Step 2: Run it to fail**, then **Step 3: implement** `region.go` after `Grade` in
`physical.go` (same shape: typed string, `regions` slice in display order, `Regions()` clone,
`Valid()`), `ParseRegion` comparing `strings.ToLower` with spaces and hyphens turned into `_`
against the ids, plus `"region free"`→`RegionFree`. In `CopyDetails` add, after `Price`:

```go
	// Region is the copy's region (PAL, NTSC-J…); empty: not stated. Any kind may have one.
	Region Region
```

and in `normalize`, next to the grade check: `if !d.Region.Valid() { return d, invalid("region %q is not valid", d.Region) }`.
`dropOtherKindFields` does not touch it.

- [ ] **Step 4: Store it** — `copyDoc` gets `Region string \`json:"region,omitempty"\`` after
`Grade`; map it both ways where `Grade` is mapped. Test: extend the existing copy round-trip test
in the sqlite package with `Region: game.RegionPAL` and assert it reads back; a document without the
field reads back as `""` (additive field, no version bump).

- [ ] **Step 5: CSV** — add `{"region", func(r exportRow) string { return string(r.copy.Region) }}`
after `"autoLinks"` (new columns go last so older files and the golden order stay readable), and
in the reader `d.Region = r.region()`:

```go
func (r *rowReader) region() game.Region {
	v := r.get("region")
	if v == "" {
		return ""
	}

	g, ok := game.ParseRegion(v)
	if !ok {
		r.warn("region %q is not one of pal, ntsc_u, ntsc_j, ntsc_k, ntsc_c, asia, region_free", v)

		return ""
	}

	return g
}
```

Document the column in the package comment. Update the golden files (`go test ./internal/adapters/outbound/csvfile/ -update`
if the package has the flag; otherwise edit them by hand) and add an import case with
`region` = `NTSC-J` and one with `Europe` (warning, empty region). Add a row whose platform is
`Mega Drive / Genesis` to the round-trip test.

- [ ] **Step 6: API** — add the `CopyRegion` enum after `CopyGrade` in `game.proto` and the field
`CopyRegion region = 18;` (comment: "The copy's region; unspecified: not stated.") to
`CopyDetails`. `task generate`. In `game_mapper.go` add `regionToPB`/`regionFromPB` maps like
`gradeToPB`/`gradeFromPB` and map the field both ways with `enumFromPB(regionFromPB, d.Region, "copy region")`.
Extend the mapper test with a PAL copy. In `web/src/lib/model.ts`, `emptyDetails` gets
`region: CopyRegion.UNSPECIFIED`, and export `REGIONS` (every value but UNSPECIFIED, in enum order)
and `regionKey = (r: CopyRegion) => CopyRegion[r].toLowerCase()`.

- [ ] **Step 7: Run** `task test`. Expected: PASS. **Commit** —
`git commit -m "Copies: a region (PAL, NTSC-U, NTSC-J…) on every copy, stored, in the CSV and in the API"`

---

### Task 4: Region from CeX lookups

**Files:**
- Modify: `internal/application/media/ports.go` (`BarcodeMatch.Region game.Region`),
  `internal/adapters/outbound/cex/provider.go` (+ `provider_test.go`),
  `proto/gamevault/v1/lookup.proto` (`CopyRegion region = 7;` in `BarcodeMatch`, import
  `gamevault/v1/game.proto` if not imported), `internal/adapters/inbound/rpc/lookup_handler.go`
  (+ `lookup_test.go`)

**Interfaces:**
- Consumes: `game.Region` (Task 3), `regionToPB` (Task 3, rpc package).
- Produces: `BarcodeMatch.region` in the API, read by Scan in Task 10.

- [ ] **Step 1: Failing test** in `cex/provider_test.go`: a lookup answered by the `es` catalog
returns `Region: game.RegionPAL`; answered by `mx`, `game.RegionNTSCU`.
- [ ] **Step 2: Implement**:

```go
// countryRegions is the region of the boxes each CeX country sells: Europe, Australia and India are
// PAL markets, Mexico an NTSC-U one.
var countryRegions = map[string]game.Region{
	"uk": game.RegionPAL,
	"es": game.RegionPAL,
	"ie": game.RegionPAL,
	"pt": game.RegionPAL,
	"it": game.RegionPAL,
	"pl": game.RegionPAL,
	"au": game.RegionPAL,
	"in": game.RegionPAL,
	"mx": game.RegionNTSCU,
}
```

`match(b box)` becomes `match(b box, country string)` and sets `Region: countryRegions[country]`.
A test checks every entry of `Countries` has a region (so a new country forces a decision).
Other barcode providers leave Region empty. Map it in `lookup_handler.go` with `regionToPB`.
- [ ] **Step 3:** `task generate`, `task test`. **Commit** —
`git commit -m "Barcode lookups: CeX says the region of the boxes its country sells"`

---

### Task 5: Provider tables cover the catalog

**Files:**
- Create: `internal/domain/game/coverage.go`, `internal/domain/game/coverage_test.go`
- Modify: `internal/adapters/outbound/igdb/match.go`, `igdb/systems_test.go`, `igdb/releases_test.go`,
  `internal/adapters/outbound/thegamesdb/provider.go`, `thegamesdb/systems_test.go`,
  `internal/adapters/outbound/wikipedia/details.go`, `wikipedia/systems_test.go`,
  `cmd/gamevault/vocabulary_test.go`

**Interfaces:**
- Consumes: `Systems()` (Task 1).
- Produces:
  ```go
  // CheckProviderCoverage reports how a provider's table disagrees with the catalog; nil when it covers it.
  func CheckProviderCoverage[V any](table map[string][]V, excluded []string) error
  ```

- [ ] **Step 1: Failing test** `coverage_test.go`:

```go
func TestCheckProviderCoverage(t *testing.T) {
	full := map[string][]int{}
	for _, s := range Systems() {
		full[s.Name] = []int{1}
	}

	t.Run("GIVEN a table mapping every system", func(t *testing.T) {
		assert.NoError(t, CheckProviderCoverage(full, nil))
	})

	t.Run("GIVEN a table missing one system", func(t *testing.T) {
		missing := maps.Clone(full)
		delete(missing, "SNES")

		t.Run("THEN it fails naming it", func(t *testing.T) {
			assert.ErrorContains(t, CheckProviderCoverage(missing, nil), `"SNES"`)
		})

		t.Run("OR it passes when the system is excluded", func(t *testing.T) {
			assert.NoError(t, CheckProviderCoverage(missing, []string{"SNES"}))
		})
	})

	t.Run("GIVEN a key that is not a catalog name, an empty entry, or a system mapped and excluded", func(t *testing.T) {
		bad := maps.Clone(full)
		bad["Super Nintendo"] = []int{4}
		bad["NES"] = nil

		err := CheckProviderCoverage(bad, []string{"PS4"})
		assert.ErrorContains(t, err, `"Super Nintendo"`)
		assert.ErrorContains(t, err, `"NES"`)
		assert.ErrorContains(t, err, `"PS4"`)
	})
}
```

- [ ] **Step 2: Implement** `coverage.go`:

```go
// CheckProviderCoverage reports how a provider's table of systems disagrees with the catalog: a
// catalog system with neither an entry nor an exclusion, a key that is not a catalog name, an empty
// entry, or a system both mapped and excluded. Each provider's tests call it, so adding a system to
// the catalog forces a decision in every provider. Nil means the table covers the catalog.
func CheckProviderCoverage[V any](table map[string][]V, excluded []string) error {
	var errs []error

	for _, s := range catalog {
		ids, mapped := table[s.Name]
		skipped := slices.Contains(excluded, s.Name)

		switch {
		case mapped && skipped:
			errs = append(errs, fmt.Errorf("system %q is both mapped and excluded", s.Name))
		case mapped && len(ids) == 0:
			errs = append(errs, fmt.Errorf("system %q has an empty entry", s.Name))
		case !mapped && !skipped:
			errs = append(errs, fmt.Errorf("system %q is neither mapped nor excluded", s.Name))
		}
	}

	for name := range table {
		if !slices.ContainsFunc(catalog, func(s System) bool { return s.Name == name }) {
			errs = append(errs, fmt.Errorf("%q is not a catalog system", name))
		}
	}

	return errors.Join(errs...)
}
```

- [ ] **Step 3: Replace the three tables** with the IGDB, TheGamesDB and Wikidata blocks of the
appendix, verbatim (the 20 existing rows keep their `game.System…` constants; new rows use the
catalog name as a string literal), keeping each table's doc comment, and add each block's
`unmappedSystems` with a doc comment (the provider has no platform of its own for them). Replace each `systems_test.go` body with:

```go
// TestSystems_coverTheCatalog checks the table of <provider>'s platforms against the catalog: a
// system missing from it, or a key spelled differently, silently drops the platform filter.
func TestSystems_coverTheCatalog(t *testing.T) {
	require.NoError(t, game.CheckProviderCoverage(platformIDs, unmappedSystems))
}
```

(`platformNames` for TheGamesDB, `platformItems` for Wikidata.) In `igdb/releases_test.go` add a
case: a release whose `platforms` is `[508]` comes back with `Platforms == []string{"Switch 2"}`
(508 is IGDB's Switch 2 platform id in the appendix).

- [ ] **Step 4: Vocabulary test** — in `cmd/gamevault/vocabulary_test.go` add, and skip literals
inside these declarations (as `allowed` does for `rawVocabulary`):

```go
// systemTables are provider tables keyed by catalog system names written as literals: a hundred
// systems have no game constant, and game.CheckProviderCoverage fails on a key that is not a
// catalog name, so they cannot drift. Literals inside them are not checked.
var systemTables = []struct {
	file string
	decl string
}{
	{"internal/adapters/outbound/igdb/match.go", "platformIDs"},
	{"internal/adapters/outbound/thegamesdb/provider.go", "platformNames"},
	{"internal/adapters/outbound/wikipedia/details.go", "platformItems"},
}
```

Remove the now unused `rawVocabulary` entry for TheGamesDB's `"PC"`. If other literals in
adapters now equal a new catalog name (the test names them), replace them with the constant or add
a `rawVocabulary` entry with the reason.

- [ ] **Step 5:** `task lint`, `task test`. **Commit** —
`git commit -m "Providers: IGDB, TheGamesDB and Wikidata map every catalog system or exclude it explicitly"`

---

### Task 6: Steam, SteamGridDB and PlayStation read the catalog

**Files:**
- Modify: `internal/adapters/outbound/steam/store.go` (+ test), `internal/adapters/outbound/steamgriddb/covers.go`
  (+ test), `internal/adapters/outbound/playstation/provider.go` (+ test), `cmd/gamevault/vocabulary_test.go`

**Interfaces:**
- Consumes: `game.IsModernSystem`, `game.SystemPattern` (Tasks 1, 2).

- [ ] **Step 1: Failing tests**:
  - Steam: `modernEnough` is true for a game with a copy on `"Switch 2"` and false for one only on
    `"SNES"` (add to the existing `FindLinks` / `modernEnough` tests).
  - SteamGridDB: `withoutStoreTags("Super Metroid [SNES]") == "Super Metroid"`,
    `withoutStoreTags("Gunstar Heroes (Mega Drive / Genesis)") == "Gunstar Heroes"`,
    `withoutStoreTags("Doom (2016)") == "Doom (2016)"` (unchanged).
  - PlayStation: `platformName("PS2") == game.SystemPS2`, `platformName("ps1") == game.SystemPS1`.
- [ ] **Step 2: Implement**:
  - Steam: delete `modernSystems`; `modernEnough` checks `game.IsModernSystem(game.SystemOf(p))`.
    Keep the comment's reasoning on the catalog's `Modern` field.
  - SteamGridDB: keep the store and key words in `storeWords`, drop the hand-written console words
    (`xbox…`, `playstation…`, `ps…`, `psp`, `nintendo…`, `switch`, `wii…`, `gamecube`, `n?3?ds`),
    and build the regexps from `storeWords + "|" + game.SystemPattern()` in a `var` block.
  - PlayStation: `sonyPlatforms` gains `"PS2": game.SystemPS2` and `"PS1": game.SystemPS1`; add the
    two literals to `rawVocabulary` next to the other Sony codes.
- [ ] **Step 3:** `task test`. **Commit** —
`git commit -m "Steam, SteamGridDB and PlayStation take systems from the catalog"`

---

### Task 7: CatalogService.ListSystems and the web's catalog helpers

**Files:**
- Create: `proto/gamevault/v1/catalog.proto`, `internal/adapters/inbound/rpc/catalog_handler.go`,
  `internal/adapters/inbound/rpc/catalog_test.go`, `web/src/lib/systems.ts`, `web/tests/systems.test.mjs`
- Modify: `internal/adapters/inbound/rpc/server.go`, `internal/app/app.go`,
  `web/src/lib/rpc.ts` (or wherever the Connect clients are created: add `catalogClient`),
  `web/src/state/AppData.tsx`, `web/src/lib/editions.ts`, `web/src/lib/model.ts`,
  `web/tests/editions.test.mjs`, `internal/domain/game/webparity_test.go`

**Interfaces:**
- Produces (proto):
  ```proto
  // SystemInfo is one game system of the catalog (see game.System).
  message SystemInfo {
    string name = 1;       // the stored name, never translated
    string maker = 2;
    string kind = 3;       // "pc", "home", "handheld", "computer"
    int32 year = 4;
    repeated string aliases = 5;
    string short = 6;      // badge label; empty: the name
    bool modern = 7;
  }
  message ListSystemsRequest {}
  message ListSystemsResponse { repeated SystemInfo systems = 1; } // display order
  service CatalogService { rpc ListSystems(ListSystemsRequest) returns (ListSystemsResponse); }
  ```
- Produces (web, `lib/systems.ts`, structural types only so Node tests import it):
  ```ts
  export interface SystemInfo { name: string; maker: string; kind: string; year: number; aliases: string[]; short: string }
  export function systemKey(s: string): string
  export function findSystem(catalog: readonly SystemInfo[], text: string): SystemInfo | undefined
  export interface PickerOption { name: string; alias?: string; maker: string }
  export interface PickerGroup { maker: string | null /* null: "In your collection" */; options: PickerOption[] }
  export function pickerGroups(catalog: readonly SystemInfo[], owned: readonly string[], query: string): PickerGroup[]
  ```
  `AppData.systems: SystemInfo[]`; `systemOf(platform: string, systems: readonly string[]): string` in `editions.ts`.

- [ ] **Step 1: Failing Go test** `catalog_test.go` (use the rpc test harness the other `*_test.go`
files use; signed in):

```go
func TestListSystems(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a signed-in caller", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		client := gamevaultv1connect.NewCatalogServiceClient(c.client, c.baseURL)

		t.Run("WHEN it lists the systems", func(t *testing.T) {
			res, err := client.ListSystems(ctx, connect.NewRequest(&pb.ListSystemsRequest{}))
			require.NoError(t, err)

			t.Run("THEN it gets the whole catalog in order, PC first, with makers and aliases", func(t *testing.T) {
				require.Len(t, res.Msg.Systems, len(game.Systems()))
				assert.Equal(t, "PC", res.Msg.Systems[0].Name)

				i := slices.IndexFunc(res.Msg.Systems, func(s *pb.SystemInfo) bool { return s.Name == "SNES" })
				require.GreaterOrEqual(t, i, 0)
				assert.Equal(t, "Nintendo", res.Msg.Systems[i].Maker)
				assert.Contains(t, res.Msg.Systems[i].Aliases, "Super Famicom")
			})
		})
	})
}
```

(Match the harness's real helper names — read `harness_test.go` first.)

- [ ] **Step 2: Implement** the proto (`task generate`), and:

```go
var _ gamevaultv1connect.CatalogServiceHandler = (*CatalogHandler)(nil)

// CatalogHandler serves the catalog of game systems.
type CatalogHandler struct{}

// NewCatalogHandler builds the handler.
func NewCatalogHandler() *CatalogHandler { return &CatalogHandler{} }

// ListSystems returns every system of the catalog, in display order.
func (h *CatalogHandler) ListSystems(context.Context, *connect.Request[pb.ListSystemsRequest]) (*connect.Response[pb.ListSystemsResponse], error) {
	out := &pb.ListSystemsResponse{}

	for _, s := range game.Systems() {
		out.Systems = append(out.Systems, &pb.SystemInfo{
			Name:    s.Name,
			Maker:   s.Maker,
			Kind:    string(s.Kind),
			Year:    int32(s.Year),
			Aliases: s.Aliases,
			Short:   s.Short,
			Modern:  s.Modern,
		})
	}

	return connect.NewResponse(out), nil
}
```

`Handlers.Catalog *CatalogHandler` (documented), mounted in `NewHTTPHandler`, built in
`internal/app/app.go`. It needs a signed-in caller like every procedure not in `publicProcedures`;
it is not in `tokenProcedures`.

- [ ] **Step 3: Failing Node test** `web/tests/systems.test.mjs`:

```js
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { findSystem, pickerGroups, systemKey } from '../src/lib/systems.ts';

const sys = (name, maker, aliases = [], kind = 'home') => ({ name, maker, kind, year: 2000, aliases, short: '' });
const catalog = [
  sys('PC', 'PC', ['Steam Deck'], 'pc'),
  sys('PS4', 'Sony', ['PlayStation 4']),
  sys('SNES', 'Nintendo', ['Super Nintendo', 'Super Famicom', 'SFC']),
  sys('NES', 'Nintendo', ['Famicom']),
  sys('Mega Drive / Genesis', 'Sega', ['Mega Drive', 'Genesis', 'Sega Genesis']),
];

test('systemKey agrees with the server on the shared cases', () => {
  for (const [input, want] of JSON.parse(readFileSync(new URL('./systemKey.cases.json', import.meta.url), 'utf8'))) {
    assert.equal(systemKey(input), want, input);
  }
});

test('findSystem finds by name or alias, ignoring case, spaces, hyphens and dots', () => {
  assert.equal(findSystem(catalog, 'super-famicom')?.name, 'SNES');
  assert.equal(findSystem(catalog, 'p.s. 4')?.name, 'PS4');
  assert.equal(findSystem(catalog, 'Amiga'), undefined);
});

test('without a query: the user\'s systems first, then every system by maker', () => {
  const groups = pickerGroups(catalog, ['SNES', 'Commodore PET'], '');
  assert.deepEqual(groups.map((g) => g.maker), [null, 'PC', 'Sony', 'Nintendo', 'Sega']);
  assert.deepEqual(groups[0].options.map((o) => o.name), ['SNES', 'Commodore PET']);
});

test('a query filters by name and alias, and shows the alias that matched', () => {
  const groups = pickerGroups(catalog, ['SNES'], 'famicom');
  assert.deepEqual(groups.map((g) => g.maker), ['Nintendo']);
  assert.deepEqual(groups[0].options, [
    { name: 'SNES', alias: 'Super Famicom', maker: 'Nintendo' },
    { name: 'NES', alias: 'Famicom', maker: 'Nintendo' },
  ]);
});

test('a query matching a name shows no alias', () => {
  assert.deepEqual(pickerGroups(catalog, [], 'ps4')[0].options, [{ name: 'PS4', maker: 'Sony' }]);
});

test('no match gives no groups (the picker offers the typed text)', () => {
  assert.deepEqual(pickerGroups(catalog, [], 'amiga'), []);
});
```

(With a query, the "In your collection" group is not shown: the search spans everything once.)

- [ ] **Step 4: Implement** `lib/systems.ts` (`systemKey`: `s.toLowerCase().replace(/[\s.\-]/gu, '')`;
`findSystem`; `pickerGroups`: empty query → owned group (owned names in the given order; catalog
entries take their maker, others `maker: ''`) then one group per maker in catalog order; with a
query → per maker, options whose name key includes the query key, else whose first matching alias
key includes it (alias shown); groups without options dropped).

In `editions.ts`: delete `SYSTEMS`, change `systemOf(platform: string, systems: readonly string[])`
to look the name up in `systems` (case-insensitive) after the PC and store tables, and update its
doc comment (the catalog comes from `CatalogService`). In `model.ts`: delete `PHYSICAL_PLATFORMS`
and its comment. Update `editions.test.mjs` to pass a systems list to `systemOf`. In `AppData.tsx`:
load `catalogClient.listSystems({})` once with the others into `systems: SystemInfo[]` (exposed on
the context, documented; a failure sets `error` like the others).

In `webparity_test.go`, rewrite `TestSystems_webAgrees` to check only what the web still keeps:
`PC_PLATFORMS` and `STORE_SYSTEMS` (editions.ts) and `STORE_PLATFORMS` (model.ts) against
`game.SystemOf` for every store platform in `game.KnownPlatforms()` whose system differs from its
name, in both directions; the consoles come from the API. Update its doc comment accordingly.

Every caller of the deleted arrays fails to type-check now; Tasks 8–10 replace them. In this task,
make the build pass with the smallest change: callers read `useAppData().systems.map((s) => s.name)`
where they used `SYSTEMS` / `PHYSICAL_PLATFORMS` (the datalists stay until Task 8).

- [ ] **Step 5:** `task generate`, `task lint`, `task test`. **Commit** —
`git commit -m "API: CatalogService lists the systems; the web loads them once and drops its own lists"`

---

### Task 8: The system picker

**Files:**
- Create: `web/src/components/SystemPicker.tsx`
- Modify: `web/src/styles.css`, `web/src/features/library/CopyForm.tsx`,
  `web/src/features/library/NewGameDialog.tsx`, `web/src/features/scan/ScanPage.tsx`,
  `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Consumes: `pickerGroups`, `SystemInfo` (Task 7), `systemCounts` (`editions.ts`), `useAppData`.
- Produces: `<SystemPicker value onChange placeholder? autoFocus? label />` — `value: string`,
  `onChange(v: string)`, `label: string` (the visible label text; the component renders the
  `<label>`).

- [ ] **Step 1: Build the component.** Behaviour:
  - Desktop: an `<input role="combobox" aria-expanded aria-controls aria-autocomplete="list"
    aria-activedescendant>`; typing sets the value (free text is allowed) and opens a popover
    `role="listbox"` under it with groups (`role="group"`, `aria-label` = maker or
    `t('systemPicker.yours')`). Options read "SNES" or "SNES · Super Famicom" (alias matched).
    With text that is not exactly a catalog name, a last option `t('systemPicker.use', { text })`
    ("Use “…”") keeps the typed text. Arrow keys move the active option (wrapping), Enter picks it
    (and does not submit the form), Escape closes (and stops there: use `useEscape`), Tab closes,
    a click outside closes. Picking sets the value to the catalog name and closes. The owned list
    is `systemCounts(games)` names, most games first.
  - Phones (`window.matchMedia('(max-width: 860px)')`, read on open): the input is read-only and
    opens a bottom sheet (`role="dialog"`, `aria-modal`) with a 16 px search field on top
    (autofocused), the same grouped list below, and the "Use “…”" option; picking closes it.
  - Motion per `.claude/docs/ui.md`: the popover fades/scales from 0.97 at ≤ 200 ms with
    `--ease-out` via `@starting-style`; the sheet slides from `translateY(100%)` with
    `--ease-drawer`; nothing animates on keyboard navigation; `prefers-reduced-motion` collapses
    it. Layers: popover `--z-popover`, sheet `--z-modal`. Tokens only (radii, colours).
  - i18n keys (en / es): `systemPicker.search` ("Search systems" / "Buscar sistemas"),
    `systemPicker.yours` ("In your collection" / "En tu colección"), `systemPicker.use`
    ("Use “{{text}}”" / "Usar «{{text}}»"), `systemPicker.none` ("No system matches" /
    "Ningún sistema coincide"), `systemPicker.otherMaker` ("Other" / "Otros").
- [ ] **Step 2: Use it** in `CopyForm` (the System field, and the Platform field when the kind is
  physical; stores keep their datalist), `NewGameDialog` (physical platform) and `ScanPage`
  (the default platform). Remove the replaced datalists.
- [ ] **Step 3:** `task test` (type check + i18n). **Commit** —
`git commit -m "Web: a searchable system picker grouped by maker, the user's systems first, a bottom sheet on phones"`

---

### Task 9: Badges and the library's system filter from the catalog

**Files:**
- Modify: `web/src/components/PlatformBadge.tsx`, `web/src/state/AppData.tsx`,
  `web/src/features/library/FilterPanel.tsx`, `web/src/styles.css`, locales

**Interfaces:**
- Consumes: `AppData.systems` (Task 7).
- Produces: `registerSystems(list: SystemInfo[])` in `PlatformBadge.tsx` (called by AppData right
  after the catalog loads, before its state is set, so the first render of a page already has it).

- [ ] **Step 1: Badges.** Keep `LOOKS` for the stores and the 20 existing systems (their icons and
  labels do not change). For any other catalog system, `platformLook` returns
  `{ glyph: MAKER_GLYPHS[maker], label: short || name, bg: MAKER_BG[maker] ?? '#4b5568' }`, with:

  ```ts
  /** Each maker's colour: platform colours encode who made the system. */
  const MAKER_BG: Record<string, string> = {
    Nintendo: NINTENDO_BG, Sony: PS_BG, Microsoft: XBOX_BG, Sega: '#0089cf', Atari: '#c4161c', NEC: '#24408e',
    SNK: '#1f1f1f', Bandai: '#e4002b', Commodore: '#3d4f9e', Sinclair: '#2b2b2b', Amstrad: '#1f6f43',
  };
  const MAKER_GLYPHS: Record<string, Glyph | undefined> = { Sega: si(siSega), Sony: PS, Microsoft: XBOX };
  ```

  Unknown names keep the neutral chip. The catalog lives in a module-level `Map` keyed by
  `systemKey(name)` filled by `registerSystems` (comment: `platformLook` is used outside components,
  e.g. the home's collection block).
- [ ] **Step 2: Filter.** In `FilterPanel`, when the user has more than 8 systems, group the
  system choices under maker headings (catalog order; systems not in the catalog under
  `t('systemPicker.otherMaker')`); with 8 or fewer, the flat list stays.
- [ ] **Step 3:** `task test`. **Commit** —
`git commit -m "Web: badges take each maker's colour and the catalog's short label; system filters grouped by maker"`

---

### Task 10: Region in the UI

**Files:**
- Modify: `web/src/features/library/PhysicalInputs.tsx` (`RegionSelect`),
  `web/src/features/library/CopyForm.tsx`, `web/src/features/library/CopyCard.tsx`,
  `web/src/lib/editions.ts` (`Filters.regions`), `web/tests/editions.test.mjs`,
  `web/src/features/library/FilterPanel.tsx`, `web/src/features/scan/ScanPage.tsx`, locales

**Interfaces:**
- Consumes: `CopyRegion`, `REGIONS`, `regionKey` (Task 3), `BarcodeMatch.region` (Task 4).

- [ ] **Step 1: Failing Node test** in `editions.test.mjs`: with `Filters.regions = [1]` (PAL),
  a game with a copy whose `details.region` is 1 matches and one without does not;
  `activeFilterCount` counts regions; `NO_FILTERS.regions` is `[]`. `CopyLike.details` gains
  `region?: number`.
- [ ] **Step 2: Implement**:
  - `Filters.regions: number[]` ("CopyRegion's numbers"), matched like systems (a copy in one of
    them), counted, and pruned nowhere (enum values do not disappear).
  - `RegionSelect` like `GradeSelect`: "Not set" first, then `REGIONS` with `t('region.<key>')`.
    Shown in the copy form for every kind.
  - `CopyCard` shows `t('region.short.<key>')` as a small tag when set.
  - `FilterPanel` gets a "Region" section listing the regions present in the user's copies, with
    counts, only when at least one copy has a region.
  - Scan: `Defaults.region` (remembered like the grade); a row takes the lookup's region when the
    match has one, else the default.
  - i18n (en / es): `copy.region` ("Region" / "Región"), `region.unspecified` ("Not set" /
    "Sin indicar"), `region.pal` ("PAL (Europe, Australia)" / "PAL (Europa, Australia)"),
    `region.ntsc_u` ("NTSC-U (Americas)" / "NTSC-U (América)"), `region.ntsc_j` ("NTSC-J (Japan)" /
    "NTSC-J (Japón)"), `region.ntsc_k` ("NTSC-K (Korea)" / "NTSC-K (Corea)"), `region.ntsc_c`
    ("NTSC-C (China)" / "NTSC-C (China)"), `region.asia` ("Asia" / "Asia"), `region.region_free`
    ("Region free" / "Sin región"), `region.short.*` ("PAL", "US", "JP", "KR", "CN", "Asia",
    "Free" / "PAL", "US", "JP", "KR", "CN", "Asia", "Libre"), `library.filters.regions`
    ("Region" / "Región").
- [ ] **Step 3:** `task test`. **Commit** —
`git commit -m "Web: a copy's region in the form, on the card, as a library filter, remembered by Scan"`

---

### Task 11: Docs, probes and the visual check

**Files:**
- Modify: `docs/technical.md`, `docs/plugins.md`, `.claude/memory/data-model.md`, README only if
  its platforms text names the console list.

- [ ] **Step 1: Docs.** `docs/technical.md`: the catalog (fields, order, aliases and matching,
  compatibility of hand-typed names), the region (values, CSV column, CeX), the picker, the
  badges, the API. `docs/plugins.md`: a provider that maps systems keeps a table keyed by catalog
  names and calls `game.CheckProviderCoverage` in a test, listing the systems it cannot map.
  Memory `data-model.md`: one line on the catalog and the region.
- [ ] **Step 2: Probes** (bogus credentials only): TheGamesDB `GET /v1/Platforms?apikey=bogus`
  expecting a clean 403-style error; IGDB `POST /v4/platforms` with a bogus Client-ID and token
  expecting 401/403. Note results in the PR; nothing is stored.
- [ ] **Step 3: Visual check.** `task lint`, `task test`, `task test:race`; `task test-server`;
  in the browser pane at http://127.0.0.1:8093/?v=N: the copy form's picker (search "famicom",
  "megadrive", arrows + Enter, free text), the region select, the library filter, a badge of a new
  system; then the mobile preset (bottom sheet, 16 px input); reset to desktop;
  `task test-server:stop`.
- [ ] **Step 4: Commit, push, PR** — `git push -u origin feature/systems-catalog`,
  `gh pr create --base dev` with a summary of what changed, what was verified against fakes only
  (IGDB/TheGamesDB ids are from their public lists; the providers were not called with real keys).

---

## Appendix: the catalog

The tables below were researched on 2026-10-15: IGDB ids from RomM's public IGDB platform list (not the IGDB API), TheGamesDB names verbatim from thegamesdb.net/list_platforms.php, Wikidata QIDs from SPARQL and label search. Merged as aliases: Famicom (NES), Super Famicom (SNES), Nomad (Mega Drive), Neo Geo MVS (AES), Commodore 128 (C64), Evercade VS/EXP (Evercade). Not systems: Analogue Pocket (plays Game Boy carts), Intellivision Amico (never shipped). Copy each block verbatim; gofmt aligns them.

### `catalog` (internal/domain/game/systems.go)

```go
	// PC.
	{
		Name:    SystemPC,
		Maker:   "PC",
		Kind:    SystemKindPC,
		Year:    1981,
		Aliases: []string{"Windows", "Microsoft Windows", "Windows PC", "PC (Microsoft Windows)", "PC (Windows)", "DOS", "MS-DOS", "Linux", "Mac", "macOS", "Mac OS", "Mac OS X", "OS X", "Steam Deck", "SteamOS", "IBM PC", "PC DVD", "PC CD", "PC CD-ROM", "PC DVD-ROM"},
		Modern:  true,
	},
	// Sony.
	{
		Name:    SystemPS5,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2020,
		Aliases: []string{"PlayStation 5", "PS 5", "Sony PlayStation 5", "PS5 Pro", "PS5 Slim", "PlayStation 5 Pro", "PlayStation 5 Slim"},
		Modern:  true,
	},
	{
		Name:    SystemPS4,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"PlayStation 4", "PS 4", "Sony PlayStation 4", "PS4 Pro", "PS4 Slim", "PlayStation 4 Pro", "PlayStation 4 Slim"},
		Modern:  true,
	},
	{
		Name:    SystemPS3,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"PlayStation 3", "PS 3", "Sony PlayStation 3", "PS3 Slim", "PS3 Super Slim"},
	},
	{
		Name:    SystemPS2,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2000,
		Aliases: []string{"PlayStation 2", "PS 2", "Sony PlayStation 2", "PS2 Slim", "PStwo", "PS2 Slimline"},
	},
	{
		Name:    SystemPS1,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"PlayStation", "PlayStation 1", "PlayStation One", "PSX", "PS one", "PSone", "Sony PlayStation", "Sony PlayStation 1"},
	},
	{
		Name:    SystemPSVita,
		Maker:   "Sony",
		Kind:    SystemKindHandheld,
		Year:    2011,
		Aliases: []string{"PlayStation Vita", "PSV", "PSVita", "PS Vita TV", "PlayStation Vita TV", "Sony PS Vita", "Sony PlayStation Vita"},
		Short:   "Vita",
	},
	{
		Name:    SystemPSP,
		Maker:   "Sony",
		Kind:    SystemKindHandheld,
		Year:    2004,
		Aliases: []string{"PlayStation Portable", "Sony PSP", "PSP Go", "PSP Street", "PSP-1000", "PSP-2000", "PSP-3000"},
	},
	// Microsoft.
	{
		Name:    SystemXboxSeries,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2020,
		Aliases: []string{"Xbox Series X", "Xbox Series S", "Xbox Series X|S", "Xbox Series X/S", "Series X|S", "Series X/S", "Xbox Series X S", "XSX", "Microsoft Xbox Series X"},
		Modern:  true,
		Short:   "XS",
	},
	{
		Name:    SystemXboxOne,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"Xbox One S", "Xbox One X", "XB1", "Microsoft Xbox One", "XboxOne", "Xbone"},
		Modern:  true,
		Short:   "XONE",
	},
	{
		Name:    SystemXbox360,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2005,
		Aliases: []string{"X360", "XBOX360", "Xbox360", "Microsoft Xbox 360", "Xbox 360 S", "Xbox 360 E", "XB360"},
		Short:   "X360",
	},
	{
		Name:    SystemXbox,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2001,
		Aliases: []string{"Original Xbox", "Xbox (original)", "Xbox Classic", "OG Xbox", "Microsoft Xbox"},
		Short:   "XBOX",
	},
	// Nintendo.
	{
		Name:    "Switch 2",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2025,
		Aliases: []string{"Nintendo Switch 2", "NS2", "Switch2", "Nintendo Switch 2 Edition"},
		Modern:  true,
		Short:   "NSW2",
	},
	{
		Name:    SystemSwitch,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2017,
		Aliases: []string{"Nintendo Switch", "Nintendo Switch OLED", "Switch OLED", "Switch Lite", "Nintendo Switch Lite"},
		Modern:  true,
		Short:   "NSW",
	},
	{
		Name:    SystemWiiU,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2012,
		Aliases: []string{"Nintendo Wii U", "WiiU", "Wii-U"},
		Short:   "WiiU",
	},
	{
		Name:    SystemWii,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"Nintendo Wii", "Wii Mini"},
	},
	{
		Name:    SystemGameCube,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2001,
		Aliases: []string{"Nintendo GameCube", "Game Cube", "Nintendo Game Cube", "GCN", "NGC", "GC"},
		Short:   "GC",
	},
	{
		Name:    "Nintendo 64DD",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1999,
		Aliases: []string{"64DD", "N64DD", "Nintendo 64 Disk Drive", "Nintendo 64 Disk Drive (64DD)"},
		Short:   "64DD",
	},
	{
		Name:    SystemN64,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1996,
		Aliases: []string{"Nintendo 64", "Nintendo64", "N 64", "Ultra 64"},
	},
	{
		Name:    "SNES",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"Super Nintendo", "Super Nintendo Entertainment System", "Super NES", "Super Famicom", "Super Family Computer", "SFC"},
	},
	{
		Name:    "Famicom Disk System",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1986,
		Aliases: []string{"Family Computer Disk System", "Famicom Disk", "Disk System", "Nintendo Famicom Disk System"},
		Short:   "FDS",
	},
	{
		Name:    "NES",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1983,
		Aliases: []string{"Nintendo Entertainment System", "Famicom", "Family Computer", "Nintendo Famicom", "Famicom (Nintendo)", "Nintendo NES"},
	},
	{
		Name:    System3DS,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2011,
		Aliases: []string{"Nintendo 3DS", "New 3DS", "New Nintendo 3DS", "3DS XL", "New 3DS XL", "N3DS", "2DS", "Nintendo 2DS", "New 2DS XL", "New Nintendo 2DS XL"},
	},
	{
		Name:    SystemDS,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2004,
		Aliases: []string{"Nintendo DS", "NDS", "DS Lite", "Nintendo DS Lite", "DSi", "Nintendo DSi", "DSi XL", "NDS Lite"},
		Short:   "NDS",
	},
	{
		Name:    "Game Boy Advance",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2001,
		Aliases: []string{"Nintendo Game Boy Advance", "GBA SP", "Game Boy Advance SP", "Game Boy Micro", "Gameboy Advance"},
		Short:   "GBA",
	},
	{
		Name:    "Pokémon Mini",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2001,
		Aliases: []string{"Pokemon Mini", "Nintendo Pokémon Mini", "Nintendo Pokemon Mini", "Pokemon mini"},
	},
	{
		Name:    "Game Boy Color",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1998,
		Aliases: []string{"Nintendo Game Boy Color", "Gameboy Color"},
		Short:   "GBC",
	},
	{
		Name:    "Virtual Boy",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1995,
		Aliases: []string{"Nintendo Virtual Boy", "VirtualBoy"},
		Short:   "VB",
	},
	{
		Name:    SystemGameBoy,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1989,
		Aliases: []string{"Nintendo Game Boy", "Gameboy", "Game Boy Original", "Game Boy Classic", "Game Boy Pocket", "Game Boy Light", "Gameboy Pocket"},
		Short:   "GB",
	},
	{
		Name:    "Game & Watch",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1980,
		Aliases: []string{"Game and Watch", "Nintendo Game & Watch", "Nintendo Game and Watch", "GnW"},
		Short:   "G&W",
	},
	// Sega.
	{
		Name:    "Dreamcast",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1998,
		Aliases: []string{"Sega Dreamcast"},
		Short:   "DC",
	},
	{
		Name:    "32X",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Sega 32X", "Mega 32X", "Sega Mega 32X", "Sega Genesis 32X", "Super 32X", "Sega Super 32X"},
	},
	{
		Name:    "Saturn",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Sega Saturn"},
		Short:   "SAT",
	},
	{
		Name:    "Pico",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Sega Pico", "Sega Mega Pico", "Mega Pico", "Kids Computer Pico"},
		Short:   "PICO",
	},
	{
		Name:    "Mega-CD / Sega CD",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"Sega CD", "Mega-CD", "Mega CD", "Sega Mega-CD", "Sega Mega CD", "SegaCD"},
		Short:   "MCD",
	},
	{
		Name:    "Mega Drive / Genesis",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1988,
		Aliases: []string{"Sega Genesis", "Sega Mega Drive", "Mega Drive", "Genesis", "Sega Megadrive", "Megadrive", "Sega Mega Drive/Genesis", "Mega Drive/Genesis", "Sega Nomad", "Genesis Nomad", "Sega Genesis Nomad"},
		Short:   "MD",
	},
	{
		Name:    "Master System",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1985,
		Aliases: []string{"Sega Master System", "Mark III", "Sega Mark III", "Sega Master System / Mark III", "Master System / Mark III", "Sega Master System/Mark III", "SMS"},
		Short:   "SMS",
	},
	{
		Name:    "SG-1000",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1983,
		Aliases: []string{"SG1000", "Sega SG-1000", "SEGA SG-1000", "SC-3000", "Sega SC-3000"},
		Short:   "SG1K",
	},
	{
		Name:    "Game Gear",
		Maker:   "Sega",
		Kind:    SystemKindHandheld,
		Year:    1990,
		Aliases: []string{"Sega Game Gear", "GameGear"},
		Short:   "GG",
	},
	// Atari.
	{
		Name:    "Atari Jaguar CD",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1995,
		Aliases: []string{"Jaguar CD"},
		Short:   "JCD",
	},
	{
		Name:    "Atari Jaguar",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Jaguar", "Atari Jaguar 64"},
		Short:   "JAG",
	},
	{
		Name:    "Atari 7800",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1986,
		Aliases: []string{"Atari 7800 ProSystem", "Atari ProSystem"},
		Short:   "7800",
	},
	{
		Name:    "Atari 5200",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Atari 5200 SuperSystem", "Atari 5200 Super System"},
		Short:   "5200",
	},
	{
		Name:    "Atari 2600",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1977,
		Aliases: []string{"Atari VCS", "Atari Video Computer System", "Atari 2600 VCS"},
		Short:   "2600",
	},
	{
		Name:    "Atari Lynx",
		Maker:   "Atari",
		Kind:    SystemKindHandheld,
		Year:    1989,
		Aliases: []string{"Lynx", "Atari Lynx II", "Epyx Handy"},
		Short:   "LYNX",
	},
	{
		Name:    "Atari ST",
		Maker:   "Atari",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"Atari ST/STE", "Atari STE", "Atari 520ST", "Atari 1040ST"},
		Short:   "ST",
	},
	{
		Name:    "Atari 8-bit",
		Maker:   "Atari",
		Kind:    SystemKindComputer,
		Year:    1979,
		Aliases: []string{"Atari 800", "Atari 400", "Atari XL", "Atari XE", "Atari 800XL", "Atari 130XE", "Atari XEGS", "Atari 8 bit", "Atari 8-bit family"},
		Short:   "A8",
	},
	// NEC.
	{
		Name:    "PC-FX",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"NEC PC-FX", "PCFX"},
		Short:   "PCFX",
	},
	{
		Name:    "SuperGrafx",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1989,
		Aliases: []string{"PC Engine SuperGrafx", "NEC SuperGrafx"},
		Short:   "SGFX",
	},
	{
		Name:    "PC Engine CD / TurboGrafx-CD",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1988,
		Aliases: []string{"PC Engine CD", "TurboGrafx-CD", "TurboGrafx CD", "Turbografx-16/PC Engine CD", "CD-ROM2", "PC Engine CD-ROM2", "Super CD-ROM2", "PC Engine Super CD-ROM2", "Arcade CD-ROM2", "PC Engine Duo", "TurboDuo", "TurboGrafx Duo", "PC Engine Duo-R", "PC Engine Duo-RX"},
		Short:   "PCECD",
	},
	{
		Name:    "PC Engine / TurboGrafx-16",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1987,
		Aliases: []string{"PC Engine", "TurboGrafx-16", "TurboGrafx 16", "TurboGrafx16", "TG16", "TG-16", "TurboGrafx-16/PC Engine", "PC Engine / TurboGrafx 16", "PC Engine CoreGrafx", "CoreGrafx", "PC Engine GT", "TurboExpress", "PC Engine LT"},
		Short:   "PCE",
	},
	{
		Name:    "PC-98",
		Maker:   "NEC",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"PC-9800", "PC-9800 Series", "NEC PC-98", "NEC PC-9800", "PC-9801", "PC98", "PC-9821", "NEC PC-9801"},
		Short:   "PC98",
	},
	{
		Name:    "PC-88",
		Maker:   "NEC",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"PC-8800", "PC-8800 Series", "NEC PC-88", "NEC PC-8801", "PC-8801", "PC88", "PC-8801 Series"},
		Short:   "PC88",
	},
	// SNK.
	{
		Name:    "Neo Geo CD",
		Maker:   "SNK",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"NeoGeo CD", "Neo-Geo CD", "SNK Neo Geo CD", "NGCD"},
		Short:   "NGCD",
	},
	{
		Name:    "Neo Geo AES",
		Maker:   "SNK",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"Neo Geo", "NeoGeo", "Neo-Geo", "Neo Geo MVS", "Neo-Geo AES", "Neo Geo Advanced Entertainment System", "SNK Neo Geo", "NGAES", "Neo Geo Multi Video System"},
		Short:   "NEO",
	},
	{
		Name:    "Neo Geo Pocket Color",
		Maker:   "SNK",
		Kind:    SystemKindHandheld,
		Year:    1999,
		Aliases: []string{"NGPC", "Neo-Geo Pocket Color", "NeoGeo Pocket Color", "SNK Neo Geo Pocket Color"},
		Short:   "NGPC",
	},
	{
		Name:    "Neo Geo Pocket",
		Maker:   "SNK",
		Kind:    SystemKindHandheld,
		Year:    1998,
		Aliases: []string{"NGP", "Neo-Geo Pocket", "NeoGeo Pocket", "SNK Neo Geo Pocket"},
		Short:   "NGP",
	},
	// Bandai.
	{
		Name:    "Playdia",
		Maker:   "Bandai",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Bandai Playdia"},
	},
	{
		Name:    "WonderSwan Color",
		Maker:   "Bandai",
		Kind:    SystemKindHandheld,
		Year:    2000,
		Aliases: []string{"WSC", "Wonder Swan Color", "Bandai WonderSwan Color", "SwanCrystal", "Swan Crystal"},
		Short:   "WSC",
	},
	{
		Name:    "WonderSwan",
		Maker:   "Bandai",
		Kind:    SystemKindHandheld,
		Year:    1999,
		Aliases: []string{"Wonder Swan", "Bandai WonderSwan"},
		Short:   "WS",
	},
	// Acorn.
	{
		Name:    "Acorn Archimedes",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1987,
		Aliases: []string{"Archimedes", "Acorn Archimedes A3000", "Acorn A3000", "Acorn A5000", "Acorn A500", "Acorn RISC PC", "RISC PC", "Acorn Risc PC", "Acorn A3020", "Acorn A310"},
	},
	{
		Name:    "Acorn Electron",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1983,
		Aliases: []string{},
		Short:   "ELEC",
	},
	{
		Name:    "BBC Micro",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"BBC Microcomputer System", "BBC Micro Model B", "Acorn BBC Micro", "BBC Model B", "BBC B", "BBC Master", "BBC Master Compact", "Acorn BBC Master", "BBC Microcomputer", "Acorn BBC Model B"},
		Short:   "BBC",
	},
	// Amstrad.
	{
		Name:    "Amstrad GX4000",
		Maker:   "Amstrad",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"GX4000", "Amstrad GX-4000", "Amstrad CPC Plus console"},
	},
	{
		Name:    "Amstrad CPC",
		Maker:   "Amstrad",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Amstrad CPC 464", "Amstrad CPC 6128", "Amstrad CPC 664", "CPC 464", "CPC 6128", "ACPC", "Schneider CPC", "Amstrad CPC+"},
		Short:   "CPC",
	},
	// Apple.
	{
		Name:    "Apple Pippin",
		Maker:   "Apple",
		Kind:    SystemKindHome,
		Year:    1996,
		Aliases: []string{"Pippin", "Bandai Pippin", "Bandai Pippin Atmark", "Pippin Atmark", "Apple Bandai Pippin"},
	},
	{
		Name:    "Mac (classic)",
		Maker:   "Apple",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Macintosh (classic)", "Classic Mac", "Mac OS Classic", "Classic Mac OS", "Macintosh 128K", "Mac 68k", "Macintosh Plus", "Mac OS 9", "System 7", "Macintosh Classic"},
		Short:   "MAC",
	},
	{
		Name:    "Apple II",
		Maker:   "Apple",
		Kind:    SystemKindComputer,
		Year:    1977,
		Aliases: []string{"Apple ][", "Apple 2", "Apple II+", "Apple IIe", "Apple IIc", "Apple IIGS", "Apple IIe Platinum", "Apple 2e", "Apple ][+"},
		Short:   "A2",
	},
	// Blaze.
	{
		Name:    "Evercade",
		Maker:   "Blaze",
		Kind:    SystemKindHandheld,
		Year:    2020,
		Aliases: []string{"Evercade VS", "Evercade EXP", "Blaze Evercade", "Blaze Evercade VS", "Blaze Evercade EXP", "Evercade VS/EXP", "Evercade Handheld", "Evercade Home Console"},
		Short:   "EVER",
	},
	// Coleco.
	{
		Name:    "ColecoVision",
		Maker:   "Coleco",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Coleco Vision", "Coleco ColecoVision"},
		Short:   "CV",
	},
	// Commodore.
	{
		Name:    "Amiga CD32",
		Maker:   "Commodore",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"CD32", "Commodore CD32", "Commodore Amiga CD32", "Amiga CD 32"},
		Short:   "CD32",
	},
	{
		Name:    "Commodore CDTV",
		Maker:   "Commodore",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"CDTV", "Amiga CDTV", "Commodore Dynamic Total Vision"},
		Short:   "CDTV",
	},
	{
		Name:    "Amiga",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"Commodore Amiga", "Amiga 500", "Amiga 1200", "Amiga 2000", "Amiga 600", "Amiga 3000", "Amiga 4000", "AmigaOS", "Commodore Amiga 500", "A500", "A1200"},
		Short:   "AMIGA",
	},
	{
		Name:    "Commodore 16 / Plus/4",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Commodore 16", "Commodore Plus/4", "Commodore Plus 4", "Plus/4", "Commodore 116", "C16", "C116", "Plus4", "C+4"},
		Short:   "C16",
	},
	{
		Name:    "Commodore 64",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"C64", "Commodore C64", "C=64", "Commodore 64/128", "Commodore C64/128/MAX", "Commodore 128", "C128", "Commodore MAX", "Commodore MAX Machine", "Ultimax", "Commodore 64C", "C64C"},
		Short:   "C64",
	},
	{
		Name:    "VIC-20",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1980,
		Aliases: []string{"Commodore VIC-20", "VIC 20", "Commodore VIC 20", "VC-20", "VC20", "Commodore VC 20"},
		Short:   "VIC20",
	},
	{
		Name:    "Commodore PET",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1977,
		Aliases: []string{"Commodore 2001", "Commodore CBM"},
	},
	// Dragon Data.
	{
		Name:    "Dragon 32/64",
		Maker:   "Dragon Data",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Dragon 32", "Dragon 64", "Dragon Data Dragon 32", "Dragon Data Dragon 64", "Dragon 200"},
	},
	// Fairchild.
	{
		Name:    "Channel F",
		Maker:   "Fairchild",
		Kind:    SystemKindHome,
		Year:    1976,
		Aliases: []string{"Fairchild Channel F", "Fairchild Video Entertainment System", "Channel F System II", "Fairchild Channel F System II"},
		Short:   "CHF",
	},
	// Fujitsu.
	{
		Name:    "FM Towns",
		Maker:   "Fujitsu",
		Kind:    SystemKindComputer,
		Year:    1989,
		Aliases: []string{"FM Towns Marty", "FM-Towns", "Fujitsu FM Towns", "FM Towns II", "Fujitsu FM Towns Marty", "FM Towns Car Marty"},
		Short:   "FMT",
	},
	{
		Name:    "Fujitsu FM-7",
		Maker:   "Fujitsu",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"FM-7", "FM7", "FM-77", "Fujitsu FM77", "FM-8", "Fujitsu FM-8", "Fujitsu FM77AV", "FM77AV", "FM-77AV"},
	},
	// GCE.
	{
		Name:    "Vectrex",
		Maker:   "GCE",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"GCE Vectrex", "Milton Bradley Vectrex", "MB Vectrex"},
		Short:   "VEC",
	},
	// MSX.
	{
		Name:    "MSX2",
		Maker:   "MSX",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"MSX 2", "MSX2+", "MSX 2+", "MSX turboR", "MSX Turbo R"},
	},
	{
		Name:    "MSX",
		Maker:   "MSX",
		Kind:    SystemKindComputer,
		Year:    1983,
		Aliases: []string{"MSX1", "MSX 1", "MSX (1)", "MSX Computer"},
	},
	// Magnavox.
	{
		Name:    "Odyssey 2",
		Maker:   "Magnavox",
		Kind:    SystemKindHome,
		Year:    1978,
		Aliases: []string{"Magnavox Odyssey 2", "Magnavox Odyssey²", "Odyssey²", "Odyssey 2 / Videopac G7000", "Philips Videopac G7000", "Videopac G7000", "Videopac", "Philips Videopac", "Philips Videopac+ G7400", "Videopac+ G7400", "Videopac+", "Philips Odyssey"},
		Short:   "ODY2",
	},
	{
		Name:    "Magnavox Odyssey",
		Maker:   "Magnavox",
		Kind:    SystemKindHome,
		Year:    1972,
		Aliases: []string{"Magnavox Odyssey 1"},
	},
	// Mattel.
	{
		Name:    "HyperScan",
		Maker:   "Mattel",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"Mattel HyperScan"},
	},
	{
		Name:    "Intellivision",
		Maker:   "Mattel",
		Kind:    SystemKindHome,
		Year:    1979,
		Aliases: []string{"Mattel Intellivision", "INTV", "Intellivision II"},
		Short:   "INTV",
	},
	// Nokia.
	{
		Name:    "N-Gage",
		Maker:   "Nokia",
		Kind:    SystemKindHandheld,
		Year:    2003,
		Aliases: []string{"Nokia N-Gage", "N Gage", "N-Gage QD", "Nokia N-Gage QD", "NGage"},
		Short:   "NGAGE",
	},
	// Ouya.
	{
		Name:    "Ouya",
		Maker:   "Ouya",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"Ouya Console"},
		Short:   "OUYA",
	},
	// Panasonic.
	{
		Name:    "3DO",
		Maker:   "Panasonic",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"3DO Interactive Multiplayer", "Panasonic 3DO", "3DO Multiplayer", "Goldstar 3DO", "Panasonic REAL 3DO", "REAL 3DO"},
	},
	// Panic.
	{
		Name:    "Playdate",
		Maker:   "Panic",
		Kind:    SystemKindHandheld,
		Year:    2022,
		Aliases: []string{"Panic Playdate", "Playdate Console"},
		Short:   "PDATE",
	},
	// Philips.
	{
		Name:    "CD-i",
		Maker:   "Philips",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"Philips CD-i", "Philips CDI", "Compact Disc Interactive", "Magnavox CD-i", "Philips Compact Disc Interactive"},
		Short:   "CDI",
	},
	// Sharp.
	{
		Name:    "Sharp X68000",
		Maker:   "Sharp",
		Kind:    SystemKindComputer,
		Year:    1987,
		Aliases: []string{"X68000", "X68K", "Sharp X68K", "Sharp X68000 ACE", "Sharp X68000 Super", "Sharp X68000 XVI", "Sharp X68000 Compact", "Sharp X68030", "X68030"},
		Short:   "X68K",
	},
	{
		Name:    "Sharp X1",
		Maker:   "Sharp",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Sharp X1 Turbo", "X1 Turbo", "Sharp X1turbo", "Sharp X1twin", "X1twin", "Sharp X1C"},
	},
	// Sinclair.
	{
		Name:    "Sinclair QL",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"ICL One Per Desk", "One Per Desk", "Merlin Tonto", "Sinclair QL Computer"},
	},
	{
		Name:    "ZX Spectrum",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Sinclair ZX Spectrum", "ZX Spectrum+", "ZX Spectrum 48K", "ZX Spectrum 128K", "ZX Spectrum +2", "ZX Spectrum +3", "Timex Sinclair 2068", "Timex TS2068"},
		Short:   "ZX",
	},
	{
		Name:    "ZX81",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"Sinclair ZX81", "Timex Sinclair 1000", "Timex Sinclair 1500", "ZX 81"},
	},
	// Tiger.
	{
		Name:    "Game.com",
		Maker:   "Tiger",
		Kind:    SystemKindHandheld,
		Year:    1997,
		Aliases: []string{"Game com", "Tiger Game.com", "Tiger Game com", "Gamecom"},
		Short:   "GCOM",
	},
	{
		Name:    "R-Zone",
		Maker:   "Tiger",
		Kind:    SystemKindHandheld,
		Year:    1995,
		Aliases: []string{"Tiger R-Zone", "RZone", "R Zone"},
	},
	// Other.
	{
		Name:    "V.Smile",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    2004,
		Aliases: []string{"VSmile", "V Smile", "VTech V.Smile", "VTech V Smile", "V.Smile Pocket", "V.Smile Motion"},
	},
	{
		Name:    "Nuon",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    2000,
		Aliases: []string{"VM Labs NUON", "VM Labs Nuon", "Samsung Nuon"},
	},
	{
		Name:    "Casio Loopy",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1995,
		Aliases: []string{"Loopy", "Casio Loopy My Seal Computer", "Loopy My Seal Computer"},
	},
	{
		Name:    "LaserActive",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Pioneer LaserActive", "LaserActive (Pioneer)", "Pioneer LaserActive LD-ROM", "Mega LD", "LaserActive Mega LD", "LaserActive PC Engine Pack", "LD-ROM2"},
	},
	{
		Name:    "Super Cassette Vision",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1984,
		Aliases: []string{"Epoch Super Cassette Vision", "SCV", "Yeno Super Cassette Vision", "Yeno SCV"},
	},
	{
		Name:    "Emerson Arcadia 2001",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Arcadia 2001"},
	},
	{
		Name:    "Epoch Cassette Vision",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1981,
		Aliases: []string{"Cassette Vision"},
	},
	{
		Name:    "Interton VC 4000",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1978,
		Aliases: []string{"VC 4000", "VC4000", "Interton VC4000", "Interton Video Computer 4000", "Interton Video 2000"},
	},
	{
		Name:    "Bally Astrocade",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1977,
		Aliases: []string{"Astrocade", "Bally Arcade", "Bally Professional Arcade", "Astrovision Professional Arcade", "Bally Professional Arcade (Astrocade)"},
	},
	{
		Name:    "Gizmondo",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    2005,
		Aliases: []string{},
	},
	{
		Name:    "Tapwave Zodiac",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    2003,
		Aliases: []string{"Tapwave Zodiac 1", "Tapwave Zodiac 2"},
	},
	{
		Name:    "Mega Duck",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1993,
		Aliases: []string{"Cougar Boy", "Mega Duck / Cougar Boy", "Mega Duck/Cougar Boy"},
	},
	{
		Name:    "Watara Supervision",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1992,
		Aliases: []string{"Watara/QuickShot Supervision", "QuickShot Supervision", "Watara QuickShot Supervision", "Watara Super Vision"},
	},
	{
		Name:    "Gamate",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1990,
		Aliases: []string{"Bit Corporation Gamate", "Bit Corp Gamate"},
	},
	{
		Name:    "Microvision",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1979,
		Aliases: []string{"Milton Bradley Microvision", "MB Microvision"},
	},
	{
		Name:    "Thomson MO5",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"MO5", "Thomson M05", "Thomson MO5E", "Thomson TO7", "Thomson TO7/70", "Thomson TO8", "Thomson TO9", "Thomson MO6"},
	},
	{
		Name:    "Texas Instruments TI-99/4A",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"TI-99/4A", "TI-99", "TI 99/4A", "TI-99/4", "Texas Instruments TI-99", "TI99", "TI-99/4A Home Computer"},
	},
	{
		Name:    "TRS-80 Color Computer",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1980,
		Aliases: []string{"TRS-80 CoCo", "CoCo", "Tandy Color Computer", "Tandy CoCo", "Radio Shack Color Computer", "TRS-80 Color Computer 2", "TRS-80 Color Computer 3", "Color Computer 3", "CoCo 3"},
	},
```

### `pinnedSystemNames` (systems_test.go)

```go
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
```

### IGDB (`igdb/match.go`)

```go
var platformIDs = map[string][]int{
	game.SystemPC: {6, 14, 3},
	game.SystemPS5: {167},
	game.SystemPS4: {48},
	game.SystemPS3: {9},
	game.SystemPS2: {8},
	game.SystemPS1: {7},
	game.SystemPSVita: {46},
	game.SystemPSP: {38},
	game.SystemXboxSeries: {169},
	game.SystemXboxOne: {49},
	game.SystemXbox360: {12},
	game.SystemXbox: {11},
	"Switch 2": {508},
	game.SystemSwitch: {130},
	game.SystemWiiU: {41},
	game.SystemWii: {5},
	game.SystemGameCube: {21},
	"Nintendo 64DD": {416},
	game.SystemN64: {4},
	"SNES": {19, 58},
	"Famicom Disk System": {51},
	"NES": {18, 99},
	game.System3DS: {37},
	game.SystemDS: {20},
	"Game Boy Advance": {24},
	"Pokémon Mini": {166},
	"Game Boy Color": {22},
	"Virtual Boy": {87},
	game.SystemGameBoy: {33},
	"Game & Watch": {307},
	"Dreamcast": {23},
	"32X": {30},
	"Saturn": {32},
	"Pico": {339},
	"Mega-CD / Sega CD": {78},
	"Mega Drive / Genesis": {29},
	"Master System": {64},
	"SG-1000": {84},
	"Game Gear": {35},
	"Atari Jaguar CD": {410},
	"Atari Jaguar": {62},
	"Atari 7800": {60},
	"Atari 5200": {66},
	"Atari 2600": {59},
	"Atari Lynx": {61},
	"Atari ST": {63},
	"Atari 8-bit": {65},
	"PC-FX": {274},
	"SuperGrafx": {128},
	"PC Engine CD / TurboGrafx-CD": {150},
	"PC Engine / TurboGrafx-16": {86},
	"PC-98": {149},
	"PC-88": {125},
	"Neo Geo CD": {136},
	"Neo Geo AES": {80, 79},
	"Neo Geo Pocket Color": {120},
	"Neo Geo Pocket": {119},
	"Playdia": {308},
	"WonderSwan Color": {123},
	"WonderSwan": {57},
	"Acorn Archimedes": {116},
	"Acorn Electron": {134},
	"BBC Micro": {69},
	"Amstrad GX4000": {506},
	"Amstrad CPC": {25},
	"Apple Pippin": {476},
	"Apple II": {75},
	"Evercade": {309},
	"ColecoVision": {68},
	"Amiga CD32": {114},
	"Commodore CDTV": {158},
	"Amiga": {16},
	"Commodore 16 / Plus/4": {93, 94},
	"Commodore 64": {15},
	"VIC-20": {71},
	"Commodore PET": {90},
	"Dragon 32/64": {153},
	"Channel F": {127},
	"FM Towns": {118},
	"Fujitsu FM-7": {152},
	"Vectrex": {70},
	"MSX2": {53},
	"MSX": {27},
	"Odyssey 2": {133},
	"Magnavox Odyssey": {88},
	"HyperScan": {407},
	"Intellivision": {67},
	"N-Gage": {42},
	"Ouya": {72},
	"3DO": {50},
	"Playdate": {381},
	"CD-i": {117},
	"Sharp X68000": {121},
	"Sharp X1": {77},
	"Sinclair QL": {406},
	"ZX Spectrum": {26},
	"ZX81": {373},
	"Game.com": {379},
	"R-Zone": {475},
	"V.Smile": {439},
	"Nuon": {122},
	"Casio Loopy": {380},
	"LaserActive": {487},
	"Super Cassette Vision": {376},
	"Emerson Arcadia 2001": {473},
	"Epoch Cassette Vision": {375},
	"Interton VC 4000": {138},
	"Bally Astrocade": {91},
	"Gizmondo": {474},
	"Tapwave Zodiac": {44},
	"Mega Duck": {408},
	"Watara Supervision": {415},
	"Gamate": {378},
	"Microvision": {89},
	"Thomson MO5": {156},
	"Texas Instruments TI-99/4A": {129},
	"TRS-80 Color Computer": {151},
}

var unmappedSystems = []string{"Mac (classic)"}
```

### TheGamesDB (`thegamesdb/provider.go`)

```go
var platformNames = map[string][]string{
	game.SystemPC: {"PC"},
	game.SystemPS5: {"Sony Playstation 5"},
	game.SystemPS4: {"Sony Playstation 4"},
	game.SystemPS3: {"Sony Playstation 3"},
	game.SystemPS2: {"Sony Playstation 2"},
	game.SystemPS1: {"Sony Playstation"},
	game.SystemPSVita: {"Sony Playstation Vita"},
	game.SystemPSP: {"Sony Playstation Portable", "Sony PSP"},
	game.SystemXboxSeries: {"Microsoft Xbox Series X", "Microsoft Xbox Series X|S"},
	game.SystemXboxOne: {"Microsoft Xbox One"},
	game.SystemXbox360: {"Microsoft Xbox 360"},
	game.SystemXbox: {"Microsoft Xbox"},
	"Switch 2": {"Nintendo Switch 2"},
	game.SystemSwitch: {"Nintendo Switch"},
	game.SystemWiiU: {"Nintendo Wii U"},
	game.SystemWii: {"Nintendo Wii"},
	game.SystemGameCube: {"Nintendo GameCube"},
	game.SystemN64: {"Nintendo 64"},
	"SNES": {"Super Nintendo (SNES)"},
	"Famicom Disk System": {"Famicom Disk System"},
	"NES": {"Nintendo Entertainment System (NES)"},
	game.System3DS: {"Nintendo 3DS"},
	game.SystemDS: {"Nintendo DS"},
	"Game Boy Advance": {"Nintendo Game Boy Advance"},
	"Pokémon Mini": {"Nintendo Pokémon Mini"},
	"Game Boy Color": {"Nintendo Game Boy Color"},
	"Virtual Boy": {"Nintendo Virtual Boy"},
	game.SystemGameBoy: {"Nintendo Game Boy"},
	"Game & Watch": {"Game & Watch"},
	"Dreamcast": {"Sega Dreamcast"},
	"32X": {"Sega 32X"},
	"Saturn": {"Sega Saturn"},
	"Pico": {"Sega Pico"},
	"Mega-CD / Sega CD": {"Sega CD"},
	"Mega Drive / Genesis": {"Sega Genesis", "Sega Mega Drive"},
	"Master System": {"Sega Master System"},
	"SG-1000": {"SEGA SG-1000"},
	"Game Gear": {"Sega Game Gear"},
	"Atari Jaguar CD": {"Atari Jaguar CD"},
	"Atari Jaguar": {"Atari Jaguar"},
	"Atari 7800": {"Atari 7800"},
	"Atari 5200": {"Atari 5200"},
	"Atari 2600": {"Atari 2600"},
	"Atari Lynx": {"Atari Lynx"},
	"Atari ST": {"Atari ST"},
	"Atari 8-bit": {"Atari 800", "Atari XE"},
	"PC-FX": {"PC-FX"},
	"PC Engine CD / TurboGrafx-CD": {"TurboGrafx CD"},
	"PC Engine / TurboGrafx-16": {"TurboGrafx 16"},
	"PC-98": {"PC-98"},
	"PC-88": {"PC-88"},
	"Neo Geo CD": {"Neo Geo CD"},
	"Neo Geo AES": {"Neo Geo"},
	"Neo Geo Pocket Color": {"Neo Geo Pocket Color"},
	"Neo Geo Pocket": {"Neo Geo Pocket"},
	"Playdia": {"Playdia"},
	"WonderSwan Color": {"WonderSwan Color"},
	"WonderSwan": {"WonderSwan"},
	"Acorn Archimedes": {"Acorn Archimedes"},
	"Acorn Electron": {"Acorn Electron"},
	"BBC Micro": {"BBC Micro"},
	"Amstrad GX4000": {"Amstrad GX4000"},
	"Amstrad CPC": {"Amstrad CPC"},
	"Apple Pippin": {"Apple Pippin"},
	"Apple II": {"Apple II"},
	"Evercade": {"Evercade"},
	"ColecoVision": {"Colecovision"},
	"Amiga CD32": {"Amiga CD32"},
	"Amiga": {"Amiga"},
	"Commodore 16 / Plus/4": {"Commodore 16", "Commodore Plus/4"},
	"Commodore 64": {"Commodore 64", "Commodore 128"},
	"VIC-20": {"Commodore VIC-20"},
	"Commodore PET": {"Commodore PET"},
	"Dragon 32/64": {"Dragon 32/64"},
	"Channel F": {"Fairchild Channel F"},
	"FM Towns": {"FM Towns Marty"},
	"Fujitsu FM-7": {"Fujitsu FM-7"},
	"Vectrex": {"Vectrex"},
	"MSX": {"MSX"},
	"Odyssey 2": {"Magnavox Odyssey 2"},
	"Magnavox Odyssey": {"Magnavox Odyssey 1"},
	"HyperScan": {"HyperScan"},
	"Intellivision": {"Intellivision"},
	"N-Gage": {"N-Gage"},
	"Ouya": {"Ouya"},
	"3DO": {"3DO"},
	"Playdate": {"Playdate"},
	"CD-i": {"Philips CD-i"},
	"Sharp X68000": {"Sharp X68000"},
	"Sharp X1": {"Sharp X1"},
	"Sinclair QL": {"Sinclair QL"},
	"ZX Spectrum": {"Sinclair ZX Spectrum"},
	"ZX81": {"Sinclair ZX81"},
	"Game.com": {"Game.com"},
	"R-Zone": {"R-Zone"},
	"V.Smile": {"V.Smile"},
	"Nuon": {"Nuon"},
	"Casio Loopy": {"Casio Loopy"},
	"LaserActive": {"Pioneer LaserActive"},
	"Super Cassette Vision": {"Epoch Super Cassette Vision"},
	"Emerson Arcadia 2001": {"Emerson Arcadia 2001"},
	"Epoch Cassette Vision": {"Epoch Cassette Vision"},
	"Interton VC 4000": {"Interton VC 4000"},
	"Bally Astrocade": {"Bally Astrocade"},
	"Gizmondo": {"Gizmondo"},
	"Tapwave Zodiac": {"Tapwave Zodiac"},
	"Mega Duck": {"Mega Duck"},
	"Watara Supervision": {"Watara Supervision"},
	"Gamate": {"Gamate"},
	"Microvision": {"Milton Bradley Microvision"},
	"Thomson MO5": {"Thomson M05"},
	"Texas Instruments TI-99/4A": {"Texas Instruments TI-99/4A"},
	"TRS-80 Color Computer": {"TRS-80 Color Computer"},
}

var unmappedSystems = []string{"Nintendo 64DD", "SuperGrafx", "Mac (classic)", "Commodore CDTV", "MSX2"}
```

### Wikidata (`wikipedia/details.go`)

```go
var platformItems = map[string][]string{
	game.SystemPC: {"Q1406", "Q14116", "Q388", "Q16338"},
	game.SystemPS5: {"Q63184502"},
	game.SystemPS4: {"Q5014725"},
	game.SystemPS3: {"Q10683"},
	game.SystemPS2: {"Q10680"},
	game.SystemPS1: {"Q10677"},
	game.SystemPSVita: {"Q188808"},
	game.SystemPSP: {"Q170325"},
	game.SystemXboxSeries: {"Q98973368", "Q64513817", "Q98967383"},
	game.SystemXboxOne: {"Q13361286", "Q66712951"},
	game.SystemXbox360: {"Q48263"},
	game.SystemXbox: {"Q132020"},
	"Switch 2": {"Q122761124"},
	game.SystemSwitch: {"Q19610114"},
	game.SystemWiiU: {"Q56942"},
	game.SystemWii: {"Q8079"},
	game.SystemGameCube: {"Q182172"},
	"Nintendo 64DD": {"Q253044"},
	game.SystemN64: {"Q184839"},
	"SNES": {"Q183259"},
	"Famicom Disk System": {"Q135321"},
	"NES": {"Q172742", "Q491640"},
	game.System3DS: {"Q203597", "Q17679679"},
	game.SystemDS: {"Q170323", "Q637178"},
	"Game Boy Advance": {"Q188642"},
	"Pokémon Mini": {"Q1759168"},
	"Game Boy Color": {"Q203992"},
	"Virtual Boy": {"Q164651"},
	game.SystemGameBoy: {"Q186437"},
	"Game & Watch": {"Q215034"},
	"Dreamcast": {"Q184198"},
	"32X": {"Q1063978"},
	"Saturn": {"Q200912"},
	"Pico": {"Q1374482"},
	"Mega-CD / Sega CD": {"Q1047516"},
	"Mega Drive / Genesis": {"Q10676", "Q540868"},
	"Master System": {"Q209868"},
	"SG-1000": {"Q1136956"},
	"Game Gear": {"Q751719"},
	"Atari Jaguar CD": {"Q2739670"},
	"Atari Jaguar": {"Q650601"},
	"Atari 7800": {"Q753600"},
	"Atari 5200": {"Q743222"},
	"Atari 2600": {"Q206261"},
	"Atari Lynx": {"Q753657"},
	"Atari ST": {"Q627302"},
	"Atari 8-bit": {"Q249075"},
	"PC-FX": {"Q1136902"},
	"SuperGrafx": {"Q202375"},
	"PC Engine CD / TurboGrafx-CD": {"Q10854461"},
	"PC Engine / TurboGrafx-16": {"Q1057377"},
	"PC-98": {"Q183505"},
	"PC-88": {"Q1338888"},
	"Neo Geo CD": {"Q2703883"},
	"Neo Geo AES": {"Q64428080", "Q1054350"},
	"Neo Geo Pocket Color": {"Q1977455"},
	"Neo Geo Pocket": {"Q939881"},
	"Playdia": {"Q1198088"},
	"WonderSwan Color": {"Q1048035"},
	"WonderSwan": {"Q1065792"},
	"Acorn Archimedes": {"Q41357"},
	"Acorn Electron": {"Q342163"},
	"BBC Micro": {"Q749976"},
	"Amstrad GX4000": {"Q981935"},
	"Amstrad CPC": {"Q478829"},
	"Apple Pippin": {"Q621312"},
	"Apple II": {"Q3017175"},
	"Evercade": {"Q74408971", "Q106764042", "Q114381833"},
	"ColecoVision": {"Q1046862"},
	"Amiga CD32": {"Q695161"},
	"Commodore CDTV": {"Q955368"},
	"Amiga": {"Q100047"},
	"Commodore 16 / Plus/4": {"Q1115913", "Q868568"},
	"Commodore 64": {"Q99775", "Q1115919"},
	"VIC-20": {"Q918232"},
	"Commodore PET": {"Q946661"},
	"Dragon 32/64": {"Q57741692", "Q10268693"},
	"Channel F": {"Q1053294"},
	"FM Towns": {"Q531896", "Q923987"},
	"Fujitsu FM-7": {"Q2379925"},
	"Vectrex": {"Q767631"},
	"MSX2": {"Q11232203"},
	"MSX": {"Q853547"},
	"Odyssey 2": {"Q576932"},
	"Magnavox Odyssey": {"Q744987"},
	"HyperScan": {"Q3144093"},
	"Intellivision": {"Q1061441"},
	"N-Gage": {"Q336434"},
	"Ouya": {"Q1391641"},
	"3DO": {"Q229429"},
	"Playdate": {"Q63993524"},
	"CD-i": {"Q1023103"},
	"Sharp X68000": {"Q1758277"},
	"Sharp X1": {"Q2710884"},
	"Sinclair QL": {"Q602253"},
	"ZX Spectrum": {"Q23882"},
	"ZX81": {"Q263250"},
	"Game.com": {"Q2498396"},
	"R-Zone": {"Q7273280"},
	"V.Smile": {"Q1055215"},
	"Nuon": {"Q3111078"},
	"Casio Loopy": {"Q661952"},
	"LaserActive": {"Q3276319"},
	"Super Cassette Vision": {"Q8018077"},
	"Emerson Arcadia 2001": {"Q1196518"},
	"Epoch Cassette Vision": {"Q1140676"},
	"Interton VC 4000": {"Q302411"},
	"Bally Astrocade": {"Q805385"},
	"Gizmondo": {"Q909005"},
	"Tapwave Zodiac": {"Q523532"},
	"Mega Duck": {"Q733136"},
	"Watara Supervision": {"Q732683"},
	"Gamate": {"Q2395462"},
	"Microvision": {"Q1475303"},
	"Thomson MO5": {"Q2396081"},
	"Texas Instruments TI-99/4A": {"Q454390"},
	"TRS-80 Color Computer": {"Q1411846"},
}

var unmappedSystems = []string{"Mac (classic)"}
```

