# Richer physical copies — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give copies a structured grade and contents (physical only), a purchase price with currency (every kind) and a default-currency preference, end to end: domain, storage (document v2 with conversion of the old free-text condition), API, CSV (English only), copy form, game sheet, Scan batch defaults and System preferences.

**Architecture:** New value types in `internal/domain/game` (`Grade`, `Content`/`Contents` as a bit set, `Money` in the currency's minor units) replace `CopyDetails.Condition`; `settings.Preferences` holds the default currency. The sqlite adapter writes game documents as version 2 and converts version-1 copies' `condition` on read. Proto gains `CopyGrade`, `CopyContent`, `Money` and `Get/UpdatePreferences`; the CSV codec gains four columns and loses every Spanish alias; the React UI edits and shows the new fields.

**Tech Stack:** Go 1.26, SQLite (JSON documents), ConnectRPC/buf, React 19 + TypeScript, i18next, testify.

**Spec:** `docs/superpowers/specs/2026-10-09-physical-copy-details-design.md`

## Global Constraints

- Everything runs in the toolchain container through Task (`task lint`, `task test`, `task generate`, `task go -- test ./pkg/ -run X -v`); never Go/Node on the host.
- Load the `write-go` skill before Go. `task lint` enforces one struct field per line in struct literals and types (`tools/fieldlines`), blank lines between documented members and docs on interface methods (`tools/docspacing`), golangci-lint.
- Tests: GIVEN / WHEN / THEN subtests with testify; `context.WithTimeout(t.Context(), 10*time.Second)`, never a bare context.
- Never edit an old migration; this work needs no SQL migration (documents).
- Never open `config/`; real-data checks use `task test-server` copies.
- Repository text is English; every UI string goes through `t()` with keys in both `en.json` and `es.json` (`task i18n`).
- CSV is English only: no Spanish header or value aliases.
- Branch `feature/physical-copy-details`, one PR to `main`, no tags.

## Review Focus

- A currency without two decimals (JPY has none, BHD has three) must keep its real value: `Money.Amount` is in the currency's own minor unit, CSV and UI use its real number of decimals (tests in Task 1 and Task 4).
- A scan of a store library must never clear the grade, contents or price a user set on that copy (test in Task 1).
- A copy whose old condition was free text the conversion does not know must not lose it: it goes to the notes (test in Task 2).
- A price typed with the "other" decimal separator, or with thousands separators (`1.234,50`, `1,234.50`), must be read as the user meant, and garbage must be refused, not saved as 0 (tests in Task 5's `parseAmount`).
- Changing a copy's kind from physical to key must drop its grade, contents and location but keep its price (test in Task 1).

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/domain/game/physical.go` (create) | `Grade`, `Content`, `Contents`, `Money`, `CurrencyDigits` |
| `internal/domain/game/physical_test.go` (create) | Their validation and normalization |
| `internal/domain/game/copy.go` (modify) | `CopyDetails` fields, `normalize`, `applyImport` |
| `internal/domain/settings/settings.go` (modify) | `Preferences`, repository methods |
| `internal/adapters/outbound/sqlite/docs.go`, `docs_test.go`, `settings_repository.go` (modify) | Copy document v2, v1 conversion, preferences storage |
| `proto/gamevault/v1/game.proto`, `system.proto` (modify) + generated code | API |
| `internal/adapters/inbound/rpc/mapper.go`, `system_handler.go`, `server_test.go` (modify) | API mapping, preferences RPC, end-to-end test |
| `internal/application/system/service.go` (modify) | Preferences use case |
| `internal/adapters/outbound/csvfile/codec.go`, `codec_test.go` (modify) | CSV columns, English only |
| `internal/application/transfer/service.go` (modify) | Default currency on import |
| `cmd/gamevault/main.go` (modify) | Wiring |
| `web/src/lib/model.ts`, `web/src/lib/money.ts` (create) | Grade/content keys, amount parsing and formatting, currencies |
| `web/src/features/library/CopyForm.tsx`, `GameDetail.tsx`, `web/src/features/scan/ScanPage.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/styles.css`, `web/src/i18n/locales/{en,es}.json` (modify) | UI |
| `docs/technical.md`, `.claude/memory/data-model.md` (modify) | Docs |

---

### Task 1: Domain types and CopyDetails

**Files:**
- Create: `internal/domain/game/physical.go`, `internal/domain/game/physical_test.go`
- Modify: `internal/domain/game/copy.go`, `internal/domain/game/consolidate_test.go` (one new test), `internal/domain/settings/settings.go`
- Modify (keep the build green after removing `Condition`): `internal/adapters/inbound/rpc/mapper.go` (drop the two `Condition:` lines), `internal/adapters/outbound/sqlite/docs.go` (drop `Condition: c.Condition` in both mappings; keep the `copyDoc.Condition` field, Task 2 uses it), `internal/adapters/outbound/csvfile/codec.go` (drop `Condition: get("condition")`; write `""` where `c.Condition` was exported — Task 4 rewrites the columns), `internal/adapters/outbound/sqlite/docs_test.go` (drop `Condition: "Complete"` from `sampleGame`)

**Interfaces:**
- Produces:
  - `type Grade string` with `GradeSealed`, `GradeMint`, `GradeVeryGood`, `GradeGood`, `GradeAcceptable`, `GradeDamaged`; `Grades []Grade` (in that order); `func (g Grade) Valid() bool` (empty is valid).
  - `type Content string` with `ContentBox`, `ContentManual`, `ContentMedia`, `ContentExtras`; `AllContents []Content` (in that order).
  - `type Contents uint8` (a set); `func ContentsOf(cs ...Content) (Contents, error)`; `func (c Contents) Has(x Content) bool`; `func (c Contents) List() []Content` (fixed order).
  - `type Money struct { Amount int64; Currency string }`; `func (m Money) normalize() (Money, error)`; `func (m Money) IsZero() bool`.
  - `func CurrencyDigits(code string) int` (ISO 4217 minor-unit digits; 2 unless listed).
  - `CopyDetails` gains `Grade Grade`, `Contents Contents`, `Price Money`; loses `Condition`.
  - `settings.Preferences{Currency string}`; `func (p Preferences) Validate() (Preferences, error)`; `settings.Repository` gains `Preferences(ctx) (Preferences, error)` and `SavePreferences(ctx, Preferences) error`.

- [ ] **Step 1: Write the failing domain tests** — `internal/domain/game/physical_test.go`

```go
package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContents(t *testing.T) {
	t.Run("GIVEN contents given out of order and repeated", func(t *testing.T) {
		c, err := ContentsOf(ContentMedia, ContentBox, ContentMedia)
		require.NoError(t, err)

		t.Run("THEN they are a set, listed in the fixed order", func(t *testing.T) {
			assert.Equal(t, []Content{ContentBox, ContentMedia}, c.List())
			assert.True(t, c.Has(ContentBox))
			assert.False(t, c.Has(ContentManual))

			same, _ := ContentsOf(ContentBox, ContentMedia)
			assert.Equal(t, same, c)
		})
	})

	t.Run("GIVEN an unknown content", func(t *testing.T) {
		_, err := ContentsOf(ContentBox, "poster")

		t.Run("THEN it is refused", func(t *testing.T) {
			assert.Error(t, err)
		})
	})
}

func TestGrade(t *testing.T) {
	for _, g := range append([]Grade{""}, Grades...) {
		assert.True(t, g.Valid(), "grade %q", g)
	}

	assert.False(t, Grade("excellent").Valid())
}

func TestMoney(t *testing.T) {
	cases := []struct {
		name string
		in   Money
		want Money
		ok   bool
	}{
		{"a price is kept, currency upper-cased", Money{Amount: 2995, Currency: " eur "}, Money{Amount: 2995, Currency: "EUR"}, true},
		{"no amount means no price", Money{Amount: 0, Currency: "EUR"}, Money{}, true},
		{"an amount needs a currency", Money{Amount: 100}, Money{}, false},
		{"a currency is three letters", Money{Amount: 100, Currency: "EURO"}, Money{}, false},
		{"no negative prices", Money{Amount: -1, Currency: "EUR"}, Money{}, false},
	}
	for _, c := range cases {
		got, err := c.in.normalize()
		if !c.ok {
			assert.Error(t, err, c.name)
			continue
		}

		require.NoError(t, err, c.name)
		assert.Equal(t, c.want, got, c.name)
	}
}

func TestCurrencyDigits(t *testing.T) {
	assert.Equal(t, 2, CurrencyDigits("EUR"))
	assert.Equal(t, 0, CurrencyDigits("JPY"))
	assert.Equal(t, 3, CurrencyDigits("BHD"))
	assert.Equal(t, 2, CurrencyDigits("XYZ"), "unknown codes default to 2")
}

func TestCopyDetails_physicalFields(t *testing.T) {
	contents, _ := ContentsOf(ContentBox, ContentMedia)
	physical := CopyDetails{
		Kind:     KindPhysical,
		Platform: "PS4",
		Grade:    GradeVeryGood,
		Contents: contents,
		Location: "Shelf",
		Price:    Money{Amount: 2995, Currency: "eur"},
	}

	t.Run("GIVEN a physical copy with grade, contents, location and price", func(t *testing.T) {
		got, err := physical.normalize()
		require.NoError(t, err)

		t.Run("THEN they are kept", func(t *testing.T) {
			assert.Equal(t, GradeVeryGood, got.Grade)
			assert.Equal(t, contents, got.Contents)
			assert.Equal(t, Money{Amount: 2995, Currency: "EUR"}, got.Price)
		})
	})

	t.Run("GIVEN the same copy turned into a key", func(t *testing.T) {
		key := physical
		key.Kind = KindKey
		got, err := key.normalize()
		require.NoError(t, err)

		t.Run("THEN grade, contents and location go, and the price stays", func(t *testing.T) {
			assert.Empty(t, got.Grade)
			assert.Zero(t, got.Contents)
			assert.Empty(t, got.Location)
			assert.Equal(t, int64(2995), got.Price.Amount)
		})
	})

	t.Run("GIVEN an invalid grade or price", func(t *testing.T) {
		bad := physical
		bad.Grade = "excellent"
		_, errGrade := bad.normalize()

		bad = physical
		bad.Price = Money{Amount: 10}
		_, errPrice := bad.normalize()

		t.Run("THEN the copy is refused", func(t *testing.T) {
			assert.Error(t, errGrade)
			assert.Error(t, errPrice)
		})
	})
}
```

And in `consolidate_test.go`, a scan must keep the user's values:

```go
func TestConsolidator_keepsWhatTheUserSet(t *testing.T) {
	t.Run("GIVEN a library copy the user priced", func(t *testing.T) {
		r := NewConsolidator(nil).Apply("s", []ImportedCopy{{
			ExternalID: "steam:1",
			Title:      "Portal",
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "Steam",
			},
		}}, t0)
		g := r.Changed[0]
		_, err := g.UpdateCopy(g.Copies()[0].ID, CopyDetails{
			Kind:     KindLibrary,
			Platform: "Steam",
			Price:    Money{Amount: 999, Currency: "EUR"},
		}, t0)
		require.NoError(t, err)

		t.Run("WHEN the library is scanned again", func(t *testing.T) {
			NewConsolidator([]*Game{g}).Apply("s", []ImportedCopy{{
				ExternalID: "steam:1",
				Title:      "Portal",
				Details: CopyDetails{
					Kind:     KindLibrary,
					Platform: "Steam",
				},
			}}, t0)

			t.Run("THEN the price is still there", func(t *testing.T) {
				assert.Equal(t, Money{Amount: 999, Currency: "EUR"}, g.Copies()[0].Price)
			})
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/domain/game/ -run 'TestContents|TestGrade|TestMoney|TestCurrencyDigits|TestCopyDetails_physicalFields|TestConsolidator_keepsWhatTheUserSet' -v`
Expected: build failure (`undefined: ContentsOf`, `undefined: Money`…).

- [ ] **Step 3: Write `physical.go`**

```go
package game

import (
	"slices"
	"strings"
)

// Grade is how good a physical copy looks, as collectors grade it. Empty means not stated.
type Grade string

// Values of Grade, best first.
const (
	GradeSealed     Grade = "sealed"
	GradeMint       Grade = "mint"
	GradeVeryGood   Grade = "very_good"
	GradeGood       Grade = "good"
	GradeAcceptable Grade = "acceptable"
	GradeDamaged    Grade = "damaged"
)

// Grades lists every grade, best first.
var Grades = []Grade{GradeSealed, GradeMint, GradeVeryGood, GradeGood, GradeAcceptable, GradeDamaged}

// Valid reports whether g is a known grade or empty.
func (g Grade) Valid() bool { return g == "" || slices.Contains(Grades, g) }

// Content is one part of what a physical copy comes with.
type Content string

// Values of Content, in the order they are listed.
const (
	ContentBox    Content = "box"
	ContentManual Content = "manual"
	ContentMedia  Content = "media"  // the disc or cartridge
	ContentExtras Content = "extras" // map, poster, figure, art book…
)

// AllContents lists every content, in the order they are listed.
var AllContents = []Content{ContentBox, ContentManual, ContentMedia, ContentExtras}

// Contents is the set of parts a physical copy comes with. It is a bit set, so it has no
// duplicates, always lists in the same order and compares with ==. Zero means not stated.
type Contents uint8

// ContentsOf builds the set from its parts, refusing unknown ones.
func ContentsOf(cs ...Content) (Contents, error) {
	var out Contents

	for _, c := range cs {
		i := slices.Index(AllContents, c)
		if i < 0 {
			return 0, invalid("content %q is not one of box, manual, media, extras", c)
		}

		out |= 1 << i
	}

	return out, nil
}

// Has reports whether the set holds c.
func (c Contents) Has(x Content) bool {
	i := slices.Index(AllContents, x)
	return i >= 0 && c&(1<<i) != 0
}

// List returns the parts in the fixed order.
func (c Contents) List() []Content {
	var out []Content

	for _, x := range AllContents {
		if c.Has(x) {
			out = append(out, x)
		}
	}

	return out
}

// valid reports whether c holds only known parts.
func (c Contents) valid() bool { return c < 1<<len(AllContents) }

// Money is an amount of an ISO 4217 currency, in that currency's minor unit (cents for EUR and
// USD, yen for JPY, fils for BHD): see CurrencyDigits. The zero value means no price.
type Money struct {
	Amount   int64
	Currency string
}

// IsZero reports whether there is no price.
func (m Money) IsZero() bool { return m.Amount == 0 }

func (m Money) normalize() (Money, error) {
	m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))

	switch {
	case m.Amount < 0:
		return Money{}, invalid("a price cannot be negative")
	case m.Amount == 0:
		return Money{}, nil
	case !isCurrencyCode(m.Currency):
		return Money{}, invalid("a price needs its currency as a three-letter code (EUR, USD…)")
	}

	return m, nil
}

// IsCurrencyCode reports whether s looks like an ISO 4217 code: three letters A–Z.
func IsCurrencyCode(s string) bool { return isCurrencyCode(s) }

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// currencyDigits lists the ISO 4217 currencies whose minor unit is not two digits.
var currencyDigits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// CurrencyDigits returns how many decimals a currency has (2 unless ISO 4217 says otherwise).
func CurrencyDigits(code string) int {
	if d, ok := currencyDigits[strings.ToUpper(code)]; ok {
		return d
	}

	return 2
}
```

(`IsCurrencyCode` is exported for the CSV codec in Task 4.)

- [ ] **Step 4: Change `CopyDetails`** in `copy.go`

Replace `Condition  string  // physical only` with:

```go
	Grade      Grade    // physical only
	Contents   Contents // physical only
	Location   string   // physical only
	Barcode    Barcode  // physical only: EAN/UPC printed on the box
	Price      Money    // what was paid, any kind
	Notes      string
```

(keep the existing `Location`, `Barcode`, `Notes` lines, only once). In `normalize`, remove `&d.Condition` from the trimmed list and, after the kind/status checks:

```go
	if !d.Grade.Valid() {
		return d, invalid("grade %q is not valid", d.Grade)
	}

	if !d.Contents.valid() {
		return d, invalid("the copy's contents are not valid")
	}

	price, err := d.Price.normalize()
	if err != nil {
		return d, err
	}

	d.Price = price

	if d.Kind != KindPhysical {
		d.Barcode, d.Grade, d.Contents, d.Location = "", "", 0, ""
	}
```

(replacing the existing `if d.Kind != KindPhysical { d.Barcode = "" }`). In `applyImport`, remove `set(&c.Condition, in.Condition)` and add after the barcode block:

```go
	if in.Grade != "" {
		c.Grade = in.Grade
	}

	if in.Contents != 0 {
		c.Contents = in.Contents
	}

	if !in.Price.IsZero() {
		c.Price = in.Price
	}
```

`return c.CopyDetails != before` stays valid: every field is comparable.

- [ ] **Step 5: Preferences** in `internal/domain/settings/settings.go`

```go
// Preferences are the user's choices that shape the UI and the imports.
type Preferences struct {
	// Currency is the default currency of new prices (ISO 4217); empty until the user picks one.
	Currency string
}

// Validate normalizes the currency and checks it is a three-letter code (or empty).
func (p Preferences) Validate() (Preferences, error) {
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Currency != "" && !isCurrencyCode(p.Currency) {
		return p, &ValidationError{"the default currency must be a three-letter code (EUR, USD…)"}
	}

	return p, nil
}

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}
```

(`settings` must not import `game`; the three-line check is duplicated on purpose.) Add to `Repository`:

```go
	// Preferences returns the saved preferences, empty when none were saved.
	Preferences(ctx context.Context) (Preferences, error)

	// SavePreferences stores the preferences.
	SavePreferences(ctx context.Context, p Preferences) error
```

Add a stub to `internal/adapters/outbound/sqlite/settings_repository.go` so the build stays green until Task 2 implements it:

```go
// Preferences returns the saved preferences (implemented in Task 2).
func (r *SettingsRepository) Preferences(context.Context) (settings.Preferences, error) {
	return settings.Preferences{}, nil
}

// SavePreferences stores the preferences (implemented in Task 2).
func (r *SettingsRepository) SavePreferences(context.Context, settings.Preferences) error { return nil }
```

Any other implementation of `settings.Repository` in tests (search `SaveLogging(` in `_test.go`) gets the same two methods.

- [ ] **Step 6: Keep the build green** — the callers listed under Files: drop `Condition` from `mapper.go` (both directions), `docs.go` (both mappings; keep the struct field), `codec.go` (decode: remove the `Condition:` line; encode: replace `c.Condition` with `""`), `docs_test.go` (`sampleGame`).

- [ ] **Step 7: Run the tests**

Run: `task go -- test ./internal/domain/... ./internal/adapters/... -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: every package `ok`, the new tests PASS.

- [ ] **Step 8: Lint and commit**

`task lint:fix && task lint` → `0 issues.`

```bash
git add internal/domain internal/adapters
git commit -m "Add grade, contents and price to copies, and a default-currency preference"
```

---

### Task 2: Copy document v2 and stored preferences

**Files:**
- Modify: `internal/adapters/outbound/sqlite/docs.go`, `docs_test.go`, `settings_repository.go`, `internal/adapters/outbound/sqlite/auth_repository_test.go` (or a new `settings_repository_test.go`)

**Interfaces:**
- Consumes: Task 1's `Grade`, `Contents`, `ContentsOf`, `Money`, `settings.Preferences`.
- Produces: `copyDoc` fields `grade`, `contents`, `priceAmount`, `priceCurrency`; `gameDocVersion = 2`, `sourceDocVersion = 1`, `providerDocVersion = 1`; `convertCondition(text string) (game.Grade, game.Contents, bool)`; working `Preferences`/`SavePreferences`.

- [ ] **Step 1: Write the failing tests** (append to `docs_test.go`)

```go
func TestDocuments_physicalFields(t *testing.T) {
	t.Run("GIVEN a physical copy with grade, contents and price", func(t *testing.T) {
		contents, _ := game.ContentsOf(game.ContentBox, game.ContentMedia)
		g := game.Rehydrate("g1", game.Info{Title: "Halo 3"}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Xbox 360",
				Status:   game.StatusOwned,
				Grade:    game.GradeGood,
				Contents: contents,
				Price: game.Money{
					Amount:   1500,
					Currency: "JPY",
				},
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the new fields come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":2`)
				assert.Contains(t, raw, `"contents":["box","media"]`)
			})
		})
	})
}

func TestDocuments_conditionConversion(t *testing.T) {
	cases := []struct {
		condition string
		grade     game.Grade
		contents  []game.Content
		note      string
	}{
		{"Sealed", game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"precintado", game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"Complete (case + manual)", "", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"Completo (caja + manual)", "", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{" Case and disc ", "", []game.Content{game.ContentBox, game.ContentMedia}, ""},
		{"Caja y disco", "", []game.Content{game.ContentBox, game.ContentMedia}, ""},
		{"Disc only", "", []game.Content{game.ContentMedia}, ""},
		{"Sólo disco", "", []game.Content{game.ContentMedia}, ""},
		{"Damaged", game.GradeDamaged, nil, ""},
		{"Dañado", game.GradeDamaged, nil, ""},
		{"Like new, no slip cover", "", nil, "signed\nCondition: Like new, no slip cover"},
	}

	for _, c := range cases {
		t.Run("GIVEN a version-1 copy whose condition is "+c.condition, func(t *testing.T) {
			raw := `{"v":1,"title":"Halo 3","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",
				"copies":[{"id":"c1","kind":"physical","status":"owned","notes":"signed","condition":` + strconv.Quote(c.condition) + `}]}`

			got, err := decodeGame("g1", raw)
			require.NoError(t, err)

			t.Run("THEN it becomes a grade and contents, or a note", func(t *testing.T) {
				cp := got.Copies()[0]
				assert.Equal(t, c.grade, cp.Grade)
				assert.Equal(t, c.contents, cp.Contents.List())

				want := c.note
				if want == "" {
					want = "signed"
				}

				assert.Equal(t, want, cp.Notes)
			})
		})
	}
}
```

Add `"strconv"` to the test imports. And the preferences round trip (in a new `settings_repository_test.go`):

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/settings"
)

func TestSettingsRepository_preferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	repo := NewSettingsRepository(openTest(t))

	t.Run("GIVEN no saved preferences", func(t *testing.T) {
		p, err := repo.Preferences(ctx)

		t.Run("THEN they are empty", func(t *testing.T) {
			require.NoError(t, err)
			assert.Empty(t, p.Currency)
		})
	})

	t.Run("WHEN a default currency is saved", func(t *testing.T) {
		require.NoError(t, repo.SavePreferences(ctx, settings.Preferences{Currency: "GBP"}))

		t.Run("THEN it is read back", func(t *testing.T) {
			p, err := repo.Preferences(ctx)
			require.NoError(t, err)
			assert.Equal(t, "GBP", p.Currency)
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run 'TestDocuments_physicalFields|TestDocuments_conditionConversion|TestSettingsRepository_preferences' -v`
Expected: FAIL — `"v":2` missing, grades empty, the stub returns no preferences.

- [ ] **Step 3: Implement in `docs.go`**

Replace `const docVersion = 1` and `checkVersion` with per-kind versions:

```go
// Format versions of each document kind. Adding a field keeps a version; changing the meaning of
// one bumps it, and the decoder converts older documents when it reads them (see
// docs/superpowers/specs/2026-10-09-json-documents-design.md).
const (
	// gameDocVersion 2 replaced the copies' free-text condition with grade and contents.
	gameDocVersion     = 2
	sourceDocVersion   = 1
	providerDocVersion = 1
)

func checkVersion(v, current int) error {
	if v > current {
		return fmt.Errorf("document version %d: %w", v, errNewerDocument)
	}

	return nil
}
```

Use `gameDocVersion`/`sourceDocVersion`/`providerDocVersion` in the encoders and `checkVersion(doc.V, <kind>DocVersion)` in the decoders. In `copyDoc`, keep `Condition` (read-only, for version 1: comment it) and add:

```go
	Grade         string   `json:"grade,omitempty"`
	Contents      []string `json:"contents,omitempty"`
	PriceAmount   int64    `json:"priceAmount,omitempty"`
	PriceCurrency string   `json:"priceCurrency,omitempty"`
```

with the comment on `Condition`: `// Condition is the free-text condition of version-1 documents, converted when read; never written.` In `encodeGame`, set:

```go
			Grade:         string(c.Grade),
			Contents:      contentStrings(c.Contents),
			PriceAmount:   c.Price.Amount,
			PriceCurrency: c.Price.Currency,
```

In `decodeGame`, build the copy, then:

```go
		contents, err := contentsOf(c.Contents)
		if err != nil {
			return nil, err
		}

		cp.Grade, cp.Contents = game.Grade(c.Grade), contents
		cp.Price = game.Money{
			Amount:   c.PriceAmount,
			Currency: c.PriceCurrency,
		}

		if doc.V < 2 && c.Condition != "" {
			if grade, contents, ok := convertCondition(c.Condition); ok {
				cp.Grade, cp.Contents = grade, contents
			} else {
				cp.Notes = strings.TrimSpace(cp.Notes + "\nCondition: " + strings.TrimSpace(c.Condition))
			}
		}
```

(restructure the existing `append(copies, game.Copy{…})` into building `cp` then appending it). Helpers:

```go
func contentStrings(c game.Contents) []string {
	out := make([]string, 0, len(game.AllContents))
	for _, x := range c.List() {
		out = append(out, string(x))
	}

	return out
}

func contentsOf(raw []string) (game.Contents, error) {
	parts := make([]game.Content, 0, len(raw))
	for _, s := range raw {
		parts = append(parts, game.Content(s))
	}

	return game.ContentsOf(parts...)
}

// oldConditions maps the texts the copy form suggested before version 2 (in the UI language of the
// time: English or Spanish) to a grade and contents.
var oldConditions = map[string]struct {
	grade    game.Grade
	contents []game.Content
}{
	"sealed":                   {game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"precintado":               {game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"complete (case + manual)": {"", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"completo (caja + manual)": {"", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"case and disc":            {"", []game.Content{game.ContentBox, game.ContentMedia}},
	"caja y disco":             {"", []game.Content{game.ContentBox, game.ContentMedia}},
	"disc only":                {"", []game.Content{game.ContentMedia}},
	"sólo disco":               {"", []game.Content{game.ContentMedia}},
	"damaged":                  {game.GradeDamaged, nil},
	"dañado":                   {game.GradeDamaged, nil},
}

// convertCondition reads a version-1 condition; ok is false for text that is not one of them.
func convertCondition(text string) (game.Grade, game.Contents, bool) {
	old, ok := oldConditions[strings.ToLower(strings.TrimSpace(text))]
	if !ok {
		return "", 0, false
	}

	contents, _ := game.ContentsOf(old.contents...) // the table only holds known contents

	return old.grade, contents, true
}
```

(`contentStrings` returns `[]string{}` for no contents; `omitempty` drops an empty slice.)

- [ ] **Step 4: Implement the preferences** in `settings_repository.go` (replace the stubs)

```go
const keyPreferences = "preferences"

type preferencesJSON struct {
	Currency string `json:"currency,omitempty"`
}

// Preferences returns the saved preferences, empty when none were saved.
func (r *SettingsRepository) Preferences(ctx context.Context) (settings.Preferences, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyPreferences).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings.Preferences{}, nil
	}

	if err != nil {
		return settings.Preferences{}, err
	}

	var v preferencesJSON
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return settings.Preferences{}, err
	}

	return settings.Preferences(v), nil
}

// SavePreferences stores the preferences.
func (r *SettingsRepository) SavePreferences(ctx context.Context, p settings.Preferences) error {
	b, err := json.Marshal(preferencesJSON(p))
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keyPreferences, string(b), formatTime(time.Now()))

	return err
}
```

- [ ] **Step 5: Run the sqlite tests**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: all PASS (the existing `TestDocuments_game` asserts `doc["v"]` is 1: change that assertion to `2`).

- [ ] **Step 6: Lint and commit**

`task lint:fix && task lint` → `0 issues.`

```bash
git add internal/adapters/outbound/sqlite
git commit -m "Store grade, contents and price in copy documents (v2), converting the old condition"
```

---

### Task 3: API and preferences use case

**Files:**
- Modify: `proto/gamevault/v1/game.proto`, `proto/gamevault/v1/system.proto`, then `task generate`
- Modify: `internal/adapters/inbound/rpc/mapper.go`, `internal/adapters/inbound/rpc/system_handler.go`, `internal/application/system/service.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/application/system/service_test.go`

**Interfaces:**
- Consumes: Task 1 types, Task 2 repository methods.
- Produces: `pb.CopyGrade`, `pb.CopyContent`, `pb.Money`, `CopyDetails.grade/contents/price`; `SystemService.GetPreferences/UpdatePreferences` with `pb.Preferences{currency}`; `system.NewService(games, db, prefs settings.Repository, now, log, status, backupDir, keep)`; `(*system.Service).Preferences(ctx)`, `UpdatePreferences(ctx, settings.Preferences) (settings.Preferences, error)`.

- [ ] **Step 1: Proto**

In `game.proto`, in `CopyDetails` replace the `condition` field (and its comment) with `reserved 9; reserved "condition";` and add after `barcode = 12`:

```proto
  // Physical only: how good it looks. Unspecified: not stated.
  CopyGrade grade = 13;
  // Physical only: what it comes with, without duplicates. Empty: not stated.
  repeated CopyContent contents = 14;
  // What was paid; absent or zero amount: no price.
  Money price = 15;
```

and the three new types (from the spec) next to `CopyStatus`. In `system.proto`:

```proto
// Preferences are the user's choices that shape the UI and the imports.
message Preferences {
  // Default currency of new prices (ISO 4217); empty until chosen.
  string currency = 1;
}
message GetPreferencesRequest {}
message GetPreferencesResponse {
  Preferences preferences = 1;
}
message UpdatePreferencesRequest {
  Preferences preferences = 1;
}
message UpdatePreferencesResponse {
  Preferences preferences = 1;
}
```

and in `SystemService`: `rpc GetPreferences(GetPreferencesRequest) returns (GetPreferencesResponse);` and `rpc UpdatePreferences(UpdatePreferencesRequest) returns (UpdatePreferencesResponse);`.

Run: `task generate` — expected: no errors; `internal/gen` and `web/src/gen` updated.

- [ ] **Step 2: Write the failing end-to-end test** (in `server_test.go`, using the existing `newServer`/clients helpers; add a `system` client the same way the others are built if missing)

```go
func TestPhysicalCopyDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		t.Run("WHEN a game is created with a graded, priced physical copy", func(t *testing.T) {
			res, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
				Title: "Halo 3",
				Copies: []*pb.CopyDetails{{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "Xbox 360",
					Grade:    pb.CopyGrade_COPY_GRADE_VERY_GOOD,
					Contents: []pb.CopyContent{pb.CopyContent_COPY_CONTENT_MEDIA, pb.CopyContent_COPY_CONTENT_BOX},
					Price: &pb.Money{
						AmountMinor: 2995,
						Currency:    "eur",
					},
				}},
			}))
			require.NoError(t, err)

			t.Run("THEN it comes back normalized", func(t *testing.T) {
				d := res.Msg.Game.Copies[0].Details
				assert.Equal(t, pb.CopyGrade_COPY_GRADE_VERY_GOOD, d.Grade)
				assert.Equal(t, []pb.CopyContent{pb.CopyContent_COPY_CONTENT_BOX, pb.CopyContent_COPY_CONTENT_MEDIA}, d.Contents)
				assert.Equal(t, int64(2995), d.Price.GetAmountMinor())
				assert.Equal(t, "EUR", d.Price.GetCurrency())
			})
		})

		t.Run("WHEN the default currency is set to an invalid code", func(t *testing.T) {
			_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{
				Preferences: &pb.Preferences{Currency: "EURO"},
			}))

			t.Run("THEN it is refused as invalid", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN it is set to gbp", func(t *testing.T) {
			_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{
				Preferences: &pb.Preferences{Currency: "gbp"},
			}))
			require.NoError(t, err)

			t.Run("THEN it is read back upper-cased", func(t *testing.T) {
				got, err := c.system.GetPreferences(ctx, connect.NewRequest(&pb.GetPreferencesRequest{}))
				require.NoError(t, err)
				assert.Equal(t, "GBP", got.Msg.Preferences.GetCurrency())
			})
		})
	})
}
```

Run: `task go -- test ./internal/adapters/inbound/rpc/ -run TestPhysicalCopyDetails -v`
Expected: FAIL (grade unspecified, no price; `UpdatePreferences` unimplemented).

- [ ] **Step 3: Mapper** (`mapper.go`)

```go
var gradeToPB = map[game.Grade]pb.CopyGrade{
	game.GradeSealed:     pb.CopyGrade_COPY_GRADE_SEALED,
	game.GradeMint:       pb.CopyGrade_COPY_GRADE_MINT,
	game.GradeVeryGood:   pb.CopyGrade_COPY_GRADE_VERY_GOOD,
	game.GradeGood:       pb.CopyGrade_COPY_GRADE_GOOD,
	game.GradeAcceptable: pb.CopyGrade_COPY_GRADE_ACCEPTABLE,
	game.GradeDamaged:    pb.CopyGrade_COPY_GRADE_DAMAGED,
}

var contentToPB = map[game.Content]pb.CopyContent{
	game.ContentBox:    pb.CopyContent_COPY_CONTENT_BOX,
	game.ContentManual: pb.CopyContent_COPY_CONTENT_MANUAL,
	game.ContentMedia:  pb.CopyContent_COPY_CONTENT_MEDIA,
	game.ContentExtras: pb.CopyContent_COPY_CONTENT_EXTRAS,
}
```

and `gradeFromPB = invert(gradeToPB)`, `contentFromPB = invert(contentToPB)` in the existing `var (…)` block. In `detailsToPB` add:

```go
		Grade:    gradeToPB[d.Grade],
		Contents: contentsToPB(d.Contents),
		Price:    moneyToPB(d.Price),
```

and in `detailsFromPB`:

```go
	contents, err := contentsFromPB(d.Contents)
	if err != nil {
		return game.CopyDetails{}, err
	}
```

then `Grade: gradeFromPB[d.Grade], Contents: contents, Price: game.Money{Amount: d.GetPrice().GetAmountMinor(), Currency: d.GetPrice().GetCurrency()}` — written one field per line. Helpers:

```go
func contentsToPB(c game.Contents) []pb.CopyContent {
	var out []pb.CopyContent
	for _, x := range c.List() {
		out = append(out, contentToPB[x])
	}

	return out
}

func contentsFromPB(in []pb.CopyContent) (game.Contents, error) {
	parts := make([]game.Content, 0, len(in))
	for _, x := range in {
		if c, ok := contentFromPB[x]; ok {
			parts = append(parts, c)
		}
	}

	return game.ContentsOf(parts...)
}

func moneyToPB(m game.Money) *pb.Money {
	if m.IsZero() {
		return nil
	}

	return &pb.Money{
		AmountMinor: m.Amount,
		Currency:    m.Currency,
	}
}
```

`toConnectError` already maps `*settings.ValidationError` and `*game.ValidationError` to InvalidArgument.

- [ ] **Step 4: Preferences use case** (`internal/application/system/service.go`)

Add a `prefs settings.Repository` field and parameter: `func NewService(games game.Repository, db DatabaseBackup, prefs settings.Repository, now port.Clock, log *slog.Logger, status Status, backupDir string, keep int) *Service`, and:

```go
// Preferences returns the user's preferences.
func (s *Service) Preferences(ctx context.Context) (settings.Preferences, error) {
	return s.prefs.Preferences(ctx)
}

// UpdatePreferences validates and stores the user's preferences.
func (s *Service) UpdatePreferences(ctx context.Context, p settings.Preferences) (settings.Preferences, error) {
	p, err := p.Validate()
	if err != nil {
		return p, err
	}

	return p, s.prefs.SavePreferences(ctx, p)
}
```

Update callers: `cmd/gamevault/main.go` (`system.NewService(games, db, settingsRepo, now, log, …)`), `server_test.go` (`sqlite.NewSettingsRepository(db)`), `service_test.go` (`nil` for prefs).

- [ ] **Step 5: Handler** (`system_handler.go`)

```go
// GetPreferences returns the user's preferences.
func (h *SystemHandler) GetPreferences(ctx context.Context, _ *connect.Request[pb.GetPreferencesRequest]) (*connect.Response[pb.GetPreferencesResponse], error) {
	p, err := h.system.Preferences(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.GetPreferencesResponse{Preferences: &pb.Preferences{Currency: p.Currency}}), nil
}

// UpdatePreferences stores the user's preferences.
func (h *SystemHandler) UpdatePreferences(ctx context.Context, req *connect.Request[pb.UpdatePreferencesRequest]) (*connect.Response[pb.UpdatePreferencesResponse], error) {
	p, err := h.system.UpdatePreferences(ctx, settings.Preferences{Currency: req.Msg.GetPreferences().GetCurrency()})
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdatePreferencesResponse{Preferences: &pb.Preferences{Currency: p.Currency}}), nil
}
```

- [ ] **Step 6: Run the tests**

Run: `task go -- test ./internal/adapters/inbound/rpc/ ./internal/application/... -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `TestPhysicalCopyDetails` PASS, everything else still `ok`.

- [ ] **Step 7: Lint and commit**

`task lint:fix && task lint` → `0 issues.` (CI also checks `task generate` changes nothing: the generated code goes in this commit.)

```bash
git add proto internal/gen web/src/gen internal/adapters/inbound/rpc internal/application/system cmd/gamevault
git commit -m "Expose grade, contents, price and the default currency in the API"
```

---

### Task 4: CSV, English only, with the new columns

**Files:**
- Modify: `internal/adapters/outbound/csvfile/codec.go`, `codec_test.go`, `internal/application/transfer/service.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`

**Interfaces:**
- Consumes: Task 1 types and `game.CurrencyDigits`; `settings.Repository.Preferences`.
- Produces: columns `grade`, `contents`, `price`, `currency`; `parsePrice(text, currency string) (amount int64, ok bool)`; `formatPrice(m game.Money) string`; `transfer.NewService(games, tx, prefs settings.Repository, now, codec)`.

- [ ] **Step 1: Write the failing tests** (`codec_test.go`)

Replace `TestDecodeSpanishSemicolon` (its headers are Spanish, which are now unknown columns) with an English semicolon test, and add:

```go
func TestDecode_semicolonEnglish(t *testing.T) {
	in := "title;platform;kind;acquiredOn\nHalo 3;Xbox 360;physical;12/05/2008\n"

	copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, copies, 1)
	assert.Equal(t, game.KindPhysical, copies[0].Details.Kind)
	assert.Equal(t, game.Date("2008-05-12"), copies[0].Details.AcquiredOn)
}

func TestDecode_physicalColumns(t *testing.T) {
	t.Run("GIVEN rows with grade, contents and prices", func(t *testing.T) {
		in := "title,kind,grade,contents,price,currency\n" +
			"Halo 3,physical,very_good,media box,\"29,95\",eur\n" +
			"Zelda,physical,,,1500,JPY\n" +
			"Okami,physical,,,12.5,\n" +
			"Bad,physical,excellent,poster,abc,EURO\n"

		t.Run("WHEN they are imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)
			require.Len(t, copies, 4)

			t.Run("THEN valid values are read, in the currency's own decimals", func(t *testing.T) {
				d := copies[0].Details
				assert.Equal(t, game.GradeVeryGood, d.Grade)
				assert.Equal(t, []game.Content{game.ContentBox, game.ContentMedia}, d.Contents.List())
				assert.Equal(t, game.Money{Amount: 2995, Currency: "EUR"}, d.Price)
				assert.Equal(t, game.Money{Amount: 1500, Currency: "JPY"}, copies[1].Details.Price)
			})

			t.Run("AND a price without currency waits for the default one", func(t *testing.T) {
				assert.Equal(t, game.Money{Amount: 1250}, copies[2].Details.Price)
			})

			t.Run("AND invalid values are reported and left out, keeping the row", func(t *testing.T) {
				assert.Empty(t, copies[3].Details.Grade)
				assert.Zero(t, copies[3].Details.Contents)
				assert.True(t, copies[3].Details.Price.IsZero())
				assert.Len(t, warnings, 4) // grade, content, price, currency
			})
		})
	})

	t.Run("GIVEN the Spanish headers older files used", func(t *testing.T) {
		_, warnings, err := Codec{}.Decode(strings.NewReader("title,plataforma,notas\nHalo 3,Xbox 360,x\n"))

		t.Run("THEN they are unknown columns", func(t *testing.T) {
			require.NoError(t, err)
			assert.Len(t, warnings, 2)
		})
	})
}

func TestEncode_physicalColumns(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g, _ := game.New("Halo 3", now)
	contents, _ := game.ContentsOf(game.ContentBox, game.ContentMedia)
	_, err := g.AddCopy(game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "Xbox 360",
		Grade:    game.GradeGood,
		Contents: contents,
		Price: game.Money{
			Amount:   1500,
			Currency: "JPY",
		},
	}, now)
	require.NoError(t, err)

	var b strings.Builder
	require.NoError(t, Codec{}.Encode(&b, []*game.Game{g}))

	copies, warnings, err := Codec{}.Decode(strings.NewReader(b.String()))
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Contains(t, b.String(), "good,box media,1500,JPY")
	assert.Equal(t, game.Money{Amount: 1500, Currency: "JPY"}, copies[0].Details.Price)
}
```

And in `server_test.go` (or a transfer-level test) the default currency fill:

```go
func TestImportCsv_defaultCurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{Preferences: &pb.Preferences{Currency: "GBP"}}))
	require.NoError(t, err)

	t.Run("WHEN a CSV row has a price without currency", func(t *testing.T) {
		_, err := c.system.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("title,kind,price\nOkami,physical,12.50\n")}))
		require.NoError(t, err)

		t.Run("THEN it gets the default currency", func(t *testing.T) {
			list, err := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Games, 1)
			assert.Equal(t, "GBP", list.Msg.Games[0].Copies[0].Details.Price.GetCurrency())
		})
	})
}
```

Run: `task go -- test ./internal/adapters/outbound/csvfile/ ./internal/adapters/inbound/rpc/ -run 'TestDecode|TestEncode|TestImportCsv_defaultCurrency' -v`
Expected: FAIL (columns unknown, Spanish headers still read).

- [ ] **Step 2: Codec** (`codec.go`)

- `columns`: replace `"condition"` with `"grade", "contents", "price", "currency"` (keep the rest in order: `…, "edition", "grade", "contents", "location", "price", "currency", "notes", "links", "externalId", "barcode"`).
- `aliases`: keep exactly the English ones — `"game": "title"`, `"store": "platform"`, `"type": "kind"`, `"bundle": "origin"`, `"ean": "barcode"`, `"upc": "barcode"` — and delete every Spanish key.
- `kindAliases`: keep exactly `key`, `cdkey`, `library`, `physical`; delete `clave`, `biblioteca`, `fisico`, `físico`, `disco`.
- Package doc: drop "Spanish aliases are accepted".
- Decode, after the barcode block:

```go
		if v := get("grade"); v != "" {
			if g := game.Grade(strings.ToLower(v)); g.Valid() {
				d.Grade = g
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: grade %q is not one of sealed, mint, very_good, good, acceptable, damaged", row, v))
			}
		}

		if v := get("contents"); v != "" {
			var parts []game.Content
			for _, f := range strings.Fields(strings.ToLower(v)) {
				parts = append(parts, game.Content(f))
			}

			if c, err := game.ContentsOf(parts...); err == nil {
				d.Contents = c
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: %v", row, err))
			}
		}

		currency := strings.ToUpper(get("currency"))
		if currency != "" && !game.IsCurrencyCode(currency) {
			warnings = append(warnings, fmt.Sprintf("row %d: currency %q is not a three-letter code", row, currency))
			currency = ""
		}

		if v := get("price"); v != "" {
			if amount, ok := parsePrice(v, currency); ok {
				d.Price = game.Money{
					Amount:   amount,
					Currency: currency,
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: price %q is not an amount like 29.95", row, v))
			}
		}
```

Helpers:

```go
// parsePrice reads "29.95" or "29,95" (at most the currency's decimals) into minor units. An
// unknown currency (empty) uses two decimals until the default one is known.
func parsePrice(text, currency string) (int64, bool) {
	digits := 2
	if currency != "" {
		digits = game.CurrencyDigits(currency)
	}

	whole, frac, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), ".")
	if whole == "" || len(frac) > digits || strings.Contains(frac, ".") {
		return 0, false
	}

	n, err := strconv.ParseInt(whole+frac+strings.Repeat("0", digits-len(frac)), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}

	return n, true
}

// formatPrice writes an amount with its currency's decimals and a dot: 2995 EUR is "29.95".
func formatPrice(m game.Money) string {
	if m.IsZero() {
		return ""
	}

	digits := game.CurrencyDigits(m.Currency)
	s := strconv.FormatInt(m.Amount, 10)

	if digits == 0 {
		return s
	}

	s = strings.Repeat("0", max(0, digits+1-len(s))) + s

	return s[:len(s)-digits] + "." + s[len(s)-digits:]
}
```

Note `parsePrice("1500", "JPY")` → 1500 and `parsePrice("12.5", "")` → 1250 (two decimals assumed); `transfer.Service` rescales nothing: a price parsed without currency is re-read with the default currency's digits (Step 3).

- Encode: write `string(c.Grade)`, `strings.Join(contentStrings, " ")` (build from `c.Contents.List()`), `formatPrice(c.Price)`, `c.Price.Currency` in the column positions above.

- [ ] **Step 3: Default currency on import** (`transfer/service.go`)

Add `prefs settings.Repository` to `Service` and `NewService(games game.Repository, tx port.TxManager, prefs settings.Repository, now port.Clock, codec Codec)`. In `Import`, after decoding:

```go
	prefs, err := s.prefs.Preferences(ctx)
	if err != nil {
		return report, err
	}

	missing := 0

	for i := range copies {
		p := &copies[i].Details.Price
		if p.IsZero() || p.Currency != "" {
			continue
		}

		if prefs.Currency == "" {
			*p = game.Money{}
			missing++

			continue
		}

		// The codec assumed two decimals; a default currency with other decimals rescales.
		p.Amount = rescale(p.Amount, 2, game.CurrencyDigits(prefs.Currency))
		p.Currency = prefs.Currency
	}

	if missing > 0 {
		warnings = append(warnings, fmt.Sprintf("%d prices have no currency and no default currency is set (System → Preferences): they were not imported", missing))
	}
```

with

```go
// rescale moves an amount from one number of decimals to another (12.50 with 2 → 13 with 0 rounds
// half up).
func rescale(amount int64, from, to int) int64 {
	for ; from < to; from++ {
		amount *= 10
	}

	for ; from > to; from-- {
		amount = (amount + 5) / 10
	}

	return amount
}
```

Update callers: `cmd/gamevault/main.go` (`transfer.NewService(games, db, settingsRepo, now, csvfile.Codec{})`), `server_test.go` (`sqlite.NewSettingsRepository(db)`).

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/csvfile/ ./internal/application/transfer/ ./internal/adapters/inbound/rpc/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: all PASS.

- [ ] **Step 5: Lint and commit**

`task lint:fix && task lint` → `0 issues.`

```bash
git add internal/adapters/outbound/csvfile internal/application/transfer internal/adapters/inbound/rpc cmd/gamevault
git commit -m "CSV: grade, contents, price and currency columns, English only"
```

---

### Task 5: Web UI

**Files:**
- Create: `web/src/lib/money.ts`
- Modify: `web/src/lib/model.ts`, `web/src/features/library/CopyForm.tsx`, `web/src/features/library/GameDetail.tsx`, `web/src/features/scan/ScanPage.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/styles.css`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Consumes: generated `CopyGrade`, `CopyContent`, `Money`, `systemClient.getPreferences/updatePreferences`.
- Produces: `parseAmount(text, digits): bigint | null`, `formatAmount(minor: bigint, currency, locale): string`, `currencyDigits(currency): number`, `regionCurrency(locale): string`, `currencyList(): string[]`, `GRADES`, `CONTENTS`, `gradeKey`, `contentKey`.

The UI has no unit-test runner; `task test` type-checks it. `parseAmount` is the one piece with real logic: Step 1 checks it with Node in the toolchain before it is used.

- [ ] **Step 1: `money.ts` and its check**

```ts
/** Number of decimals of a currency, as the browser knows it (EUR 2, JPY 0, BHD 3). */
export function currencyDigits(currency: string): number {
  try {
    return new Intl.NumberFormat('en', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2;
  } catch {
    return 2;
  }
}

/**
 * Reads an amount typed by a person into minor units, or null when it is not one. The last "." or
 * "," followed by at most `digits` digits is the decimal separator; any other "." "," or space is a
 * thousands separator: "1.234,50", "1,234.50" and "1234,5" all mean 1234.50.
 */
export function parseAmount(text: string, digits: number): bigint | null {
  const s = text.trim().replace(/\s/g, '');
  if (!/^\d[\d.,]*$/.test(s)) return null;
  const last = Math.max(s.lastIndexOf('.'), s.lastIndexOf(','));
  let whole = s;
  let frac = '';
  if (last >= 0 && s.length - last - 1 <= digits && s.length - last - 1 > 0) {
    whole = s.slice(0, last);
    frac = s.slice(last + 1);
  }
  whole = whole.replace(/[.,]/g, '');
  if (!/^\d+$/.test(whole) || !/^\d*$/.test(frac)) return null;
  return BigInt(whole + frac.padEnd(digits, '0'));
}

/** Formats minor units as money in the UI language: 2995n EUR in Spanish is "29,95 €". */
export function formatAmount(minor: bigint, currency: string, locale: string): string {
  const digits = currencyDigits(currency);
  const value = Number(minor) / 10 ** digits;
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(value);
  } catch {
    return `${value.toFixed(digits)} ${currency}`;
  }
}

/** The amount as the form shows it for editing, in the UI language: 2995n EUR in Spanish is "29,95". */
export function amountInput(minor: bigint, currency: string, locale: string): string {
  if (minor === 0n) return '';
  const digits = currencyDigits(currency);
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false })
    .format(Number(minor) / 10 ** digits);
}

const REGION_CURRENCY: Record<string, string> = {
  US: 'USD', GB: 'GBP', JP: 'JPY', CA: 'CAD', AU: 'AUD', NZ: 'NZD', CH: 'CHF', MX: 'MXN', BR: 'BRL',
  AR: 'ARS', CL: 'CLP', CO: 'COP', SE: 'SEK', NO: 'NOK', DK: 'DKK', PL: 'PLN', CZ: 'CZK', HU: 'HUF',
  KR: 'KRW', CN: 'CNY', IN: 'INR', ZA: 'ZAR', TR: 'TRY',
};
const EURO = ['AT', 'BE', 'CY', 'DE', 'EE', 'ES', 'FI', 'FR', 'GR', 'HR', 'IE', 'IT', 'LT', 'LU', 'LV', 'MT', 'NL', 'PT', 'SI', 'SK'];

/** The currency of a locale's region ("es-ES" → EUR, "en-US" → USD); EUR when unknown. */
export function regionCurrency(locale: string): string {
  const region = (() => {
    try {
      return new Intl.Locale(locale).maximize().region ?? '';
    } catch {
      return '';
    }
  })();
  if (EURO.includes(region)) return 'EUR';
  return REGION_CURRENCY[region] ?? 'EUR';
}

/** Every currency the browser knows, sorted; a short list on browsers without Intl.supportedValuesOf. */
export function currencyList(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
  return intl.supportedValuesOf?.('currency') ?? ['AUD', 'BRL', 'CAD', 'CHF', 'EUR', 'GBP', 'JPY', 'MXN', 'USD'];
}
```

Check it (scratch file in the session scratchpad, not the repo) — run inside the toolchain with Node's TypeScript stripping:

```bash
cp web/src/lib/money.ts "$S/money.mts" && cat >> "$S/money.mts" <<'EOF'
const cases: [string, number, bigint | null][] = [
  ['29,95', 2, 2995n], ['29.95', 2, 2995n], ['1.234,50', 2, 123450n], ['1,234.50', 2, 123450n],
  ['1234,5', 2, 123450n], ['1.234', 2, 123400n], ['1500', 0, 1500n], ['12.345', 3, 12345n],
  ['', 2, null], ['abc', 2, null], ['-5', 2, null], ['12,3,4', 2, 12340n],
];
for (const [text, d, want] of cases) {
  const got = parseAmount(text, d);
  console.log(got === want ? 'ok  ' : 'FAIL', JSON.stringify(text), d, String(got));
}
EOF
docker run --rm -v "$S:/s" -w /s node:24-bookworm-slim node --experimental-strip-types money.mts
```

Expected: every line starts with `ok`. (`1.234` with two decimals is 1234.00: three digits after the separator make it a thousands separator.) Any FAIL: fix `parseAmount` and re-run.

- [ ] **Step 2: Model helpers** (`model.ts`)

```ts
export const GRADES = [CopyGrade.SEALED, CopyGrade.MINT, CopyGrade.VERY_GOOD, CopyGrade.GOOD, CopyGrade.ACCEPTABLE, CopyGrade.DAMAGED] as const;
export const CONTENTS = [CopyContent.BOX, CopyContent.MANUAL, CopyContent.MEDIA, CopyContent.EXTRAS] as const;

/** Translation key suffix: t(`grade.${gradeKey(g)}`), t(`content.${contentKey(c)}`). */
export const gradeKey = (g: CopyGrade) => CopyGrade[g].toLowerCase();
export const contentKey = (c: CopyContent) => CopyContent[c].toLowerCase();
```

(import `CopyGrade`, `CopyContent` from the generated `game_pb`). In `emptyDetails`, remove `condition: ''` and add `grade: CopyGrade.UNSPECIFIED, contents: []` (`price` stays undefined).

- [ ] **Step 3: Copy form** (`CopyForm.tsx`)

- Props gain `locations: string[]` and `defaultCurrency: string`.
- State: `const [amount, setAmount] = useState(() => d.price ? amountInput(d.price.amountMinor, d.price.currency, i18n.language) : '')` and `const [currency, setCurrency] = useState(d.price?.currency || defaultCurrency)`; `const minor = amount.trim() === '' ? 0n : parseAmount(amount, currencyDigits(currency))`; `const amountInvalid = minor === null`.
- Physical block: replace the condition `<label>` with:

```tsx
              <label>
                {t('copy.grade')}
                <select value={d.grade} onChange={(e) => set('grade', Number(e.target.value))}>
                  <option value={CopyGrade.UNSPECIFIED}>{t('grade.unspecified')}</option>
                  {GRADES.map((g) => <option key={g} value={g}>{t(`grade.${gradeKey(g)}`)}</option>)}
                </select>
              </label>
              <div className="span2 field">
                <span className="field-label">{t('copy.contents')}</span>
                <div className="toggles" role="group" aria-label={t('copy.contents')}>
                  {CONTENTS.map((c) => {
                    const on = d.contents.includes(c);
                    return (
                      <button type="button" key={c} className={on ? 'toggle on' : 'toggle'} aria-pressed={on}
                        onClick={() => set('contents', on ? d.contents.filter((x) => x !== c) : [...d.contents, c])}>
                        {t(`content.${contentKey(c)}`)}
                      </button>
                    );
                  })}
                </div>
              </div>
```

  and give the location input `list="copy-locations"` with `<datalist id="copy-locations">{locations.map((l) => <option key={l} value={l} />)}</datalist>`.
- For every kind, after the acquired date:

```tsx
          <label>
            {t('copy.price')}
            <span className="row tight">
              <input inputMode="decimal" value={amount} aria-invalid={amountInvalid}
                onChange={(e) => setAmount(e.target.value)} placeholder={amountInput(2995n, currency, i18n.language)} />
              <select className="currency" value={currency} onChange={(e) => setCurrency(e.target.value)} aria-label={t('copy.currency')}>
                {currencyList().map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </span>
            {amountInvalid && <span className="help error">{t('copy.priceInvalid')}</span>}
          </label>
```

- `submit`: return early when `amountInvalid`; call `onSubmit({ ...d, price: minor ? { amountMinor: minor, currency } : undefined })` (create the `Money` message with the generated `create(MoneySchema, …)` if the plain object does not type-check).
- Disable the save button when `amountInvalid`.

Callers of `CopyForm` (in `GameDetail.tsx`): pass `locations` = the sorted, distinct, non-empty `location` of every copy of every game in `useAppData().games`, and `defaultCurrency` from a `usePreferredCurrency()` hook in `money.ts`'s companion file `web/src/lib/usePreferences.ts`:

```ts
import { useEffect, useState } from 'react';
import { systemClient } from '../api/client';
import { regionCurrency } from './money';

/** The default currency: the saved one, else the browser region's. */
export function usePreferredCurrency(locale: string): string {
  const [currency, setCurrency] = useState(() => regionCurrency(locale));
  useEffect(() => {
    systemClient.getPreferences({}).then((r) => { if (r.preferences?.currency) setCurrency(r.preferences.currency); }, () => {});
  }, []);
  return currency;
}
```

- [ ] **Step 4: Copy line in the sheet** (`GameDetail.tsx`)

Replace `d.condition` in `extra` with the new parts, keeping the order grade · contents · price · location:

```tsx
          const physical = [
            d.grade ? t(`grade.${gradeKey(d.grade)}`) : '',
            d.contents.length ? d.contents.map((c) => t(`content.${contentKey(c)}`)).join(', ') : '',
            d.price && d.price.amountMinor ? formatAmount(d.price.amountMinor, d.price.currency, i18n.language) : '',
            d.location,
          ];
          const extra = [d.origin, d.edition, ...physical, fmt.date(d.acquiredOn), d.barcode && `EAN ${d.barcode}`].filter(Boolean);
```

- [ ] **Step 5: Scan batch defaults** (`ScanPage.tsx`)

`Defaults` becomes `{ platform: string; grade: CopyGrade; contents: CopyContent[]; location: string }`; `loadDefaults` spreads the stored JSON over `{ platform: '', grade: CopyGrade.UNSPECIFIED, contents: [], location: '' }` and drops any stored `condition` key. The batch chips show the grade (or `—`) and the contents (or `—`); the batch fields use the same grade select and content toggles as the copy form; `add` sends `grade: defaults.grade, contents: defaults.contents` instead of `condition`.

- [ ] **Step 6: System → Preferences and the CSV help** (`SystemPage.tsx`)

A new `<section className="card">` before Backups:

```tsx
      <section className="card">
        <h2>{t('system.preferences')}</h2>
        <label className="field">
          {t('system.currency')}
          <select value={currency} onChange={(e) => saveCurrency(e.target.value)}>
            {[regionCurrency(i18n.language), ...currencyList().filter((c) => c !== regionCurrency(i18n.language))]
              .map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <span className="help">{t('system.currencyHelp')}</span>
        </label>
      </section>
```

with `currency` loaded from `systemClient.getPreferences({})` (falling back to `regionCurrency(i18n.language)`) and `saveCurrency` calling `systemClient.updatePreferences({ preferences: { currency: c } })` then updating the state (errors shown with the page's existing error alert). Replace `condition` in the CSV template string with `grade;contents` and add `price;currency` after `acquiredOn`.

- [ ] **Step 7: Texts and styles**

`en.json` / `es.json`: remove `copy.condition` and `copy.conditions.*`; add `copy.grade` (Grade / Estado), `copy.contents` (Contents / Contenido), `copy.price` (Purchase price / Precio de compra), `copy.currency` (Currency / Moneda), `copy.priceInvalid` (Not an amount, e.g. 29.95 / No es un importe, p. ej. 29,95), `grade.unspecified/sealed/mint/very_good/good/acceptable/damaged` (Not stated / Sealed / Mint / Very good / Good / Acceptable / Damaged — Sin indicar / Precintado / Como nuevo / Muy bueno / Bueno / Aceptable / Dañado), `content.box/manual/media/extras` (Box / Manual / Disc or cartridge / Extras — Caja / Manual / Disco o cartucho / Extras), `system.preferences` (Preferences / Preferencias), `system.currency` (Default currency / Moneda por defecto), `system.currencyHelp` (Used for new prices and for CSV rows without a currency. / Se usa en los precios nuevos y en las filas de CSV sin moneda.), `scan.batch` chips reuse `copy.grade` and `copy.contents`. Update `system.csvColumns` in both: replace `condition` with `grade (sealed, mint, very_good, good, acceptable, damaged), contents (box manual media extras)`, add `price, currency` after `acquiredOn`, and in `es.json` remove the sentence about Spanish names.

`styles.css`:

```css
.toggles { display: flex; flex-wrap: wrap; gap: 6px; }
.toggle { border-radius: 999px; padding: 6px 12px; }
.toggle.on { background: var(--accent); border-color: var(--accent); color: var(--accent-fg, #111); }
.field-label { font-size: 13px; color: var(--muted); font-weight: 550; }
select.currency { flex: 0 0 auto; width: auto; }
.help.error { color: var(--danger); }
```

(use the variable names `styles.css` already defines for the accent and danger colors; check with `grep -n -- '--accent\|--danger' web/src/styles.css`.)

- [ ] **Step 8: Check and commit**

Run: `task test` — expected: `tsc --noEmit` clean and `translations OK`. Then `task lint` (`0 issues.`).

```bash
git add web/src
git commit -m "Edit and show grade, contents and price, and choose the default currency"
```

---

### Task 6: Real data, docs, PR

**Files:**
- Modify: `docs/technical.md`, `.claude/memory/data-model.md`

- [ ] **Step 1: Look at it with the user's data**

`task test-server` (a copy of the config). In the browser pane at http://127.0.0.1:8093/?v=<new number>:
- A physical game whose copy had an old condition: the copy line shows the converted grade and contents (or the condition text in the notes).
- Edit that copy: set grade, toggle contents, pick a location suggestion, type `29,95` in Spanish (switch the UI language) and `29.95` in English; save; the line shows the price formatted. Type `abc`: the field is marked and Save is disabled.
- System → Preferences: the region's currency is preselected; change it, reload, it stays.
- Scan → batch defaults: grade and contents controls.
- Export the CSV: the new columns are there.
- Mobile preset, then reset the viewport. `task test-server:stop`.

- [ ] **Step 2: Docs and memory**

`docs/technical.md`: in the CSV section, the column list (grade, contents, price, currency; English only); in the data/storage part, that game documents are version 2 and how the old condition converts; a line on the default currency (System → Preferences, used for CSV rows without currency). `.claude/memory/data-model.md`: copy grade/contents/price, `Contents` is a bit set, `Money` in the currency's minor unit, game docs v2.

- [ ] **Step 3: Verify, push, PR**

`/verify`, then:

```bash
git add docs/technical.md .claude/memory/data-model.md docs/superpowers
git commit -m "Document grade, contents, price and the default currency"
git push -u origin feature/physical-copy-details
gh pr create --base main --title "Richer physical copies: grade, contents and purchase price" --body-file "$S/pr-body.md"
```

The PR body says what changes, links spec and plan, lists what was verified with real data and what only in tests, and any rulings.
