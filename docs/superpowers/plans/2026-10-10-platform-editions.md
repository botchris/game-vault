# Platform Editions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A game owned on several systems gets one edition per system, each with its own cover, so the library can list a card per edition ("By platform") or per game ("By game") without misrepresenting any copy.

**Architecture:** Every copy gets an effective system (the user's override, else the source's, else `game.SystemOf(platform)`). Editions are derived from the copies; the game stores only the covers chosen per system and an optional main system, in game documents of version 3 (version-2 documents are converted when read). The media service resolves, caches and serves covers per (game, system); each cover provider's `Applies` decides by system. The API exposes the editions and two new requests; the web groups games into editions with pure functions in `web/src/lib/editions.ts` and shows editions in the library and the game sheet.

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite (JSON documents); React 19 + TypeScript, i18next; Node test runner.

**Spec:** `docs/superpowers/specs/2026-10-10-platform-editions-design.md`

## Global Constraints

- Everything compiles, tests and generates in the toolchain container through Task: `task lint`, `task test`, `task generate`, `task lint:fix`. One Go package: `task go -- test ./internal/domain/game/ -run TestSystemOf -v`. Node tests: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/editions.test.mjs` (the tag is the first 12 hex digits of the SHA-256 of `build/toolchain.Dockerfile`; recompute it with `shasum -a 256 build/toolchain.Dockerfile | cut -c1-12` if that file changed). Never use host Go, Node or linters.
- Load the `write-go` skill before writing Go: a doc comment on every exported identifier, one struct field per line, a blank line between documented members, GIVEN/WHEN/THEN tests with `context.WithTimeout(t.Context(), 10*time.Second)` and testify. Test code below is written compactly; run `task lint:fix` (it puts each struct field on its own line and fixes cuddling) before `task lint`.
- The repository is public: invented test data only (titles such as "Halo 3", "Astro Bot"), no personal data or library counts in code, comments, docs, commits or the PR.
- English in the repository; every UI string through `t()` with keys in `web/src/i18n/locales/en.json` and `es.json`; `task i18n` passes.
- Systems: PC stores and launchers (Steam, Epic Games, GOG, EA App, Ubisoft Connect, Battle.net, itch.io, Amazon Games, Rockstar, Riot, "Battlestate (Tarkov)" and "PC") become `PC`; console names (PS5, PS4, PS3, PS2, PS1, PSP, PS Vita, Xbox Series, Xbox One, Xbox 360, Xbox, Switch, Wii U, Wii, GameCube, N64, 3DS, DS, Game Boy) are their own system; "PlayStation Store" → PS4, "Nintendo eShop" → Switch, "Microsoft Store / Xbox" → PC; an unknown platform is its own system (canonical casing, trimmed); an empty platform is `Other`.
- Effective system order: 1. the user's override; 2. the source's system; 3. `SystemOf(platform)`. Scans never touch the override.
- Default main edition: 1. an edition with a physical copy; 2. then the most copies; 3. then alphabetical by system.
- Game documents become `"v": 3`: `covers` (`{"PS3": {"url": "…"}}` or `{"photo": "…"}`) and `mainSystem` replace `coverUrl` and `coverPhoto`; copies gain `system` and `sourceSystem`; empty values are omitted. Document field names never change once released.
- No identifier changes: ExternalIDs, PhotoIDs and game ids stay as they are. Opening an older database writes the usual pre-migration backup.
- Covers are served at `GET /media/covers/{gameId}/{system}` (URL-escaped system) and `GET /media/covers/{gameId}` (main edition); a miss is a 404 with `Cache-Control: no-store`.
- Every external service is a plugin, and each cover provider's `Applies` decides alone, without network calls. Domain packages import nothing from application or adapters.
- Library view selector "By platform | By game", remembered in `localStorage` wrapped in try/catch; default "By platform".
- Web files that Node tests import (`web/src/lib/editions.ts`, `web/src/lib/fields.ts`) use structural types only: no generated protobuf code, no `lib/model.ts` (TypeScript enums), imports with the `.ts` extension.
- Safety: never touch `config/` or the server on port 8080; check the UI with `task test-server` (port 8093, a copy of the config, `-no-unattended`), and on that copy never scan sources whose credentials rotate (Ubisoft, Epic, GOG, Xbox, PlayStation, Battle.net, EA).
- No version is tagged.

## Review Focus

- A game whose only copies are PC store copies (Steam, a Humble Steam key, GOG) must still have exactly one edition, PC, whose cover comes from Steam as today, at both cover addresses. Tests: Task 2 (`TestEditions`, first GIVEN) and Task 3 (`TestCovers_pcOnlyGame`).
- A version-2 document whose `coverPhoto` is on a copy whose system is not the default main edition, together with a `coverUrl`, must keep the photo on its own edition, as the main one (the "by game" view looks as before), and put the URL on the default main edition. Tests: Task 2 (`TestAdoptGameCover`, third GIVEN; `TestDocuments_editions`, second GIVEN).
- An override that moves the only copy of the edition the user chose as main must clear the main choice and drop that edition's chosen cover, leaving a valid main edition. Test: Task 2 (`TestEditionCovers`, "GIVEN the user made the PS3 edition … the main one").
- Systems with spaces and slashes ("Xbox 360", a free-text "Xbox 360/S Slim") must be served at their URL-escaped cover address, end to end, and the web must build that address escaped. Tests: Task 3 (`TestEditionCoverFiles`, gamedata file names), Task 5 (`TestEditions_endToEnd`, the override step; `coverPath` Node test).
- When every edition's cover answers 404, the card must show the title's initials and stop requesting images (no retry loop), and a borrowed cover must name the system it belongs to. Test: Task 7 (`coverOrder`/`nextCover` Node tests); behaviour in Task 8's `Cover` component.

## Rulings made while planning (where the spec is silent or ambiguous)

- **A version-2 cover photo makes its edition the main one.** The spec stores `mainSystem` only for `coverUrl`; without it, a game whose v2 cover was a photo on a non-default edition would show another box in the "by game" view. `AdoptGameCover` stores the photo's system as `mainSystem`; the URL goes to the default main edition when that is another edition, and is dropped when the photo already took it.
- **A game without copies has no editions.** Its main edition is the zero `Edition` (system `""`); `GET /media/covers/{gameId}` resolves it with a query that names no system, which every provider may answer (store providers included, as today). In "By platform" it is one item with system `""`. A version-2 `coverUrl` on a game without copies is dropped (there is no edition to hold it).
- **`CoverQuery.ForPC()`** is true for system `PC` and for queries that name no system (a game without copies, a title search, the add-on base lookup): store providers apply only then. Metadata (game sheets) stays game-level: `fetchDetails` uses `QueryFor(g, "")`.
- **The cover cached before editions (`cover.jpg`) is adopted by the main edition** on its first request instead of being downloaded again for the whole library; invalidating any edition also deletes that legacy file.
- **The pre-migration backup needs a migration**: game documents are converted on read, so `0010_platform_editions.sql` is a no-op statement whose only purpose is the backup before version-3 documents start being written.
- **Invalidation is narrow:** an edition's cached cover is dropped when its fingerprint changes (chosen cover, the platforms of its copies, physical or not, and the store links), not on every copy update (a key's status).
- **System overrides are validated and named like `SystemOf`:** trimmed, at most 40 characters, at least one letter or digit, no control characters; "ps4" → "PS4", "steam" → "PC". The copy form and the CSV store no override when the value equals the automatic one.
- **`(Copy).System()` shadows the promoted field `CopyDetails.System`.** Code reads and writes the override as `c.CopyDetails.System`.
- **No hash route for the sheet.** The app has no `#/game/<id>` route today; the sheet opens on the clicked edition through state (`GameDetail`'s `system` prop), and without one on the main edition.
- **"By game" shows system badges** (one per edition, owned or pending key), and clicking one filters by System.
- **The copy form's automatic system** is the source's when the copy has no override and its effective system differs from its platform's; otherwise `systemOf(platform)` (the API does not expose the source's system).
- **`ListCoverCandidates` with an empty `system`** means the main edition. `CreateGameRequest.cover_url` and `UpdateGameRequest.cover_url` are removed with `Game.cover_url` (the web is the only client).
- **Amazon has no cover provider** (no portrait art), so it needs no change; the `example` plugin's cover provider gets the same PC rule as the real stores.
- **Quick filters (pending, expiring, redundant) are copy-level**: in "By platform" they keep an edition when one of its copies matches.

## Files

| File | Responsibility |
|---|---|
| `internal/domain/game/system.go` (new) | `SystemOf`, `SystemPC`, `SystemOther`, `normalizeSystem` |
| `internal/domain/game/edition.go` (new) | `EditionCover`, `Edition`, `ParseCoverURL`, editions, main rule, covers, reconcile, v2 adoption, fingerprints |
| `internal/domain/game/{game,copy,consolidate,photo}.go` | override and source system on copies; covers replace `coverURL`/`coverPhoto` |
| `internal/adapters/outbound/sqlite/docs.go`, `migrations/0010_platform_editions.sql` | v3 documents, v2 conversion, backup trigger |
| `internal/application/media/{service,addon,details}.go` | per-edition queries, resolution, cache, candidates, invalidation |
| `internal/adapters/outbound/gamedata/store.go` | cover files per system, legacy adoption |
| `internal/adapters/outbound/{steam,gog,epic,eaapp,battlenet,ubisoft,xbox,example,thegamesdb}` | `Applies` by system; TheGamesDB searches the edition's system |
| `internal/application/{catalog,sync}` | `SetEditionCover`, `SetMainSystem`, per-edition invalidation |
| `internal/adapters/outbound/playstation/provider.go` | source system PS4/PS5 |
| `proto/gamevault/v1/{game,provider}.proto`, `internal/adapters/inbound/rpc/*` | editions in the API, new requests, cover endpoints |
| `internal/adapters/outbound/csvfile/codec.go` | `system` column |
| `web/src/lib/editions.ts` (new), `web/tests/editions.test.mjs` (new) | grouping, filters, counts, cover order |
| `web/src/features/library/*`, `web/src/components/{Cover,PlatformBadge}.tsx`, `web/src/api/client.ts`, `web/src/lib/model.ts` | library views, sheet, cover picker, copy form |
| `docs/technical.md`, `README.md`, `.claude/memory/{data-model,provider-chains}.md`, `.claude/MEMORY.md`, `docs/plugins.md`, `.claude/docs/integrations.md` | documentation |

---

### Task 1: The system of each copy (domain + stored fields)

**Files:**
- Create: `internal/domain/game/system.go`, `internal/domain/game/system_test.go`
- Modify: `internal/domain/game/copy.go`, `internal/domain/game/consolidate.go`, `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`

**Interfaces:**
- Produces:
  - `game.SystemPC = "PC"`, `game.SystemOther = "Other"`
  - `game.SystemOf(platform string) string`
  - `game.normalizeSystem(s string) (string, error)` (package-private, used by Tasks 2 and 6 through `CopyDetails.normalize` and the consolidator)
  - `CopyDetails.System string` (the user's override; empty = automatic)
  - `Copy.SourceSystem string`
  - `(Copy).System() string` (effective system)
  - `ImportedCopy.System string`
  - copy document fields `system`, `sourceSystem`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing test** (`internal/domain/game/system_test.go`)

```go
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
		"PS5": "PS5", "PS4": "PS4", "PS3": "PS3", "PS2": "PS2", "PS1": "PS1", "PSP": "PSP", "PS Vita": "PS Vita",
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
		c, err := g.AddCopy(CopyDetails{Kind: KindLibrary, Platform: "PlayStation Store"}, t0)
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
				c, err := g.AddCopy(CopyDetails{Kind: KindPhysical, System: typed}, t0)
				require.NoError(t, err, typed)
				assert.Equal(t, want, c.CopyDetails.System, typed)
			}
		})

		t.Run("THEN names without a letter or digit, too long or with control characters are refused", func(t *testing.T) {
			for _, typed := range []string{"..", "--", strings.Repeat("x", 41), "PS\x004"} {
				_, err := g.AddCopy(CopyDetails{Kind: KindPhysical, System: typed}, t0)

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
			Details:    CopyDetails{Kind: KindLibrary, Platform: "PlayStation Store", Status: StatusOwned},
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
```

- [ ] **Step 3: Run it to verify it fails**

Run: `task go -- test ./internal/domain/game/ -run 'TestSystemOf|TestCopySystem|TestConsolidator_systems'`
Expected: FAIL to compile (`undefined: SystemOf`, `unknown field System`).

- [ ] **Step 4: Write `internal/domain/game/system.go`**

```go
package game

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Systems a copy can be played on that are not console names (consoles are their own system).
const (
	// SystemPC is the system of every PC store and launcher.
	SystemPC = "PC"

	// SystemOther is the system of a copy without a platform.
	SystemOther = "Other"
)

// maxSystemLen is the longest system name, in characters.
const maxSystemLen = 40

// pcPlatforms are the stores and launchers whose games are played on a PC.
var pcPlatforms = []string{
	"Steam", "Epic Games", "GOG", "EA App", "Ubisoft Connect", "Battle.net", "itch.io", "Amazon Games",
	"Rockstar", "Riot", "Battlestate (Tarkov)", SystemPC,
}

// storeSystems maps the stores that sell games for several systems to the most likely one.
var storeSystems = map[string]string{
	"PlayStation Store":      "PS4",
	"Nintendo eShop":         "Switch",
	"Microsoft Store / Xbox": SystemPC,
}

// SystemOf returns the system a copy on this platform is played on: PC for PC stores and
// launchers, the console itself for a console, the most likely console for a console's store, the
// platform itself (trimmed) when Game Vault does not know it, and Other when there is none.
func SystemOf(platform string) string {
	p := CanonicalPlatform(platform) // also fixes the casing of console names
	if p == "" {
		return SystemOther
	}

	if slices.Contains(pcPlatforms, p) {
		return SystemPC
	}

	if s, ok := storeSystems[p]; ok {
		return s
	}

	return p
}

// normalizeSystem checks a system named by the user or a source and returns it the way SystemOf
// names it ("ps4" → "PS4", "Steam" → "PC"). Empty stays empty: automatic.
func normalizeSystem(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	if utf8.RuneCountInString(s) > maxSystemLen {
		return "", invalid("a system name has at most %d characters", maxSystemLen)
	}

	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", invalid("system %q contains control characters", s)
	}

	if !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return "", invalid("system %q needs at least one letter or digit", s)
	}

	return SystemOf(s), nil
}
```

- [ ] **Step 5: Wire it into copies** (`copy.go`)
  - Add to `CopyDetails`, as its last field (after `Notes`):

    ```go
    	Notes      string

    	// System is the system the user says the copy is played on; empty: automatic (see Copy.System).
    	System string
    ```
  - In `CopyDetails.normalize`, after `d.Platform = CanonicalPlatform(d.Platform)`:

    ```go
    	system, err := normalizeSystem(d.System)
    	if err != nil {
    		return d, err
    	}

    	d.System = system
    ```
  - Add to `Copy`, after `ExternalID`:

    ```go
    	ExternalID string // stable identifier inside the source

    	// SourceSystem is the system the source says the copy is played on; empty when it does not say.
    	SourceSystem string
    	CreatedAt    time.Time
    ```
  - Add the method (after `IsPendingKey`). `Copy.System()` shadows the promoted field `CopyDetails.System`, so the override is always written `c.CopyDetails.System`:

    ```go
    // System returns the system the copy is played on: the user's choice, else the one its source
    // gave, else the one its platform implies (SystemOf).
    func (c Copy) System() string {
    	switch {
    	case c.CopyDetails.System != "":
    		return c.CopyDetails.System
    	case c.SourceSystem != "":
    		return c.SourceSystem
    	}

    	return SystemOf(c.Platform)
    }
    ```
  - In `applyImport`, next to `set(&c.Notes, in.Notes)`, add `set(&c.CopyDetails.System, in.System)`: only an import that names an override (a CSV row) sets it; sources never fill `Details.System`, so scans never touch it.

- [ ] **Step 6: The source's system in the consolidator** (`consolidate.go`)
  - Add to `ImportedCopy`, as its last field:

    ```go
    	Details CopyDetails

    	// System is the exact system the copy is played on, when the source knows it (PS4 or PS5 for a
    	// PlayStation Store game). It wins over the one the platform implies, never over the user's.
    	System string
    ```
  - In `Apply`, right after the `details, err := in.Details.normalize()` block:

    ```go
    		sourceSystem, err := normalizeSystem(in.System)
    		if err != nil {
    			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
    			sourceSystem = ""
    		}
    ```
  - In the existing-copy branch, after `changed := cp.applyImport(details)`:

    ```go
    			if cp.SourceSystem != sourceSystem {
    				cp.SourceSystem = sourceSystem
    				changed = true
    			}
    ```
  - In the new-copy branch, after `g.addCopy(...)` succeeds: `g.copies[len(g.copies)-1].SourceSystem = sourceSystem`.

- [ ] **Step 7: Run the domain tests**

Run: `task go -- test ./internal/domain/game/`
Expected: `ok`.

- [ ] **Step 8: Failing storage test** (append to `internal/adapters/outbound/sqlite/docs_test.go`)

```go
func TestDocuments_systems(t *testing.T) {
	t.Run("GIVEN a copy with the user's system and its source's", func(t *testing.T) {
		g := game.Rehydrate("g1", game.Info{Title: "Astro Bot"}, []game.Copy{{
			ID:           "c1",
			CopyDetails:  game.CopyDetails{Kind: game.KindLibrary, Platform: "PlayStation Store", Status: game.StatusOwned, System: "PS4"},
			SourceSystem: "PS5",
			CreatedAt:    docTime,
			UpdatedAt:    docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN both come back", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"system":"PS4"`)
				assert.Contains(t, raw, `"sourceSystem":"PS5"`)
			})

			t.Run("AND a copy with neither writes neither", func(t *testing.T) {
				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"system"`)
				assert.NotContains(t, plain, `"sourceSystem"`)
			})
		})
	})
}
```

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments_systems`
Expected: FAIL (`"system":"PS4"` not in the document).

- [ ] **Step 9: Store them** (`docs.go`). Adding fields keeps the version (the bump to 3 is Task 2).
  - Add to `copyDoc`, after `Notes`: `System string \`json:"system,omitempty"\`` and `SourceSystem string \`json:"sourceSystem,omitempty"\``.
  - In `encodeGame`'s copy literal: `System: c.CopyDetails.System,` and `SourceSystem: c.SourceSystem,`.
  - In `decodeGame`: `System: c.System,` inside `CopyDetails`, and `SourceSystem: c.SourceSystem,` on the copy.

Run the test. Expected: PASS.

- [ ] **Step 10: Lint, test, commit**

Run: `task lint:fix`, then `task lint` (expected `0 issues.`) and `task test` (expected: green).

```bash
git add internal/domain/game internal/adapters/outbound/sqlite
git commit -m "Copies: the system they are played on (derived, from the source, or the user's)"
```

---
### Task 2: Editions in the domain and in the game documents (v3)

The game's single cover (`coverURL`, `coverPhoto`) is replaced by covers chosen per system and an optional main system. Storage moves to version-3 documents in the same task, since `game.Info` loses its cover fields. Callers get the smallest change that keeps them working; three of them are **bridges** that later tasks replace, each marked with a `// Bridge (Task N):` comment so they are easy to find: the media service resolves the main edition's chosen cover as the game's (Task 3), catalog and sync drop the whole game's cached cover when its chosen covers change (Task 4), and the RPC handlers map `cover_url` / `cover_photo_id` / `SetCoverPhoto` onto the main edition (Task 5).

**Files:**
- Create: `internal/domain/game/edition.go`, `internal/domain/game/edition_test.go`, `internal/adapters/outbound/sqlite/migrations/0010_platform_editions.sql`
- Modify: `internal/domain/game/game.go`, `internal/domain/game/photo.go`, `internal/domain/game/consolidate.go`, `internal/domain/game/photo_test.go`, `internal/domain/game/game_test.go`, `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`, `internal/adapters/outbound/sqlite/migrate_test.go`, `internal/application/catalog/service.go`, `internal/application/catalog/scanned.go`, `internal/application/catalog/service_test.go`, `internal/application/catalog/scanned_test.go`, `internal/application/sync/service.go`, `internal/application/sync/exclusions.go`, `internal/application/sync/covers_test.go`, `internal/application/media/service.go`, `internal/adapters/inbound/rpc/mapper.go`, `internal/adapters/inbound/rpc/game_handler.go`

**Interfaces:**
- Consumes: Task 1 `(Copy).System()`, `SystemPC`.
- Produces:
  - `game.EditionCover{URL string; Photo PhotoID}` with `IsZero() bool`
  - `game.Edition{System string; Cover EditionCover; Main bool}`
  - `game.ParseCoverURL(s string) (string, error)`
  - `(*Game).Systems() []string`, `Editions() []Edition`, `Edition(system string) (Edition, bool)`, `MainEdition() Edition`, `MainSystem() string`, `Covers() map[string]EditionCover`
  - `(*Game).SetEditionCover(system string, cover EditionCover, now time.Time) error`
  - `(*Game).SetMainSystem(system string, now time.Time) error`
  - `(*Game).RestoreEditions(covers map[string]EditionCover, mainSystem string)` and `(*Game).AdoptGameCover(coverURL string, photo PhotoID)` (repositories only)
  - `(*Game).CoverFingerprints() map[string]string` and `game.ChangedSystems(before, after map[string]string) []string`
  - `(*Game).UpdateInfo(i Info, now) (linksChanged bool, err error)`; `Info` has no `CoverURL` / `CoverPhoto` any more
  - `catalog.(*Service).SetEditionCover(ctx, id game.ID, system string, cover game.EditionCover) (*game.Game, error)`
  - `catalog.(*Service).SetMainSystem(ctx, id game.ID, system string) (*game.Game, error)`
  - `catalog.(*Service).SetCoverPhoto` is removed
  - document version 3: `covers`, `mainSystem`

- [ ] **Step 1: Write the failing domain test** (`internal/domain/game/edition_test.go`)

```go
package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gameWith returns a game titled "Halo 3" with one copy per details.
func gameWith(t *testing.T, copies ...CopyDetails) *Game {
	t.Helper()

	g, err := New("Halo 3", t0)
	require.NoError(t, err)

	for _, d := range copies {
		_, err := g.AddCopy(d, t0)
		require.NoError(t, err)
	}

	return g
}

func on(kind Kind, platform string) CopyDetails {
	return CopyDetails{Kind: kind, Platform: platform}
}

func systemsOf(editions []Edition) []string {
	out := make([]string, 0, len(editions))
	for _, e := range editions {
		out = append(out, e.System)
	}

	return out
}

func TestEditions(t *testing.T) {
	t.Run("GIVEN a game owned only in PC stores (Steam, a Humble Steam key, GOG)", func(t *testing.T) {
		g := gameWith(t, on(KindLibrary, "Steam"), on(KindKey, "Steam"), on(KindLibrary, "GOG"))

		t.Run("THEN it has exactly one edition, PC, and it is the main one", func(t *testing.T) {
			assert.Equal(t, []Edition{{System: SystemPC, Main: true}}, g.Editions())
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})
	})

	t.Run("GIVEN copies on PC, PS3 (a disc) and Xbox 360 (a key)", func(t *testing.T) {
		g := gameWith(t, on(KindLibrary, "Steam"), on(KindLibrary, "GOG"), on(KindKey, "Xbox 360"), on(KindPhysical, "PS3"))

		t.Run("THEN the edition with a disc is the main one, first, then the rest by system", func(t *testing.T) {
			assert.Equal(t, []string{"PS3", "PC", "Xbox 360"}, systemsOf(g.Editions()))
			assert.True(t, g.Editions()[0].Main)
			assert.False(t, g.Editions()[1].Main)
		})

		t.Run("WHEN the user makes Xbox 360 the main edition", func(t *testing.T) {
			require.NoError(t, g.SetMainSystem("Xbox 360", t0))

			t.Run("THEN it comes first", func(t *testing.T) {
				assert.Equal(t, []string{"Xbox 360", "PC", "PS3"}, systemsOf(g.Editions()))
				assert.Equal(t, "Xbox 360", g.MainSystem())
			})

			t.Run("AND an empty choice goes back to the default rule", func(t *testing.T) {
				require.NoError(t, g.SetMainSystem("", t0))
				assert.Equal(t, "PS3", g.MainEdition().System)
			})
		})

		t.Run("WHEN a system the game has no copy on is chosen THEN it is refused, naming it", func(t *testing.T) {
			err := g.SetMainSystem("Wii", t0)

			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Wii")
		})
	})

	t.Run("GIVEN no physical copy", func(t *testing.T) {
		t.Run("THEN the edition with the most copies is the main one", func(t *testing.T) {
			g := gameWith(t, on(KindKey, "Switch"), on(KindLibrary, "Steam"), on(KindKey, "Steam"))
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})

		t.Run("AND on a tie, the first by system", func(t *testing.T) {
			g := gameWith(t, on(KindKey, "Switch"), on(KindLibrary, "Steam"))
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})
	})

	t.Run("GIVEN two editions with discs THEN the one with more copies is the main one", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"), on(KindKey, "Xbox 360"))
		assert.Equal(t, "Xbox 360", g.MainEdition().System)
	})

	t.Run("GIVEN a game without copies THEN it has no editions", func(t *testing.T) {
		g := gameWith(t)
		assert.Empty(t, g.Editions())
		assert.Equal(t, Edition{}, g.MainEdition())
	})
}

func TestEditionCovers(t *testing.T) {
	t.Run("GIVEN a PS3 disc with a photo and an Xbox 360 disc", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"))
		ps3 := g.Copies()[0].ID
		_, err := g.AddPhotos(ps3, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN the PS3 photo is chosen for the Xbox 360 edition THEN it is refused, naming the system", func(t *testing.T) {
			err := g.SetEditionCover("Xbox 360", EditionCover{Photo: pid(1)}, t0)

			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Xbox 360")
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming it", func(t *testing.T) {
			err := g.SetEditionCover("Wii", EditionCover{URL: "https://example.test/wii.jpg"}, t0)

			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Wii")
		})

		t.Run("WHEN a URL that is not http(s) is chosen THEN it is refused", func(t *testing.T) {
			var ve *ValidationError
			assert.ErrorAs(t, g.SetEditionCover("PS3", EditionCover{URL: "javascript:alert(1)"}, t0), &ve)
		})

		t.Run("WHEN each edition gets its own cover", func(t *testing.T) {
			require.NoError(t, g.SetEditionCover("PS3", EditionCover{Photo: pid(1)}, t0))
			require.NoError(t, g.SetEditionCover("Xbox 360", EditionCover{URL: " https://example.test/x360.jpg "}, t0))

			t.Run("THEN each edition shows its own", func(t *testing.T) {
				p, _ := g.Edition("PS3")
				x, _ := g.Edition("Xbox 360")
				assert.Equal(t, EditionCover{Photo: pid(1)}, p.Cover)
				assert.Equal(t, EditionCover{URL: "https://example.test/x360.jpg"}, x.Cover)
			})

			t.Run("AND a zero cover lets the providers choose again", func(t *testing.T) {
				require.NoError(t, g.SetEditionCover("Xbox 360", EditionCover{}, t0))

				x, _ := g.Edition("Xbox 360")
				assert.True(t, x.Cover.IsZero())
			})
		})

		t.Run("WHEN the user moves the PS3 disc to PS4", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "PS4"
			_, err := g.UpdateCopy(ps3, d, t0)
			require.NoError(t, err)

			t.Run("THEN the PS3 edition and its photo cover are gone, and the photo is not the PS4 cover", func(t *testing.T) {
				assert.Equal(t, []string{"PS4", "Xbox 360"}, systemsOf(g.Editions()))
				assert.Empty(t, g.Covers())
			})
		})
	})

	t.Run("GIVEN the user made the PS3 edition, with a chosen cover, the main one", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindLibrary, "Steam"), on(KindLibrary, "GOG"))
		require.NoError(t, g.SetEditionCover("PS3", EditionCover{URL: "https://example.test/ps3.jpg"}, t0))
		require.NoError(t, g.SetMainSystem("PS3", t0))

		t.Run("WHEN an override moves its only copy to PC", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "PC"
			_, err := g.UpdateCopy(g.Copies()[0].ID, d, t0)
			require.NoError(t, err)

			t.Run("THEN the main choice is cleared, its cover dropped, and PC is the main edition", func(t *testing.T) {
				assert.Empty(t, g.MainSystem())
				assert.Empty(t, g.Covers())
				assert.Equal(t, []Edition{{System: SystemPC, Main: true}}, g.Editions())
			})
		})
	})

	t.Run("GIVEN a cover photo two discs of the edition share", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "PS3"))
		a, b := g.Copies()[0].ID, g.Copies()[1].ID
		_, err := g.AddPhotos(a, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)
		_, err = g.AddPhotos(b, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)
		require.NoError(t, g.SetEditionCover("PS3", EditionCover{Photo: pid(1)}, t0))

		t.Run("WHEN it is removed from one disc THEN it stays, since the other disc has it", func(t *testing.T) {
			_, err := g.RemovePhoto(a, pid(1), t0)
			require.NoError(t, err)
			assert.Equal(t, pid(1), g.Covers()["PS3"].Photo)
		})

		t.Run("WHEN the disc that still has it is removed THEN the cover is dropped", func(t *testing.T) {
			_, err := g.RemoveCopy(b, t0)
			require.NoError(t, err)
			assert.Empty(t, g.Covers())
		})
	})
}

func TestEditions_absorb(t *testing.T) {
	t.Run("GIVEN a game with a chosen PS3 cover, and another with PS3 and Xbox 360 covers and a main system", func(t *testing.T) {
		kept := gameWith(t, on(KindPhysical, "PS3"))
		require.NoError(t, kept.SetEditionCover("PS3", EditionCover{URL: "https://example.test/kept.jpg"}, t0))

		other := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"))
		require.NoError(t, other.SetEditionCover("PS3", EditionCover{URL: "https://example.test/lost.jpg"}, t0))
		require.NoError(t, other.SetEditionCover("Xbox 360", EditionCover{URL: "https://example.test/x360.jpg"}, t0))
		require.NoError(t, other.SetMainSystem("Xbox 360", t0))

		t.Run("WHEN the first absorbs the second", func(t *testing.T) {
			kept.Absorb(other, t0)

			t.Run("THEN the editions follow the copies, the kept game's cover wins and the other's fill the gaps", func(t *testing.T) {
				assert.Equal(t, []string{"Xbox 360", "PS3"}, systemsOf(kept.Editions()))
				assert.Equal(t, map[string]EditionCover{
					"PS3":      {URL: "https://example.test/kept.jpg"},
					"Xbox 360": {URL: "https://example.test/x360.jpg"},
				}, kept.Covers())
			})

			t.Run("AND the other's main system fills the kept game's empty choice", func(t *testing.T) {
				assert.Equal(t, "Xbox 360", kept.MainSystem())
			})
		})
	})

	t.Run("GIVEN both games chose a main system WHEN one absorbs the other THEN the kept game's choice wins", func(t *testing.T) {
		kept := gameWith(t, on(KindPhysical, "PS3"))
		require.NoError(t, kept.SetMainSystem("PS3", t0))
		other := gameWith(t, on(KindPhysical, "Xbox 360"))
		require.NoError(t, other.SetMainSystem("Xbox 360", t0))

		kept.Absorb(other, t0)
		assert.Equal(t, "PS3", kept.MainSystem())
	})
}

func TestAdoptGameCover(t *testing.T) {
	const url = "https://example.test/halo.jpg"

	// discs returns a game with two Xbox 360 discs (the default main edition) and a PS3 disc with a photo.
	discs := func(t *testing.T) *Game {
		g := gameWith(t, on(KindPhysical, "Xbox 360"), on(KindPhysical, "Xbox 360"), on(KindPhysical, "PS3"))
		_, err := g.AddPhotos(g.Copies()[2].ID, []Photo{{ID: pid(7)}}, t0)
		require.NoError(t, err)

		return g
	}

	t.Run("GIVEN a version-2 cover URL only THEN it goes to the default main edition, stored as the main one", func(t *testing.T) {
		g := discs(t)
		g.AdoptGameCover(url, "")
		assert.Equal(t, map[string]EditionCover{"Xbox 360": {URL: url}}, g.Covers())
		assert.Equal(t, "Xbox 360", g.MainSystem())
	})

	t.Run("GIVEN a cover photo only THEN it goes to its copy's edition, which becomes the main one", func(t *testing.T) {
		g := discs(t)
		g.AdoptGameCover("", pid(7))
		assert.Equal(t, map[string]EditionCover{"PS3": {Photo: pid(7)}}, g.Covers())
		assert.Equal(t, "PS3", g.MainSystem())
	})

	t.Run("GIVEN both, the photo on another edition than the default main one", func(t *testing.T) {
		g := discs(t)
		g.AdoptGameCover(url, pid(7))

		t.Run("THEN the photo keeps its edition and the main place, and the URL goes to the default main edition", func(t *testing.T) {
			assert.Equal(t, map[string]EditionCover{"PS3": {Photo: pid(7)}, "Xbox 360": {URL: url}}, g.Covers())
			assert.Equal(t, "PS3", g.MainSystem())
		})
	})

	t.Run("GIVEN both on the same edition THEN the photo wins and the URL is dropped", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"))
		_, err := g.AddPhotos(g.Copies()[0].ID, []Photo{{ID: pid(7)}}, t0)
		require.NoError(t, err)

		g.AdoptGameCover(url, pid(7))
		assert.Equal(t, map[string]EditionCover{"PS3": {Photo: pid(7)}}, g.Covers())
		assert.Equal(t, "PS3", g.MainSystem())
	})

	t.Run("GIVEN a photo no copy has any more THEN only the URL is adopted", func(t *testing.T) {
		g := discs(t)
		g.AdoptGameCover(url, pid(9))
		assert.Equal(t, map[string]EditionCover{"Xbox 360": {URL: url}}, g.Covers())
		assert.Equal(t, "Xbox 360", g.MainSystem())
	})
}

func TestCoverFingerprints(t *testing.T) {
	t.Run("GIVEN a PS3 disc and a Steam key", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindKey, "Steam"))
		key := g.Copies()[1]

		t.Run("WHEN the key is revealed THEN no edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			d := key.CopyDetails
			d.Status = StatusRevealed
			_, err := g.UpdateCopy(key.ID, d, t0)
			require.NoError(t, err)
			assert.Empty(t, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN a GOG copy is added THEN only PC changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindLibrary, "GOG"), t0)
			require.NoError(t, err)
			assert.Equal(t, []string{SystemPC}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN a Wii disc is added THEN the new Wii edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindPhysical, "Wii"), t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"Wii"}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN the PS3 cover is chosen THEN only PS3 changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			require.NoError(t, g.SetEditionCover("PS3", EditionCover{URL: "https://example.test/ps3.jpg"}, t0))
			assert.Equal(t, []string{"PS3"}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN the store links change THEN every edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			info := g.Info()
			info.Links = Links{LinkSteam: "620"}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"PC", "PS3", "Wii"}, ChangedSystems(before, g.CoverFingerprints()))
		})
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `task go -- test ./internal/domain/game/ -run 'TestEditions|TestEditionCovers|TestAdoptGameCover|TestCoverFingerprints'`
Expected: FAIL to compile (`undefined: Edition`).

- [ ] **Step 3: Write `internal/domain/game/edition.go`**

```go
package game

import (
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
)

// EditionCover is the cover chosen for an edition: an image URL or a photo of one of its copies.
// The zero value means the cover providers choose one.
type EditionCover struct {
	// URL is an image picked among the providers' proposals or pasted by the user.
	URL string

	// Photo is a photo of one of the edition's copies. It wins over URL.
	Photo PhotoID
}

// IsZero reports whether no cover was chosen.
func (c EditionCover) IsZero() bool { return c.URL == "" && c.Photo == "" }

// Edition is the game on one system: the copies whose System() is that system, and its cover.
// Editions are derived from the copies; only the chosen covers and the main system are stored.
type Edition struct {
	System string

	// Cover is the chosen cover; zero when the providers choose.
	Cover EditionCover

	// Main marks the edition shown when the game is listed once ("By game").
	Main bool
}

// ParseCoverURL checks a cover image address: empty, or an http(s) URL. It returns it trimmed.
func ParseCoverURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalid("cover url must be an http(s) URL")
	}

	return s, nil
}

func normalizeCover(c EditionCover) (EditionCover, error) {
	if c.Photo != "" {
		if _, err := ParsePhotoID(string(c.Photo)); err != nil {
			return c, err
		}

		return EditionCover{Photo: c.Photo}, nil // a photo wins: keeping a URL too would be ambiguous
	}

	u, err := ParseCoverURL(c.URL)

	return EditionCover{URL: u}, err
}

// systemStat counts a system's copies and says whether one is physical.
type systemStat struct {
	copies   int
	physical bool
}

func (g *Game) systemStats() map[string]systemStat {
	out := map[string]systemStat{}

	for _, c := range g.copies {
		s := out[c.System()]
		s.copies++
		s.physical = s.physical || c.Kind == KindPhysical
		out[c.System()] = s
	}

	return out
}

// Systems returns the systems the game's copies are played on, each once, in alphabetical order.
func (g *Game) Systems() []string {
	return slices.Sorted(maps.Keys(g.systemStats()))
}

func (g *Game) hasSystem(system string) bool {
	return slices.ContainsFunc(g.copies, func(c Copy) bool { return c.System() == system })
}

func (g *Game) hasPhotoOn(system string, id PhotoID) bool {
	return slices.ContainsFunc(g.copies, func(c Copy) bool { return c.System() == system && photoIndex(c.Photos, id) >= 0 })
}

// MainSystem returns the system the user chose for the main edition; empty means the default rule.
func (g *Game) MainSystem() string { return g.mainSystem }

// Covers returns the chosen covers by system (a copy).
func (g *Game) Covers() map[string]EditionCover { return maps.Clone(g.covers) }

// mainOf returns the main edition's system: the user's choice, else an edition with a physical
// copy, then the one with the most copies, then the first by system; empty without copies.
func (g *Game) mainOf() string {
	if g.mainSystem != "" && g.hasSystem(g.mainSystem) {
		return g.mainSystem
	}

	stats := g.systemStats()
	best := ""

	for _, s := range g.Systems() { // alphabetical, so a tie keeps the first
		st, b := stats[s], stats[best]
		if best == "" || (st.physical && !b.physical) || (st.physical == b.physical && st.copies > b.copies) {
			best = s
		}
	}

	return best
}

// MainEdition returns the edition shown when the game is listed once (see mainOf). A game without
// copies has no editions: its main edition is the zero Edition.
func (g *Game) MainEdition() Edition {
	main := g.mainOf()
	if main == "" {
		return Edition{}
	}

	return Edition{
		System: main,
		Cover:  g.covers[main],
		Main:   true,
	}
}

// Editions returns one edition per system of the game's copies: the main one first, then the
// others by system.
func (g *Game) Editions() []Edition {
	main := g.mainOf()
	systems := g.Systems()
	out := make([]Edition, 0, len(systems))

	if main != "" {
		out = append(out, g.MainEdition())
	}

	for _, s := range systems {
		if s != main {
			out = append(out, Edition{
				System: s,
				Cover:  g.covers[s],
			})
		}
	}

	return out
}

// Edition returns the game's edition on a system, and whether it has one.
func (g *Game) Edition(system string) (Edition, bool) {
	i := slices.IndexFunc(g.Editions(), func(e Edition) bool { return e.System == system })
	if i < 0 {
		return Edition{}, false
	}

	return g.Editions()[i], true
}

// SetEditionCover chooses the cover of the edition on system; a zero cover lets the providers
// choose again. A photo must be of one of that edition's copies.
func (g *Game) SetEditionCover(system string, cover EditionCover, now time.Time) error {
	if !g.hasSystem(system) {
		return invalid("%s: this game has no copy on that system; reload the page", system)
	}

	cover, err := normalizeCover(cover)
	if err != nil {
		return err
	}

	if cover.Photo != "" && !g.hasPhotoOn(system, cover.Photo) {
		return invalid("%s: the photo is not of a copy on that system", system)
	}

	if cover.IsZero() {
		delete(g.covers, system)
	} else {
		if g.covers == nil {
			g.covers = map[string]EditionCover{}
		}

		g.covers[system] = cover
	}

	g.updatedAt = now

	return nil
}

// SetMainSystem makes the edition on system the main one; an empty system goes back to the
// default rule.
func (g *Game) SetMainSystem(system string, now time.Time) error {
	if system != "" && !g.hasSystem(system) {
		return invalid("%s: this game has no copy on that system; reload the page", system)
	}

	g.mainSystem = system
	g.updatedAt = now

	return nil
}

// reconcileEditions drops what no longer fits the copies: covers of systems without copies, photo
// covers whose photo no copy of that system has, and a main system without copies.
func (g *Game) reconcileEditions() {
	for system, c := range g.covers {
		if !g.hasSystem(system) || (c.Photo != "" && !g.hasPhotoOn(system, c.Photo)) {
			delete(g.covers, system)
		}
	}

	if g.mainSystem != "" && !g.hasSystem(g.mainSystem) {
		g.mainSystem = ""
	}
}

// RestoreEditions sets the chosen covers and the main system read from storage, dropping what no
// longer fits the copies. Only repositories call it, right after Rehydrate.
func (g *Game) RestoreEditions(covers map[string]EditionCover, mainSystem string) {
	g.covers = maps.Clone(covers)
	g.mainSystem = mainSystem
	g.reconcileEditions()
}

// AdoptGameCover turns the single cover a game had before editions (stored documents of version 2)
// into edition covers. A photo goes to the edition of a copy that has it, and that edition becomes
// the main one, so the game looks as before. A URL goes to the main edition by the default rule,
// unless the photo took that edition; that system is stored as the main one when no photo set it.
// Only repositories call it, right after Rehydrate.
func (g *Game) AdoptGameCover(coverURL string, photo PhotoID) {
	defaultMain := g.mainOf()

	if photo != "" {
		if i := slices.IndexFunc(g.copies, func(c Copy) bool { return photoIndex(c.Photos, photo) >= 0 }); i >= 0 {
			system := g.copies[i].System()
			g.covers = map[string]EditionCover{system: {Photo: photo}}
			g.mainSystem = system
		}
	}

	if coverURL == "" || defaultMain == "" {
		return
	}

	if _, taken := g.covers[defaultMain]; !taken {
		if g.covers == nil {
			g.covers = map[string]EditionCover{}
		}

		g.covers[defaultMain] = EditionCover{URL: coverURL}
	}

	if g.mainSystem == "" {
		g.mainSystem = defaultMain
	}
}

// CoverFingerprints returns, per system of the game, a summary of what that edition's cover depends
// on: its chosen cover, the platforms of its copies (physical ones apart) and the store links. When
// a fingerprint changes, that edition's cached cover may be stale.
func (g *Game) CoverFingerprints() map[string]string {
	links := make([]string, 0, len(g.links))
	for _, k := range g.links.Keys() {
		links = append(links, k+"="+g.links[k])
	}

	out := map[string]string{}

	for _, system := range g.Systems() {
		var platforms []string

		for _, c := range g.copies {
			if c.System() != system {
				continue
			}

			p := c.Platform
			if c.Kind == KindPhysical {
				p += " (physical)"
			}

			if !slices.Contains(platforms, p) {
				platforms = append(platforms, p)
			}
		}

		slices.Sort(platforms)

		cover := g.covers[system]
		out[system] = strings.Join([]string{cover.URL, string(cover.Photo), strings.Join(platforms, "\x1f"), strings.Join(links, "\x1f")}, "\x1e")
	}

	return out
}

// ChangedSystems returns, sorted, the systems whose fingerprints differ between two results of
// CoverFingerprints, editions that appeared or disappeared included.
func ChangedSystems(before, after map[string]string) []string {
	var out []string

	for system, f := range after {
		if before[system] != f {
			out = append(out, system)
		}
	}

	for system := range before {
		if _, ok := after[system]; !ok {
			out = append(out, system)
		}
	}

	slices.Sort(out)

	return out
}
```

  `Links.Keys()` already exists (the consolidator uses it) and returns sorted keys.

- [ ] **Step 4: Replace the game's single cover** (`game.go`, `photo.go`, `consolidate.go`)
  - `Game` struct: remove `coverURL` and `coverPhoto`; add `covers map[string]EditionCover` and `mainSystem string` after `fields`.
  - `Info`: remove `CoverURL` and `CoverPhoto` with their comments.
  - `Info.normalize`: trim only `Title` and `Notes` (`i.Title, i.Notes = strings.TrimSpace(i.Title), strings.TrimSpace(i.Notes)`) and delete the cover URL check (it moved to `ParseCoverURL`). Drop the `net/url` import.
  - `Rehydrate` and `Info()`: remove the two cover lines.
  - `UpdateInfo`: rename the result to `linksChanged`, document it as "It reports whether the links changed: covers and details depend on them, so cached ones can be refreshed.", delete the cover photo check, and set `linksChanged = !i.Links.Equal(g.links)`; assign `g.title, g.links, g.notes = i.Title, i.Links, i.Notes`.
  - Delete the getters `CoverURL()` and `CoverPhoto()`.
  - `UpdateCopy`: after `g.copies[i].CopyDetails = d`, call `g.reconcileEditions()` (an override can move the copy to another system).
  - `RemoveCopy` and `RemoveCopiesFromSource`: replace `g.dropOrphanCover()` with `g.reconcileEditions()`.
  - `Absorb`: replace the cover block (from "The other game's cover photo comes along…" to the `coverURL` fill) with:

    ```go
    	// Covers chosen for this game win; the other's fill the systems this game chose none for.
    	for system, c := range other.covers {
    		if _, ok := g.covers[system]; ok {
    			continue
    		}

    		if g.covers == nil {
    			g.covers = map[string]EditionCover{}
    		}

    		g.covers[system] = c
    	}

    	if g.mainSystem == "" {
    		g.mainSystem = other.mainSystem
    	}
    ```
    and call `g.reconcileEditions()` just before `g.updatedAt = now`.
  - `photo.go`: in `RemovePhoto` replace `g.dropOrphanCover()` with `g.reconcileEditions()`; delete `hasPhoto` and `dropOrphanCover` (no callers left; `unused` would fail).
  - `consolidate.go`: in the existing-copy branch, inside `if changed {`, add `g.reconcileEditions()` first (a scan can change a copy's platform, and so its system).

- [ ] **Step 5: Update the old cover tests of the domain**
  - `photo_test.go`: delete `TestPhotos_cover` (replaced by `TestEditionCovers`) and, in the merge test, the GIVEN "a target game with a custom cover URL, and another whose cover is a photo" (replaced by `TestEditions_absorb`).
  - `game_test.go`: delete `TestUpdateInfoCover` (URL validation is covered by `TestEditionCovers`).
  - `grep -n "CoverURL\|CoverPhoto" internal/domain/game/*_test.go` must print nothing.

Run: `task go -- test ./internal/domain/game/`
Expected: `ok`.

- [ ] **Step 6: Failing storage tests**

  In `internal/adapters/outbound/sqlite/docs_test.go`:
  - `sampleGame`: remove `CoverURL` from the `Info`, assign the result of `game.Rehydrate` to `g`, then `g.RestoreEditions(map[string]game.EditionCover{"PS4": {URL: "https://example.test/hades.jpg"}}, "PS4")` and `return g`. Check `grep -n "hades.jpg\|CoverURL" internal/adapters/outbound/sqlite/docs_test.go` and update any assertion on the old cover to `got.Covers()`.
  - `TestDocuments_photos`: build the game without `CoverPhoto`, then `g.RestoreEditions(map[string]game.EditionCover{game.SystemOther: {Photo: id2}}, "")` (the copy has no platform, so its system is `Other`). Assert `assert.Equal(t, g.Covers(), got.Covers())` instead of `got.CoverPhoto()`, `"v":3` instead of `"v":2`, and `assert.NotContains(t, plain, \`"photo"\`)` instead of `"coverPhoto"`; rename the THEN to "the photos and the edition's cover photo come back, as version 3".
  - Append:

```go
func TestDocuments_editions(t *testing.T) {
	photo := game.PhotoID(strings.Repeat("c", 64))

	t.Run("GIVEN a game with a chosen cover per edition and a main system", func(t *testing.T) {
		g := game.Rehydrate("g1", game.Info{Title: "Halo 3"}, []game.Copy{
			{
				ID:          "c1",
				CopyDetails: game.CopyDetails{Kind: game.KindPhysical, Platform: "PS3", Status: game.StatusOwned},
				Photos:      []game.Photo{{ID: photo, AddedAt: docTime}},
				CreatedAt:   docTime,
				UpdatedAt:   docTime,
			},
			{
				ID:          "c2",
				CopyDetails: game.CopyDetails{Kind: game.KindPhysical, Platform: "Xbox 360", Status: game.StatusOwned},
				CreatedAt:   docTime,
				UpdatedAt:   docTime,
			},
		}, docTime, docTime)
		g.RestoreEditions(map[string]game.EditionCover{"PS3": {Photo: photo}, "Xbox 360": {URL: "https://example.test/x360.jpg"}}, "Xbox 360")

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the covers and the main system come back, in a version-3 document", func(t *testing.T) {
				assert.Equal(t, g.Covers(), got.Covers())
				assert.Equal(t, "Xbox 360", got.MainSystem())
				assert.Contains(t, raw, `"v":3`)
				assert.Contains(t, raw, `"covers":{`)
				assert.Contains(t, raw, `"mainSystem":"Xbox 360"`)
				assert.NotContains(t, raw, `"coverUrl"`)
				assert.NotContains(t, raw, `"coverPhoto"`)
			})

			t.Run("AND a game without chosen covers writes neither field", func(t *testing.T) {
				plain, err := encodeGame(game.Rehydrate("g2", game.Info{Title: "Hades"}, nil, docTime, docTime))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"covers"`)
				assert.NotContains(t, plain, `"mainSystem"`)
			})
		})
	})

	t.Run("GIVEN a version-2 document with a cover URL, and a cover photo on a copy of another edition than the default main one", func(t *testing.T) {
		const at = `"createdAt":"2026-10-09T18:30:00Z","updatedAt":"2026-10-09T18:30:00Z"`
		raw := `{"v":2,"title":"Halo 3","coverUrl":"https://example.test/halo.jpg","coverPhoto":"` + string(photo) + `",` + at + `,"copies":[` +
			`{"id":"c1","kind":"physical","platform":"Xbox 360","status":"owned",` + at + `},` +
			`{"id":"c2","kind":"physical","platform":"Xbox 360","status":"owned",` + at + `},` +
			`{"id":"c3","kind":"physical","platform":"PS3","status":"owned","photos":[{"id":"` + string(photo) + `","addedAt":"2026-10-09T18:30:00Z"}],` + at + `}]}`

		t.Run("WHEN it is read", func(t *testing.T) {
			got, err := decodeGame("g1", raw)
			require.NoError(t, err)

			t.Run("THEN the photo keeps its edition as the main one, and the URL goes to the default main edition", func(t *testing.T) {
				assert.Equal(t, map[string]game.EditionCover{"PS3": {Photo: photo}, "Xbox 360": {URL: "https://example.test/halo.jpg"}}, got.Covers())
				assert.Equal(t, "PS3", got.MainSystem())
			})

			t.Run("AND saving it again writes version 3 without the old fields", func(t *testing.T) {
				again, err := encodeGame(got)
				require.NoError(t, err)
				assert.Contains(t, again, `"v":3`)
				assert.NotContains(t, again, `"coverUrl"`)
				assert.NotContains(t, again, `"coverPhoto"`)
			})
		})
	})
}
```

  In `internal/adapters/outbound/sqlite/migrate_test.go`, append:

```go
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
```

  Add the imports the file lacks (`context`, `time`) if `TestMigration0009` does not already use them.

Run: `task go -- test ./internal/adapters/outbound/sqlite/`
Expected: FAIL to compile until Step 7 (`game.Info has no field CoverURL`), then the new tests fail.

- [ ] **Step 7: Version-3 documents** (`docs.go`)
  - The version constant:

    ```go
    	// gameDocVersion 2 replaced the copies' free-text condition with grade and contents; 3 moved the
    	// cover to editions (covers by system, mainSystem).
    	gameDocVersion     = 3
    ```
  - In `gameDoc`, replace the two cover fields with:

    ```go
    	// CoverURL and CoverPhoto are the game's single cover of version-2 documents, converted to
    	// edition covers when read (game.AdoptGameCover); never written.
    	CoverURL   string `json:"coverUrl,omitempty"`
    	CoverPhoto string `json:"coverPhoto,omitempty"`

    	// Covers are the covers chosen per system; MainSystem the user's main edition.
    	Covers     map[string]coverDoc `json:"covers,omitempty"`
    	MainSystem string              `json:"mainSystem,omitempty"`
    ```
  - Add the type:

    ```go
    // coverDoc is the stored form of a game.EditionCover: a URL or a photo id.
    type coverDoc struct {
    	URL   string `json:"url,omitempty"`
    	Photo string `json:"photo,omitempty"`
    }
    ```
  - `encodeGame`: drop the two cover fields of the literal and set `Covers: coversToDoc(g.Covers())` and `MainSystem: g.MainSystem()`.
  - `decodeGame`: drop the two cover fields from `info`, and replace the final `return` with:

    ```go
    	g := game.Rehydrate(id, info, copies, parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt))
    	if doc.V < 3 {
    		g.AdoptGameCover(doc.CoverURL, game.PhotoID(doc.CoverPhoto))
    	} else {
    		g.RestoreEditions(coversFromDoc(doc.Covers), doc.MainSystem)
    	}

    	return g, nil
    ```
  - Add:

    ```go
    func coversToDoc(covers map[string]game.EditionCover) map[string]coverDoc {
    	if len(covers) == 0 {
    		return nil
    	}

    	out := make(map[string]coverDoc, len(covers))
    	for system, c := range covers {
    		out[system] = coverDoc{
    			URL:   c.URL,
    			Photo: string(c.Photo),
    		}
    	}

    	return out
    }

    func coversFromDoc(docs map[string]coverDoc) map[string]game.EditionCover {
    	out := make(map[string]game.EditionCover, len(docs))
    	for system, d := range docs {
    		out[system] = game.EditionCover{
    			URL:   d.URL,
    			Photo: game.PhotoID(d.Photo),
    		}
    	}

    	return out
    }
    ```

- [ ] **Step 8: The backup trigger** (`internal/adapters/outbound/sqlite/migrations/0010_platform_editions.sql`)

```sql
-- Game documents become version 3 (covers per edition, docs/superpowers/specs/2026-10-10-platform-editions-design.md).
-- They are converted when read, so nothing changes here: this migration only makes Open write the
-- usual pre-migration backup before the first version-3 document is saved, since an older Game
-- Vault refuses to read those.
UPDATE games SET doc = doc WHERE 0;
```

Run: `task go -- test ./internal/adapters/outbound/sqlite/`
Expected: `ok`.

- [ ] **Step 9: Catalog** (`internal/application/catalog/service.go`, `scanned.go`)
  - `UpdateGame`: delete the four lines that copied the cover photo into `info` (from "Editing the game keeps its cover photo…" to the closing `}`); the result of `g.UpdateInfo` is now "links changed", which still drops the cached cover and details.
  - Replace `SetCoverPhoto` and `mutatePhotos` with:

    ```go
    // SetEditionCover chooses the cover of a game's edition on system; a zero cover lets the
    // providers choose again.
    func (s *Service) SetEditionCover(ctx context.Context, id game.ID, system string, cover game.EditionCover) (*game.Game, error) {
    	return s.mutateEditions(ctx, id, func(g *game.Game) error {
    		return g.SetEditionCover(system, cover, s.now())
    	})
    }

    // SetMainSystem makes a game's edition on system the one shown when the game is listed once; an
    // empty system goes back to the default rule.
    func (s *Service) SetMainSystem(ctx context.Context, id game.ID, system string) (*game.Game, error) {
    	g, err := s.mutate(ctx, id, func(_ context.Context, g *game.Game) error {
    		return g.SetMainSystem(system, s.now())
    	})
    	if err == nil {
    		s.invalidateCover(ctx, id) // Bridge (Task 3): the cached cover is the main edition's until covers are cached per edition
    	}

    	return g, err
    }

    // mutateEditions is mutate, dropping the cached cover when the change touched the chosen covers.
    func (s *Service) mutateEditions(ctx context.Context, id game.ID, fn func(*game.Game) error) (*game.Game, error) {
    	var before map[string]game.EditionCover

    	g, err := s.mutate(ctx, id, func(_ context.Context, g *game.Game) error {
    		before = g.Covers()
    		return fn(g)
    	})
    	if err == nil && !maps.Equal(before, g.Covers()) {
    		s.invalidateCover(ctx, id) // Bridge (Task 4): only the editions whose cover inputs changed
    	}

    	return g, err
    }
    ```
    and rename every `s.mutatePhotos(` call to `s.mutateEditions(` (`DeleteCopy` and the four photo use cases). Add the `maps` import.
  - `MoveCopy`: replace `srcCover game.PhotoID` with `srcCovers map[string]game.EditionCover`, set it with `srcCovers = src.Covers()`, and compare with `if src != nil && !maps.Equal(src.Covers(), srcCovers) { s.invalidateCover(ctx, from) // Bridge (Task 4) … }` (keep the existing comment).
  - `scanned.go`: `scannedTarget` no longer sets a cover; it checks the chosen covers instead, so a bad URL still fails its whole group before anything is added. Replace its loop over the group with:

    ```go
    	for _, i := range group {
    		if _, err := game.ParseCoverURL(items[i].CoverURL); err != nil {
    			return nil, err
    		}
    	}
    ```
    In `AddScannedCopies`, right after the `if !added { continue }` block, add:

    ```go
    			if items[group[0]].GameID == "" {
    				setScannedCover(g, items, group, results, now)
    			}
    ```
    and add the function:

    ```go
    // setScannedCover gives a new game the first cover one of its items chose, on the edition of that
    // item's copy: a PS3 disc's box goes to the PS3 edition.
    func setScannedCover(g *game.Game, items []ScannedCopy, group []int, results []ScannedResult, now time.Time) {
    	for _, i := range group {
    		if items[i].CoverURL == "" || results[i].CopyID == "" {
    			continue
    		}

    		for _, c := range g.Copies() {
    			if c.ID == results[i].CopyID {
    				_ = g.SetEditionCover(c.System(), game.EditionCover{URL: items[i].CoverURL}, now) // scannedTarget checked the URL
    				return
    			}
    		}
    	}
    }
    ```
  - `service_test.go`: replace `TestCoverPhotoCache` with:

```go
func TestEditionCoverCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// setup returns a service and a game with two PS3 discs; the first disc's photo is the PS3 cover.
	setup := func(t *testing.T) (*catalog.Service, *covers, *game.Game) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, cache, nil, nil)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, []game.CopyDetails{
			{Kind: game.KindPhysical, Platform: "PS3"},
			{Kind: game.KindPhysical, Platform: "PS3"},
		})
		require.NoError(t, err)

		g, err = svc.AddCopyPhotos(ctx, g.ID(), g.Copies()[0].ID, []game.Photo{{ID: photoID(1)}})
		require.NoError(t, err)

		g, err = svc.SetEditionCover(ctx, g.ID(), "PS3", game.EditionCover{Photo: photoID(1)})
		require.NoError(t, err)
		assert.Equal(t, []game.ID{g.ID()}, cache.invalidated, "choosing a cover drops the cached one")

		cache.invalidated = nil

		return svc, cache, g
	}

	t.Run("GIVEN a game whose PS3 cover is a photo of its first disc", func(t *testing.T) {
		cases := map[string]func(svc *catalog.Service, g *game.Game) error{
			"the photo is removed": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.RemoveCopyPhoto(ctx, g.ID(), g.Copies()[0].ID, photoID(1))
				return err
			},
			"the disc is deleted": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.DeleteCopy(ctx, g.ID(), g.Copies()[0].ID)
				return err
			},
			"the disc is moved to a new game": func(svc *catalog.Service, g *game.Game) error {
				_, _, err := svc.MoveCopy(ctx, g.ID(), g.Copies()[0].ID, "", "Halo 3 (Limited)")
				return err
			},
			"another cover URL is chosen for the edition": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.SetEditionCover(ctx, g.ID(), "PS3", game.EditionCover{URL: "https://example.test/halo.jpg"})
				return err
			},
		}

		for what, change := range cases {
			t.Run("WHEN "+what, func(t *testing.T) {
				svc, cache, g := setup(t)
				require.NoError(t, change(svc, g))

				t.Run("THEN the edition has no cover photo and the cached cover is dropped", func(t *testing.T) {
					got, err := svc.GetGame(ctx, g.ID())
					require.NoError(t, err)
					assert.Empty(t, got.Covers()["PS3"].Photo)
					assert.Contains(t, cache.invalidated, g.ID())
				})
			})
		}

		t.Run("WHEN only the title is edited THEN the cover photo stays", func(t *testing.T) {
			svc, _, g := setup(t)
			info := g.Info()
			info.Title = "Halo 3 (2007)"
			got, err := svc.UpdateGame(ctx, g.ID(), info)
			require.NoError(t, err)
			assert.Equal(t, photoID(1), got.Covers()["PS3"].Photo)
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming the system", func(t *testing.T) {
			svc, _, g := setup(t)
			_, err := svc.SetEditionCover(ctx, g.ID(), "Wii", game.EditionCover{URL: "https://example.test/wii.jpg"})

			var ve *game.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Wii")
		})

		t.Run("WHEN the main edition is chosen THEN the game keeps it", func(t *testing.T) {
			svc, _, g := setup(t)
			got, err := svc.SetMainSystem(ctx, g.ID(), "PS3")
			require.NoError(t, err)
			assert.Equal(t, "PS3", got.MainSystem())
		})
	})
}
```
  - `scanned_test.go`: replace `assert.Equal(t, "https://example.com/ds3.jpg", ds3.CoverURL())` with `assert.Equal(t, map[string]game.EditionCover{"Xbox 360": {URL: "https://example.com/ds3.jpg"}}, ds3.Covers(), "the cover goes to the edition of the disc that chose it")`.

- [ ] **Step 10: Sync** (`internal/application/sync/service.go`, `exclusions.go`, `covers_test.go`). Same comparison as the catalog, until Task 4:
  - `Delete`: `cover := g.CoverPhoto()` becomes `covers := g.Covers()`, and `if g.CoverPhoto() != cover` becomes `if !maps.Equal(g.Covers(), covers) { // Bridge (Task 4): per edition`.
  - `Sync`: the map becomes `covers := make(map[game.ID]map[string]game.EditionCover, len(games))` filled with `g.Covers()`, and the check `if !maps.Equal(g.Covers(), covers[g.ID()])`.
  - `exclusions.go` (`ExcludeCopy`): `cover := g.Covers()` and `stale, out = !maps.Equal(g.Covers(), cover), g`.
  - Update the comment of `invalidateCovers` to "whose chosen covers changed (a photo cover left with a source's copy)".
  - `covers_test.go`: in `photographCover`, replace the `info.CoverPhoto` lines with `require.NoError(t, g.SetEditionCover(g.Copies()[0].System(), game.EditionCover{Photo: photo}, time.Now()))`; in the THEN, `assert.Empty(t, g.Covers())` replaces `assert.Empty(t, g.CoverPhoto())`.

- [ ] **Step 11: Media and RPC bridges**
  - `internal/application/media/service.go`, in `resolve`: before the photo block add

    ```go
    	// Bridge (Task 3): until covers are resolved per edition, the main edition's chosen cover is the game's.
    	chosen := g.MainEdition().Cover
    ```
    and use `chosen.Photo` / `chosen.URL` in place of `g.CoverPhoto()` / `g.CoverURL()`.
  - `internal/adapters/inbound/rpc/mapper.go`, `gameToPB`:

    ```go
    		// Bridge (Task 5): the API still has one cover per game, the main edition's.
    		CoverUrl:     g.MainEdition().Cover.URL,
    		CoverPhotoId: string(g.MainEdition().Cover.Photo),
    ```
  - `internal/adapters/inbound/rpc/game_handler.go`:
    - `CreateGame`: drop `CoverURL` from `info`; after `h.catalog.CreateGame`, add

      ```go
      	if err == nil && req.Msg.CoverUrl != "" {
      		g, err = h.setMainCoverURL(ctx, g, req.Msg.CoverUrl) // Bridge (Task 5)
      	}
      ```
    - `UpdateGame`: drop `CoverURL` from `info`; after `h.catalog.UpdateGame`, add

      ```go
      	if err == nil && req.Msg.CoverUrl != g.MainEdition().Cover.URL {
      		g, err = h.setMainCoverURL(ctx, g, req.Msg.CoverUrl) // Bridge (Task 5)
      	}
      ```
    - Add the helper and rewrite `SetCoverPhoto`:

      ```go
      // setMainCoverURL sets a cover URL on the game's main edition. Bridge (Task 5): the API speaks of
      // one cover per game until it speaks of editions.
      func (h *GameHandler) setMainCoverURL(ctx context.Context, g *game.Game, coverURL string) (*game.Game, error) {
      	if _, err := game.ParseCoverURL(coverURL); err != nil {
      		return nil, err
      	}

      	main := g.MainEdition()
      	if main.System == "" {
      		return g, nil // a game without copies has no edition to hold a cover
      	}

      	return h.catalog.SetEditionCover(ctx, g.ID(), main.System, game.EditionCover{URL: coverURL})
      }

      // SetCoverPhoto makes one of the copies' photos the cover of its edition, and that edition the main
      // one, or stops using a photo as the main edition's cover. Bridge (Task 5): replaced by
      // SetEditionCover.
      func (h *GameHandler) SetCoverPhoto(ctx context.Context, req *connect.Request[pb.SetCoverPhotoRequest]) (*connect.Response[pb.SetCoverPhotoResponse], error) {
      	id := game.ID(req.Msg.GameId)

      	g, err := h.catalog.GetGame(ctx, id)
      	if err != nil {
      		return nil, toConnectError(err)
      	}

      	main := g.MainEdition()
      	if req.Msg.PhotoId == "" {
      		if main.Cover.Photo != "" {
      			g, err = h.catalog.SetEditionCover(ctx, id, main.System, game.EditionCover{})
      		}

      		return gameResp(g, err, func(g *pb.Game) *pb.SetCoverPhotoResponse { return &pb.SetCoverPhotoResponse{Game: g} })
      	}

      	system := ""

      	for _, c := range g.Copies() {
      		if slices.ContainsFunc(c.Photos, func(p game.Photo) bool { return string(p.ID) == req.Msg.PhotoId }) {
      			system = c.System()
      			break
      		}
      	}

      	if system == "" {
      		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the cover photo must be a photo of one of the game's copies"))
      	}

      	if g, err = h.catalog.SetEditionCover(ctx, id, system, game.EditionCover{Photo: game.PhotoID(req.Msg.PhotoId)}); err == nil {
      		g, err = h.catalog.SetMainSystem(ctx, id, system)
      	}

      	return gameResp(g, err, func(g *pb.Game) *pb.SetCoverPhotoResponse { return &pb.SetCoverPhotoResponse{Game: g} })
      }
      ```
      `setMainCoverURL` returns a `*game.ValidationError` for a bad URL, which `gameResp` / `toConnectError` turns into `InvalidArgument` as before. Check with `grep -n "func gameResp" -A 12 internal/adapters/inbound/rpc/*.go` that `gameResp` maps errors with `toConnectError`.

- [ ] **Step 12: Nothing refers to the old cover any more**

Run: `grep -rnE 'CoverURL\(\)|CoverPhoto\(\)|info\.CoverURL|info\.CoverPhoto|CoverPhoto:|dropOrphanCover' --include='*.go' internal cmd | grep -v internal/gen`
Expected: no output.

- [ ] **Step 13: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green; the RPC tests `TestCoverProviderChain`, `TestCoversAndLogs`, `TestBarcodeScanFlow` and the photo cover test pass through the bridges).

```bash
git add internal
git commit -m "Games: a cover per edition (system) and a main edition; game documents v3"
```

---
### Task 3: Covers per edition (media, game-data folders, providers, endpoints)

**Files:**
- Create: `internal/application/media/editions_test.go`, `internal/adapters/outbound/gamedata/editions_test.go`, `internal/adapters/inbound/rpc/covers_test.go`, and one `applies_test.go` in each of `internal/adapters/outbound/{steam,gog,epic,eaapp,battlenet,ubisoft,xbox,example}`
- Modify: `internal/application/media/service.go`, `internal/application/media/addon.go`, `internal/application/media/details.go`, `internal/adapters/outbound/gamedata/store.go`, `internal/adapters/outbound/{steam/store.go,gog/covers.go,epic/covers.go,eaapp/covers.go,battlenet/covers.go,ubisoft/covers.go,xbox/covers.go,example/covers.go}`, `internal/adapters/outbound/thegamesdb/provider.go`, `internal/adapters/outbound/thegamesdb/provider_test.go`, `internal/adapters/inbound/rpc/media_handler.go`, `internal/adapters/inbound/rpc/server.go`, `internal/adapters/inbound/rpc/media_rpc_handler.go`, `internal/adapters/inbound/rpc/mapper.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/application/catalog/service.go`

**Interfaces:**
- Consumes: Task 2 `Edition`, `MainEdition()`, `Edition(system)`, `Systems()`, `EditionCover`; Task 1 `(Copy).System()`, `SystemPC`.
- Produces:
  - `media.CoverQuery.System string` and `(CoverQuery).ForPC() bool`
  - `media.QueryFor(g *game.Game, system string) CoverQuery` (`""`: every copy, no system — games without copies and game sheets)
  - `media.AssetStore`: `GetCover(id game.ID, system string) (Image, bool, error)`, `PutCover(g GameRef, system string, img Image) error`, `MarkCoverMissing(g GameRef, system string, at time.Time) error`, `CoverMissingSince(id game.ID, system string) (time.Time, bool)`, `DeleteCover(id game.ID, system string) error`, `DeleteCovers(id game.ID) error`, `AdoptLegacyCover(g GameRef, system string) (Image, bool, error)`
  - `(*media.Service).Cover(ctx, id) (Image, error)` (main edition), `EditionCover(ctx, id game.ID, system string) (Image, error)`, `CoverCandidates(ctx, id game.ID, system string) ([]CoverCandidate, []string, error)`, `InvalidateEdition(ctx, id game.ID, system string) error`
  - `media.ErrNoEdition`
  - `xbox.IsXboxSystem(system string) bool`
  - `GET /media/covers/{id}/{system...}`

- [ ] **Step 1: Failing game-data test** (`internal/adapters/outbound/gamedata/editions_test.go`)

```go
package gamedata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

func TestEditionCoverFiles(t *testing.T) {
	png := media.Image{Data: []byte("png"), ContentType: "image/png"}
	g := media.GameRef{ID: "019b11c2-bbbb", Title: "Halo 3"}

	t.Run("GIVEN covers stored for two systems, one with a slash and spaces", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)
		require.NoError(t, s.PutCover(g, "PS3", jpg))
		require.NoError(t, s.PutCover(g, "Xbox 360/S Slim", png))

		t.Run("THEN each reads back on its own", func(t *testing.T) {
			img, ok, err := s.GetCover(g.ID, "PS3")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, jpg.Data, img.Data)

			img, ok, _ = s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.True(t, ok)
			assert.Equal(t, png.Data, img.Data)

			_, ok, _ = s.GetCover(g.ID, "Wii")
			assert.False(t, ok)
		})

		t.Run("AND the files stay in the game's folder, named after the system", func(t *testing.T) {
			dir := filepath.Join(root, "Halo 3 [019b11c2-bbbb]")
			ps3, _ := filepath.Glob(filepath.Join(dir, "cover-ps3-*.jpg"))
			slim, _ := filepath.Glob(filepath.Join(dir, "cover-xbox-360-s-slim-*.png"))
			assert.Len(t, ps3, 1)
			assert.Len(t, slim, 1)
		})

		t.Run("AND a missing marker is per system", func(t *testing.T) {
			at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
			require.NoError(t, s.MarkCoverMissing(g, "Wii", at))

			since, ok := s.CoverMissingSince(g.ID, "Wii")
			assert.True(t, ok)
			assert.True(t, since.Equal(at))

			_, ok = s.CoverMissingSince(g.ID, "PS3")
			assert.False(t, ok)
		})

		t.Run("AND pruning sheet images never touches covers", func(t *testing.T) {
			require.NoError(t, s.SetAssetSources(g, map[string]string{"screenshot-1": "https://x.test/1.jpg"}))

			_, ok, _ := s.GetCover(g.ID, "PS3")
			assert.True(t, ok)
		})

		t.Run("AND a sheet image cannot take a cover's name", func(t *testing.T) {
			assert.Error(t, s.PutAsset(g, "cover-ps3", jpg))
		})

		t.Run("WHEN one system's cover is deleted THEN the others stay", func(t *testing.T) {
			require.NoError(t, s.DeleteCover(g.ID, "PS3"))

			_, ok, _ := s.GetCover(g.ID, "PS3")
			assert.False(t, ok)

			_, ok, _ = s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.True(t, ok)
		})

		t.Run("WHEN every cover is deleted THEN no system has one, nor a missing marker", func(t *testing.T) {
			require.NoError(t, s.DeleteCovers(g.ID))

			_, ok, _ := s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.False(t, ok)

			_, ok = s.CoverMissingSince(g.ID, "Wii")
			assert.False(t, ok)
		})
	})

	t.Run("GIVEN a cover stored before editions", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)

		legacy := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID)+".jpg"), jpg.Data, 0o600))
		_, err = s.MigrateLegacyCovers(legacy, map[game.ID]string{g.ID: g.Title})
		require.NoError(t, err)

		t.Run("WHEN the main edition adopts it", func(t *testing.T) {
			img, ok, err := s.AdoptLegacyCover(g, "PS3")
			require.NoError(t, err)

			t.Run("THEN it is that edition's cover, and it can only be adopted once", func(t *testing.T) {
				assert.True(t, ok)
				assert.Equal(t, jpg.Data, img.Data)

				_, ok, _ = s.GetCover(g.ID, "PS3")
				assert.True(t, ok)

				_, ok, _ = s.AdoptLegacyCover(g, "PC")
				assert.False(t, ok)
			})
		})
	})
}
```

  Also update the existing `store_test.go` to the new signatures: `s.PutCover(g, "PS3", jpg)` and `s.GetCover(g.ID, "PS3")`, and its legacy-migration checks (`grep -n "GetCover\|PutCover\|cover.jpg\|DeleteCover" internal/adapters/outbound/gamedata/store_test.go`): `MigrateLegacyCovers` still writes `cover.jpg`; read it back with `AdoptLegacyCover(g, "PS3")` where the test used `GetCover(id)`.

Run: `task go -- test ./internal/adapters/outbound/gamedata/`
Expected: FAIL to compile (too many arguments in `PutCover`).

- [ ] **Step 2: Cover files per system** (`internal/adapters/outbound/gamedata/store.go`)
  - Package comment: replace the `cover.jpg` line with

    ```go
    //	    cover-ps3-1a2b3c4d.jpg   the cover of each edition, by system (cover-…​.missing when the
    //	                             last lookup found nothing); cover.jpg is a cover from before editions
    ```
  - Constants: rename `coverName` to `legacyCover` and `missingFile` to `legacyMissing` (both still used by `MigrateLegacyCovers`), and add the helpers:

    ```go
    var reNotSlug = regexp.MustCompile(`[^a-z0-9]+`)

    // coverFile is the file name, without extension, of an edition's cover: "cover-xbox-360-1a2b3c4d".
    // The slug keeps folders readable; the hash keeps apart systems with the same slug ("PS4/Pro",
    // "PS4 Pro") and makes any system name safe as a file name. A game without copies uses
    // "cover-game".
    func coverFile(system string) string {
    	if system == "" {
    		return legacyCover + "-game"
    	}

    	slug := strings.Trim(reNotSlug.ReplaceAllString(strings.ToLower(system), "-"), "-")
    	if len(slug) > 40 {
    		slug = strings.Trim(slug[:40], "-")
    	}

    	if slug == "" {
    		slug = "system"
    	}

    	sum := sha256.Sum256([]byte(system))

    	return legacyCover + "-" + slug + "-" + hex.EncodeToString(sum[:4])
    }

    // isCoverName reports whether a file name (without extension) belongs to a cover, so sheet images
    // can never use or prune it.
    func isCoverName(name string) bool {
    	return name == legacyCover || strings.HasPrefix(name, legacyCover+"-")
    }
    ```
    (imports `crypto/sha256`, `encoding/hex`).
  - Replace the Cover section with:

    ```go
    // GetCover returns the stored cover of a game's edition, if any.
    func (s *Store) GetCover(id game.ID, system string) (media.Image, bool, error) {
    	dir, ok := s.dir(id)
    	if !ok {
    		return media.Image{}, false, nil
    	}

    	return readImage(dir, coverFile(system))
    }

    // PutCover stores the cover of a game's edition and clears its missing marker.
    func (s *Store) PutCover(g media.GameRef, system string, img media.Image) error {
    	dir, err := s.ensureDir(g)
    	if err != nil {
    		return err
    	}

    	_ = os.Remove(filepath.Join(dir, coverFile(system)+".missing")) // absent is fine

    	return writeImage(dir, coverFile(system), img)
    }

    // MarkCoverMissing records when no provider had a cover for a game's edition.
    func (s *Store) MarkCoverMissing(g media.GameRef, system string, at time.Time) error {
    	dir, err := s.ensureDir(g)
    	if err != nil {
    		return err
    	}

    	return writeFile(filepath.Join(dir, coverFile(system)+".missing"), []byte(at.UTC().Format(time.RFC3339)))
    }

    // CoverMissingSince returns when a game's edition was marked as having no cover.
    func (s *Store) CoverMissingSince(id game.ID, system string) (time.Time, bool) {
    	dir, ok := s.dir(id)
    	if !ok {
    		return time.Time{}, false
    	}

    	b, err := os.ReadFile(filepath.Join(dir, coverFile(system)+".missing"))
    	if err != nil {
    		return time.Time{}, false
    	}

    	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))

    	return t, err == nil
    }

    // DeleteCover removes the cover of a game's edition and its missing marker, and the cover from
    // before editions, which the main edition would otherwise adopt instead of the new one.
    func (s *Store) DeleteCover(id game.ID, system string) error {
    	dir, ok := s.dir(id)
    	if !ok {
    		return nil
    	}

    	for _, name := range []string{coverFile(system) + ".missing", legacyMissing} {
    		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
    			return err
    		}
    	}

    	if err := removeImage(dir, legacyCover); err != nil {
    		return err
    	}

    	return removeImage(dir, coverFile(system))
    }

    // DeleteCovers removes the covers and missing markers of every edition of a game.
    func (s *Store) DeleteCovers(id game.ID) error {
    	dir, ok := s.dir(id)
    	if !ok {
    		return nil
    	}

    	entries, err := os.ReadDir(dir)
    	if err != nil {
    		return err
    	}

    	for _, e := range entries {
    		if isCoverName(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) {
    			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
    				return err
    			}
    		}
    	}

    	return nil
    }

    // AdoptLegacyCover makes the cover stored before editions (cover.jpg) the cover of the edition
    // on system, and reports whether there was one. The main edition adopts it, so upgrading does not
    // download every cover again.
    func (s *Store) AdoptLegacyCover(g media.GameRef, system string) (media.Image, bool, error) {
    	dir, ok := s.dir(g.ID)
    	if !ok {
    		return media.Image{}, false, nil
    	}

    	img, ok, err := readImage(dir, legacyCover)
    	if err != nil || !ok {
    		return media.Image{}, false, err
    	}

    	if err := s.PutCover(g, system, img); err != nil {
    		return media.Image{}, false, err
    	}

    	_ = os.Remove(filepath.Join(dir, legacyMissing)) // absent is fine

    	return img, true, removeImage(dir, legacyCover)
    }
    ```
  - Sheet images: in `GetAsset`, `PutAsset` and `SetAssetSources` replace `name == coverName` with `isCoverName(name)`; in the pruning loop of `SetAssetSources` replace `name != coverName` with `!isCoverName(name)`.

Run: `task go -- test ./internal/adapters/outbound/gamedata/`
Expected: `ok`.

- [ ] **Step 3: Failing media test** (`internal/application/media/editions_test.go`)

```go
package media_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// pcStore has art for PC editions of games linked to Steam, like the Steam store.
type pcStore struct{}

func (pcStore) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "pc-store", Kind: provider.KindCover, Name: "PC store", EnabledByDefault: true, DefaultOrder: 10}
}

func (pcStore) Test(context.Context, schema.Settings) error { return nil }

func (pcStore) Applies(q media.CoverQuery) bool { return q.ForPC() && q.Links[game.LinkSteam] != "" }

func (pcStore) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{{URL: "https://img.test/steam-" + q.Links[game.LinkSteam], Provider: "pc-store"}}, nil
}

// boxArt has box art for every console except the Wii, and none for add-ons; it records the
// systems it is asked for.
type boxArt struct{ asked []string }

func (*boxArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "boxart", Kind: provider.KindCover, Name: "Box art", EnabledByDefault: true, DefaultOrder: 20}
}

func (*boxArt) Test(context.Context, schema.Settings) error { return nil }

func (*boxArt) Applies(q media.CoverQuery) bool { return q.System != "" && q.System != game.SystemPC }

func (b *boxArt) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	b.asked = append(b.asked, q.System)
	if q.System == "Wii" || media.IsAddOn(q.Title) {
		return nil, nil
	}

	return []media.CoverCandidate{{URL: "https://img.test/box-" + q.System, Provider: "boxart"}}, nil
}

// echo "downloads" an image whose bytes are its URL, so a test sees which image a cover is.
type echo struct{}

func (echo) Fetch(_ context.Context, url string) (media.Image, error) {
	return media.Image{Data: []byte(url), ContentType: "image/png"}, nil
}

type mediaEnv struct {
	svc    *media.Service
	games  game.Repository
	assets *gamedata.Store
	box    *boxArt
}

func newMediaEnv(t *testing.T, ctx context.Context) mediaEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	games, box := sqlite.NewGameRepository(db), &boxArt{}
	svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, nil, echo{}, time.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{Covers: []media.CoverProvider{pcStore{}, box}})

	return mediaEnv{svc: svc, games: games, assets: assets, box: box}
}

// saveGame stores a game with one copy per details.
func saveGame(t *testing.T, ctx context.Context, games game.Repository, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, time.Now())
	require.NoError(t, err)

	_, err = g.UpdateInfo(game.Info{Title: title, Links: links}, time.Now())
	require.NoError(t, err)

	for _, d := range copies {
		_, err := g.AddCopy(d, time.Now())
		require.NoError(t, err)
	}

	require.NoError(t, games.Save(ctx, g))

	return g
}

func cover(t *testing.T, img media.Image, err error) string {
	t.Helper()
	require.NoError(t, err)

	return string(img.Data)
}

func TestEditionCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game linked to Steam with a Steam copy, a PS3 disc and a Wii disc", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", game.Links{game.LinkSteam: "7"},
			game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"},
			game.CopyDetails{Kind: game.KindPhysical, Platform: "PS3"},
			game.CopyDetails{Kind: game.KindPhysical, Platform: "Wii"})

		t.Run("WHEN each edition's cover is asked", func(t *testing.T) {
			pc, pcErr := env.svc.EditionCover(ctx, g.ID(), "PC")
			ps3, ps3Err := env.svc.EditionCover(ctx, g.ID(), "PS3")
			_, wiiErr := env.svc.EditionCover(ctx, g.ID(), "Wii")

			t.Run("THEN each edition has its own box, and box art was never asked for the PC edition", func(t *testing.T) {
				assert.Equal(t, "https://img.test/steam-7", cover(t, pc, pcErr))
				assert.Equal(t, "https://img.test/box-PS3", cover(t, ps3, ps3Err))
				assert.ErrorIs(t, wiiErr, media.ErrNoCover)
				assert.Equal(t, []string{"PS3", "Wii"}, env.box.asked)
			})

			t.Run("AND the main edition's cover is the PS3 one (a disc, first by system)", func(t *testing.T) {
				img, err := env.svc.Cover(ctx, g.ID())
				assert.Equal(t, "https://img.test/box-PS3", cover(t, img, err))
			})

			t.Run("AND asking again is served from the cache, and the Wii miss is remembered", func(t *testing.T) {
				_, _ = env.svc.EditionCover(ctx, g.ID(), "PS3")
				_, err := env.svc.EditionCover(ctx, g.ID(), "Wii")
				assert.ErrorIs(t, err, media.ErrNoCover)
				assert.Len(t, env.box.asked, 2)
			})

			t.Run("AND a system the game has no copy on has no cover, and nobody is asked", func(t *testing.T) {
				_, err := env.svc.EditionCover(ctx, g.ID(), "Switch")
				assert.ErrorIs(t, err, media.ErrNoCover)
				assert.Len(t, env.box.asked, 2)
			})

			t.Run("AND refreshing missing covers forgets only the Wii miss", func(t *testing.T) {
				n, err := env.svc.RefreshCovers(ctx, true)
				require.NoError(t, err)
				assert.Equal(t, 1, n)

				_, _ = env.svc.EditionCover(ctx, g.ID(), "Wii")
				_, _ = env.svc.EditionCover(ctx, g.ID(), "PS3")
				assert.Equal(t, []string{"PS3", "Wii", "Wii"}, env.box.asked)
			})
		})

		t.Run("WHEN the Wii edition gets a chosen cover", func(t *testing.T) {
			got, err := env.games.Get(ctx, g.ID())
			require.NoError(t, err)
			require.NoError(t, got.SetEditionCover("Wii", game.EditionCover{URL: "https://img.test/chosen"}, time.Now()))
			require.NoError(t, env.games.Save(ctx, got))
			require.NoError(t, env.svc.InvalidateEdition(ctx, g.ID(), "Wii"))

			t.Run("THEN that edition shows it and the others keep their cached box", func(t *testing.T) {
				wii, err := env.svc.EditionCover(ctx, g.ID(), "Wii")
				assert.Equal(t, "https://img.test/chosen", cover(t, wii, err))

				ps3, err := env.svc.EditionCover(ctx, g.ID(), "PS3")
				assert.Equal(t, "https://img.test/box-PS3", cover(t, ps3, err))
				assert.Len(t, env.box.asked, 3)
			})
		})

		t.Run("WHEN the candidates of the PC edition are listed THEN only the PC store proposes", func(t *testing.T) {
			candidates, _, err := env.svc.CoverCandidates(ctx, g.ID(), "PC")
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			assert.Equal(t, provider.ID("pc-store"), candidates[0].Provider)
		})

		t.Run("WHEN the candidates of a system the game has no copy on are listed THEN it is an error", func(t *testing.T) {
			_, _, err := env.svc.CoverCandidates(ctx, g.ID(), "Switch")
			assert.ErrorIs(t, err, media.ErrNoEdition)
		})
	})

	t.Run("GIVEN an add-on on PS3 nobody has art for, and its base game on PS3", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		saveGame(t, ctx, env.games, "Halo 3", nil, game.CopyDetails{Kind: game.KindPhysical, Platform: "PS3"})
		dlc := saveGame(t, ctx, env.games, "Halo 3 Map Pack DLC", nil, game.CopyDetails{Kind: game.KindKey, Platform: "PS3"})

		t.Run("THEN the add-on borrows the base game's PS3 box", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, dlc.ID(), "PS3")
			assert.Equal(t, "https://img.test/box-PS3", cover(t, img, err))
		})
	})

	t.Run("GIVEN a cover cached before editions for a game with a PS3 disc and a Steam copy", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", game.Links{game.LinkSteam: "7"},
			game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"},
			game.CopyDetails{Kind: game.KindPhysical, Platform: "PS3"})

		legacy := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID())+".jpg"), []byte("legacy"), 0o600))
		_, err := env.assets.MigrateLegacyCovers(legacy, map[game.ID]string{g.ID(): g.Title()})
		require.NoError(t, err)

		t.Run("THEN the main edition adopts it without asking anyone, and the PC edition resolves its own", func(t *testing.T) {
			main, err := env.svc.Cover(ctx, g.ID())
			assert.Equal(t, "legacy", cover(t, main, err))
			assert.Empty(t, env.box.asked)

			pc, err := env.svc.EditionCover(ctx, g.ID(), "PC")
			assert.Equal(t, "https://img.test/steam-7", cover(t, pc, err))
		})
	})
}
```

Run: `task go -- test ./internal/application/media/ -run TestEditionCovers`
Expected: FAIL to compile (`undefined: media.ErrNoEdition`, `q.ForPC undefined`).

- [ ] **Step 4: Queries per edition** (`internal/application/media/service.go`)
  - Bump the marker date to the moment you make the change (`date -u`), never later, with the reason:

    ```go
    var coverLogicChanged = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) // covers per edition (system)
    ```
  - `CoverQuery`: add, after `Title`,

    ```go
    	// System is the edition's system ("PC", "PS3"…); empty when the query names none (a game
    	// without copies, a title search, a game sheet).
    	System string
    ```
    and the method

    ```go
    // ForPC reports whether PC store art fits the query: its system is PC, or it names none. Store
    // providers apply only then.
    func (q CoverQuery) ForPC() bool { return q.System == "" || q.System == game.SystemPC }
    ```
  - `QueryFor`:

    ```go
    // QueryFor builds the cover query of a game's edition on system, from that edition's copies. An
    // empty system takes every copy and names no system (games without copies, game sheets).
    func QueryFor(g *game.Game, system string) CoverQuery {
    	q := CoverQuery{
    		Title:  g.Title(),
    		System: system,
    		Links:  g.Links(),
    	}
    	for _, c := range g.Copies() {
    		if c.Platform == "" || (system != "" && c.System() != system) {
    			continue
    		}

    		if c.Kind == game.KindPhysical && !slices.Contains(q.PhysicalPlatforms, c.Platform) {
    			q.PhysicalPlatforms = append(q.PhysicalPlatforms, c.Platform)
    		}

    		if !slices.Contains(q.Platforms, c.Platform) {
    			q.Platforms = append(q.Platforms, c.Platform)
    		}
    	}

    	return q
    }
    ```
  - `details.go`: `q := QueryFor(g, "") // a game's sheet describes it on every system`.
  - `suggest`: inside `if platform != "" {`, also set `q.System = game.SystemOf(platform)`.
  - `AssetStore`: replace the cover methods with the signatures of the Interfaces block, each with its doc comment:

    ```go
    	// GetCover returns the stored cover of a game's edition, and whether there is one.
    	GetCover(id game.ID, system string) (Image, bool, error)

    	// PutCover stores the cover of a game's edition, replacing the previous one.
    	PutCover(g GameRef, system string, img Image) error

    	// MarkCoverMissing remembers that no cover was found for an edition, so the lookup is not
    	// repeated on every request.
    	MarkCoverMissing(g GameRef, system string, at time.Time) error

    	// CoverMissingSince returns when no cover was last found for an edition, and whether that was
    	// recorded.
    	CoverMissingSince(id game.ID, system string) (time.Time, bool)

    	// DeleteCover removes an edition's stored cover and "no cover found" marker.
    	DeleteCover(id game.ID, system string) error

    	// DeleteCovers removes the stored covers and markers of every edition of a game.
    	DeleteCovers(id game.ID) error

    	// AdoptLegacyCover makes the cover stored before editions the cover of the edition on system,
    	// and reports whether there was one.
    	AdoptLegacyCover(g GameRef, system string) (Image, bool, error)
    ```
  - Add the sentinel next to `ErrNoCover`:

    ```go
    // ErrNoEdition means the game has no copy on the system asked for.
    var ErrNoEdition = errors.New("the game has no copy on that system: reload the page")
    ```

- [ ] **Step 5: Resolution per edition** (`service.go`). Replace `Cover` and `resolve`, and thread `system` through `coverFromChain` and `fetchAndCache`:

```go
// Cover returns the cover of the game's main edition (see game.Game.MainEdition).
func (s *Service) Cover(ctx context.Context, id game.ID) (Image, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}

	return s.EditionCover(ctx, id, g.MainEdition().System)
}

// EditionCover returns the cover of the game's edition on system, resolving and caching it on first
// use. The empty system is the cover of a game without copies.
func (s *Service) EditionCover(ctx context.Context, id game.ID, system string) (Image, error) {
	if img, ok, err := s.store.GetCover(id, system); err != nil || ok {
		return img, err
	}

	if since, ok := s.store.CoverMissingSince(id, system); ok && since.After(coverLogicChanged) && s.now().Sub(since) < s.retryMissing {
		return Image{}, ErrNoCover
	}
	// Concurrent requests for the same cover share one lookup.
	v, err, _ := s.group.Do(string(id)+"\x00"+system, func() (any, error) {
		return s.resolve(context.WithoutCancel(ctx), id, system)
	})
	if err != nil {
		return Image{}, err
	}

	return v.(Image), nil
}

// resolve finds an edition's cover: the cover cached before editions (main edition only), the
// chosen photo, the chosen URL, the provider chain with its fallback pass, the base game's cover for
// the same system (add-ons), else "missing".
func (s *Service) resolve(ctx context.Context, id game.ID, system string) (Image, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}

	edition, ok := g.Edition(system)
	if !ok && (system != "" || len(g.Copies()) > 0) {
		return Image{}, ErrNoCover // no copy on that system (any more)
	}

	ref := GameRef{g.ID(), g.Title()}

	if system == g.MainEdition().System {
		if img, ok, err := s.store.AdoptLegacyCover(ref, system); err != nil {
			s.log.Warn("adopting the cover cached before editions", "game", g.Title(), "error", err)
		} else if ok {
			return img, nil
		}
	}

	// A photo of the user's own copy chosen as the cover wins over everything.
	if id := edition.Cover.Photo; id != "" && s.photos != nil {
		img, err := s.photos.Open(id, false)
		if err == nil {
			if err := s.store.PutCover(ref, system, img); err != nil {
				return Image{}, fmt.Errorf("caching cover: %w", err)
			}

			return img, nil
		}

		s.log.Warn("cover photo unavailable, falling back", "game", g.Title(), "system", system, "error", err)
	}

	// A cover the user picked or pasted comes next.
	if u := edition.Cover.URL; u != "" {
		s.slots <- struct{}{}

		img, err := s.fetchAndCache(ctx, ref, system, u)
		<-s.slots

		if err == nil {
			return img, nil
		}

		s.log.Warn("chosen cover failed, falling back to providers", "game", g.Title(), "system", system, "url", u)
	}

	q := QueryFor(g, system)
	asked := map[provider.ID]bool{}

	img, err := s.coverFromChain(ctx, ref, system, q, asked)
	if errors.Is(err, ErrNoCover) && q.HasStoreLink() {
		// No provider that knows the game's stores had art for this edition (a game only on
		// Battle.net, a console edition of a Steam game…): ask the ones that kept out of it.
		q.Fallback = true
		img, err = s.coverFromChain(ctx, ref, system, q, asked)
	}

	if err == nil {
		return img, nil
	}

	if !errors.Is(err, ErrNoCover) {
		return Image{}, err
	}
	// Add-ons (DLC, soundtracks…) rarely have art of their own: borrow the base game's.
	if img, ok := s.addOnCover(ctx, g, system); ok {
		return img, nil
	}

	if len(asked) > 0 {
		s.log.Info("no cover found", "game", g.Title(), "system", system)

		if err := s.store.MarkCoverMissing(ref, system, s.now()); err != nil {
			s.log.Warn("marking missing cover", "error", err)
		}
	}

	return Image{}, ErrNoCover
}
```

  `coverFromChain(ctx, ref GameRef, system string, q CoverQuery, asked map[provider.ID]bool)` and `fetchAndCache(ctx, g GameRef, system, url string)` gain the `system` parameter and pass it to `PutCover(g, system, img)`; their bodies are otherwise unchanged.

- [ ] **Step 6: Candidates, refresh and invalidation** (`service.go`)

```go
// CoverCandidates asks every enabled provider that applies to the game's edition on system (the
// main edition when system is empty) for images, for the "choose cover" dialog. Failing providers
// are reported as warnings.
func (s *Service) CoverCandidates(ctx context.Context, id game.ID, system string) ([]CoverCandidate, []string, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if system == "" {
		system = g.MainEdition().System
	}

	if _, ok := g.Edition(system); !ok && len(g.Copies()) > 0 {
		return nil, nil, fmt.Errorf("%w (%s)", ErrNoEdition, system)
	}

	chain, err := s.chain(ctx)
	if err != nil {
		return nil, nil, err
	}

	q := QueryFor(g, system)
	var (
		out      []CoverCandidate
		warnings []string
	)

	for _, p := range chain {
		impl := s.covers[p.ID()]
		if !impl.Applies(q) {
			continue
		}

		c, err := impl.Covers(ctx, q, p.Settings())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			continue
		}

		out = append(out, c...)
	}

	return out, warnings, nil
}

// RefreshCovers drops cached covers so they are resolved again on next view. With missingOnly, only
// the editions' "not found" markers are dropped (cheap: real covers stay cached). It returns how many
// games were affected.
func (s *Service) RefreshCovers(ctx context.Context, missingOnly bool) (int, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return 0, err
	}

	n := 0

	for _, g := range games {
		if !missingOnly {
			if err := s.store.DeleteCovers(g.ID()); err != nil {
				return n, err
			}

			n++

			continue
		}

		systems := g.Systems()
		if len(systems) == 0 {
			systems = []string{""}
		}

		dropped := false

		for _, system := range systems {
			if _, missing := s.store.CoverMissingSince(g.ID(), system); !missing {
				continue
			}

			if err := s.store.DeleteCover(g.ID(), system); err != nil {
				return n, err
			}

			dropped = true
		}

		if dropped {
			n++
		}
	}

	return n, nil
}

// InvalidateEdition drops the cached cover of a game's edition (implements catalog.CoverCache and
// sync.CoverCache): its chosen cover or its copies changed.
func (s *Service) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	return s.store.DeleteCover(id, system)
}
```

- [ ] **Step 7: Add-ons per system** (`internal/application/media/addon.go`)

```go
// addOnCover finds a cover for an add-on's edition nobody has art for: the base game's cover for
// the same system, from the catalog when you own the base game on it, else from a store after
// finding the base game in its catalog by title.
func (s *Service) addOnCover(ctx context.Context, g *game.Game, system string) (Image, bool) {
	if !IsAddOn(g.Title()) {
		return Image{}, false
	}

	ref := GameRef{g.ID(), g.Title()}
	if catalog, err := s.games.List(ctx); err == nil {
		if base := baseGameOf(g, catalog); base != nil {
			if img, ok := s.baseEditionCover(ctx, base, system); ok {
				if err := s.store.PutCover(ref, system, img); err == nil {
					s.log.Info("add-on cover taken from its base game", "game", g.Title(), "base", base.Title(), "system", system)
					return img, true
				}
			}
		}
	}

	for _, ls := range s.searchers {
		if img, ok := s.baseCoverIn(ctx, ls, g, ref, system); ok {
			return img, true
		}
	}

	return Image{}, false
}

// baseEditionCover is the base game's cover on system (its main edition's when system is empty),
// when the base game has a copy on it.
func (s *Service) baseEditionCover(ctx context.Context, base *game.Game, system string) (Image, bool) {
	if system == "" {
		system = base.MainEdition().System
	}

	if _, ok := base.Edition(system); !ok && system != "" {
		return Image{}, false
	}

	img, err := s.EditionCover(ctx, base.ID(), system)

	return img, err == nil
}
```

  `baseCoverIn(ctx, ls, g, ref, system string)`: set `System: system` in its `CoverQuery` literal (store providers then only answer for PC or an empty system) and call `s.coverFromChain(ctx, ref, system, cq, map[provider.ID]bool{})`.

- [ ] **Step 8: Each provider decides by system**
  - `steam/store.go`: `func (s *Store) Applies(q media.CoverQuery) bool { return q.ForPC() && AppIDOf(q) != 0 }` with the comment "Applies reports whether the game is linked to Steam and the query is for its PC edition (or names no system)." `steam/details.go` stays as it is: game sheets are not per system.
  - `gog/covers.go`, `epic/covers.go`, `eaapp/covers.go`, `battlenet/covers.go`, `example/covers.go`: `return q.ForPC() && q.Links[LinkedStore.Key] != ""`, and append "…, for its PC edition" to each doc comment.
  - `ubisoft/covers.go`: start `Applies` with `if !q.ForPC() { return false }`.
  - `xbox/covers.go`:

    ```go
    // xboxSystems are the consoles whose games the Microsoft Store catalog has art for.
    var xboxSystems = []string{"Xbox Series", "Xbox One", "Xbox 360", "Xbox"}

    // IsXboxSystem reports whether system is an Xbox console.
    func IsXboxSystem(system string) bool { return slices.Contains(xboxSystems, system) }

    // Applies reports whether the game is linked to the Microsoft Store and the query is for its PC
    // edition, an Xbox edition, or names no system.
    func (c *Covers) Applies(q media.CoverQuery) bool {
    	return (q.ForPC() || IsXboxSystem(q.System)) && q.Links[LinkedStore.Key] != ""
    }
    ```
  - `thegamesdb/provider.go`: `Applies` keeps its quota rule (it is now evaluated per edition: `HasPhysical` looks at the edition's copies). In `Covers`, search the edition's system's box art:

    ```go
    	platforms := q.PhysicalPlatforms
    	if len(platforms) == 0 {
    		platforms = q.Platforms
    	}

    	if q.System != "" {
    		platforms = []string{q.System} // an edition's box art: its system names TheGamesDB's platform
    	}
    ```
    Update the doc comment: "Covers searches the game by title on the edition's system (or, without one, the platforms of its physical copies or all its platforms) and returns front box art, exact title matches first."
  - In `internal/adapters/inbound/rpc/server_test.go`, the fake `fakeSteamStore.Applies` becomes `return q.ForPC() && q.Links[game.LinkSteam] != ""`.

- [ ] **Step 9: Provider tests by system.** Create `applies_test.go` in each package of the table, with this content, substituting the three placeholders `<pkg>`, `<provider>` and `<link>` from the table (the code is otherwise identical):

```go
package <pkg>

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

func TestCovers_appliesBySystem(t *testing.T) {
	p := <provider>
	linked := game.Links{<link>: "1"}

	for name, tc := range map[string]struct {
		q    media.CoverQuery
		want bool
	}{
		"its PC edition":                    {media.CoverQuery{System: game.SystemPC, Links: linked}, true},
		"a game without copies":             {media.CoverQuery{Links: linked}, true},
		"its PS3 edition":                   {media.CoverQuery{System: "PS3", Links: linked}, false},
		"a PC edition not linked to it":     {media.CoverQuery{System: game.SystemPC}, false},
	} {
		assert.Equal(t, tc.want, p.Applies(tc.q), name)
	}
}
```

| Directory | `<pkg>` | `<provider>` | `<link>` |
|---|---|---|---|
| `steam` | `steam` | `&Store{}` | `game.LinkSteam` |
| `gog` | `gog` | `&Covers{}` | `LinkedStore.Key` |
| `epic` | `epic` | `&Covers{}` | `LinkedStore.Key` |
| `eaapp` | `eaapp` | `&Covers{}` | `LinkedStore.Key` |
| `battlenet` | `battlenet` | `&Covers{}` | `LinkedStore.Key` |
| `ubisoft` | `ubisoft` | `&Covers{}` | `LinkedStore.Key` |
| `xbox` | `xbox` | `&Covers{}` | `LinkedStore.Key` |
| `example` | `example` | `&Covers{}` | `LinkedStore.Key` |

  Check each package name with `head -1 internal/adapters/outbound/<dir>/covers.go` (Steam's is in `store.go`). In `xbox/applies_test.go` add two rows: `"its Xbox 360 edition": {media.CoverQuery{System: "Xbox 360", Links: linked}, true}` and keep `"its PS3 edition"` false. In `ubisoft/applies_test.go` add `"its PC edition by a Ubisoft Connect copy": {media.CoverQuery{System: game.SystemPC, Platforms: []string{Platform}}, true}` and `"its PS4 edition by a Ubisoft Connect copy": {media.CoverQuery{System: "PS4", Platforms: []string{Platform}}, false}`.

  In `thegamesdb/provider_test.go` append:

```go
func TestCovers_editionSystem(t *testing.T) {
	srv, calls := fakeServer(t)
	p := testProvider(srv)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the digital PS3 edition of a game linked to Steam", func(t *testing.T) {
		q := media.CoverQuery{Title: "Halo 3", System: "PS3", Platforms: []string{"PlayStation Store"}, Links: game.Links{game.LinkSteam: "1"}}

		t.Run("THEN it keeps its quota on the first pass and helps on the fallback pass", func(t *testing.T) {
			assert.False(t, p.Applies(q))
			q.Fallback = true
			assert.True(t, p.Applies(q))
		})

		t.Run("WHEN its covers are searched THEN the search is filtered to the edition's system", func(t *testing.T) {
			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})
			require.NoError(t, err)
			assert.Contains(t, (*calls)[len(*calls)-1], "filter%5Bplatform%5D=12")
		})
	})
}
```
  (add `time`, `assert` and `require` to the imports.)

- [ ] **Step 10: Endpoints** (`internal/adapters/inbound/rpc/media_handler.go`, `server.go`, `media_rpc_handler.go`, `mapper.go`)
  - Replace `coverHandler` with a handler for both routes:

    ```go
    // coverHandler serves GET /media/covers/{id} (the main edition's cover) and
    // GET /media/covers/{id}/{system} (an edition's; the system is URL-escaped, so it may contain
    // spaces or slashes). Images are plain HTTP rather than RPC so browsers can load them with
    // <img src> and cache them. Clients add ?v=<updatedAt> to bust the cache.
    func coverHandler(m *media.Service) http.Handler {
    	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    		id := game.ID(r.PathValue("id"))

    		var (
    			img media.Image
    			err error
    		)

    		if system := r.PathValue("system"); system != "" {
    			img, err = m.EditionCover(r.Context(), id, system)
    		} else {
    			img, err = m.Cover(r.Context(), id)
    		}

    		switch {
    		case errors.Is(err, media.ErrNoCover), errors.Is(err, game.ErrGameNotFound):
    			w.Header().Set("Cache-Control", "no-store") // a cover may be found later: never cache the miss
    			http.Error(w, "no cover", http.StatusNotFound)

    			return
    		case err != nil:
    			http.Error(w, "cover unavailable", http.StatusBadGateway)
    			return
    		}

    		w.Header().Set("Content-Type", img.ContentType)
    		w.Header().Set("Cache-Control", "private, max-age=604800")
    		w.Header().Set("X-Content-Type-Options", "nosniff")
    		w.Write(img.Data)
    	})
    }
    ```
  - `server.go`: next to `mux.Handle("GET /media/covers/{id}", coverHandler(h.Media))` add `mux.Handle("GET /media/covers/{id}/{system...}", coverHandler(h.Media))`. The remainder wildcard takes the whole rest of the path, so a system with an escaped slash is one value however the mux treats `%2F`; `PathValue` returns it unescaped.
  - `media_rpc_handler.go`: `h.media.CoverCandidates(ctx, game.ID(req.Msg.GameId), "")` (the request names no system until Task 5; empty means the main edition).
  - `mapper.go` `toConnectError`: map `errors.Is(err, media.ErrNoEdition)` to `connect.CodeInvalidArgument`, next to the other invalid-argument cases.
  - `internal/application/catalog/service.go`: in `SetMainSystem`, delete the `// Bridge (Task 3)` invalidation (covers are cached per edition now; the main one changes nothing cached), so it is a plain `return s.mutate(…)`.

- [ ] **Step 11: RPC tests**
  - `server_test.go` `TestCoverProviderChain`: the Portal 2 game has only a PS3 disc, so Steam no longer proposes for it. Replace the candidates check with

    ```go
    	// A PS3 disc of a Steam game: its edition is PS3, so only box art proposes; Steam is for its PC edition.
    	cands, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Msg.Game.Id}))
    	if err != nil || len(cands.Msg.Candidates) != 1 || cands.Msg.Candidates[0].ProviderName != "Box art" {
    		t.Fatalf("candidates: %+v %v", cands, err)
    	}
    ```
    and pin `cands.Msg.Candidates[0].Url` instead of `[1]` in the `UpdateGame` call below it.
  - Create `internal/adapters/inbound/rpc/covers_test.go`:

```go
package rpc_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

// getMedia fetches a media URL and returns its status and body.
func getMedia(t *testing.T, url string) (int, []byte) {
	t.Helper()

	res, err := http.Get(url)
	require.NoError(t, err)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.StatusCode, body
}

func TestCovers_pcOnlyGame(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game owned only in PC stores: Steam, a Humble Steam key and GOG", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Hades",
			Links: map[string]string{"steam": "1145360"},
			Copies: []*pb.CopyDetails{
				{Kind: pb.CopyKind_COPY_KIND_LIBRARY, Platform: "Steam"},
				{Kind: pb.CopyKind_COPY_KIND_KEY, Platform: "Steam", Status: pb.CopyStatus_COPY_STATUS_REVEALED},
				{Kind: pb.CopyKind_COPY_KIND_LIBRARY, Platform: "GOG"},
			},
		}))
		require.NoError(t, err)

		id := created.Msg.Game.Id

		t.Run("WHEN its covers are asked at both addresses", func(t *testing.T) {
			mainStatus, main := getMedia(t, c.baseURL+"/media/covers/"+id)
			pcStatus, pc := getMedia(t, c.baseURL+"/media/covers/"+id+"/PC")

			t.Run("THEN both are Steam's art, as before editions, and box art was never asked", func(t *testing.T) {
				assert.Equal(t, http.StatusOK, mainStatus)
				assert.Equal(t, http.StatusOK, pcStatus)
				assert.Equal(t, png1x1, main)
				assert.Equal(t, png1x1, pc)
				assert.Zero(t, c.boxart.asked)
			})
		})

		t.Run("WHEN a system it has no copy on is asked THEN it is a 404 that must not be cached", func(t *testing.T) {
			res, err := http.Get(c.baseURL + "/media/covers/" + id + "/PS3")
			require.NoError(t, err)
			res.Body.Close()
			assert.Equal(t, http.StatusNotFound, res.StatusCode)
			assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
		})
	})
}
```

- [ ] **Step 12: Run everything**

Run: `task go -- test ./internal/...`
Expected: `ok` for every package. `grep -rn "Bridge (Task 3)" internal` prints nothing.

- [ ] **Step 13: Lint and commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green).

```bash
git add internal
git commit -m "Covers: resolved, cached and served per edition; providers apply by system"
```

---
### Task 4: Drop only the changed editions' covers (catalog, sync) and the PlayStation source's system

**Files:**
- Modify: `internal/application/catalog/service.go`, `internal/application/catalog/service_test.go`, `internal/application/sync/service.go`, `internal/application/sync/exclusions.go`, `internal/application/sync/covers_test.go`, `internal/adapters/outbound/playstation/provider.go`, `internal/adapters/outbound/playstation/provider_test.go`

**Interfaces:**
- Consumes: Task 2 `CoverFingerprints()`, `ChangedSystems`; Task 3 `(*media.Service).InvalidateEdition`; Task 1 `ImportedCopy.System`.
- Produces:
  - `catalog.CoverCache` and `sync.CoverCache` gain `InvalidateEdition(ctx context.Context, id game.ID, system string) error` (implemented by `*media.Service` since Task 3)

- [ ] **Step 1: Failing catalog test.** In `internal/application/catalog/service_test.go`:
  - The fake records editions too:

```go
// covers records the games whose cached cover and details were dropped, and the editions whose
// cached cover was dropped ("<game id>|<system>").
type covers struct {
	invalidated []game.ID
	editions    []string
}

func (c *covers) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	c.editions = append(c.editions, string(id)+"|"+system)
	return nil
}
```
  (keep its `Invalidate` method.)
  - In `TestEditionCoverCache`: in `setup`, replace the assertion with `assert.Equal(t, []string{string(g.ID()) + "|PS3"}, cache.editions, "choosing a cover drops that edition's cached one")` and reset `cache.editions = nil` instead of `cache.invalidated`. In the cases' THEN, replace the `cache.invalidated` assertion with

```go
					assert.Contains(t, cache.editions, string(g.ID())+"|PS3")
					assert.Empty(t, cache.invalidated, "the game's details stay")
```
  - Append inside the GIVEN of `TestEditionCoverCache`:

```go
		t.Run("WHEN an Xbox 360 disc is added THEN only the new edition's cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{Kind: game.KindPhysical, Platform: "Xbox 360"}, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{string(g.ID()) + "|Xbox 360"}, cache.editions)
		})

		t.Run("WHEN a disc's notes change THEN no cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			c := g.Copies()[1]
			d := c.CopyDetails
			d.Notes = "signed"
			_, err := svc.UpdateCopy(ctx, g.ID(), c.ID, d, nil)
			require.NoError(t, err)
			assert.Empty(t, cache.editions)
		})

		t.Run("WHEN an override moves the second disc to PS4 THEN the PS3 and PS4 covers are dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			c := g.Copies()[1]
			d := c.CopyDetails
			d.System = "PS4"
			_, err := svc.UpdateCopy(ctx, g.ID(), c.ID, d, nil)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{string(g.ID()) + "|PS3", string(g.ID()) + "|PS4"}, cache.editions)
		})

		t.Run("WHEN the main edition is chosen THEN no cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			_, err := svc.SetMainSystem(ctx, g.ID(), "PS3")
			require.NoError(t, err)
			assert.Empty(t, cache.editions)
			assert.Empty(t, cache.invalidated)
		})
```
  (Remove the older "WHEN the main edition is chosen THEN the game keeps it" case: this one replaces it, and add `assert.Equal(t, "PS3", got.MainSystem())` to it by keeping the returned game as `got`.)

Run: `task go -- test ./internal/application/catalog/ -run TestEditionCoverCache`
Expected: FAIL (`cache.editions` is empty: the catalog still drops whole games).

- [ ] **Step 2: Per-edition invalidation in the catalog** (`internal/application/catalog/service.go`)
  - The port:

```go
// CoverCache is the port used to drop cached covers when they may have changed.
type CoverCache interface {
	// Invalidate drops the game's cached covers and details, so the next request resolves them again.
	Invalidate(ctx context.Context, id game.ID) error

	// InvalidateEdition drops the cached cover of the game's edition on system.
	InvalidateEdition(ctx context.Context, id game.ID, system string) error
}
```
  - Helpers:

```go
// invalidateEditions drops the cached covers of the editions whose fingerprints differ (see
// game.Game.CoverFingerprints). Best effort, like invalidateCover.
func (s *Service) invalidateEditions(ctx context.Context, id game.ID, before, after map[string]string) {
	if s.covers == nil {
		return
	}

	for _, system := range game.ChangedSystems(before, after) {
		_ = s.covers.InvalidateEdition(ctx, id, system)
	}
}
```
  - `mutateEditions` compares fingerprints, and its callback now receives the transaction's context like `mutate`'s (`AddCopy` and `UpdateCopy` read the field definitions with it):

```go
// mutateEditions is mutate, dropping the cached covers of the editions the change touched.
func (s *Service) mutateEditions(ctx context.Context, id game.ID, fn func(context.Context, *game.Game) error) (*game.Game, error) {
	var before map[string]string

	g, err := s.mutate(ctx, id, func(ctx context.Context, g *game.Game) error {
		before = g.CoverFingerprints()
		return fn(ctx, g)
	})
	if err == nil {
		s.invalidateEditions(ctx, id, before, g.CoverFingerprints())
	}

	return g, err
}
```
  - `AddCopy` and `UpdateCopy`: replace `s.mutate(` with `s.mutateEditions(`; their callbacks already have the `(ctx context.Context, g *game.Game)` shape and stay as they are.
  - `SetEditionCover`, `DeleteCopy` and the four photo use cases: their callbacks become `func(_ context.Context, g *game.Game) error { … }` with the same bodies.
  - `MoveCopy`: replace `srcCovers` with `srcPrints map[string]string` (`srcPrints = src.CoverFingerprints()`), and after the transaction:

```go
	if src != nil {
		s.invalidateEditions(ctx, from, srcPrints, src.CoverFingerprints()) // the moved copy may have taken an edition, or its photo cover, along
	}

	if dst != nil {
		s.invalidateEditions(ctx, dst.ID(), dstPrints, dst.CoverFingerprints())
	}
```
    with `dstPrints = dst.CoverFingerprints()` taken right after `dst` is loaded or created (a new game: an empty map). Remove the `maps` import if nothing else uses it.
  - `UpdateGame`, `DeleteGame` and `MergeGames` keep `invalidateCover` (links and merges change every edition and the details).

Run: `task go -- test ./internal/application/catalog/`
Expected: `ok`.

- [ ] **Step 3: Failing sync test.** In `internal/application/sync/covers_test.go`:
  - Add `editions []string` and the same `InvalidateEdition` method to the `covers` fake.
  - `photographCover` returns the system of the photographed copy too (`return g.ID(), g.Copies()[0].System()`); in `TestCoverPhotoLeavesWithTheSourcesCopy`, read `id, system := photographCover(ctx, t, games)`, reset `cache.editions = nil`, and assert `assert.Contains(t, cache.editions, string(id)+"|"+system)` instead of `cache.invalidated`.
  - Append:

```go
// consoleSource is a library source whose game is for PS5, and which also reports the game on Steam
// once steam is set.
type consoleSource struct{ steam bool }

func (*consoleSource) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{Type: "console", Name: "Console store"}
}

func (s *consoleSource) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	copies := []game.ImportedCopy{{
		ExternalID: "psn:1",
		Title:      "Astro Bot",
		System:     "PS5",
		Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: "PlayStation Store", Status: game.StatusOwned},
	}}
	if s.steam {
		copies = append(copies, game.ImportedCopy{
			ExternalID: "steam:1",
			Title:      "Astro Bot",
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam", Status: game.StatusOwned},
		})
	}

	return copies, nil, nil
}

func (*consoleSource) Test(context.Context, source.Settings) error { return nil }

func TestSync_editions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a source whose game is for PS5", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		p := &consoleSource{}
		cache := &covers{}
		svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, cache, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

		v, err := svc.Create(ctx, "console", source.Config{Enabled: true})
		require.NoError(t, err)

		t.Run("WHEN it is scanned", func(t *testing.T) {
			_, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			list, err := games.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)

			g := list[0]

			t.Run("THEN the copy is on PS5, as the source says, and the new edition's cached cover is dropped", func(t *testing.T) {
				assert.Equal(t, "PS5", g.Copies()[0].System())
				assert.Equal(t, []string{string(g.ID()) + "|PS5"}, cache.editions)
			})

			t.Run("WHEN the user moves it to PS4 and the source is scanned again", func(t *testing.T) {
				d := g.Copies()[0].CopyDetails
				d.System = "PS4"
				_, err := g.UpdateCopy(g.Copies()[0].ID, d, time.Now())
				require.NoError(t, err)
				require.NoError(t, games.Save(ctx, g))

				cache.editions = nil
				_, err = svc.Sync(ctx, v.ID())
				require.NoError(t, err)

				got, err := games.Get(ctx, g.ID())
				require.NoError(t, err)

				t.Run("THEN the user's system survives the scan, and no cached cover is dropped", func(t *testing.T) {
					assert.Equal(t, "PS4", got.Copies()[0].System())
					assert.Equal(t, "PS5", got.Copies()[0].SourceSystem)
					assert.Empty(t, cache.editions)
				})
			})

			t.Run("WHEN a scan brings the game on Steam too THEN only the new PC edition's cached cover is dropped", func(t *testing.T) {
				p.steam = true
				cache.editions = nil
				_, err := svc.Sync(ctx, v.ID())
				require.NoError(t, err)
				assert.Equal(t, []string{string(g.ID()) + "|PC"}, cache.editions)
			})
		})
	})
}
```

Run: `task go -- test ./internal/application/sync/ -run 'TestSync_editions|TestCoverPhotoLeavesWithTheSourcesCopy'`
Expected: FAIL to compile until the port has `InvalidateEdition`, then FAIL on `cache.editions`.

- [ ] **Step 4: Per-edition invalidation in sync** (`internal/application/sync/service.go`, `exclusions.go`)
  - The port gets the same second method as the catalog's, with the same doc comments.
  - Replace `invalidateCovers` with:

```go
// staleEditions are the editions of games whose cached covers may be stale: game id → systems.
type staleEditions map[game.ID][]string

// add records the editions of g whose fingerprints changed since before.
func (st staleEditions) add(g *game.Game, before map[string]string) {
	if changed := game.ChangedSystems(before, g.CoverFingerprints()); len(changed) > 0 {
		st[g.ID()] = append(st[g.ID()], changed...)
	}
}

// invalidateEditions drops the cached covers of stale editions. Best effort: a stale cached image
// is not worth failing the use case.
func (s *Service) invalidateEditions(ctx context.Context, stale staleEditions) {
	if s.covers == nil {
		return
	}

	for id, systems := range stale {
		for _, system := range systems {
			_ = s.covers.InvalidateEdition(ctx, id, system)
		}
	}
}
```
  - `Sync`: `stale := staleEditions{}`; the map taken before consolidating becomes `prints := make(map[game.ID]map[string]string, len(games))` filled with `g.CoverFingerprints()`; after each `s.games.Save(ctx, g)`, call `stale.add(g, prints[g.ID()])` (a new game has a nil `before`, so all its editions count). After the transaction, `s.invalidateEditions(ctx, stale)`.
  - `Delete`: before changing each game, `before := g.CoverFingerprints()`; after saving it (not when it was deleted), `stale.add(g, before)`.
  - `exclusions.go` (`ExcludeCopy`): keep the whole-game `s.covers.Invalidate` when the game is deleted (its folder goes); otherwise `before := g.CoverFingerprints()` before `RemoveCopy`, and `s.invalidateEditions(ctx, staleEditions{gameID: game.ChangedSystems(before, g.CoverFingerprints())})` after the transaction. Write the deleted-game case as `_ = s.covers.Invalidate(ctx, gameID)` guarded by `s.covers != nil`.

Run: `task go -- test ./internal/application/sync/`
Expected: `ok`.

- [ ] **Step 5: The PlayStation source says which console** (`internal/adapters/outbound/playstation/provider.go`)

  In `mapTitles`, build the copy with:

```go
		platform := platformName(t.Platform)

		system := platform
		if platform == "PlayStation Store" {
			system = "" // Sony did not say which console: the platform's system applies
		}

		out = append(out, game.ImportedCopy{
			ExternalID: "psn:" + id,
			Links:      game.Links{LinkedStore.Key: id},
			Title:      name,
			System:     system,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: platform,
				Status:   game.StatusOwned,
				Origin:   "PlayStation Store",
			},
		})
```

  Append to `provider_test.go`:

```go
func TestMapTitles_systems(t *testing.T) {
	t.Run("GIVEN titles bought for PS5, for PS4 and for a console Sony names in no known way", func(t *testing.T) {
		got := mapTitles([]title{
			{Name: "Astro Bot", EntitlementID: "e1", Platform: "PS5"},
			{Name: "Gravity Rush", EntitlementID: "e2", Platform: "PS4"},
			{Name: "Old Thing", EntitlementID: "e3", Platform: "PSX"},
		})

		t.Run("THEN the copies say their console, and the unknown one leaves it to the platform", func(t *testing.T) {
			require.Len(t, got, 3)
			assert.Equal(t, "PS5", got[0].System)
			assert.Equal(t, "PS4", got[1].System)
			assert.Empty(t, got[2].System)
			assert.Equal(t, "PlayStation Store", got[2].Details.Platform)
		})
	})
}
```
  Check the `title` struct's field names with `grep -n "type title struct" -A 12 internal/adapters/outbound/playstation/provider.go` and the test file's imports (add `assert` / `require` if missing).

Run: `task go -- test ./internal/adapters/outbound/playstation/`
Expected: `ok`.

- [ ] **Step 6: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green). `grep -rn "Bridge (Task 4)" internal` must print nothing.

```bash
git add internal
git commit -m "Covers: scans and edits drop only the changed editions' covers; PlayStation copies say PS4 or PS5"
```

---
### Task 5: Editions in the API (proto, RPC) and the web on the new API

The proto drops the game-level cover and gains editions; the RPC bridges of Task 2 go. The web is changed only as far as it needs to compile and keep working on the main edition (cover picker, photo "use as cover", Edit tab); Tasks 8 and 9 build the edition UI.

**Files:**
- Create: `internal/adapters/inbound/rpc/editions_test.go`, `web/src/lib/editions.ts`, `web/tests/editions.test.mjs`
- Modify: `proto/gamevault/v1/game.proto`, `proto/gamevault/v1/provider.proto`, `proto/gamevault/v1/lookup.proto` (a comment), generated code (`task generate`), `internal/adapters/inbound/rpc/{mapper.go,game_handler.go,media_rpc_handler.go,server_test.go,photos_test.go}`, `web/src/api/client.ts`, `web/src/components/Cover.tsx`, `web/src/lib/model.ts`, `web/src/features/library/{GameDetail.tsx,CoverPicker.tsx,CopyPhotos.tsx,GameSheet.tsx}`

**Interfaces:**
- Consumes: Task 2 catalog `SetEditionCover`, `SetMainSystem`, `Editions()`, `MainSystem()`; Task 3 `CoverCandidates(ctx, id, system)`, `media.ErrNoEdition`; Task 1 `(Copy).System()`, `CopyDetails.System`.
- Produces:
  - proto `Edition { string system = 1; string cover_url = 2; string cover_photo_id = 3; bool main = 4; }`
  - `Game.editions` (16), `Game.main_system` (17); `Game.cover_url` (8) and `Game.cover_photo_id` (12) reserved
  - `Copy.effective_system` (12); `CopyDetails.system` (17)
  - `CreateGameRequest.cover_url` (5) and `UpdateGameRequest.cover_url` (5) reserved
  - `rpc SetEditionCover(SetEditionCoverRequest) returns (SetEditionCoverResponse)` with `oneof cover { string url = 3; string photo_id = 4; bool clear = 5; }`
  - `rpc SetMainEdition(SetMainEditionRequest) returns (SetMainEditionResponse)`
  - `ListCoverCandidatesRequest.system` (2)
  - `rpc SetCoverPhoto` and its messages removed
  - web: `coverUrl(g: Game, system?: string): string`; `lib/editions.ts`: `mainSystem(g)`, `coverPath(gameId, system)`; `Cover` takes an optional `system`; `CoverPicker({ game, system, onPick, onClose })` with `CoverChoice`

- [ ] **Step 1: Proto** (`proto/gamevault/v1/game.proto`)
  - `CopyDetails`, after `fields = 16`:

    ```proto
      // The system the copy is played on, as the user set it ("PS4", "PC"…); empty: automatic (the
      // source's system, else the one the platform implies). See Copy.effective_system.
      string system = 17;
    ```
  - `Copy`, after `valued_at = 11`:

    ```proto
      // The system the copy is played on: details.system, else its source's, else the one its
      // platform implies. Read only.
      string effective_system = 12;
    ```
  - Before `message Game`, add:

    ```proto
    // Edition is a game on one system: the copies whose effective_system is that system, and its cover.
    // The cover image is served at GET /media/covers/{game_id}/{system} (system URL-escaped).
    message Edition {
      string system = 1;
      // The chosen cover: an image URL, or a photo of one of the edition's copies. Both empty: the
      // cover providers choose.
      string cover_url = 2;
      string cover_photo_id = 3;
      // The edition shown when the game is listed once.
      bool main = 4;
    }
    ```
  - `Game`: `reserved 3, 8, 12;` and `reserved "steam_app_id", "cover_url", "cover_photo_id";` replace the existing reserved lines; delete the `cover_url` and `cover_photo_id` fields with their comments; add after `fields = 15`:

    ```proto
      // One per system of the game's copies: the main edition first, then by system. The main
      // edition's cover is also served at GET /media/covers/{id}.
      repeated Edition editions = 16;
      // The system the user chose for the main edition; empty: the default rule (an edition with a
      // physical copy, then the most copies, then by system).
      string main_system = 17;
    ```
  - `CreateGameRequest` and `UpdateGameRequest`: delete `cover_url = 5` and reserve it (`reserved 2, 5;` / `reserved 3, 5;` and the names `"steam_app_id", "cover_url"`).
  - Replace `SetCoverPhotoRequest` / `SetCoverPhotoResponse` with:

    ```proto
    // SetEditionCoverRequest chooses the cover of a game's edition: an image URL, one of the photos of
    // that edition's copies, or clear (the cover providers choose again).
    message SetEditionCoverRequest {
      string game_id = 1;
      string system = 2;
      oneof cover {
        string url = 3;
        string photo_id = 4;
        bool clear = 5;
      }
    }
    message SetEditionCoverResponse {
      Game game = 1;
    }

    // SetMainEditionRequest makes a game's edition the one shown when the game is listed once; an
    // empty system goes back to the default rule.
    message SetMainEditionRequest {
      string game_id = 1;
      string system = 2;
    }
    message SetMainEditionResponse {
      Game game = 1;
    }
    ```
  - In `service GameService`, replace `rpc SetCoverPhoto(…)` with:

    ```proto
      // Chooses an edition's cover. An unknown system, or a photo of another system's copy, is
      // InvalidArgument with a message that names the system.
      rpc SetEditionCover(SetEditionCoverRequest) returns (SetEditionCoverResponse);
      rpc SetMainEdition(SetMainEditionRequest) returns (SetMainEditionResponse);
    ```
  - `provider.proto`, `ListCoverCandidatesRequest`: add `// The edition's system; empty: the main edition.` and `string system = 2;`. Change the `CoverService` comment "To pin a candidate, set it as the game's cover_url with GameService.UpdateGame." to "To pin a candidate, choose it with GameService.SetEditionCover."
  - `lookup.proto`: "Set it as the new game's cover_url" becomes "Send it as the ScannedCopy's cover_url".

Run: `task generate`
Expected: no errors; `internal/gen` and `web/src/gen` change. The build now fails until Steps 2-6.

- [ ] **Step 2: Failing RPC test** (`internal/adapters/inbound/rpc/editions_test.go`)

```go
package rpc_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func editionSystems(g *pb.Game) []string {
	out := make([]string, 0, len(g.Editions))
	for _, e := range g.Editions {
		out = append(out, e.System)
	}

	return out
}

func TestEditions_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	_, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{Id: "boxart", Enabled: true, Settings: map[string]string{"api_key": "k"}}))
	require.NoError(t, err)

	t.Run("GIVEN a game with a PS3 disc, an Xbox 360 disc and a Steam copy", func(t *testing.T) {
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Halo 3",
			Links: map[string]string{"steam": "620"},
			Copies: []*pb.CopyDetails{
				{Kind: pb.CopyKind_COPY_KIND_PHYSICAL, Platform: "PS3"},
				{Kind: pb.CopyKind_COPY_KIND_PHYSICAL, Platform: "Xbox 360"},
				{Kind: pb.CopyKind_COPY_KIND_LIBRARY, Platform: "Steam"},
			},
		}))
		require.NoError(t, err)

		g := created.Msg.Game
		xbox := g.Copies[1]

		t.Run("THEN it lists an edition per system, the main one first, and each copy's system", func(t *testing.T) {
			assert.Equal(t, []string{"PS3", "PC", "Xbox 360"}, editionSystems(g))
			assert.True(t, g.Editions[0].Main)
			assert.Empty(t, g.MainSystem)
			assert.Equal(t, "PS3", g.Copies[0].EffectiveSystem)
			assert.Equal(t, "PC", g.Copies[2].EffectiveSystem)
		})

		t.Run("WHEN the candidates of each edition are listed THEN each gets its own providers", func(t *testing.T) {
			ps3, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Id, System: "PS3"}))
			require.NoError(t, err)
			require.Len(t, ps3.Msg.Candidates, 1)
			assert.Equal(t, "Box art", ps3.Msg.Candidates[0].ProviderName)

			pc, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Id, System: "PC"}))
			require.NoError(t, err)
			assert.Len(t, pc.Msg.Candidates, 2, "the Steam store's two images")

			_, err = c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Id, System: "Wii"}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})

		t.Run("WHEN the Xbox 360 edition gets a cover and becomes the main one", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id, System: "Xbox 360", Cover: &pb.SetEditionCoverRequest_Url{Url: "https://boxart.test/x360.png"},
			}))
			require.NoError(t, err)

			res, err := c.games.SetMainEdition(ctx, connect.NewRequest(&pb.SetMainEditionRequest{GameId: g.Id, System: "Xbox 360"}))
			require.NoError(t, err)

			t.Run("THEN the game says so", func(t *testing.T) {
				got := res.Msg.Game
				assert.Equal(t, "Xbox 360", got.MainSystem)
				assert.Equal(t, []string{"Xbox 360", "PC", "PS3"}, editionSystems(got))
				assert.Equal(t, "https://boxart.test/x360.png", got.Editions[0].CoverUrl)
			})

			t.Run("AND the main cover address serves that edition's image, also at its escaped address", func(t *testing.T) {
				mainStatus, main := getMedia(t, c.baseURL+"/media/covers/"+g.Id)
				editionStatus, edition := getMedia(t, c.baseURL+"/media/covers/"+g.Id+"/Xbox%20360")
				assert.Equal(t, http.StatusOK, mainStatus)
				assert.Equal(t, http.StatusOK, editionStatus)
				assert.Equal(t, edition, main)
			})
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming it", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id, System: "Wii", Cover: &pb.SetEditionCoverRequest_Clear{Clear: true},
			}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			assert.Contains(t, err.Error(), "Wii")
		})

		t.Run("WHEN a request names no cover THEN it is refused", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{GameId: g.Id, System: "PS3"}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})

		t.Run("WHEN an override moves the Xbox 360 disc to a system typed with a slash", func(t *testing.T) {
			res, err := c.games.UpdateCopy(ctx, connect.NewRequest(&pb.UpdateCopyRequest{
				GameId:  g.Id,
				CopyId:  xbox.Id,
				Details: &pb.CopyDetails{Kind: pb.CopyKind_COPY_KIND_PHYSICAL, Platform: "Xbox 360", System: "Xbox 360/S Slim"},
			}))
			require.NoError(t, err)

			got := res.Msg.Game

			t.Run("THEN the Xbox 360 edition, its cover and the main choice are gone, and the copy is on the typed system", func(t *testing.T) {
				assert.Equal(t, []string{"PS3", "PC", "Xbox 360/S Slim"}, editionSystems(got))
				assert.Empty(t, got.MainSystem)
				assert.Equal(t, "Xbox 360/S Slim", got.Copies[1].Details.System)
				assert.Equal(t, "Xbox 360/S Slim", got.Copies[1].EffectiveSystem)
			})

			t.Run("AND that edition's cover is served at its URL-escaped address", func(t *testing.T) {
				_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
					GameId: g.Id, System: "Xbox 360/S Slim", Cover: &pb.SetEditionCoverRequest_Url{Url: "https://boxart.test/slim.png"},
				}))
				require.NoError(t, err)

				status, body := getMedia(t, c.baseURL+"/media/covers/"+g.Id+"/"+url.PathEscape("Xbox 360/S Slim"))
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, png1x1, body)
			})
		})
	})
}
```

- [ ] **Step 3: Mapper and handlers** (`internal/adapters/inbound/rpc/mapper.go`, `game_handler.go`, `media_rpc_handler.go`)
  - `gameToPB`: delete the two bridged cover lines; add `Editions: editionsToPB(g),` and `MainSystem: g.MainSystem(),`; in each copy, `EffectiveSystem: c.System(),`.
  - `detailsToPB`: `System: d.System,`; `detailsFromPB`: `System: d.System,`.
  - Add:

    ```go
    func editionsToPB(g *game.Game) []*pb.Edition {
    	editions := g.Editions()
    	out := make([]*pb.Edition, 0, len(editions))

    	for _, e := range editions {
    		out = append(out, &pb.Edition{
    			System:       e.System,
    			CoverUrl:     e.Cover.URL,
    			CoverPhotoId: string(e.Cover.Photo),
    			Main:         e.Main,
    		})
    	}

    	return out
    }
    ```
  - `game_handler.go`: delete the bridges (`setMainCoverURL`, its calls in `CreateGame` and `UpdateGame`, and `SetCoverPhoto`), and add:

    ```go
    // SetEditionCover chooses the cover of a game's edition: a URL, a photo of one of its copies, or
    // none (the providers choose).
    func (h *GameHandler) SetEditionCover(ctx context.Context, req *connect.Request[pb.SetEditionCoverRequest]) (*connect.Response[pb.SetEditionCoverResponse], error) {
    	var cover game.EditionCover

    	switch c := req.Msg.Cover.(type) {
    	case *pb.SetEditionCoverRequest_Url:
    		cover.URL = c.Url
    	case *pb.SetEditionCoverRequest_PhotoId:
    		id, err := game.ParsePhotoID(c.PhotoId)
    		if err != nil {
    			return nil, toConnectError(err)
    		}

    		cover.Photo = id
    	case *pb.SetEditionCoverRequest_Clear:
    	default:
    		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("choose a cover, a photo or automatic, then try again"))
    	}

    	g, err := h.catalog.SetEditionCover(ctx, game.ID(req.Msg.GameId), req.Msg.System, cover)

    	return gameResp(g, err, func(g *pb.Game) *pb.SetEditionCoverResponse { return &pb.SetEditionCoverResponse{Game: g} })
    }

    // SetMainEdition makes a game's edition the one shown when the game is listed once.
    func (h *GameHandler) SetMainEdition(ctx context.Context, req *connect.Request[pb.SetMainEditionRequest]) (*connect.Response[pb.SetMainEditionResponse], error) {
    	g, err := h.catalog.SetMainSystem(ctx, game.ID(req.Msg.GameId), req.Msg.System)

    	return gameResp(g, err, func(g *pb.Game) *pb.SetMainEditionResponse { return &pb.SetMainEditionResponse{Game: g} })
    }
    ```
  - `media_rpc_handler.go`: `h.media.CoverCandidates(ctx, game.ID(req.Msg.GameId), req.Msg.System)`.
  - `grep -rn "Bridge (Task 5)" internal` must print nothing.

- [ ] **Step 4: Update the RPC tests that used the game-level cover**
  - `photos_test.go`, "WHEN a photo becomes the cover": call `c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{GameId: g.Id, System: g.Copies[0].EffectiveSystem, Cover: &pb.SetEditionCoverRequest_PhotoId{PhotoId: ids[0]}}))` and assert `ids[0]` against `res.Msg.Game.Editions[0].CoverPhotoId` (the only edition). The GET of `/media/covers/<id>` stays. In "AND editing the title keeps it, but a new cover URL replaces it", `kept` is the `UpdateGame` with only the title (assert `kept.Msg.Game.Editions[0].CoverPhotoId == ids[0]`), and `replaced` is a `SetEditionCover` with `Cover: &pb.SetEditionCoverRequest_Url{Url: "https://example.test/halo.jpg"}` (assert its `Editions[0].CoverPhotoId` is empty). Rename the step "AND editing the title keeps it, but choosing a cover URL for the edition replaces it".
  - `server_test.go`:
    - `TestCoverProviderChain`, "Pinning a candidate": `c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{GameId: g.Msg.Game.Id, System: "PS3", Cover: &pb.SetEditionCoverRequest_Url{Url: cands.Msg.Candidates[0].Url}}))`.
    - `TestCoversAndLogs`, "Invalid cover URLs are rejected by the domain": first add a copy (`c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{GameId: id, Details: &pb.CopyDetails{Kind: pb.CopyKind_COPY_KIND_LIBRARY, Platform: "Steam"}}))`), then `SetEditionCover` with `System: "PC"` and `Url: "file:///etc/passwd"`, expecting `InvalidArgument`. Check the name of the add-copy request message with `grep -n "message AddCopyRequest" -A 4 proto/gamevault/v1/game.proto`.
    - `TestBarcodeScanFlow`: drop `CoverUrl` from `CreateGameRequest`, and right after it pin the suggestion with `SetEditionCover` on `System: s.Platform` (`"PS3"`).

Run: `task go -- test ./internal/adapters/inbound/rpc/`
Expected: `ok`.

- [ ] **Step 5: Failing Node test for the web helpers** (`web/tests/editions.test.mjs`)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { coverPath, mainSystem } from '../src/lib/editions.ts';

const edition = (system, over = {}) => ({ system, coverUrl: '', coverPhotoId: '', main: false, ...over });

test('coverPath escapes the system, and without one is the main edition address', () => {
  assert.equal(coverPath('g1', 'Xbox 360'), '/media/covers/g1/Xbox%20360');
  assert.equal(coverPath('g1', 'Xbox 360/S Slim'), '/media/covers/g1/Xbox%20360%2FS%20Slim');
  assert.equal(coverPath('g1', ''), '/media/covers/g1');
});

test('mainSystem is the edition marked main, or none without editions', () => {
  assert.equal(mainSystem({ editions: [edition('PS3', { main: true }), edition('PC')] }), 'PS3');
  assert.equal(mainSystem({ editions: [] }), '');
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/editions.test.mjs`
Expected: FAIL (`Cannot find module …/src/lib/editions.ts`).

- [ ] **Step 6: `web/src/lib/editions.ts` (first part)**

```ts
// Editions in the web: a game on each system it is played on. Structural types only, so Node tests
// run it as is (no generated code, no TypeScript enums); imports carry the .ts extension.

export interface EditionLike { system: string; coverUrl: string; coverPhotoId: string; main: boolean }

/** The system of the game's main edition; '' for a game without copies. */
export function mainSystem(g: { editions: EditionLike[] }): string {
  return g.editions.find((e) => e.main)?.system ?? '';
}

/** Path of an edition's cover (URL-escaped: systems may hold spaces or slashes); without a system,
 *  the main edition's. */
export function coverPath(gameId: string, system: string): string {
  const base = `/media/covers/${encodeURIComponent(gameId)}`;
  return system ? `${base}/${encodeURIComponent(system)}` : base;
}
```

Run the Node test. Expected: PASS.

- [ ] **Step 7: The web on the new API**
  - `web/src/api/client.ts`:

    ```ts
    /**
     * Cover image URL of a game's edition on `system`, the main edition's when it is omitted. Images
     * are plain HTTP (not RPC) so <img> can load and cache them; the updatedAt version busts the
     * browser cache when a cover changes.
     */
    export function coverUrl(g: Game, system?: string): string {
      return `${baseUrl}${coverPath(g.id, system ?? mainSystem(g))}?v=${g.updatedAt?.seconds ?? 0}`;
    }
    ```
    with `import { coverPath, mainSystem } from '../lib/editions';`.
  - `web/src/components/Cover.tsx`: add `system?: string` to the props and use `coverUrl(game, system)`.
  - `web/src/lib/model.ts`: `gameInfo` drops `coverUrl`; `emptyDetails` gains `system: ''`. If the type check flags other `CopyDetailsInput` literals (the scan page builds copy details), add `system: ''` to them.
  - `web/src/features/library/GameSheet.tsx`: remove `game.coverUrl` from the `useCallback` dependencies and its comment's "or cover" (covers no longer touch the sheet).
  - `web/src/features/library/CoverPicker.tsx`: new props and the pinned choice as a oneof value:

    ```tsx
    /** What the picker chose for the edition's cover (SetEditionCoverRequest.cover). */
    export type CoverChoice = { case: 'url'; value: string } | { case: 'photoId'; value: string } | { case: 'clear'; value: true };

    export default function CoverPicker({ game, system, onPick, onClose }: {
      game: Game;
      /** The edition whose cover is chosen. */
      system: string;
      onPick: (choice: CoverChoice) => void;
      onClose: () => void;
    }) {
      const edition = game.editions.find((e) => e.system === system);
      const current = edition?.coverPhotoId ? null : edition?.coverUrl ?? '';
      // The candidates, warnings and error state stay as they are.
      useEffect(() => {
        coverClient.listCoverCandidates({ gameId: game.id, system })
          .then((res) => { setCandidates(res.candidates); setWarnings(res.warnings); })
          .catch((e) => setError(errorMessage(e)));
      }, [game.id, system]);
    ```
    "Automatic" calls `onPick({ case: 'clear', value: true })`, a candidate `onPick({ case: 'url', value: c.url })`. Task 9 adds photos and a pasted URL.
  - `web/src/features/library/GameDetail.tsx`:
    - `GamePatch` drops `'coverUrl'`.
    - The cover picker dialog, on the main edition for now:

      ```tsx
      {dialog?.type === 'coverPicker' && mainSystem(game) && (
        <CoverPicker game={game} system={mainSystem(game)} onClose={() => setDialog(null)}
          onPick={(cover) => {
            run(async () => putGame((await gameClient.setEditionCover({ gameId: game.id, system: mainSystem(game), cover })).game!));
            setDialog(null);
          }} />
      )}
      ```
      and the "Choose cover…" button is shown only when `mainSystem(game)` is not empty.
    - `EditTab`: `infoOf` drops `coverUrl`, `dirty` drops the `coverUrl` comparison, and the cover URL `<label>` with its input is deleted (a URL is pasted in the cover picker from Task 9 on; keep the `game.coverUrl` and `game.coverUrlPlaceholder` strings for it).
  - `web/src/features/library/CopyPhotos.tsx`: a photo is the cover of its copy's edition:

    ```tsx
    const isCover = game.editions.find((e) => e.system === copy.effectiveSystem)?.coverPhotoId === photo.id;
    ```
    (the same test for the thumbnail star), and the button calls

    ```tsx
    gameClient.setEditionCover({
      gameId: game.id, system: copy.effectiveSystem,
      cover: isCover ? { case: 'clear', value: true } : { case: 'photoId', value: photo.id },
    })
    ```
  - `grep -rn "coverPhotoId\|setCoverPhoto\|\.coverUrl" web/src --include='*.ts' --include='*.tsx' | grep -v "src/gen/"` lists only `CoverPicker`, `CopyPhotos`, the scan page's suggestions (`coverUrl` of a suggestion) and `editions.ts`.

- [ ] **Step 8: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green, TypeScript type check included).

```bash
git add proto internal web/src web/tests
git commit -m "API: editions on games, SetEditionCover and SetMainEdition; the web on the new API"
```

---
### Task 6: CSV `system` column

**Files:**
- Modify: `internal/adapters/outbound/csvfile/codec.go`, `internal/adapters/outbound/csvfile/codec_test.go`

**Interfaces:**
- Consumes: Task 1 `game.SystemOf`, `(Copy).System()`, `CopyDetails.System`.
- Produces: the column `system`, last in the export.

- [ ] **Step 1: Failing test** (append to `codec_test.go`)

```go
func TestSystem_column(t *testing.T) {
	t.Run("GIVEN rows whose system matches their platform's, differs from it, or is empty", func(t *testing.T) {
		in := "title,platform,kind,system\n" +
			"Hades,Steam,library,PC\n" +
			"Astro Bot,PlayStation Store,library,ps5\n" +
			"Halo 3,Xbox 360,physical,\n"

		t.Run("WHEN they are imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)
			require.Len(t, copies, 3)
			assert.Empty(t, warnings)

			t.Run("THEN only a system that differs from the platform's is stored as the copy's own", func(t *testing.T) {
				assert.Empty(t, copies[0].Details.System, "PC is what Steam implies: automatic")
				assert.Equal(t, "ps5", copies[1].Details.System, "the domain names it PS5 when the copy is saved")
				assert.Empty(t, copies[2].Details.System, "empty means automatic")
			})
		})
	})

	t.Run("GIVEN a game with a copy whose system the user changed and one on automatic", func(t *testing.T) {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		g, _ := game.New("Astro Bot", now)
		_, err := g.AddCopy(game.CopyDetails{Kind: game.KindLibrary, Platform: "PlayStation Store", System: "PS5"}, now)
		require.NoError(t, err)
		_, err = g.AddCopy(game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"}, now)
		require.NoError(t, err)

		t.Run("WHEN it is exported and imported again", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Codec{}.Encode(&b, []*game.Game{g}))

			copies, _, err := Codec{}.Decode(strings.NewReader(b.String()))
			require.NoError(t, err)
			require.Len(t, copies, 2)

			t.Run("THEN the export writes each copy's effective system, and the import keeps the user's choice only", func(t *testing.T) {
				assert.Contains(t, b.String(), ",PS5\n")
				assert.Contains(t, b.String(), ",PC\n")
				assert.Equal(t, "PS5", copies[0].Details.System)
				assert.Empty(t, copies[1].Details.System)
			})
		})
	})
}
```

Run: `task go -- test ./internal/adapters/outbound/csvfile/ -run TestSystem_column`
Expected: FAIL (`system` is reported as an unknown column).

- [ ] **Step 2: The column** (`codec.go`)
  - Package comment: append `, system` to the column list, and add a paragraph:

    ```go
    // system is the system the copy is played on (PC, PS4, Switch…). The export writes each copy's
    // effective system; the import keeps a value only when it differs from the one the platform
    // implies (it is then the copy's own system), so an export imported again changes nothing.
    ```
  - `columns`: append `"system"` after `"barcode"`.
  - `Decode`, after the `d := game.CopyDetails{…}` literal:

    ```go
    		if s := get("system"); s != "" && game.SystemOf(s) != game.SystemOf(d.Platform) {
    			d.System = s // the domain checks it and names it like SystemOf when the copy is saved
    		}
    ```
  - `Encode`: append `c.System()` to the record, after `string(c.Barcode)`.

Run: `task go -- test ./internal/adapters/outbound/csvfile/`
Expected: `ok`.

- [ ] **Step 3: Lint, test, commit**

Run: `task lint:fix`, `task lint`, `task test`. Expected: green.

```bash
git add internal/adapters/outbound/csvfile
git commit -m "CSV: a system column (effective system out, the copy's own system in)"
```

---
### Task 7: Editions in the web as pure functions (`web/src/lib/editions.ts`)

Grouping, filtering, counting, sorting and the cover fallback order are pure functions tested in Node. The library filters move here from `FilterPanel.tsx` (they must run in Node, and they gain the System filter).

**Files:**
- Modify: `web/src/lib/editions.ts`, `web/tests/editions.test.mjs`, `web/src/features/library/FilterPanel.tsx`, `web/src/features/library/LibraryPage.tsx` (imports only)

**Interfaces:**
- Consumes: Task 5 `Game.editions`, `Copy.effectiveSystem`; `lib/fields.ts` `matchesFieldFilters`, `pruneFieldFilter`, `Definition`, `FieldFilter`, `Values`.
- Produces (all exported from `web/src/lib/editions.ts`):
  - types `EditionLike`, `CopyLike`, `GameLike`, `Grouping = 'platform' | 'game'`, `LibraryItem<G> { key: string; game: G; system: string; view: G }`, `Filters` (with `systems: string[]`)
  - `SYSTEMS: string[]`, `systemOf(platform: string): string`, `mainSystem(g)`, `coverPath(gameId, system)`
  - `editionsOf<G>(g: G): LibraryItem<G>[]`, `libraryItems<G>(games: G[], grouping: Grouping): LibraryItem<G>[]`
  - `MANUAL`, `NO_FILTERS`, `activeFilterCount(f)`, `pruneFilters(f, defs)`, `matchesFilters(g, f, defs)`
  - `filterItems<G>(items, f, defs, checks?: { copyLevel?: (view: G) => boolean; gameLevel?: (game: G) => boolean }): LibraryItem<G>[]`
  - `systemCounts(views: GameLike[]): [string, number][]`
  - `sortItems<G>(items, cmp: (a: G, b: G) => number): LibraryItem<G>[]`
  - `itemCounts(items): { items: number; games: number }`
  - `coverOrder(g, system: string): string[]`, `nextCover(order: string[], failed: ReadonlySet<string>): string | null`
  - `groupCopies<C extends CopyLike>(copies: C[], editions: EditionLike[], current: string): { system: string; copies: C[] }[]`

- [ ] **Step 1: Failing tests** (replace `web/tests/editions.test.mjs`; it keeps Task 5's two tests)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  NO_FILTERS, activeFilterCount, coverOrder, coverPath, editionsOf, filterItems, groupCopies, itemCounts, libraryItems, mainSystem,
  nextCover, sortItems, systemCounts, systemOf,
} from '../src/lib/editions.ts';

// Plain objects shaped like the generated messages; kinds: 1 key, 2 library, 3 physical.
const edition = (system, over = {}) => ({ system, coverUrl: '', coverPhotoId: '', main: false, ...over });
const copy = (system, platform, kind, over = {}) => ({ effectiveSystem: system, sourceId: '', redundant: false, details: { kind, platform, fields: {} }, ...over });
const game = (id, title, copies, editions, over = {}) => ({ id, title, genres: [], playStatus: 0, fields: {}, copies, editions, ...over });

const halo = game('g1', 'Halo 3', [
  copy('PS3', 'PS3', 3),
  copy('PC', 'Steam', 2),
  copy('Xbox 360', 'Xbox 360', 1, { sourceId: 's1', redundant: true }),
], [edition('PS3', { main: true }), edition('PC'), edition('Xbox 360')], { genres: ['Shooter'], playStatus: 2 });
const hades = game('g2', 'Hades', [copy('PC', 'Steam', 2), copy('PC', 'GOG', 2)], [edition('PC', { main: true })]);
const empty = game('g3', 'Empty', [], []);

const keys = (items, f = {}, checks) => filterItems(items, { ...NO_FILTERS, ...f }, [], checks).map((i) => i.key);

test('coverPath escapes the system, and without one is the main edition address', () => {
  assert.equal(coverPath('g1', 'Xbox 360'), '/media/covers/g1/Xbox%20360');
  assert.equal(coverPath('g1', 'Xbox 360/S Slim'), '/media/covers/g1/Xbox%20360%2FS%20Slim');
  assert.equal(coverPath('g1', ''), '/media/covers/g1');
});

test('mainSystem is the edition marked main, or none without editions', () => {
  assert.equal(mainSystem(halo), 'PS3');
  assert.equal(mainSystem(empty), '');
});

test('systemOf mirrors the server', () => {
  assert.equal(systemOf('Steam'), 'PC');
  assert.equal(systemOf('microsoft store / xbox'), 'PC');
  assert.equal(systemOf('PlayStation Store'), 'PS4');
  assert.equal(systemOf('Nintendo eShop'), 'Switch');
  assert.equal(systemOf('xbox 360'), 'Xbox 360');
  assert.equal(systemOf(' Amiga '), 'Amiga');
  assert.equal(systemOf(''), 'Other');
});

test('editionsOf: one item per edition holding its copies; a game without copies is one item', () => {
  const items = editionsOf(halo);
  assert.deepEqual(items.map((i) => i.key), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(items[1].view.copies.map((c) => c.details.platform), ['Steam']);
  assert.equal(items[1].game, halo, 'the item keeps the whole game');
  assert.deepEqual(editionsOf(empty).map((i) => [i.key, i.system]), [['g3', '']]);
});

test('libraryItems: by game, one item per game on its main edition', () => {
  const items = libraryItems([halo, hades, empty], 'game');
  assert.deepEqual(items.map((i) => [i.key, i.system]), [['g1', 'PS3'], ['g2', 'PC'], ['g3', '']]);
  assert.equal(items[0].view, halo);
  assert.deepEqual(itemCounts(libraryItems([halo, hades], 'platform')), { items: 4, games: 2 });
  assert.deepEqual(itemCounts(items), { items: 3, games: 3 });
});

test('"By platform": copy-level filters keep an edition when one of its copies matches', () => {
  const items = libraryItems([halo, hades], 'platform');
  assert.deepEqual(keys(items, { kind: 1 }), ['g1|Xbox 360']);
  assert.deepEqual(keys(items, { platforms: ['Steam'] }), ['g1|PC', 'g2|PC']);
  assert.deepEqual(keys(items, { sources: ['s1'] }), ['g1|Xbox 360']);
  assert.deepEqual(keys(items, {}, { copyLevel: (v) => v.copies.some((c) => c.redundant) }), ['g1|Xbox 360']);
});

test('game-level filters and the search apply to all of a game\'s editions', () => {
  const items = libraryItems([halo, hades], 'platform');
  assert.deepEqual(keys(items, { genres: ['Shooter'] }), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(keys(items, { play: [2] }), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(keys(items, {}, { gameLevel: (g) => g.copies.some((c) => c.details.platform === 'GOG') }), ['g2|PC']);
});

test('"By game": filters look at all of the game\'s copies', () => {
  const items = libraryItems([halo, hades], 'game');
  assert.deepEqual(keys(items, { kind: 1 }), ['g1']);
  assert.deepEqual(keys(items, { platforms: ['Steam'] }), ['g1', 'g2']);
  assert.deepEqual(keys(items, {}, { copyLevel: (v) => v.copies.some((c) => c.redundant) }), ['g1']);
});

test('the System filter matches editions in "By platform" and games in "By game"', () => {
  assert.deepEqual(keys(libraryItems([halo, hades], 'platform'), { systems: ['PS3', 'Xbox 360'] }), ['g1|PS3', 'g1|Xbox 360']);
  assert.deepEqual(keys(libraryItems([halo, hades], 'game'), { systems: ['PS3', 'Xbox 360'] }), ['g1']);
  assert.equal(activeFilterCount({ ...NO_FILTERS, systems: ['PS3', 'PC'] }), 2);
});

test('System options are counted by editions in "By platform" and by games in "By game", most first', () => {
  const ps3Twice = game('g4', 'Ico', [copy('PS3', 'PS3', 3), copy('PS3', 'PS3', 3)], [edition('PS3', { main: true })]);
  const views = (grouping) => libraryItems([halo, hades, ps3Twice], grouping).map((i) => i.view);
  assert.deepEqual(systemCounts(views('platform')), [['PC', 2], ['PS3', 2], ['Xbox 360', 1]]);
  assert.deepEqual(systemCounts(views('game')), [['PC', 2], ['PS3', 2], ['Xbox 360', 1]]);
  assert.deepEqual(systemCounts([halo]), [['PC', 1], ['PS3', 1], ['Xbox 360', 1]], 'a game counts once per system');
});

test('sorting breaks ties by system, and the copies sort counts the edition\'s copies', () => {
  const items = libraryItems([halo, hades], 'platform');
  const byTitle = (a, b) => a.title.localeCompare(b.title);
  assert.deepEqual(sortItems(items, byTitle).map((i) => i.key), ['g2|PC', 'g1|PC', 'g1|PS3', 'g1|Xbox 360']);
  const byCopies = (a, b) => b.copies.length - a.copies.length;
  assert.deepEqual(sortItems(items, byCopies).map((i) => i.key), ['g2|PC', 'g1|PC', 'g1|PS3', 'g1|Xbox 360']);
});

test('a card tries its own cover, then the main edition\'s, then the others\', and stops after the last', () => {
  assert.deepEqual(coverOrder(halo, 'Xbox 360'), ['Xbox 360', 'PS3', 'PC']);
  assert.deepEqual(coverOrder(halo, 'PS3'), ['PS3', 'PC', 'Xbox 360']);
  assert.deepEqual(coverOrder(halo, ''), ['PS3', 'PC', 'Xbox 360']);
  assert.deepEqual(coverOrder(empty, ''), ['']);

  const order = coverOrder(halo, 'Xbox 360');
  assert.equal(nextCover(order, new Set()), 'Xbox 360');
  assert.equal(nextCover(order, new Set(['Xbox 360'])), 'PS3');
  assert.equal(nextCover(order, new Set(order)), null, 'every cover failed: initials, and no more requests');
});

test('groupCopies: the current edition first, then in the editions\' order', () => {
  const groups = groupCopies(halo.copies, halo.editions, 'Xbox 360');
  assert.deepEqual(groups.map((g) => [g.system, g.copies.length]), [['Xbox 360', 1], ['PS3', 1], ['PC', 1]]);
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/editions.test.mjs`
Expected: FAIL (`does not provide an export named 'NO_FILTERS'`).

- [ ] **Step 2: Complete `web/src/lib/editions.ts`** (it replaces the file of Task 5)

```ts
// Editions in the web: a game on each system it is played on, as the library lists them ("By
// platform": one item per edition; "By game": one per game), the library filters, the System
// options, an edition's copies and the order in which a card tries covers. Structural types only,
// so Node tests run it as is (no generated code, no TypeScript enums); imports carry the .ts
// extension.
import { matchesFieldFilters, pruneFieldFilter, type Definition, type FieldFilter, type Values } from './fields.ts';

export interface EditionLike { system: string; coverUrl: string; coverPhotoId: string; main: boolean }
export interface CopyLike {
  effectiveSystem: string;
  sourceId: string;
  details?: { kind: number; platform: string; fields: Values };
}
export interface GameLike {
  id: string;
  title: string;
  genres: string[];
  playStatus: number;
  fields: Values;
  copies: CopyLike[];
  editions: EditionLike[];
}

export type Grouping = 'platform' | 'game';

/** One card or row of the library: a game's edition ("By platform") or the game itself ("By game"). */
export interface LibraryItem<G extends GameLike> {
  key: string;
  game: G;
  /** The edition's system; in "By game", the main edition's ('' for a game without copies). */
  system: string;
  /** The game as filters and sorts see it: in "By platform", with only the edition's copies. */
  view: G;
}

/** Systems offered in the copy form: PC, then the console names game.SystemOf knows. */
export const SYSTEMS = [
  'PC', 'PS5', 'PS4', 'PS3', 'PS2', 'PS1', 'PSP', 'PS Vita', 'Xbox Series', 'Xbox One', 'Xbox 360', 'Xbox',
  'Switch', 'Wii U', 'Wii', 'GameCube', 'N64', '3DS', 'DS', 'Game Boy',
];
const PC_PLATFORMS = [
  'steam', 'epic games', 'gog', 'ea app', 'ubisoft connect', 'battle.net', 'itch.io', 'amazon games', 'rockstar', 'riot',
  'battlestate (tarkov)', 'pc',
];
const STORE_SYSTEMS: Record<string, string> = { 'playstation store': 'PS4', 'nintendo eshop': 'Switch', 'microsoft store / xbox': 'PC' };

/** The system a platform implies; mirrors game.SystemOf in the server. */
export function systemOf(platform: string): string {
  const p = platform.trim();
  if (!p) return 'Other';
  const k = p.toLowerCase();
  if (PC_PLATFORMS.includes(k)) return 'PC';
  if (STORE_SYSTEMS[k]) return STORE_SYSTEMS[k]!;
  return SYSTEMS.find((s) => s.toLowerCase() === k) ?? p;
}

/** The system of the game's main edition; '' for a game without copies. */
export function mainSystem(g: { editions: EditionLike[] }): string {
  return g.editions.find((e) => e.main)?.system ?? '';
}

/** Path of an edition's cover (URL-escaped: systems may hold spaces or slashes); without a system,
 *  the main edition's. */
export function coverPath(gameId: string, system: string): string {
  const base = `/media/covers/${encodeURIComponent(gameId)}`;
  return system ? `${base}/${encodeURIComponent(system)}` : base;
}

/** A game's editions as library items, in the server's order (main first); a game without copies
 *  is one item with no system. */
export function editionsOf<G extends GameLike>(g: G): LibraryItem<G>[] {
  if (g.editions.length === 0) return [{ key: g.id, game: g, system: '', view: g }];
  return g.editions.map((e) => ({
    key: `${g.id}|${e.system}`,
    game: g,
    system: e.system,
    view: { ...g, copies: g.copies.filter((c) => c.effectiveSystem === e.system) } as G,
  }));
}

/** The library's items: every edition ("By platform") or every game on its main edition ("By game"). */
export function libraryItems<G extends GameLike>(games: G[], grouping: Grouping): LibraryItem<G>[] {
  return grouping === 'game'
    ? games.map((g) => ({ key: g.id, game: g, system: mainSystem(g), view: g }))
    : games.flatMap((g) => editionsOf(g));
}

export interface Filters {
  /** A copy kind (CopyKind's number); 0: any. */
  kind: number;
  platforms: string[];
  /** Systems (PC, PS4…): an item matches when one of its copies is on one of them. */
  systems: string[];
  genres: string[];
  /** Source ids; MANUAL stands for copies added by hand or from a CSV. */
  sources: string[];
  /** Play statuses (PlayStatus's numbers); 0 stands for games the user has not given one. */
  play: number[];
  /** Custom field values by field id: choice ids, 'yes' / 'no', or '' for no value. */
  fields: FieldFilter;
}

export const MANUAL = '';

export const NO_FILTERS: Filters = { kind: 0, platforms: [], systems: [], genres: [], sources: [], play: [], fields: {} };

export const activeFilterCount = (f: Filters) => (f.kind ? 1 : 0) + f.platforms.length + f.systems.length + f.genres.length
  + f.sources.length + f.play.length + Object.values(f.fields).reduce((n, keys) => n + keys.length, 0);

/** The filters without custom field values that no longer exist (a deleted field, a removed
 *  choice). The same object when nothing was dropped. */
export function pruneFilters(f: Filters, defs: Definition[]): Filters {
  const fields = pruneFieldFilter(f.fields, defs);
  return fields === f.fields ? f : { ...f, fields };
}

/** A game (or an edition's view of it) matches when it has a copy of the kind on one of the
 *  platforms, a copy on one of the systems, a copy from one of the sources, one of the genres, one of
 *  the play statuses and one of the chosen values of each filtered custom field. */
export function matchesFilters(g: GameLike, f: Filters, defs: Definition[]): boolean {
  if (f.kind || f.platforms.length) {
    const ok = g.copies.some((c) => (!f.kind || c.details?.kind === f.kind) && (!f.platforms.length || f.platforms.includes(c.details?.platform ?? '')));
    if (!ok) return false;
  }
  if (f.systems.length && !g.copies.some((c) => f.systems.includes(c.effectiveSystem))) return false;
  if (f.sources.length && !g.copies.some((c) => f.sources.includes(c.sourceId))) return false;
  if (f.genres.length && !g.genres.some((x) => f.genres.includes(x))) return false;
  if (f.play.length && !f.play.includes(g.playStatus)) return false;
  return matchesFieldFilters(g, defs, f.fields);
}

/** The items that pass the filters. Copy-level checks (kind, platform, system, source, copy fields
 *  and `copyLevel`, the quick filters) look at the item's view, so in "By platform" an edition stays
 *  when one of its copies matches; game-level ones (genres, play status, game fields and `gameLevel`,
 *  the search) see the same game in every edition. */
export function filterItems<G extends GameLike>(items: LibraryItem<G>[], f: Filters, defs: Definition[],
  checks: { copyLevel?: (view: G) => boolean; gameLevel?: (game: G) => boolean } = {}): LibraryItem<G>[] {
  return items.filter((i) => matchesFilters(i.view, f, defs)
    && (!checks.copyLevel || checks.copyLevel(i.view))
    && (!checks.gameLevel || checks.gameLevel(i.game)));
}

/** How many items are on each system, most first then by name: pass the items' views, so editions
 *  are counted in "By platform" and games in "By game". */
export function systemCounts(views: GameLike[]): [string, number][] {
  const counts = new Map<string, number>();
  for (const v of views) for (const s of new Set(v.copies.map((c) => c.effectiveSystem))) counts.set(s, (counts.get(s) ?? 0) + 1);
  return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

/** The items sorted by `cmp` on their views (so "Most copies" counts an edition's copies), ties by system. */
export function sortItems<G extends GameLike>(items: LibraryItem<G>[], cmp: (a: G, b: G) => number): LibraryItem<G>[] {
  return [...items].sort((a, b) => cmp(a.view, b.view) || a.system.localeCompare(b.system));
}

/** How many items and how many distinct games a list holds ("320 editions of 300 games"). */
export function itemCounts(items: LibraryItem<GameLike>[]): { items: number; games: number } {
  return { items: items.length, games: new Set(items.map((i) => i.game.id)).size };
}

/** The systems whose cover a card tries, in order: its own edition's (the main one's when it names
 *  none), then the main edition's, then the others'. A game without copies: [''] (its only address). */
export function coverOrder(g: { editions: EditionLike[] }, system: string): string[] {
  const main = mainSystem(g);
  const all = [system || main, main, ...g.editions.map((e) => e.system)];
  return all.filter((s, i) => all.indexOf(s) === i);
}

/** The next cover to try, or null when all failed (the card then shows initials and stops asking). */
export function nextCover(order: string[], failed: ReadonlySet<string>): string | null {
  return order.find((s) => !failed.has(s)) ?? null;
}

/** A game's copies grouped by system: the current edition first, then in the editions' order. */
export function groupCopies<C extends CopyLike>(copies: C[], editions: EditionLike[], current: string): { system: string; copies: C[] }[] {
  const systems = [current, ...editions.map((e) => e.system), ...copies.map((c) => c.effectiveSystem)];
  return systems
    .filter((s, i) => s && systems.indexOf(s) === i)
    .map((system) => ({ system, copies: copies.filter((c) => c.effectiveSystem === system) }))
    .filter((g) => g.copies.length > 0);
}
```

  In "sorting breaks ties": `byCopies` gives Hades (2 copies) first, then Halo's editions (1 copy each) tied and ordered by system — which is what the test expects.

- [ ] **Step 3: Use them from the library** (`FilterPanel.tsx`, `LibraryPage.tsx`)
  - `FilterPanel.tsx`: delete `Filters`, `MANUAL`, `NO_FILTERS`, `activeFilterCount`, `pruneFilters` and `matchesFilters` and import them from `../../lib/editions`; the `filters.kind` and `filters.play` comparisons with `CopyKind` / `PlayStatus` values keep working (the enums are numbers).
  - `LibraryPage.tsx`: import `NO_FILTERS`, `activeFilterCount`, `matchesFilters`, `pruneFilters`, `type Filters` from `../../lib/editions` instead of `./FilterPanel` (Task 8 replaces `matchesFilters` with `filterItems`).

- [ ] **Step 4: Run the tests and the type check**

Run: the Node command of Step 1 (expected: all pass), then `task test` (expected: green).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/editions.ts web/tests/editions.test.mjs web/src/features/library/FilterPanel.tsx web/src/features/library/LibraryPage.tsx
git commit -m "Web: editions, library filters with System, counts and cover order as pure functions"
```

---
### Task 8: The library by platform or by game

**Files:**
- Modify: `web/src/features/library/LibraryPage.tsx`, `web/src/features/library/PosterGrid.tsx`, `web/src/features/library/FilterPanel.tsx`, `web/src/components/Cover.tsx`, `web/src/components/PlatformBadge.tsx`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 7 (`libraryItems`, `filterItems`, `sortItems`, `systemCounts`, `itemCounts`, `coverOrder`, `nextCover`, `Filters.systems`, `LibraryItem`, `Grouping`); Task 5 `coverUrl(game, system)`.
- Produces:
  - `Cover({ game, system?, className?, onShown? })`: tries the edition's cover, then the main edition's, then the others', labels a borrowed box, and ends on the initials; `onShown(system | null)` reports what it shows
  - `systemHoldings(game: Game): PlatformHolding[]` and `SystemBadges({ game, onSelect?, active? })` in `PlatformBadge.tsx`; `PlatformBadges` gains `small?: boolean`
  - `PosterGrid({ items, grouping, onOpen, onSystem, activeSystems })`
  - `LibraryPage` opens the sheet with `{ id, system }` (Task 9 reads `system`)

- [ ] **Step 1: Texts.** Add to both locales (`task i18n` checks they match):

| Key | en | es |
|---|---|---|
| `library.grouping.label` | Group | Agrupar |
| `library.grouping.platform` | By platform | Por plataforma |
| `library.grouping.game` | By game | Por juego |
| `library.showingEditions` | {{editions}} editions of {{games}} games | {{editions}} ediciones de {{games}} juegos |
| `library.filters.systems` | System | Sistema |
| `edition.borrowed` | {{system}} box | Carátula de {{system}} |

- [ ] **Step 2: The cover with its fallback** (`web/src/components/Cover.tsx`)

```tsx
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import { coverOrder, nextCover } from '../lib/editions';

/**
 * A game's cover on one edition (the main one when `system` is omitted). When the edition has no box
 * of its own (the server answers 404), it tries the main edition's, then the other editions', and
 * labels the borrowed box with its system; when none has one, it shows the title's initials and asks
 * no more. `onShown` reports the system whose box is shown, or null.
 */
export function Cover({ game, system = '', className = '', onShown }: {
  game: Game;
  system?: string;
  className?: string;
  onShown?: (system: string | null) => void;
}) {
  const { t } = useTranslation();
  const order = coverOrder(game, system);
  // Failures are forgotten when the game changes (a new cover was chosen) or the edition does.
  const version = `${game.id}|${game.updatedAt?.seconds ?? 0}|${order.join('|')}`;
  const [failed, setFailed] = useState<{ version: string; systems: ReadonlySet<string> }>({ version, systems: new Set() });
  const tried = failed.version === version ? failed.systems : new Set<string>();
  const shown = nextCover(order, tried);

  useEffect(() => { onShown?.(shown); }, [shown, onShown]);

  return (
    <div className={`cover ${className}`}>
      {shown === null ? (
        <div className="cover-placeholder" aria-hidden="true">
          <span>{initials(game.title)}</span>
        </div>
      ) : (
        <>
          <img key={shown} src={coverUrl(game, shown)} alt="" loading="lazy" decoding="async"
            onError={() => setFailed({ version, systems: new Set([...tried, shown]) })} />
          {shown !== order[0] && <span className="cover-borrowed">{t('edition.borrowed', { system: shown })}</span>}
        </>
      )}
    </div>
  );
}
```
  (keep `initials` as it is.) A game without copies has the order `['']`: one request to the main address, then the initials.

- [ ] **Step 3: System badges** (`web/src/components/PlatformBadge.tsx`)
  - Generalize `platformHoldings` so it can group by system:

```tsx
/** What you have on each of a game's platforms (or systems, with `by`), one entry each. */
function holdingsBy(game: Game, by: (c: Copy) => string): PlatformHolding[] {
  const map = new Map<string, PlatformHolding>();
  for (const c of game.copies) {
    const d = c.details;
    if (!d) continue;
    if ([CopyStatus.SOLD, CopyStatus.GIFTED, CopyStatus.EXPIRED].includes(d.status)) continue;
    const platform = by(c);
    if (!platform) continue;
    const holding: Holding = isPendingKey(c) ? 'key' : 'owned';
    const k = platform.toLowerCase();
    const prev = map.get(k);
    if (!prev || (prev.holding === 'key' && holding === 'owned')) map.set(k, { platform, holding, kind: d.kind });
  }
  return [...map.values()].sort((a, b) => (a.holding === b.holding ? 0 : a.holding === 'owned' ? -1 : 1) || KIND_ORDER[a.kind] - KIND_ORDER[b.kind]);
}

export function platformHoldings(game: Game): PlatformHolding[] {
  return holdingsBy(game, (c) => c.details?.platform ?? '');
}

/** The systems a game is played on, one entry each, owned before pending keys. */
export function systemHoldings(game: Game): PlatformHolding[] {
  return holdingsBy(game, (c) => c.effectiveSystem);
}
```
    (keep `platformHoldings`' doc comment; import `type Copy` from `../lib/model`.)
  - `PlatformBadges` gains `small = false`, rendered as `<span className={`pbadges ${small ? 'small' : ''}`}>`.
  - Add, after `PlatformBadges`:

```tsx
/** A game's systems as badges ("By game" view); a click filters the library by that system. */
export function SystemBadges({ game, max = 3, onSelect, active = [] }: {
  game: Game;
  max?: number;
  onSelect?: (system: string) => void;
  active?: string[];
}) {
  const all = systemHoldings(game);
  const shown = all.length > max ? all.slice(0, max - 1) : all;
  const rest = all.length - shown.length;
  return (
    <span className="pbadges">
      {shown.map((h) => <PlatformBadge key={h.platform} {...h} onSelect={onSelect} active={active.includes(h.platform)} />)}
      {rest > 0 && <span className="pbadge more" title={all.slice(shown.length).map((h) => h.platform).join(', ')}>+{rest}</span>}
    </span>
  );
}
```

- [ ] **Step 4: The library page** (`LibraryPage.tsx`)
  - State: `const [grouping, setGrouping] = usePref<Grouping>('gamevault.libraryGrouping', 'platform', ['platform', 'game']);` and the open sheet becomes `const [open, setOpen] = useState<{ id: string; system: string } | null>(null);`.
  - Items and their views:

```tsx
  const items = useMemo(() => libraryItems(games, grouping), [games, grouping]);
  const views = useMemo(() => items.map((i) => i.view), [items]);
```
  - `counts` counts views instead of games (quick chips count editions in "By platform"): replace `games` with `views` in its three `filter` calls; `revealedRedundant` still reads every game's copies.
  - `base`:

```tsx
  const base = useMemo(() => {
    const needle = q.trim().toLocaleLowerCase(i18n.language);
    const quickOk = (v: Game) => (quick === 'pending' ? v.copies.some(isPendingKey)
      : quick === 'expiring' ? isExpiring(v)
        : quick === 'redundant' ? v.copies.some((c) => c.redundant) : true);
    const searchOk = (g: Game) => !needle || [g.title, g.notes, ...g.genres,
      ...g.copies.flatMap((c) => [c.details?.origin, c.details?.notes, c.details?.location, c.details?.edition]), searchableText(g, fields)]
      .join(' ').toLocaleLowerCase(i18n.language).includes(needle);
    return filterItems(items, filters, fields, { copyLevel: quickOk, gameLevel: searchOk });
  }, [items, q, quick, filters, fields, i18n.language]);

  const lettersWithGames = useMemo(() => new Set(base.map((i) => initialOf(i.game.title))), [base]);
```
  - `filtered`: keep the `cmp` table and return `sortItems(base.filter((i) => !letter || initialOf(i.game.title) === letter), cmp[sortBy])`. Add `grouping` to the `setShown(CHUNK)` effect's dependencies.
  - Opening and moving through the list go by item:

```tsx
  const openIndex = open ? filtered.findIndex((i) => i.game.id === open.id && (grouping === 'game' || i.system === open.system)) : -1;
  const openAt = (i: number) => {
    setOpen({ id: filtered[i]!.game.id, system: filtered[i]!.system });
    if (i >= shown) setShown(Math.ceil((i + 1) / CHUNK) * CHUNK);
  };
```
  - Clicking a system badge filters by that system, like the platform badges did:

```tsx
  const filterSystem = (system: string) => setFilters((f) => ({
    ...f, systems: f.systems.length === 1 && f.systems[0] === system ? [] : [system],
  }));
```
  - Header count:

```tsx
        <span className="page-count">{grouping === 'platform'
          ? t('library.showingEditions', { editions: filtered.length.toLocaleString(i18n.language), games: itemCounts(filtered).games.toLocaleString(i18n.language) })
          : t('library.showing', { shown: filtered.length.toLocaleString(i18n.language), total: games.length.toLocaleString(i18n.language) })}</span>
```
  - The grouping selector, right before the Covers / List toggle:

```tsx
        <div className="segmented grouping-toggle" role="group" aria-label={t('library.grouping.label')}>
          {(['platform', 'game'] as const).map((g) => (
            <button key={g} className={grouping === g ? 'active' : ''} aria-pressed={grouping === g} onClick={() => setGrouping(g)}>
              {t(`library.grouping.${g}`)}
            </button>
          ))}
        </div>
```
  - Remove the imports that became unused (`matchesFilters`, the old `PlatformBadges` import if `GameRow` no longer uses it): the type check fails on unused locals.
  - `FilterPanel` gets `games={views}` (its counts then count editions or games) and keeps `detailsCached`; the genres-partial line keeps comparing with `games.length` by receiving a new prop `total={games.length}`.
  - Results: `<PosterGrid items={visible} grouping={grouping} onOpen={(i) => setOpen({ id: i.game.id, system: i.system })} onSystem={filterSystem} activeSystems={filters.systems} />`; the list maps `visible` to `<GameRow key={i.key} item={i} grouping={grouping} … />`.
  - The sheet: `{open && <GameDetail gameId={open.id} system={open.system} onClose={() => setOpen(null)} onOpenGame={(id) => setOpen({ id, system: '' })} nav={nav} onPlatform={…} />}` (`GameDetail`'s `system` prop is added in Task 9; until then add it to `Props` as optional and unused: `/** The edition to open on; empty: the main one. */ system?: string;`). `NewGameDialog`'s `onCreated(id)` opens `{ id, system: '' }`.
  - `GameRow({ item, grouping, onOpen, onSystem, activeSystems })` renders `<Cover game={item.game} system={item.system} className="row-cover" />`, counts `pending`, `redundant` and the deadline from `item.view`, and shows the badges of Step 5.

- [ ] **Step 5: Edition cards** (`PosterGrid.tsx`)

```tsx
import { useTranslation } from 'react-i18next';
import { Cover } from '../../components/Cover';
import { PlatformBadge, PlatformBadges, SystemBadges } from '../../components/PlatformBadge';
import type { Grouping, LibraryItem } from '../../lib/editions';
import { daysUntil, isPendingKey, nextDeadline, type Game } from '../../lib/model';

/** The badges of a library item: in "By platform", the edition's system (and, on PC, its stores,
 *  small); in "By game", every system of the game. */
export function ItemBadges({ item, grouping, onSystem, activeSystems = [] }: {
  item: LibraryItem<Game>;
  grouping: Grouping;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
}) {
  if (grouping === 'game') return <SystemBadges game={item.game} onSelect={onSystem} active={activeSystems} />;
  if (!item.system) return null;
  return (
    <span className="pbadges">
      <PlatformBadge platform={item.system} onSelect={onSystem} active={activeSystems.includes(item.system)} />
      {item.system === 'PC' && <PlatformBadges game={item.view} small />}
    </span>
  );
}

/** Box-art view of the library: the covers are the interface. */
export default function PosterGrid({ items, grouping, onOpen, onSystem, activeSystems }: {
  items: LibraryItem<Game>[];
  grouping: Grouping;
  onOpen: (item: LibraryItem<Game>) => void;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
}) {
  const { t } = useTranslation();
  return (
    <ul className="posters">
      {items.map((item) => {
        const v = item.view;
        const pending = v.copies.filter(isPendingKey).length;
        const redundant = v.copies.filter((c) => c.redundant).length;
        const days = daysUntil(nextDeadline(v));
        return (
          <li key={item.key} className="poster-card">
            {/* The title button stretches over the whole card; the badges sit above it. */}
            <span className="poster-art">
              <Cover game={item.game} system={item.system} />
              <span className="poster-flags">
                {days !== null && days <= 30 && <span className="flag danger">{t('common.daysLeft', { count: days })}</span>}
                {redundant > 0 && <span className="flag warn">{t('library.redundantBadge', { count: redundant })}</span>}
                {pending > 0 && redundant === 0 && <span className="flag">{t('library.pendingBadge', { count: pending })}</span>}
              </span>
              <span className="poster-platforms"><ItemBadges item={item} grouping={grouping} onSystem={onSystem} activeSystems={activeSystems} /></span>
            </span>
            <button className="poster-open" onClick={() => onOpen(item)}>
              <span className="poster-title">{item.game.title}</span>
              {item.game.releaseYear > 0 && <span className="poster-meta">{item.game.releaseYear}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
```
  `GameRow` uses `ItemBadges` too (import it from `./PosterGrid`).

- [ ] **Step 6: The System filter** (`FilterPanel.tsx`): a section before Platform, counted with `systemCounts(games)` (the views the page passes):

```tsx
        <section>
          <h3>{t('library.filters.systems')}</h3>
          <div className="choice-row">
            {systems.map(([s, n]) => (
              <button key={s} className={`choice ${filters.systems.includes(s) ? 'active' : ''}`} aria-pressed={filters.systems.includes(s)}
                onClick={() => onChange({ ...filters, systems: toggle(filters.systems, s) })}>{s}<span className="choice-count">{n}</span></button>
            ))}
          </div>
        </section>
```
  with `const systems = useMemo(() => systemCounts(games), [games]);`. The genres-partial note uses the new `total` prop instead of `games.length`.

- [ ] **Step 7: Styles** (`web/src/styles.css`, near `.cover` and `.pbadges`)

```css
/* A box borrowed from another edition: named, so the edition's own badge is not contradicted. */
.cover-borrowed {
  position: absolute; left: 6px; bottom: 6px; max-width: calc(100% - 12px);
  font-size: 10.5px; font-weight: 650; padding: 2px 6px; border-radius: 5px;
  background: rgb(16 18 30 / .78); color: #fff; backdrop-filter: blur(6px);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; pointer-events: none;
}
.pbadges.small .pbadge { height: 20px; padding: 0 5px; font-size: 10.5px; }
.pbadges.small .pbadge.icon-only { width: 20px; padding: 0; }
.pbadges.small .pbadge svg { width: 12px; height: 12px; }
.grouping-toggle button { min-height: 34px; }
```
  The poster's `.poster-platforms` already sits at the bottom of the art: check in the browser (Step 9) that `.cover-borrowed` does not hide the badges, and move it to `top: 6px` (under the flags' row) if it does. On phones, the toolbar wraps: the grouping toggle must fit at 375 px without a horizontal scroll (add `.grouping-toggle { flex: none; }` and let `.library-toolbar` wrap if it does not already).

- [ ] **Step 8: Type check and tests**

Run: `task test`. Expected: green (`task i18n` included).

- [ ] **Step 9: Browser check.** `task test-server`, then open http://127.0.0.1:8093/?v=<a new number> in the browser pane:
  - "By platform" is the default; a game owned on two consoles shows two cards, each with its own box and system badge; a PC edition shows its stores' small badges; the header reads "N editions of M games";
  - an edition without its own box shows another edition's with the "<system> box" label; a game no edition has a box for shows initials and (Network panel) stops requesting covers;
  - "By game" shows one card per game with its system badges; clicking a badge filters by that System; the System filter counts editions in "By platform" and games in "By game";
  - reload: the grouping is remembered.
  Then the `mobile` preset (reload): the toolbar fits, no horizontal scroll. Reset with the `desktop` preset, `task test-server:stop`.

- [ ] **Step 10: Commit**

```bash
git add web/src
git commit -m "Library: by platform (a card per edition) or by game, System filter, borrowed boxes labelled"
```

---
### Task 9: The game sheet by edition, the cover picker and the copy's System field

**Files:**
- Modify: `web/src/features/library/GameDetail.tsx`, `web/src/features/library/CoverPicker.tsx`, `web/src/features/library/CopyForm.tsx`, `web/src/features/library/CopyPhotos.tsx`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 7 `mainSystem`, `groupCopies`, `systemOf`, `SYSTEMS`; Task 8 `Cover` with `onShown`; Task 5 `gameClient.setEditionCover`, `gameClient.setMainEdition`, `CoverChoice`.
- Produces:
  - `GameDetail({ gameId, system?, … })`: opens on `system`, else on the main edition
  - `CoverPicker({ game, system, photosOnly?, onPick, onClose })`: the edition's copies' photos, Automatic, the providers' candidates, and a pasted URL
  - `CopyForm({ initial?, sourceSystem?, … })`: a System field

- [ ] **Step 1: Texts.** Add to both locales:

| Key | en | es |
|---|---|---|
| `edition.chips` | Editions | Ediciones |
| `edition.borrowedHelp` | No box of its own yet: showing the {{system}} box. | Aún no tiene carátula propia: se ve la de {{system}}. |
| `edition.noBox` | No box for this edition yet. | Esta edición aún no tiene carátula. |
| `edition.usePhoto` | Use a photo | Usar una foto |
| `edition.useAsMain` | Use as the game's cover | Usar como portada del juego |
| `edition.isMain` | The game's cover | Portada del juego |
| `coverPicker.titleFor` | Choose the {{system}} cover | Elegir la carátula de {{system}} |
| `coverPicker.photos` | Photos of your copies | Fotos de tus copias |
| `coverPicker.paste` | Use this image | Usar esta imagen |
| `copy.system` | System | Sistema |
| `copy.systemHelp` | Automatic: {{system}}. Change it if this copy is played on another system. | Automático: {{system}}. Cámbialo si esta copia se juega en otro sistema. |

  Change `photos.useAsCover` to "Use as this edition's cover" / "Usar como carátula de esta edición" and `photos.isCover` to "This edition's cover" / "Carátula de esta edición".

- [ ] **Step 2: The current edition in the sheet** (`GameDetail.tsx`)
  - `Props` gains `/** The edition to open on; empty or absent: the main one. */ system?: string;`, passed down to `GameDetailBody`.
  - In `GameDetailBody`:

```tsx
  const [chosen, setChosen] = useState(system ?? '');
  // The edition shown: the one chosen, while the game still has it, else the main one.
  const current = game.editions.some((e) => e.system === chosen) ? chosen : mainSystem(game);
  const edition = game.editions.find((e) => e.system === current);
  // The system whose box the hero shows (null: none), to say when it is borrowed.
  const [shownBox, setShownBox] = useState<string | null>(current);
  const borrowed = shownBox !== current;
  const editionPhotos = game.copies.filter((c) => c.effectiveSystem === current).flatMap((c) => c.photos);
```
    Follow the edition the library opens: `useEffect(() => setChosen(system ?? ''), [game.id, system]);` (‹ › in "By platform" can move to another edition of the same game, so `game.id` alone is not enough).
  - The `Dialog` type's `coverPicker` member becomes `{ type: 'coverPicker'; photosOnly?: boolean }`.
  - Hero: the backdrop and the cover show the current edition, the title line shows its system, and chips switch editions:

```tsx
          <div className="hero-backdrop" aria-hidden="true"><Cover game={game} system={current} /></div>
          …
          <div className="hero-cover">
            <Cover game={game} system={current} onShown={setShownBox} />
            {current && borrowed && (
              <p className="cover-note">{shownBox ? t('edition.borrowedHelp', { system: shownBox }) : t('edition.noBox')}</p>
            )}
            {current && (
              <button type="button" className="ghost small-button" onClick={() => setDialog({ type: 'coverPicker' })}>
                <Icon name="image" size={16} />{t('coverPicker.open')}
              </button>
            )}
            {current && borrowed && editionPhotos.length > 0 && (
              <button type="button" className="ghost small-button" onClick={() => setDialog({ type: 'coverPicker', photosOnly: true })}>
                <Icon name="image" size={16} />{t('edition.usePhoto')}
              </button>
            )}
          </div>
          <div className="hero-body">
            <h2 id="sheet-title" className="hero-title">{game.title}</h2>
            {game.editions.length >= 2 && (
              <div className="edition-chips" role="group" aria-label={t('edition.chips')}>
                {game.editions.map((e) => (
                  <button key={e.system} type="button" className={`edition-chip ${e.system === current ? 'active' : ''}`}
                    aria-pressed={e.system === current} onClick={() => setChosen(e.system)}>{e.system}</button>
                ))}
              </div>
            )}
            <div className="hero-line">
              {release && <span className="hero-year">{release}</span>}
              {current && <PlatformBadge platform={current} full />}
              {platformHoldings(game).map((h) => <PlatformBadge key={h.platform} {...h} full onSelect={onPlatform} />)}
              {edition && (edition.main
                ? <span className="badge">{t('edition.isMain')}</span>
                : <button type="button" className="link" disabled={busy}
                  onClick={() => run(async () => putGame((await gameClient.setMainEdition({ gameId: game.id, system: current })).game!))}>
                  {t('edition.useAsMain')}
                </button>)}
            </div>
```
    Show `edition.isMain` only when the game has two or more editions (with one it says nothing useful).
  - The picker dialog works on the current edition:

```tsx
      {dialog?.type === 'coverPicker' && current && (
        <CoverPicker game={game} system={current} photosOnly={dialog.photosOnly} onClose={() => setDialog(null)}
          onPick={(cover) => {
            run(async () => putGame((await gameClient.setEditionCover({ gameId: game.id, system: current, cover })).game!));
            setDialog(null);
          }} />
      )}
```
  - The add-copy dialog passes no `sourceSystem`; `EditCopyDialog` passes the copy's source system when the API lets us infer it:

```tsx
  // The API exposes the effective system only: without an override, a system that differs from the
  // platform's is the source's.
  const d = copy.details!;
  const sourceSystem = !d.system && copy.effectiveSystem !== systemOf(d.platform) ? copy.effectiveSystem : '';
  return <CopyForm initial={{ ...d }} sourceSystem={sourceSystem} … />;
```

- [ ] **Step 3: Copies grouped by system** (`GameDetail.tsx`, `CopiesTab`)
  - `CopiesTab` receives `current: string`. Move the body of the `game.copies.map((c) => { … <li> … })` callback, unchanged, into a component in the same file, `function CopyCard({ game, copy, busy, run, setDialog, redeeming, setRedeeming, onDelete }: …)`, so it can be rendered per group (`markRedeemed` and `deleteCopy` stay in `CopiesTab` and are passed down).
  - Render the groups (a heading only when the game has two or more systems):

```tsx
      {groupCopies(game.copies, game.editions, current).map((group) => (
        <section key={group.system} className="copies-group">
          {game.editions.length >= 2 && <h3 className="copies-system"><PlatformBadge platform={group.system} full /></h3>}
          <ul className="copy-cards">
            {group.copies.map((c) => <CopyCard key={c.id} game={game} copy={c} … />)}
          </ul>
        </section>
      ))}
```

- [ ] **Step 4: The cover picker per edition** (`CoverPicker.tsx`)

```tsx
export default function CoverPicker({ game, system, photosOnly = false, onPick, onClose }: {
  game: Game;
  /** The edition whose cover is chosen. */
  system: string;
  /** Show only the photos of the edition's copies ("Use a photo"). */
  photosOnly?: boolean;
  onPick: (choice: CoverChoice) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const edition = game.editions.find((e) => e.system === system);
  // The photos of the edition's copies, each once (copies may share one).
  const photos = game.copies.filter((c) => c.effectiveSystem === system).flatMap((c) => c.photos)
    .filter((p, i, all) => all.findIndex((x) => x.id === p.id) === i);
  const [candidates, setCandidates] = useState<CoverCandidate[] | null>(photosOnly ? [] : null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [pasted, setPasted] = useState('');

  useEffect(() => {
    if (photosOnly) return;
    coverClient.listCoverCandidates({ gameId: game.id, system })
      .then((res) => { setCandidates(res.candidates); setWarnings(res.warnings); })
      .catch((e) => setError(errorMessage(e)));
  }, [game.id, system, photosOnly]);
```
  The modal (title `t('coverPicker.titleFor', { system })`) shows, in order:
  1. when there are photos, a `<h3>{t('coverPicker.photos')}</h3>` and a `posters picker-grid` of buttons with `<img src={photoUrl(p.id, true)} />`, `selected` when `edition?.coverPhotoId === p.id`, picking `{ case: 'photoId', value: p.id }`;
  2. unless `photosOnly`: the intro, errors and warnings, "Automatic" (`selected` when the edition has neither a URL nor a photo, picking `{ case: 'clear', value: true }`), the candidates (`selected` when `edition?.coverUrl === c.url`, picking `{ case: 'url', value: c.url }`) and the "none" message as today;
  3. unless `photosOnly`: a form to paste an image address, using the strings the Edit tab used:

```tsx
          <form className="row tight cover-paste" onSubmit={(e) => { e.preventDefault(); if (pasted.trim()) onPick({ case: 'url', value: pasted.trim() }); }}>
            <label className="grow">
              {t('game.coverUrl')}
              <input type="url" value={pasted} placeholder={t('game.coverUrlPlaceholder')} onChange={(e) => setPasted(e.target.value)} />
            </label>
            <button type="submit" disabled={!pasted.trim()}>{t('coverPicker.paste')}</button>
          </form>
```
  A URL the server refuses comes back as the sheet's error (the `run` in `GameDetail` shows it).

- [ ] **Step 5: The System field** (`CopyForm.tsx`)
  - Props gain `/** The system the copy's source gives it, if any: what "automatic" means for it. */ sourceSystem?: string;`.
  - After the Platform `<label>`:

```tsx
          <label>
            {t('copy.system')}
            <input list="copy-systems" value={d.system} placeholder={automatic} onChange={(e) => set('system', e.target.value)} />
            <datalist id="copy-systems">{SYSTEMS.map((s) => <option key={s} value={s} />)}</datalist>
            <span className="help">{t('copy.systemHelp', { system: automatic })}</span>
          </label>
```
    with `const automatic = sourceSystem || systemOf(d.platform);` (it follows the platform as it is typed).
  - In `submit`, the system goes back to automatic when it is empty or names what automatic gives:

```tsx
      const typed = d.system.trim();
      const system = typed === '' || systemOf(typed) === automatic ? '' : typed;
      await onSubmit({ ...d, system, price: …, fields: values });
```

- [ ] **Step 6: Styles** (`web/src/styles.css`)

```css
.edition-chips { display: flex; flex-wrap: wrap; gap: 6px; margin: 6px 0 2px; }
.edition-chip { border-radius: 999px; min-height: 30px; padding: 3px 12px; font-size: 13.5px; font-weight: 600;
  background: rgb(255 255 255 / .1); color: rgb(255 255 255 / .86); border-color: rgb(255 255 255 / .18); }
.edition-chip.active { background: #fff; color: var(--ink, #181c2c); border-color: #fff; }
.cover-note { margin: 0; font-size: 12.5px; color: rgb(255 255 255 / .78); }
.copies-group + .copies-group { margin-top: 18px; }
.copies-system { margin: 0 0 8px; font-size: 14px; }
.cover-paste { margin-top: 14px; align-items: flex-end; flex-wrap: wrap; }
```
  The hero is always dark, so its chips use light-on-dark colours (check `.hero` in `styles.css` and reuse its tokens if it defines them). The amber accent stays for primary actions.

- [ ] **Step 7: Type check and tests**

Run: `task test`. Expected: green.

- [ ] **Step 8: Browser check.** `task test-server`, then http://127.0.0.1:8093/?v=<a new number>. With a game of the copy that has copies on two consoles (if none does, add a PS3 disc and an Xbox 360 disc to a game of the copy by hand):
  - clicking its Xbox 360 card opens the sheet on Xbox 360 (chip pressed, Xbox 360 box and badge); the chips switch editions; the Copies tab lists the current edition's copies first, under system headings;
  - on an edition without its own box, the hero shows the borrowed box with "No box of its own yet…", **Choose cover…** and, when its copies have photos, **Use a photo**;
  - **Choose cover…** lists only that system's proposals; picking one changes that edition's card only; pasting an image URL works; **Use a photo** lists only that edition's copies' photos;
  - **Use as the game's cover** on a non-main edition: in "By game", the card now shows that box;
  - editing a copy: the System field shows the automatic system as its placeholder; typing "PS4" on a PS3 disc moves it to a PS4 edition (a new card, a new chip); emptying it brings it back.
  Then the `mobile` preset (reload): chips wrap, the picker and the copy form fit at 375 px. Reset with `desktop`, `task test-server:stop`.

- [ ] **Step 9: Commit**

```bash
git add web/src
git commit -m "Game sheet: editions as chips, a cover per edition, use as the game's cover; copies by system; System field"
```

---
### Task 10: Documentation, memory and the final check

**Files:**
- Modify: `docs/technical.md`, `README.md`, `docs/plugins.md`, `.claude/docs/integrations.md`, `.claude/commands/new-cover-provider.md`, `.claude/memory/data-model.md`, `.claude/memory/provider-chains.md`, `.claude/MEMORY.md`, `.claude/docs/ui.md`

**Interfaces:**
- Consumes: everything above. Produces: documentation only.

- [ ] **Step 1: `docs/technical.md`**
  - **Domain model** table: add a row `| Edition | Derived from the copies | One per system of a game's copies (PC, PS3, Xbox 360…). It holds only its chosen cover (a URL or a photo of one of its copies); the game stores those by system and an optional main system. |`, and in the `Copy` row add "and the system it is played on (see below)".
  - After the play status paragraph, a section **Systems and editions**: a copy's platform says where it lives; its **system** is where it is played: the user's override, else the source's (PlayStation copies say PS4 or PS5), else the one its platform implies (the mapping table of the spec's "System of a copy", in one sentence per group). Scans never touch the override; an unknown platform is its own system and an empty one is `Other`. A game has one **edition** per system; the **main edition** is the one the user chose (**Use as the game's cover**), else one with a physical copy, then the one with the most copies, then the first by system. Chosen covers whose system has no copy left are dropped, a photo cover must be of a copy of that system, and a main system without copies is cleared. Merging games keeps the kept game's covers and main system and fills the gaps from the other. Redundant keys still compare platforms; play status, rating, notes and custom fields stay on the game.
  - In the copies paragraph that says "Game documents are version 2", say "version 3": version-2 documents are converted when read (a cover photo goes to its copy's edition, which becomes the main one; a cover URL to the default main edition, unless the photo took it); migration `0010` only triggers the pre-migration backup.
  - **CSV** paragraph: add `system` to the column list and one sentence: the export writes each copy's effective system; the import keeps it as the copy's own system only when it differs from the one the platform implies; empty means automatic.
  - **Metadata providers and covers**: covers are resolved per edition. Replace the table's "Applies to" cells: chosen cover → "the edition it was chosen for"; TheGamesDB → "any system, searching that system's box art: physical copies, editions no store knows, and on the fallback pass"; Steam, Epic, GOG, Ubisoft, EA, Battle.net → "the PC edition of games linked to that store"; Xbox → "the PC and Xbox editions of games linked to the Microsoft Store". Then: the resolution order of an edition (chosen photo, chosen URL, provider chain with the fallback pass, the base game's cover for the same system for add-ons, else "missing", remembered 7 days per edition); `GET /media/covers/{gameId}/{system}` (URL-escaped) and `GET /media/covers/{gameId}` (main edition); a 404 is `no-store`; **Choose cover…** proposes the edition's system's covers and the photos of that edition's copies; a scan or an edit drops only the cached covers of the editions whose copies, chosen cover or links changed. Game sheets (details) stay per game.
  - The **config directory** table, `game-data/` row: `cover-<system>-<hash>.jpg` per edition (and `.missing` markers); a `cover.jpg` from before editions is adopted by the main edition on first view.
  - **Browsing the library**: the "By platform | By game" selector (remembered per device, "By platform" by default), edition cards (system badge, small store badges on PC, a borrowed box labelled "<system> box"), "N editions of M games", the System filter (counted by editions or by games), copy-level filters keep an edition when one of its copies matches, game-level ones and the search apply to all of a game's editions, ties sorted by system. Replace "Platform badges on every cover…" with the system badges of "By game".
  - **Game sheets**: it opens on the clicked edition (else the main one); chips switch editions; the hero says when the box is borrowed and offers **Choose cover…** and **Use a photo**; **Use as the game's cover**; copies grouped by system; the copy form's **System** field.
  - **Photos of copies**: "One of them can be the game's cover" becomes "One of them can be the cover of its copy's edition"; `SetCoverPhoto` becomes `SetEditionCover` in the API list.

- [ ] **Step 2: README.md.** In the "A proper catalog" bullet, after "box art", add one clause: "for each platform you own a game on (a PS3 disc and an Xbox 360 disc each show their own box)". Nothing else changes (no implementation details).

- [ ] **Step 3: Contributor guides**
  - `docs/plugins.md`, "Rules for media providers", the `Applies(q)` bullet: add "`q.System` is the edition's system (`""` when the query names none): a store provider applies only when `q.ForPC()`; a console catalog checks the system (see `xbox.IsXboxSystem`)."
  - `.claude/docs/integrations.md`, "A new cover provider", step 2: the same sentence.
  - `.claude/commands/new-cover-provider.md`: "`Applies` decides without network calls, by `q.System` (`q.ForPC()` for PC stores); quota-limited providers respect `HasStoreLink` and `Fallback`."
  - `.claude/docs/ui.md`: in Structure, `LibraryPage` "(… by platform or by game, …)", `GameDetail` "(… edition chips …)", `components/Cover.tsx` "(edition cover with the borrowed-box fallback)", and `lib/editions.ts` "(editions, library filters, cover order: pure, Node-tested)"; `api/client.ts`: `coverUrl(game, system)`.

- [ ] **Step 4: Project memory**
  - `.claude/memory/data-model.md`: a new bullet **Editions (2026-10-10):** effective system per copy (`CopyDetails.System` override, `Copy.SourceSystem`, `SystemOf(platform)`), editions derived, `Game.covers` by system and `mainSystem`, game documents v3 (`covers`, `mainSystem`, copies' `system`, `sourceSystem`; v2 converted on read: photo → its edition and main, URL → default main), migration 0010 only for the backup. Systems names (`PC`, console names, `Other`) are stored in documents and cover file names: do not rename them. Link [[provider-chains]].
  - `.claude/memory/provider-chains.md`: covers are per (game, system); store providers apply only when `q.ForPC()`, Xbox to PC and Xbox systems, TheGamesDB to any system with its quota rules per edition (and its search filtered to the edition's system); `coverLogicChanged` bumped on 2026-10-10 for editions; the legacy `cover.jpg` is adopted by the main edition.
  - `.claude/MEMORY.md`: update the two index lines' summaries ("… store Links and editions (a cover per system) …", "… covers per edition; Applies by system …").

- [ ] **Step 5: The whole branch**

Run: `task lint`, `task test`, and the CI freshness check: `task generate && git status --porcelain proto internal/gen web/src/gen` (expected: nothing to commit).

- [ ] **Step 6: Final browser pass.** `task test-server -- --full` (copies images, so legacy `cover.jpg` adoption is visible), http://127.0.0.1:8093/?v=<a new number>, desktop then `mobile`:
  - both library views; a game with editions on two consoles, each with its own box; a borrowed cover with its label; switching editions in the sheet; choosing an edition's cover; changing the main edition; a system override on a copy; the System filter;
  - covers that existed before show at once in "By game" (adopted, not downloaded again).
  Reset the viewport to `desktop`, then `task test-server:stop`.

- [ ] **Step 7: Commit**

```bash
git add docs README.md .claude
git commit -m "Docs: platform editions (model, covers, library, CSV) and project memory"
```

---

## Self-review

- **Spec coverage.** System of a copy, override, source system, effective order → Task 1. Editions, covers map, main system and its rule, orphans, override away from a photo, merge, `SetEditionCover` / `SetMainSystem` validation → Task 2. Documents v3, v2 conversion, backup → Task 2. `CoverQuery.System`, `QueryFor(g, system)`, `Applies` per provider, resolution order, add-ons per system, cache and markers per (game, system), `coverLogicChanged`, both endpoints, 404 `no-store`, candidates by system → Task 3. Sync invalidation per edition, override kept by scans, PlayStation PS4/PS5 → Tasks 1 and 4. API (`editions`, `main_system`, `effective_system`, `CopyDetails.system`, `SetEditionCover`, `SetMainEdition`, candidates `system`, errors naming the system, `cover_url`/`cover_photo` removed) → Task 5. CSV → Task 6. Pure web functions and Node tests (grouping, filters in both views, System filter, counts, cover fallback order) → Task 7. Library views, edition cards, System filter, counts, sorting ties, borrowed label → Task 8. Sheet on the clicked edition, chips, borrowed note, Choose cover / Use a photo, Use as the game's cover, copies grouped, System field → Task 9. Docs, README line, memory → Task 10. Browser checks → Tasks 8, 9, 10.
- **Bridges.** Task 2 adds three kinds of bridges, each tagged `// Bridge (Task N)`; Tasks 3, 4 and 5 remove them and grep for the tag.
- **Names used across tasks.** `SystemOf`, `SystemPC`, `SystemOther`, `(Copy).System()`, `CopyDetails.System`, `Copy.SourceSystem`, `ImportedCopy.System`; `EditionCover`, `Edition`, `Editions()`, `Edition(system)`, `MainEdition()`, `MainSystem()`, `Covers()`, `SetEditionCover`, `SetMainSystem`, `RestoreEditions`, `AdoptGameCover`, `CoverFingerprints`, `ChangedSystems`, `ParseCoverURL`; `CoverQuery.System`, `ForPC()`, `QueryFor(g, system)`, `EditionCover(ctx, id, system)`, `InvalidateEdition`, `ErrNoEdition`, `AdoptLegacyCover`, `DeleteCovers`; proto `Edition`, `SetEditionCover`, `SetMainEdition`; web `coverPath`, `mainSystem`, `libraryItems`, `filterItems`, `systemCounts`, `sortItems`, `itemCounts`, `coverOrder`, `nextCover`, `groupCopies`, `systemOf`, `SYSTEMS`, `CoverChoice`.
