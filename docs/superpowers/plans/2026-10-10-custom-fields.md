# Custom Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users define their own fields (nine types, per game or per copy), fill them in on the game sheet and the copy form, and filter and search the library by them.

**Architecture:** The values live in the domain as `game.FieldValues`, a map from field id to value, on the game (`Info.Fields`) and on each copy (`Copy.Fields`), stored inside the game document. A new `internal/domain/field` package holds the definitions (`Definition`, `Set`) and validates values against them; it imports `game`, never the reverse. Definitions are one JSON document in the `settings` table. The catalog validates values on every write. A new `fields` application service manages definitions and the bulk value changes (delete a field, remove or merge a list value) in one transaction. The new RPC service `FieldService` sits next to field maps on `Game` and `CopyDetails`. The web loads the definitions with the app data and renders a control per type; filtering and search are pure functions.

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite (JSON documents); React 19 + TypeScript, i18next; Node test runner.

**Spec:** `docs/superpowers/specs/2026-10-10-custom-fields-design.md`

## Global Constraints

- Everything builds and tests in the toolchain container through Task (`task lint`, `task test`, `task generate`).
- Load the `write-go` skill before writing Go: a doc comment on every exported identifier, one struct field per line, a blank line between documented members, GIVEN/WHEN/THEN tests with time-bounded contexts.
- The repository is public: no personal data in code, comments, docs, test data, commits or the PR (see `.claude/memory/public-project.md`).
- English in the repository; every UI string through `t()` in `en.json` and `es.json`; `task i18n` passes.
- No schema migration: definitions go in the `settings` table (key `fields`), values in the game document.
- Limits: 50 fields; 200 values per list; field and value names 1-60 characters; text 250 characters; long text 10,000; numbers within ±1e12 (in hundredths when 2 decimals); durations up to 100,000 hours (6,000,000 minutes).
- Type and scope are fixed once a field exists.
- Scans never touch field values.
- Web files the Node tests import must not import generated protobuf code or `lib/model.ts` (TypeScript enums): `lib/fields.ts` uses structural types only.

## Review Focus

- A value for a field that was deleted meanwhile (stale page) must be refused with a clear message, never stored.
- A copy field limited to physical copies must not be accepted on a key, and it must not be offered in a key's form.
- Removing a list value used by a multilist that already holds the merge target must leave one copy of it, not two.
- Merging two games where both have a value for the same game field keeps the kept game's value.
- A number field with 2 decimals shown and edited as "12.50" must round-trip exactly (hundredths), with no floating-point drift.

## Rulings made while planning (deviations from the spec's wording)

- **Copy values live on `Copy.Fields`, not inside `CopyDetails`.** A map would make `CopyDetails` incomparable: `applyImport` compares it. It also keeps scans away from the values by construction. The API still carries them in `CopyDetails.fields`, and the handlers pass them separately to the catalog.
- **The value type is `game.FieldValue`** (in the game package) and the definitions are in `field`, which imports `game`. A `field.Value` held by `game` would be an import cycle.

---

### Task 1: Field values on games and copies (domain)

**Files:**
- Create: `internal/domain/game/fields.go`, `internal/domain/game/fields_test.go`
- Modify: `internal/domain/game/game.go`, `internal/domain/game/copy.go`

**Interfaces:**
- Produces:
  - `game.FieldValue{Text string; Bool *bool; Number *int64; Money *Money; Date string; Minutes *int64; Choice string; Choices []string}` with `IsZero() bool`
  - `game.FieldValues map[string]FieldValue`
  - `Info.Fields FieldValues` and `Copy.Fields FieldValues`
  - `(*Game).Fields() FieldValues`
  - `(*Game).SetCopyFields(id ID, values FieldValues, now time.Time) error`
  - `(*Game).RemoveFieldValues(fieldID string) (gameHad bool, copies int)`
  - `(*Game).ReplaceChoice(fieldID, from, to string) bool`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing test** (`internal/domain/game/fields_test.go`)

```go
package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func yes() *bool { b := true; return &b }

func TestFieldValues(t *testing.T) {
	now := time.Now()

	t.Run("GIVEN a game with values on the game and on a copy", func(t *testing.T) {
		g, err := New("Halo 3", now)
		require.NoError(t, err)
		info := g.Info()
		info.Fields = FieldValues{"review": {Text: "Great"}, "empty": {Text: ""}}
		_, err = g.UpdateInfo(info, now)
		require.NoError(t, err)
		c, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, now)
		require.NoError(t, err)
		require.NoError(t, g.SetCopyFields(c.ID, FieldValues{"sealed": {Bool: yes()}, "tags": {Choices: []string{"a", "b"}}}, now))

		t.Run("THEN empty values are not stored and the rest reads back", func(t *testing.T) {
			assert.Equal(t, FieldValues{"review": {Text: "Great"}}, g.Fields())
			assert.True(t, *g.Copies()[0].Fields["sealed"].Bool)
		})

		t.Run("AND reading never exposes the game's own maps", func(t *testing.T) {
			g.Fields()["review"] = FieldValue{Text: "changed"}
			g.Copies()[0].Fields["tags"].Choices[0] = "x"
			assert.Equal(t, "Great", g.Fields()["review"].Text)
			assert.Equal(t, []string{"a", "b"}, g.Copies()[0].Fields["tags"].Choices)
		})

		t.Run("WHEN a scan updates the copy THEN its values stay", func(t *testing.T) {
			changed := g.copies[0].applyImport(CopyDetails{Kind: KindPhysical, Platform: "Xbox 360"})
			assert.True(t, changed)
			assert.True(t, *g.Copies()[0].Fields["sealed"].Bool)
		})

		t.Run("WHEN a list value is replaced THEN game and copies change, without duplicates", func(t *testing.T) {
			assert.True(t, g.ReplaceChoice("tags", "a", "b"))
			assert.Equal(t, []string{"b"}, g.Copies()[0].Fields["tags"].Choices)
			assert.True(t, g.ReplaceChoice("tags", "b", ""))
			_, ok := g.Copies()[0].Fields["tags"]
			assert.False(t, ok, "a multilist left empty is removed")
		})

		t.Run("WHEN a field is removed THEN its values go and the counts say where", func(t *testing.T) {
			gameHad, copies := g.RemoveFieldValues("sealed")
			assert.False(t, gameHad)
			assert.Equal(t, 1, copies)
			gameHad, _ = g.RemoveFieldValues("review")
			assert.True(t, gameHad)
			assert.Empty(t, g.Fields())
		})
	})

	t.Run("GIVEN two games with values WHEN one absorbs the other", func(t *testing.T) {
		a, _ := New("Halo 3", now)
		b, _ := New("Halo 3", now)
		ia, ib := a.Info(), b.Info()
		ia.Fields = FieldValues{"review": {Text: "Kept"}}
		ib.Fields = FieldValues{"review": {Text: "Lost"}, "by": {Text: "A friend"}}
		_, _ = a.UpdateInfo(ia, now)
		_, _ = b.UpdateInfo(ib, now)
		a.Absorb(b, now)

		t.Run("THEN the kept game's values win and gaps are filled", func(t *testing.T) {
			assert.Equal(t, "Kept", a.Fields()["review"].Text)
			assert.Equal(t, "A friend", a.Fields()["by"].Text)
		})
	})
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-gomod:/go/pkg/mod -v gamevault-gobuild:/root/.cache/go-build gamevault-toolchain:446777a3814f go test ./internal/domain/game/ -run TestFieldValues`
Expected: FAIL to compile (`undefined: FieldValues`).

- [ ] **Step 4: Write `internal/domain/game/fields.go`**

```go
package game

import (
	"slices"
	"time"
)

// FieldValue is the value of one custom field (see the field package). Exactly the member that
// matches the field's type is set; the field package validates it.
type FieldValue struct {
	// Text is the value of text and long-text fields.
	Text string

	// Bool is the value of yes/no fields.
	Bool *bool

	// Number is the value of number fields, in hundredths when the field has 2 decimals.
	Number *int64

	// Money is the value of money fields.
	Money *Money

	// Date is the value of date fields: "YYYY", "YYYY-MM" or "YYYY-MM-DD".
	Date string

	// Minutes is the value of duration fields.
	Minutes *int64

	// Choice is the value of list fields: the id of one of the field's choices.
	Choice string

	// Choices is the value of multilist fields: ids of the field's choices.
	Choices []string
}

// IsZero reports whether the value is empty. Empty values are never stored.
func (v FieldValue) IsZero() bool {
	return v.Text == "" && v.Bool == nil && v.Number == nil && v.Money == nil && v.Date == "" &&
		v.Minutes == nil && v.Choice == "" && len(v.Choices) == 0
}

func (v FieldValue) clone() FieldValue {
	out := v
	out.Choices = slices.Clone(v.Choices)

	if v.Bool != nil {
		b := *v.Bool
		out.Bool = &b
	}

	if v.Number != nil {
		n := *v.Number
		out.Number = &n
	}

	if v.Money != nil {
		m := *v.Money
		out.Money = &m
	}

	if v.Minutes != nil {
		m := *v.Minutes
		out.Minutes = &m
	}

	return out
}

// FieldValues maps a custom field's id to its value.
type FieldValues map[string]FieldValue

// compact returns a deep copy without empty values, nil when nothing is left.
func (f FieldValues) compact() FieldValues {
	var out FieldValues

	for id, v := range f {
		if v.IsZero() {
			continue
		}

		if out == nil {
			out = FieldValues{}
		}

		out[id] = v.clone()
	}

	return out
}

// Fields returns the game's custom field values (a copy).
func (g *Game) Fields() FieldValues { return g.fields.compact() }

// SetCopyFields replaces a copy's custom field values. The caller validates them against the
// field definitions first.
func (g *Game) SetCopyFields(id ID, values FieldValues, now time.Time) error {
	i := g.indexOf(id)
	if i < 0 {
		return ErrCopyNotFound
	}

	g.copies[i].Fields = values.compact()
	g.copies[i].UpdatedAt = now
	g.updatedAt = now

	return nil
}

// RemoveFieldValues drops a field's values from the game and its copies (the field was deleted).
// It reports whether the game had a value and how many copies had one.
func (g *Game) RemoveFieldValues(fieldID string) (gameHad bool, copies int) {
	if _, ok := g.fields[fieldID]; ok {
		delete(g.fields, fieldID)

		gameHad = true
	}

	for i := range g.copies {
		if _, ok := g.copies[i].Fields[fieldID]; ok {
			delete(g.copies[i].Fields, fieldID)

			copies++
		}
	}

	return gameHad, copies
}

// ReplaceChoice replaces a list value with another in a field's values, or clears it when to is
// empty (the value was removed from the list). A multilist never ends up with the same value
// twice. It reports whether anything changed.
func (g *Game) ReplaceChoice(fieldID, from, to string) bool {
	changed := replaceChoice(g.fields, fieldID, from, to)

	for i := range g.copies {
		if replaceChoice(g.copies[i].Fields, fieldID, from, to) {
			changed = true
		}
	}

	return changed
}

func replaceChoice(values FieldValues, fieldID, from, to string) bool {
	v, ok := values[fieldID]
	if !ok {
		return false
	}

	switch {
	case v.Choice == from:
		v.Choice = to
	case slices.Contains(v.Choices, from):
		next := make([]string, 0, len(v.Choices))
		for _, c := range v.Choices {
			if c == from {
				c = to
			}

			if c != "" && !slices.Contains(next, c) {
				next = append(next, c)
			}
		}

		v.Choices = next
	default:
		return false
	}

	if v.IsZero() {
		delete(values, fieldID)
	} else {
		values[fieldID] = v
	}

	return true
}

// fillFields adds to dst the values of src for fields dst has no value for.
func fillFields(dst, src FieldValues) FieldValues {
	out := dst.compact()

	for id, v := range src.compact() {
		if _, ok := out[id]; ok {
			continue
		}

		if out == nil {
			out = FieldValues{}
		}

		out[id] = v
	}

	return out
}
```

- [ ] **Step 5: Wire it into the game and the copy**
  - `game.go`:
    - Add `fields FieldValues` to the `Game` struct, after `rating`.
    - Add to `Info`, after `Rating`:
      ```go
      // Fields are the game's custom field values, by field id.
      Fields FieldValues
      ```
    - In `New`/`Rehydrate`, wherever `info.PlayStatus` is copied in, add `fields: info.Fields.compact(),`.
    - In `Info()`, add `Fields: g.fields.compact(),`.
    - In `UpdateInfo`, add `g.fields = i.Fields.compact()` next to `g.playStatus, g.rating = …`.
    - In `Absorb`, after the rating, add `g.fields = fillFields(g.fields, other.fields)`.
  - `copy.go`:
    - Add to `Copy`, after `Estimates`:
      ```go
      // Fields are the copy's custom field values, by field id.
      Fields FieldValues
      ```
    - In `clone()`, add `c.Fields = c.Fields.compact()` (it returns a deep copy).

  Check with `grep -n "func (c Copy) clone" -A 12 internal/domain/game/copy.go` that `clone` copies `Photos` and `Estimates` the same way, and follow that style.

- [ ] **Step 6: Run the test and the domain package.**

Run: `... go test ./internal/domain/game/`. Expected: `ok`.

- [ ] **Step 7: Lint and commit**

Run: `task lint` (`task lint:fix` first if it reports layout). Expected: `0 issues.`

```bash
git add internal/domain/game
git commit -m "Games: custom field values on the game and on each copy"
```

---

### Task 2: Field definitions and validation (`internal/domain/field`)

**Files:**
- Create: `internal/domain/field/field.go`, `internal/domain/field/validate.go`, `internal/domain/field/field_test.go`

**Interfaces:**
- Consumes: `game.FieldValue`, `game.FieldValues`, `game.Kind`, `game.Money`.
- Produces:
  - `field.Type` (`TypeText`, `TypeLongText`, `TypeBool`, `TypeNumber`, `TypeMoney`, `TypeDate`, `TypeDuration`, `TypeList`, `TypeMultiList`)
  - `field.Scope` (`ScopeGame`, `ScopeCopy`)
  - `field.Choice{ID, Name string}`
  - `field.Definition{ID, Name string; Type; Scope; Kinds []game.Kind; Decimals int; Unit, Currency string; Choices []Choice}`
  - `field.Set`: `NewSet([]Definition)`, `Definitions()`, `Get(id) (Definition, bool)`, `Add(d) (Definition, error)`, `Update(d) error`, `Move(id string, index int) error`, `Remove(id) error`, `AddChoice(fieldID, name string) (Choice, error)`, `RemoveChoice(fieldID, choiceID, mergeInto string) error`, `Validate(scope Scope, kind game.Kind, values game.FieldValues) (game.FieldValues, error)`
  - `field.ErrNotFound`; `*field.ValidationError`
  - `field.Repository` port: `Fields(ctx) (*Set, error)` and `SaveFields(ctx, *Set) error`

- [ ] **Step 1: Write the failing test** (`internal/domain/field/field_test.go`)

```go
package field

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

func i64(n int64) *int64 { return &n }

func TestDefinitions(t *testing.T) {
	t.Run("GIVEN a new set", func(t *testing.T) {
		s := NewSet(nil)

		t.Run("THEN fields get ids, names are unique and choices get ids", func(t *testing.T) {
			d, err := s.Add(Definition{Name: "Given to", Type: TypeList, Scope: ScopeCopy, Choices: []Choice{{Name: "Ana"}}})
			require.NoError(t, err)
			assert.NotEmpty(t, d.ID)
			assert.NotEmpty(t, d.Choices[0].ID)

			_, err = s.Add(Definition{Name: "given TO", Type: TypeText, Scope: ScopeGame})
			assert.Error(t, err, "names are unique, ignoring case")
		})

		t.Run("THEN broken definitions are refused", func(t *testing.T) {
			for name, d := range map[string]Definition{
				"no name":           {Type: TypeText, Scope: ScopeGame},
				"unknown type":      {Name: "X", Type: "colour", Scope: ScopeGame},
				"unknown scope":     {Name: "X", Type: TypeText, Scope: "shelf"},
				"3 decimals":        {Name: "X", Type: TypeNumber, Scope: ScopeGame, Decimals: 3},
				"kinds on a game":   {Name: "X", Type: TypeText, Scope: ScopeGame, Kinds: []game.Kind{game.KindKey}},
				"bad currency":      {Name: "X", Type: TypeMoney, Scope: ScopeGame, Currency: "euro"},
				"repeated choice":   {Name: "X", Type: TypeList, Scope: ScopeGame, Choices: []Choice{{Name: "A"}, {Name: "a"}}},
				"choices on a text": {Name: "X", Type: TypeText, Scope: ScopeGame, Choices: []Choice{{Name: "A"}}},
			} {
				_, err := NewSet(nil).Add(d)
				var v *ValidationError
				assert.True(t, errors.As(err, &v), name)
			}
		})
	})

	t.Run("GIVEN a list field WHEN it is updated", func(t *testing.T) {
		s := NewSet(nil)
		d, _ := s.Add(Definition{Name: "Shelf", Type: TypeList, Scope: ScopeGame, Choices: []Choice{{Name: "A"}}})

		t.Run("THEN the type and scope cannot change", func(t *testing.T) {
			changed := d
			changed.Type = TypeText
			assert.Error(t, s.Update(changed))
		})

		t.Run("THEN choices are renamed in place and new ones get ids, but none disappears", func(t *testing.T) {
			changed := d
			changed.Choices = []Choice{{ID: d.Choices[0].ID, Name: "Top"}, {Name: "Bottom"}}
			require.NoError(t, s.Update(changed))
			got, _ := s.Get(d.ID)
			assert.Equal(t, "Top", got.Choices[0].Name)
			assert.NotEmpty(t, got.Choices[1].ID)

			changed.Choices = got.Choices[1:]
			assert.Error(t, s.Update(changed), "removing a value goes through RemoveChoice")
		})

		t.Run("THEN AddChoice reuses an existing name", func(t *testing.T) {
			got, _ := s.Get(d.ID)
			c, err := s.AddChoice(d.ID, "top")
			require.NoError(t, err)
			assert.Equal(t, got.Choices[0].ID, c.ID)
		})
	})

	t.Run("GIVEN 50 fields THEN a 51st is refused", func(t *testing.T) {
		s := NewSet(nil)
		for i := range MaxFields {
			_, err := s.Add(Definition{Name: strings.Repeat("x", i+1), Type: TypeText, Scope: ScopeGame})
			require.NoError(t, err)
		}

		_, err := s.Add(Definition{Name: "one more", Type: TypeText, Scope: ScopeGame})
		assert.Error(t, err)
	})
}

func TestValidate(t *testing.T) {
	s := NewSet(nil)
	text, _ := s.Add(Definition{Name: "By", Type: TypeText, Scope: ScopeGame})
	weight, _ := s.Add(Definition{Name: "Weight", Type: TypeNumber, Scope: ScopeCopy, Decimals: 2, Unit: "g"})
	sealed, _ := s.Add(Definition{Name: "Sealed", Type: TypeBool, Scope: ScopeCopy, Kinds: []game.Kind{game.KindPhysical}})
	tags, _ := s.Add(Definition{Name: "Awards", Type: TypeMultiList, Scope: ScopeGame, Choices: []Choice{{Name: "A"}, {Name: "B"}}})
	day, _ := s.Add(Definition{Name: "Gifted", Type: TypeDate, Scope: ScopeGame})
	cost, _ := s.Add(Definition{Name: "Shipping", Type: TypeMoney, Scope: ScopeCopy, Currency: "EUR"})
	yes := true

	t.Run("GIVEN valid values THEN they are normalized", func(t *testing.T) {
		got, err := s.Validate(ScopeGame, "", game.FieldValues{
			text.ID: {Text: "  A friend  "},
			tags.ID: {Choices: []string{tags.Choices[1].ID, tags.Choices[0].ID, tags.Choices[1].ID}},
			day.ID:  {Date: "2024-02"},
		})
		require.NoError(t, err)
		assert.Equal(t, "A friend", got[text.ID].Text)
		assert.Equal(t, []string{tags.Choices[0].ID, tags.Choices[1].ID}, got[tags.ID].Choices, "field order, no repeats")
		assert.Equal(t, "2024-02", got[day.ID].Date)

		got, err = s.Validate(ScopeCopy, game.KindPhysical, game.FieldValues{
			weight.ID: {Number: i64(45050)},
			sealed.ID: {Bool: &yes},
			cost.ID:   {Money: &game.Money{Amount: 499}},
		})
		require.NoError(t, err)
		assert.Equal(t, "EUR", got[cost.ID].Money.Currency, "the field's currency fills in")
	})

	t.Run("GIVEN invalid values THEN each is refused, naming the field", func(t *testing.T) {
		for name, tc := range map[string]struct {
			scope  Scope
			kind   game.Kind
			values game.FieldValues
		}{
			"unknown field":        {ScopeGame, "", game.FieldValues{"gone": {Text: "x"}}},
			"wrong type":           {ScopeGame, "", game.FieldValues{text.ID: {Bool: &yes}}},
			"copy field on a game": {ScopeGame, "", game.FieldValues{weight.ID: {Number: i64(1)}}},
			"kind not allowed":     {ScopeCopy, game.KindKey, game.FieldValues{sealed.ID: {Bool: &yes}}},
			"unknown choice":       {ScopeGame, "", game.FieldValues{tags.ID: {Choices: []string{"nope"}}}},
			"bad date":             {ScopeGame, "", game.FieldValues{day.ID: {Date: "2024-13"}}},
			"too long":             {ScopeGame, "", game.FieldValues{text.ID: {Text: strings.Repeat("x", 251)}}},
			"too big":              {ScopeCopy, game.KindPhysical, game.FieldValues{weight.ID: {Number: i64(MaxNumber*100 + 1)}}},
			"other currency":       {ScopeCopy, game.KindPhysical, game.FieldValues{cost.ID: {Money: &game.Money{Amount: 1, Currency: "USD"}}}},
		} {
			_, err := s.Validate(tc.scope, tc.kind, tc.values)
			var v *ValidationError
			assert.True(t, errors.As(err, &v), name)
		}
	})

	t.Run("GIVEN a removed choice with a merge target THEN RemoveChoice keeps the field consistent", func(t *testing.T) {
		require.NoError(t, s.RemoveChoice(tags.ID, tags.Choices[0].ID, tags.Choices[1].ID))
		got, _ := s.Get(tags.ID)
		assert.Len(t, got.Choices, 1)
		assert.Error(t, s.RemoveChoice(tags.ID, got.Choices[0].ID, "nope"), "the merge target must exist")
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `... go test ./internal/domain/field/`
Expected: FAIL (`no Go files` or `undefined: NewSet`).

- [ ] **Step 3: Write `internal/domain/field/field.go`**

```go
// Package field defines custom fields: the fields users add to games or copies, of nine types,
// and the validation of their values (stored on games as game.FieldValues).
package field

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"gamevault/internal/domain/game"
)

// Type is a field's kind of value.
type Type string

// Values of Type.
const (
	TypeText      Type = "text"
	TypeLongText  Type = "longtext"
	TypeBool      Type = "bool"
	TypeNumber    Type = "number"
	TypeMoney     Type = "money"
	TypeDate      Type = "date"
	TypeDuration  Type = "duration"
	TypeList      Type = "list"
	TypeMultiList Type = "multilist"
)

// Scope says what a field describes: a game, or each of its copies.
type Scope string

// Values of Scope.
const (
	ScopeGame Scope = "game"
	ScopeCopy Scope = "copy"
)

// Limits of fields and values.
const (
	MaxFields      = 50
	MaxChoices     = 200
	MaxNameLength  = 60
	MaxText        = 250
	MaxLongText    = 10_000
	MaxNumber      = 1_000_000_000_000
	MaxMinutes     = 100_000 * 60
	maxUnitLength  = 10
)

var types = []Type{TypeText, TypeLongText, TypeBool, TypeNumber, TypeMoney, TypeDate, TypeDuration, TypeList, TypeMultiList}

// ErrNotFound means there is no field (or list value) with that id.
var ErrNotFound = errors.New("custom field not found: reload the page")

// ValidationError reports a field definition or value that breaks a rule.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Choice is one value of a list or multilist field.
type Choice struct {
	// ID never changes, so renaming the value renames it everywhere.
	ID string

	// Name is what the user sees.
	Name string
}

// Definition is one custom field.
type Definition struct {
	// ID never changes.
	ID string

	// Name is unique among the fields, ignoring case.
	Name string

	// Type is fixed once the field exists.
	Type Type

	// Scope is fixed once the field exists.
	Scope Scope

	// Kinds limits a copy field to these copy kinds; empty means every kind.
	Kinds []game.Kind

	// Decimals is 0 or 2, for number fields.
	Decimals int

	// Unit is shown after a number ("g", "%"…), for number fields.
	Unit string

	// Currency is the money field's currency (ISO 4217); empty means the default currency.
	Currency string

	// Choices are the values of a list or multilist field, in order.
	Choices []Choice
}

// AppliesTo reports whether a copy field applies to copies of this kind.
func (d Definition) AppliesTo(kind game.Kind) bool {
	return d.Scope == ScopeCopy && (len(d.Kinds) == 0 || slices.Contains(d.Kinds, kind))
}

func (d Definition) clone() Definition {
	d.Kinds = slices.Clone(d.Kinds)
	d.Choices = slices.Clone(d.Choices)

	return d
}

func (d Definition) choice(id string) (Choice, bool) {
	i := slices.IndexFunc(d.Choices, func(c Choice) bool { return c.ID == id })
	if i < 0 {
		return Choice{}, false
	}

	return d.Choices[i], true
}

// Set is the ordered list of custom fields.
type Set struct {
	defs []Definition
}

// NewSet returns a set holding these definitions, in order (from storage).
func NewSet(defs []Definition) *Set {
	s := &Set{}
	for _, d := range defs {
		s.defs = append(s.defs, d.clone())
	}

	return s
}

// Definitions returns the fields in order (copies).
func (s *Set) Definitions() []Definition {
	out := make([]Definition, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d.clone())
	}

	return out
}

// Get returns the field with this id.
func (s *Set) Get(id string) (Definition, bool) {
	i := s.index(id)
	if i < 0 {
		return Definition{}, false
	}

	return s.defs[i].clone(), true
}

func (s *Set) index(id string) int {
	return slices.IndexFunc(s.defs, func(d Definition) bool { return d.ID == id })
}

// Add creates a field, giving it and its choices ids.
func (s *Set) Add(d Definition) (Definition, error) {
	if len(s.defs) >= MaxFields {
		return Definition{}, invalid("there can be at most %d custom fields", MaxFields)
	}

	d.ID = uuid.NewString()
	for i := range d.Choices {
		d.Choices[i].ID = uuid.NewString()
	}

	d, err := s.check(d)
	if err != nil {
		return Definition{}, err
	}

	s.defs = append(s.defs, d)

	return d.clone(), nil
}

// Update changes a field's name, copy kinds, number and money options and list values. The type
// and scope never change, and list values are only removed with RemoveChoice (they are in use).
func (s *Set) Update(d Definition) error {
	i := s.index(d.ID)
	if i < 0 {
		return ErrNotFound
	}

	old := s.defs[i]
	if d.Type != old.Type || d.Scope != old.Scope {
		return invalid("the type of %q and whether it describes a game or a copy cannot change", old.Name)
	}

	for _, c := range old.Choices {
		if !slices.ContainsFunc(d.Choices, func(n Choice) bool { return n.ID == c.ID }) {
			return invalid("%q: remove a value with its own button (it may be in use)", old.Name)
		}
	}

	d.Choices = slices.Clone(d.Choices)
	for j := range d.Choices {
		if d.Choices[j].ID == "" {
			d.Choices[j].ID = uuid.NewString()
		} else if _, ok := old.choice(d.Choices[j].ID); !ok {
			return ErrNotFound
		}
	}

	d, err := s.check(d)
	if err != nil {
		return err
	}

	s.defs[i] = d

	return nil
}

// Move puts a field at index (clamped to the list).
func (s *Set) Move(id string, index int) error {
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}

	d := s.defs[i]
	s.defs = slices.Delete(s.defs, i, i+1)
	index = max(0, min(index, len(s.defs)))
	s.defs = slices.Insert(s.defs, index, d)

	return nil
}

// Remove deletes a field. Its values must be removed from games in the same transaction.
func (s *Set) Remove(id string) error {
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}

	s.defs = slices.Delete(s.defs, i, i+1)

	return nil
}

// AddChoice adds a value to a list or multilist field, or returns the existing one with that name
// (ignoring case).
func (s *Set) AddChoice(fieldID, name string) (Choice, error) {
	i := s.index(fieldID)
	if i < 0 {
		return Choice{}, ErrNotFound
	}

	d := s.defs[i]
	name = strings.TrimSpace(name)

	if j := slices.IndexFunc(d.Choices, func(c Choice) bool { return strings.EqualFold(c.Name, name) }); j >= 0 {
		return d.Choices[j], nil
	}

	c := Choice{
		ID:   uuid.NewString(),
		Name: name,
	}
	d.Choices = append(slices.Clone(d.Choices), c)

	d, err := s.check(d)
	if err != nil {
		return Choice{}, err
	}

	s.defs[i] = d

	return c, nil
}

// RemoveChoice removes a value from a list or multilist field. mergeInto, when set, is the value
// that replaces it in games (it must be another value of the field); games are updated by the
// caller in the same transaction.
func (s *Set) RemoveChoice(fieldID, choiceID, mergeInto string) error {
	i := s.index(fieldID)
	if i < 0 {
		return ErrNotFound
	}

	d := s.defs[i]
	if _, ok := d.choice(choiceID); !ok {
		return ErrNotFound
	}

	if mergeInto != "" {
		if _, ok := d.choice(mergeInto); !ok || mergeInto == choiceID {
			return invalid("%q: choose another value of the list to merge into", d.Name)
		}
	}

	d.Choices = slices.DeleteFunc(slices.Clone(d.Choices), func(c Choice) bool { return c.ID == choiceID })
	s.defs[i] = d

	return nil
}

// check normalizes a definition and enforces the rules, against the other fields of the set.
func (s *Set) check(d Definition) (Definition, error) {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > MaxNameLength {
		return d, invalid("a field needs a name of 1 to %d characters", MaxNameLength)
	}

	for _, o := range s.defs {
		if o.ID != d.ID && strings.EqualFold(o.Name, d.Name) {
			return d, invalid("there is already a field named %q", o.Name)
		}
	}

	if !slices.Contains(types, d.Type) {
		return d, invalid("%q: %q is not a field type", d.Name, d.Type)
	}

	if d.Scope != ScopeGame && d.Scope != ScopeCopy {
		return d, invalid("%q must describe a game or a copy", d.Name)
	}

	if d.Scope == ScopeGame && len(d.Kinds) > 0 {
		return d, invalid("%q: only copy fields can be limited to some copy kinds", d.Name)
	}

	for _, k := range d.Kinds {
		if !k.Valid() {
			return d, invalid("%q: %q is not a copy kind", d.Name, k)
		}
	}

	d.Unit = strings.TrimSpace(d.Unit)
	if d.Type != TypeNumber && (d.Decimals != 0 || d.Unit != "") {
		return d, invalid("%q: decimals and units are for number fields", d.Name)
	}

	if d.Decimals != 0 && d.Decimals != 2 {
		return d, invalid("%q: a number has 0 or 2 decimals", d.Name)
	}

	if len([]rune(d.Unit)) > maxUnitLength {
		return d, invalid("%q: the unit can have at most %d characters", d.Name, maxUnitLength)
	}

	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	if d.Type != TypeMoney && d.Currency != "" {
		return d, invalid("%q: a currency is for money fields", d.Name)
	}

	if d.Currency != "" && !isCurrencyCode(d.Currency) {
		return d, invalid("%q: the currency must be a three-letter code (EUR, USD…)", d.Name)
	}

	isList := d.Type == TypeList || d.Type == TypeMultiList
	if !isList && len(d.Choices) > 0 {
		return d, invalid("%q: only list fields have values to choose from", d.Name)
	}

	if len(d.Choices) > MaxChoices {
		return d, invalid("%q can have at most %d values", d.Name, MaxChoices)
	}

	for i, c := range d.Choices {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || len([]rune(c.Name)) > MaxNameLength {
			return d, invalid("%q: each value needs a name of 1 to %d characters", d.Name, MaxNameLength)
		}

		if slices.ContainsFunc(d.Choices[:i], func(o Choice) bool { return strings.EqualFold(strings.TrimSpace(o.Name), c.Name) }) {
			return d, invalid("%q has the value %q twice", d.Name, c.Name)
		}

		d.Choices[i] = c
	}

	return d.clone(), nil
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

// Repository stores the custom field definitions.
type Repository interface {
	// Fields returns the definitions, an empty set when none were saved.
	Fields(ctx context.Context) (*Set, error)

	// SaveFields replaces the stored definitions.
	SaveFields(ctx context.Context, s *Set) error
}
```

- [ ] **Step 4: Write `internal/domain/field/validate.go`**

```go
package field

import (
	"slices"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

// Validate checks values for a game (scope game, kind empty) or for a copy of the given kind, and
// returns them normalized: text trimmed, multilist choices in the field's order without repeats,
// the field's currency filled in, empty values dropped. Unknown fields are refused (the page may
// be older than a deleted field).
func (s *Set) Validate(scope Scope, kind game.Kind, values game.FieldValues) (game.FieldValues, error) {
	out := game.FieldValues{}

	for id, v := range values {
		d, ok := s.Get(id)
		if !ok {
			return nil, invalid("a custom field no longer exists: reload the page and try again")
		}

		if d.Scope != scope {
			return nil, invalid("%q describes a %s, not a %s", d.Name, d.Scope, scope)
		}

		if scope == ScopeCopy && !d.AppliesTo(kind) {
			return nil, invalid("%q does not apply to %s copies", d.Name, kind)
		}

		v, err := d.normalize(v)
		if err != nil {
			return nil, err
		}

		if !v.IsZero() {
			out[id] = v
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	return out, nil
}

// normalize checks that only the member of the field's type is set and that it is within limits.
func (d Definition) normalize(v game.FieldValue) (game.FieldValue, error) {
	wrong := invalid("%q got a value of another type: reload the page and try again", d.Name)

	switch d.Type {
	case TypeText, TypeLongText:
		text := strings.TrimSpace(v.Text)
		if !(game.FieldValue{Bool: v.Bool, Number: v.Number, Money: v.Money, Date: v.Date, Minutes: v.Minutes, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		limit := MaxText
		if d.Type == TypeLongText {
			limit = MaxLongText
		}

		if len([]rune(text)) > limit {
			return v, invalid("%q can have at most %d characters", d.Name, limit)
		}

		return game.FieldValue{Text: text}, nil
	case TypeBool:
		if !(game.FieldValue{Text: v.Text, Number: v.Number, Money: v.Money, Date: v.Date, Minutes: v.Minutes, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		return game.FieldValue{Bool: v.Bool}, nil
	case TypeNumber:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Money: v.Money, Date: v.Date, Minutes: v.Minutes, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		limit := int64(MaxNumber)
		if d.Decimals == 2 {
			limit *= 100
		}

		if v.Number != nil && (*v.Number > limit || *v.Number < -limit) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Number: v.Number}, nil
	case TypeMoney:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Number: v.Number, Date: v.Date, Minutes: v.Minutes, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		if v.Money == nil {
			return game.FieldValue{}, nil
		}

		m := *v.Money
		m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))

		if m.Currency == "" {
			m.Currency = d.Currency
		}

		if d.Currency != "" && m.Currency != d.Currency {
			return v, invalid("%q is in %s", d.Name, d.Currency)
		}

		if m.Amount < 0 || m.Amount > MaxNumber*100 || (m.Currency != "" && !isCurrencyCode(m.Currency)) {
			return v, invalid("%q is not a valid amount", d.Name)
		}

		return game.FieldValue{Money: &m}, nil
	case TypeDate:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Number: v.Number, Money: v.Money, Minutes: v.Minutes, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		date := strings.TrimSpace(v.Date)
		if date != "" && !validPartialDate(date) {
			return v, invalid("%q must be a year, a year and month, or a full date", d.Name)
		}

		return game.FieldValue{Date: date}, nil
	case TypeDuration:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Number: v.Number, Money: v.Money, Date: v.Date, Choice: v.Choice, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		if v.Minutes != nil && (*v.Minutes < 0 || *v.Minutes > MaxMinutes) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Minutes: v.Minutes}, nil
	case TypeList:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Number: v.Number, Money: v.Money, Date: v.Date, Minutes: v.Minutes, Choices: v.Choices}).IsZero() {
			return v, wrong
		}

		if _, ok := d.choice(v.Choice); v.Choice != "" && !ok {
			return v, invalid("%q: that value is no longer in the list, reload the page", d.Name)
		}

		return game.FieldValue{Choice: v.Choice}, nil
	case TypeMultiList:
		if !(game.FieldValue{Text: v.Text, Bool: v.Bool, Number: v.Number, Money: v.Money, Date: v.Date, Minutes: v.Minutes, Choice: v.Choice}).IsZero() {
			return v, wrong
		}

		var ids []string

		for _, c := range d.Choices {
			if slices.Contains(v.Choices, c.ID) {
				ids = append(ids, c.ID)
			}
		}

		for _, id := range v.Choices {
			if !slices.Contains(ids, id) {
				return v, invalid("%q: a value is no longer in the list, reload the page", d.Name)
			}
		}

		return game.FieldValue{Choices: ids}, nil
	default:
		return v, wrong
	}
}

func validPartialDate(s string) bool {
	for _, layout := range []string{"2006", "2006-01", time.DateOnly} {
		if _, err := time.Parse(layout, s); err == nil && len(s) == len(layout) {
			return true
		}
	}

	return false
}
```

- [ ] **Step 5: Run the tests.** Expected: `ok gamevault/internal/domain/field`. Check that `github.com/google/uuid` is already in `go.mod` (`grep -n uuid go.mod`); it is, because `game.NewID` uses it.

- [ ] **Step 6: Lint and commit**

Run: `task lint` (`task lint:fix` first). Expected: `0 issues.`

```bash
git add internal/domain/field
git commit -m "Custom fields: definitions of nine types and validation of their values"
```

---

### Task 3: Storage

**Files:**
- Modify: `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`
- Create: `internal/adapters/outbound/sqlite/fields_repository.go`, `internal/adapters/outbound/sqlite/fields_repository_test.go`

**Interfaces:**
- Consumes: Task 1 `FieldValue`, `FieldValues`, `Info.Fields`, `Copy.Fields`; Task 2 `field.Set`, `field.Definition`, `field.Repository`.
- Produces: `(*SettingsRepository).Fields(ctx)` and `SaveFields(ctx, *field.Set)`, implementing `field.Repository`.

- [ ] **Step 1: Failing document round trip.** In `docs_test.go`, find the test that round-trips a game document (`grep -n "func TestDocuments" internal/adapters/outbound/sqlite/docs_test.go`) and add a GIVEN. The game has `Info.Fields{"r": {Text: "Great"}, "w": {Number: &n}, "m": {Money: &game.Money{Amount: 499, Currency: "EUR"}}, "d": {Date: "2024"}, "t": {Choices: []string{"a", "b"}}}`, and a copy with `SetCopyFields(id, game.FieldValues{"s": {Bool: &yes}, "l": {Choice: "x"}, "h": {Minutes: &m}})`. Encode with `encodeGame`, decode with `decodeGame`, and assert that `got.Fields()` and `got.Copies()[0].Fields` equal the originals.

Run: `... go test ./internal/adapters/outbound/sqlite/ -run TestDocuments`. Expected: FAIL (the values are not stored).

- [ ] **Step 2: Store values.** In `docs.go`:

```go
// fieldValueDoc is the stored form of a game.FieldValue: only the member that is set.
type fieldValueDoc struct {
	Text     string   `json:"text,omitempty"`
	Bool     *bool    `json:"bool,omitempty"`
	Number   *int64   `json:"number,omitempty"`
	Amount   *int64   `json:"amount,omitempty"`
	Currency string   `json:"currency,omitempty"`
	Date     string   `json:"date,omitempty"`
	Minutes  *int64   `json:"minutes,omitempty"`
	Choice   string   `json:"choice,omitempty"`
	Choices  []string `json:"choices,omitempty"`
}

func fieldsToDoc(values game.FieldValues) map[string]fieldValueDoc {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]fieldValueDoc, len(values))
	for id, v := range values {
		d := fieldValueDoc{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}
		if v.Money != nil {
			amount := v.Money.Amount
			d.Amount, d.Currency = &amount, v.Money.Currency
		}

		out[id] = d
	}

	return out
}

func fieldsFromDoc(docs map[string]fieldValueDoc) game.FieldValues {
	if len(docs) == 0 {
		return nil
	}

	out := make(game.FieldValues, len(docs))
	for id, d := range docs {
		v := game.FieldValue{
			Text:    d.Text,
			Bool:    d.Bool,
			Number:  d.Number,
			Date:    d.Date,
			Minutes: d.Minutes,
			Choice:  d.Choice,
			Choices: d.Choices,
		}
		if d.Amount != nil {
			v.Money = &game.Money{Amount: *d.Amount, Currency: d.Currency}
		}

		out[id] = v
	}

	return out
}
```

- Add `Fields map[string]fieldValueDoc \`json:"fields,omitempty"\`` to `gameDoc` (after `Rating`) and to `copyDoc` (after `ValuedAt`).
- In the game encoder set `Fields: fieldsToDoc(g.Fields())`, and in the copy encoder `Fields: fieldsToDoc(c.Fields)`.
- In the decoder set `info.Fields = fieldsFromDoc(doc.Fields)`, and on each copy `Fields: fieldsFromDoc(cd.Fields)`.

Follow how `Estimates` and `Rating` are mapped in the same functions.

Run the test. Expected: PASS.

- [ ] **Step 3: Failing repository test** (`fields_repository_test.go`)

```go
package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

func TestFieldsRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := sqlite.NewSettingsRepository(db)

	t.Run("GIVEN no fields saved THEN the set is empty", func(t *testing.T) {
		s, err := repo.Fields(ctx)
		require.NoError(t, err)
		assert.Empty(t, s.Definitions())
	})

	t.Run("GIVEN fields of several kinds WHEN saved THEN they read back in order", func(t *testing.T) {
		s := field.NewSet(nil)
		_, err := s.Add(field.Definition{Name: "Sealed", Type: field.TypeBool, Scope: field.ScopeCopy, Kinds: []game.Kind{game.KindPhysical}})
		require.NoError(t, err)
		_, err = s.Add(field.Definition{Name: "Awards", Type: field.TypeMultiList, Scope: field.ScopeGame, Choices: []field.Choice{{Name: "GOTY"}}})
		require.NoError(t, err)
		_, err = s.Add(field.Definition{Name: "Weight", Type: field.TypeNumber, Scope: field.ScopeCopy, Decimals: 2, Unit: "g"})
		require.NoError(t, err)
		require.NoError(t, repo.SaveFields(ctx, s))

		got, err := repo.Fields(ctx)
		require.NoError(t, err)
		assert.Equal(t, s.Definitions(), got.Definitions())
	})
}
```

Check the constructor's name (`grep -n "func NewSettingsRepository" internal/adapters/outbound/sqlite/*.go`) and adapt the call.

Run it. Expected: FAIL to compile (`repo.Fields undefined`).

- [ ] **Step 4: Implement** `fields_repository.go`. Follow the pattern of `Preferences` in `settings_repository.go`: read the row with key `"fields"`, unmarshal, and return `field.NewSet(nil)` when the row is missing. The stored shape is `{"v":1,"fields":[{"id","name","type","scope","kinds","decimals","unit","currency","choices":[{"id","name"}]}]}`, with `omitempty` on the optional members. Add the compile-time check `var _ field.Repository = (*SettingsRepository)(nil)`. Wrap read errors with context, e.g. `fmt.Errorf("reading custom fields: %w", err)`, and refuse a document whose `v` is not 1, with an error that says the database is newer than the server.

Run the tests. Expected: PASS.

- [ ] **Step 5: Lint, commit**

```bash
git add internal/adapters/outbound/sqlite
git commit -m "Storage: custom field definitions in the settings, values in game and copy documents"
```

---

### Task 4: Use cases

**Files:**
- Modify: `internal/application/catalog/service.go`, `internal/application/catalog/service_test.go`, `internal/application/catalog/scanned_test.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`
- Create: `internal/application/fields/service.go`, `internal/application/fields/service_test.go`, `internal/application/catalog/fields_test.go`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces:
  - `catalog.NewService(games, tx, now, covers, photos, fields field.Repository)`: a new last parameter; nil means no fields are defined.
  - `(*catalog.Service).CreateGame(ctx, info, copies []game.CopyDetails, copyFields ...game.FieldValues)`: `copyFields[i]` belongs to `copies[i]`.
  - `(*catalog.Service).AddCopy(ctx, id, d, fields game.FieldValues)` and `UpdateCopy(ctx, id, copyID, d, fields game.FieldValues)`.
  - `UpdateGame` validates `info.Fields`.
  - `fields.NewService(defs field.Repository, games game.Repository, tx port.TxManager)` with:
    - `List(ctx) ([]field.Definition, error)`
    - `Create(ctx, d) (field.Definition, error)`
    - `Update(ctx, d) (field.Definition, error)`
    - `Move(ctx, id string, index int) error`
    - `Usage(ctx, id) (games, copies int, err error)`
    - `Delete(ctx, id) (games, copies int, err error)`
    - `RemoveChoice(ctx, fieldID, choiceID, mergeInto string) error`
    - `AddChoice(ctx, fieldID, name string) (field.Choice, error)`

- [ ] **Step 1: Failing catalog test** (`internal/application/catalog/fields_test.go`)

```go
package catalog_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

func TestCatalogValidatesFieldValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	defs := sqlite.NewSettingsRepository(db)
	set := field.NewSet(nil)
	review, _ := set.Add(field.Definition{Name: "Review", Type: field.TypeText, Scope: field.ScopeGame})
	sealed, _ := set.Add(field.Definition{Name: "Sealed", Type: field.TypeBool, Scope: field.ScopeCopy, Kinds: []game.Kind{game.KindPhysical}})
	require.NoError(t, defs.SaveFields(ctx, set))

	svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, nil, nil, defs)
	yes := true

	t.Run("GIVEN a game with a valid game value and a copy with a valid copy value", func(t *testing.T) {
		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3", Fields: game.FieldValues{review.ID: {Text: " Great "}}},
			[]game.CopyDetails{{Kind: game.KindPhysical}}, game.FieldValues{sealed.ID: {Bool: &yes}})
		require.NoError(t, err)

		t.Run("THEN both are stored, normalized", func(t *testing.T) {
			assert.Equal(t, "Great", g.Fields()[review.ID].Text)
			assert.True(t, *g.Copies()[0].Fields[sealed.ID].Bool)
		})

		t.Run("WHEN a key gets the physical-only field THEN it is refused, naming the field", func(t *testing.T) {
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{Kind: game.KindKey}, game.FieldValues{sealed.ID: {Bool: &yes}})
			var v *field.ValidationError
			require.True(t, errors.As(err, &v))
			assert.Contains(t, err.Error(), "Sealed")
		})

		t.Run("WHEN the game gets a value for a field that does not exist THEN it is refused", func(t *testing.T) {
			info := g.Info()
			info.Fields = game.FieldValues{"gone": {Text: "x"}}
			_, err := svc.UpdateGame(ctx, g.ID(), info)
			assert.Error(t, err)
		})

		t.Run("WHEN a copy's details are updated without its values THEN its values are cleared", func(t *testing.T) {
			got, err := svc.UpdateCopy(ctx, g.ID(), g.Copies()[0].ID, game.CopyDetails{Kind: game.KindPhysical}, nil)
			require.NoError(t, err)
			assert.Empty(t, got.Copies()[0].Fields)
		})
	})
}
```

Run: `... go test ./internal/application/catalog/ -run TestCatalogValidatesFieldValues`
Expected: FAIL to compile (`too many arguments` in `NewService`).

- [ ] **Step 2: Catalog changes** (`service.go`):
  - Add a `fields field.Repository` member and the last `NewService` parameter; document "fields may be nil: no custom fields are defined".
  - Add:

```go
// fieldSet returns the custom field definitions (an empty set when none are wired).
func (s *Service) fieldSet(ctx context.Context) (*field.Set, error) {
	if s.fields == nil {
		return field.NewSet(nil), nil
	}

	return s.fields.Fields(ctx)
}
```

  - `CreateGame(ctx, info, copies []game.CopyDetails, copyFields ...game.FieldValues)`. Before building the game, load the set and validate `info.Fields` with `set.Validate(field.ScopeGame, "", info.Fields)` (replace `info.Fields` with the result). After each `g.AddCopy(d, now)` that returned copy `c`, when `i < len(copyFields)`, validate `copyFields[i]` with `set.Validate(field.ScopeCopy, c.Kind, …)` and call `g.SetCopyFields(c.ID, values, now)`.
  - `UpdateGame`: inside the `mutate` callback, validate `info.Fields` the same way before `g.UpdateInfo`.
  - `AddCopy(ctx, id, d, fields game.FieldValues)`: after `g.AddCopy`, validate for `c.Kind` and `SetCopyFields`.
  - `UpdateCopy(ctx, id, copyID, d, fields game.FieldValues)`: after `g.UpdateCopy`, validate for the updated copy's kind and `SetCopyFields`. A nil `fields` clears the copy's values: the form always sends them all.
  - Update the four `NewService` callers (`cmd/gamevault/main.go`, `service_test.go`, `scanned_test.go`, `server_test.go`). `main.go` passes the settings repository it already builds (find it with `grep -n "NewSettingsRepository" cmd/gamevault/main.go`); the tests pass `nil`. The RPC handlers' `AddCopy`/`UpdateCopy` calls get `nil` for now (Task 5 maps the values).

Run the catalog tests and `go build ./...`. Expected: `ok`.

- [ ] **Step 3: Failing fields-service test** (`internal/application/fields/service_test.go`)

```go
package fields_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/fields"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

func TestFieldsService(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	defs, games := sqlite.NewSettingsRepository(db), sqlite.NewGameRepository(db)
	svc := fields.NewService(defs, games, db)
	cat := catalog.NewService(games, db, time.Now, nil, nil, defs)

	awards, err := svc.Create(ctx, field.Definition{Name: "Awards", Type: field.TypeMultiList, Scope: field.ScopeGame,
		Choices: []field.Choice{{Name: "GOTY"}, {Name: "Best art"}}})
	require.NoError(t, err)
	given, err := svc.Create(ctx, field.Definition{Name: "Given to", Type: field.TypeText, Scope: field.ScopeCopy})
	require.NoError(t, err)

	goty, art := awards.Choices[0].ID, awards.Choices[1].ID
	_, err = cat.CreateGame(ctx, game.Info{Title: "A", Fields: game.FieldValues{awards.ID: {Choices: []string{goty, art}}}},
		[]game.CopyDetails{{Kind: game.KindPhysical}}, game.FieldValues{given.ID: {Text: "Someone"}})
	require.NoError(t, err)
	_, err = cat.CreateGame(ctx, game.Info{Title: "B", Fields: game.FieldValues{awards.ID: {Choices: []string{goty}}}}, nil)
	require.NoError(t, err)

	t.Run("GIVEN values in two games WHEN a value is merged into another", func(t *testing.T) {
		require.NoError(t, svc.RemoveChoice(ctx, awards.ID, goty, art))

		t.Run("THEN every game holds the target once and the list lost the value", func(t *testing.T) {
			list, err := games.List(ctx)
			require.NoError(t, err)

			for _, g := range list {
				assert.Equal(t, []string{art}, g.Fields()[awards.ID].Choices, g.Title())
			}

			defsNow, err := svc.List(ctx)
			require.NoError(t, err)
			assert.Len(t, defsNow[0].Choices, 1)
		})
	})

	t.Run("GIVEN a copy field with a value WHEN it is deleted", func(t *testing.T) {
		gamesN, copiesN, err := svc.Usage(ctx, given.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, gamesN)
		assert.Equal(t, 1, copiesN)

		gamesN, copiesN, err = svc.Delete(ctx, given.ID)
		require.NoError(t, err)

		t.Run("THEN its values are gone and the counts said where they were", func(t *testing.T) {
			assert.Equal(t, 1, copiesN)
			assert.Equal(t, 0, gamesN)

			list, err := games.List(ctx)
			require.NoError(t, err)

			for _, g := range list {
				for _, c := range g.Copies() {
					assert.NotContains(t, c.Fields, given.ID)
				}
			}
		})
	})

	t.Run("GIVEN a multilist WHEN a value is typed that exists with another case THEN it is reused", func(t *testing.T) {
		c, err := svc.AddChoice(ctx, awards.ID, "best ART")
		require.NoError(t, err)
		assert.Equal(t, art, c.ID)
	})
}
```

Run: `... go test ./internal/application/fields/`. Expected: FAIL (`no Go files`).

- [ ] **Step 4: Write `internal/application/fields/service.go`**

```go
// Package fields implements the use cases to define custom fields and to apply the changes that
// touch every game holding a value (deleting a field, removing or merging a list value).
package fields

import (
	"context"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

// Service manages custom field definitions.
type Service struct {
	defs  field.Repository
	games game.Repository
	tx    port.TxManager
}

// NewService builds the service.
func NewService(defs field.Repository, games game.Repository, tx port.TxManager) *Service {
	return &Service{
		defs:  defs,
		games: games,
		tx:    tx,
	}
}

// List returns the fields in order.
func (s *Service) List(ctx context.Context) ([]field.Definition, error) {
	set, err := s.defs.Fields(ctx)
	if err != nil {
		return nil, err
	}

	return set.Definitions(), nil
}

// Create adds a field at the end of the list.
func (s *Service) Create(ctx context.Context, d field.Definition) (field.Definition, error) {
	var out field.Definition

	err := s.edit(ctx, func(set *field.Set) error {
		var err error
		out, err = set.Add(d)

		return err
	})

	return out, err
}

// Update changes a field (see field.Set.Update).
func (s *Service) Update(ctx context.Context, d field.Definition) (field.Definition, error) {
	var out field.Definition

	err := s.edit(ctx, func(set *field.Set) error {
		if err := set.Update(d); err != nil {
			return err
		}

		out, _ = set.Get(d.ID)

		return nil
	})

	return out, err
}

// Move puts a field at index.
func (s *Service) Move(ctx context.Context, id string, index int) error {
	return s.edit(ctx, func(set *field.Set) error { return set.Move(id, index) })
}

// AddChoice adds a value to a list field, or returns the existing one with that name.
func (s *Service) AddChoice(ctx context.Context, fieldID, name string) (field.Choice, error) {
	var out field.Choice

	err := s.edit(ctx, func(set *field.Set) error {
		var err error
		out, err = set.AddChoice(fieldID, name)

		return err
	})

	return out, err
}

// Usage counts the games and copies that hold a value for the field, to warn before deleting it.
func (s *Service) Usage(ctx context.Context, id string) (games, copies int, err error) {
	list, err := s.games.List(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, g := range list {
		if _, ok := g.Fields()[id]; ok {
			games++
		}

		for _, c := range g.Copies() {
			if _, ok := c.Fields[id]; ok {
				copies++
			}
		}
	}

	return games, copies, nil
}

// Delete removes a field and, in the same transaction, its values from every game and copy. It
// returns how many games and copies lost a value.
func (s *Service) Delete(ctx context.Context, id string) (games, copies int, err error) {
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, copies = 0, 0

		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := set.Remove(id); err != nil {
			return err
		}

		list, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		for _, g := range list {
			gameHad, n := g.RemoveFieldValues(id)
			if !gameHad && n == 0 {
				continue
			}

			if gameHad {
				games++
			}

			copies += n

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}
		}

		return s.defs.SaveFields(ctx, set)
	})

	return games, copies, err
}

// RemoveChoice removes a list value and, in the same transaction, clears it from every game and
// copy, or replaces it with mergeInto.
func (s *Service) RemoveChoice(ctx context.Context, fieldID, choiceID, mergeInto string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := set.RemoveChoice(fieldID, choiceID, mergeInto); err != nil {
			return err
		}

		list, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		for _, g := range list {
			if !g.ReplaceChoice(fieldID, choiceID, mergeInto) {
				continue
			}

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}
		}

		return s.defs.SaveFields(ctx, set)
	})
}

// edit loads the definitions, applies fn and saves them, in one transaction.
func (s *Service) edit(ctx context.Context, fn func(*field.Set) error) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := fn(set); err != nil {
			return err
		}

		return s.defs.SaveFields(ctx, set)
	})
}
```

`ReplaceChoice` and `RemoveFieldValues` change the game without touching `updatedAt`. That is acceptable: they are bulk housekeeping, not edits to one game. Leave it as is.

- [ ] **Step 5: Run the tests, lint, commit.** Run `... go test ./internal/application/... ./internal/domain/...` and `task lint`. Expected: `ok`, then `0 issues.`

```bash
git add internal/application cmd/gamevault internal/adapters/inbound/rpc
git commit -m "Custom fields: catalog validates values; fields service for definitions and bulk changes"
```

---

### Task 5: API

**Files:**
- Modify: `proto/gamevault/v1/game.proto`, `internal/adapters/inbound/rpc/game_handler.go`, `mapper.go`, `server.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`
- Create: `proto/gamevault/v1/field.proto`, `internal/adapters/inbound/rpc/field_handler.go`, `internal/adapters/inbound/rpc/fields_test.go`
- Generated: `internal/gen`, `web/src/gen`

**Interfaces:**
- Consumes: Task 4.
- Produces (TS after generation):
  - `fieldClient` with:
    - `listFields`, `createField`, `updateField`, `moveField`, `deleteField`, `fieldUsage`, `removeChoice`, `addChoice`
  - `FieldDefinition{id, name, type, scope, kinds: CopyKind[], decimals, unit, currency, choices: {id, name}[]}`
  - `FieldValue{value: {case: 'text'|'bool'|'number'|'money'|'date'|'minutes'|'choice'|'choices', value}}` (`choices` holds a `ChoiceList{ids}`)
  - `Game.fields` and `CopyDetails.fields`: `{[id: string]: FieldValue}`
  - `UpdateGameRequest.fields`

- [ ] **Step 1: Proto.** In `game.proto`:

```proto
// FieldValue is the value of a custom field (see FieldService); only the member of the field's
// type is set.
message FieldValue {
  oneof value {
    string text = 1;
    bool bool = 2;
    // In hundredths when the field has 2 decimals.
    int64 number = 3;
    Money money = 4;
    // "YYYY", "YYYY-MM" or "YYYY-MM-DD".
    string date = 5;
    int64 minutes = 6;
    // A list value's id.
    string choice = 7;
    ChoiceList choices = 8;
  }
}
message ChoiceList {
  repeated string ids = 1;
}
```

- `CopyDetails` gets `map<string, FieldValue> fields = 16;` (the copy's custom field values).
- `Game` gets `map<string, FieldValue> fields = 15;` (the game's).
- `UpdateGameRequest` gets `map<string, FieldValue> fields = 9;`.
- `CreateGameRequest` gets `map<string, FieldValue> fields = 7;`.

These numbers were checked free while planning.

Create `proto/gamevault/v1/field.proto` (same package and `go_package` as `game.proto`, importing it for `CopyKind`):

```proto
syntax = "proto3";

package gamevault.v1;

import "gamevault/v1/game.proto";

option go_package = "gamevault/internal/gen/gamevault/v1;gamevaultv1";

// FieldChoice is one value of a list or multilist field.
message FieldChoice {
  string id = 1;
  string name = 2;
}

// FieldDefinition is a custom field.
message FieldDefinition {
  string id = 1;
  string name = 2;
  // text, longtext, bool, number, money, date, duration, list, multilist.
  string type = 3;
  // game or copy.
  string scope = 4;
  // Copy fields only: the copy kinds the field applies to; empty means all.
  repeated CopyKind kinds = 5;
  int32 decimals = 6;
  string unit = 7;
  string currency = 8;
  repeated FieldChoice choices = 9;
}

message ListFieldsRequest {}
message ListFieldsResponse {
  repeated FieldDefinition fields = 1;
}
message CreateFieldRequest {
  FieldDefinition field = 1;
}
message CreateFieldResponse {
  FieldDefinition field = 1;
}
message UpdateFieldRequest {
  FieldDefinition field = 1;
}
message UpdateFieldResponse {
  FieldDefinition field = 1;
}
message MoveFieldRequest {
  string id = 1;
  int32 index = 2;
}
message MoveFieldResponse {}
message FieldUsageRequest {
  string id = 1;
}
message FieldUsageResponse {
  int32 games = 1;
  int32 copies = 2;
}
message DeleteFieldRequest {
  string id = 1;
}
message DeleteFieldResponse {
  int32 games = 1;
  int32 copies = 2;
}
message RemoveChoiceRequest {
  string field_id = 1;
  string choice_id = 2;
  // Another value of the field that replaces it in games; empty clears it.
  string merge_into = 3;
}
message RemoveChoiceResponse {}
message AddChoiceRequest {
  string field_id = 1;
  string name = 2;
}
message AddChoiceResponse {
  FieldChoice choice = 1;
}

// FieldService defines the custom fields users fill in on games and copies.
service FieldService {
  rpc ListFields(ListFieldsRequest) returns (ListFieldsResponse);
  rpc CreateField(CreateFieldRequest) returns (CreateFieldResponse);
  rpc UpdateField(UpdateFieldRequest) returns (UpdateFieldResponse);
  rpc MoveField(MoveFieldRequest) returns (MoveFieldResponse);
  rpc FieldUsage(FieldUsageRequest) returns (FieldUsageResponse);
  // Removes the field and its values from every game and copy.
  rpc DeleteField(DeleteFieldRequest) returns (DeleteFieldResponse);
  // Removes a list value, clearing it from games or merging it into another value.
  rpc RemoveChoice(RemoveChoiceRequest) returns (RemoveChoiceResponse);
  // Adds a list value, or returns the existing one with that name.
  rpc AddChoice(AddChoiceRequest) returns (AddChoiceResponse);
}
```

Check how the other proto files import each other and set `go_package` (`head -12 proto/gamevault/v1/valuation.proto`), and match it. Run `task generate`. Expected: generated code; the build fails until Step 3.

- [ ] **Step 2: Failing end-to-end test** (`fields_test.go`). Extend the `clients` struct in `server_test.go` with `fields gamevaultv1connect.FieldServiceClient` and wire the handler in `newServer`, following `valuation`. The test then:
  - creates a physical-only copy field "Sealed" (bool) and a game multilist "Awards" with one choice;
  - creates a game with a physical copy carrying `Sealed: true` (`CreateGame` with the copy's `details.fields`) and the award;
  - asserts the response's `Game.Fields` and the copy's `Details.Fields` hold them;
  - `UpdateCopy` on a key copy with `Sealed` → `connect.CodeInvalidArgument`;
  - `DeleteField` on Sealed returns `copies: 1`, and a `GetGame` shows the copy without it;
  - `ListFields` returns Awards only.

Use GIVEN/WHEN/THEN like `valuation_test.go`.

Run: `... go test ./internal/adapters/inbound/rpc/ -run TestFields`. Expected: FAIL to compile.

- [ ] **Step 3: Mapping and handlers.**
  - `mapper.go`:
    - `fieldValuesToPB(game.FieldValues) map[string]*pb.FieldValue` and `fieldValuesFromPB(map[string]*pb.FieldValue) game.FieldValues`, a 1:1 mapping of the oneof onto the struct members (Money via the existing money mappers; `ChoiceList.Ids` ↔ `Choices`).
    - `definitionToPB` / `definitionFromPB`, with `Kinds` mapped through the existing copy-kind mapping used by `detailsFromPB`.
    - `gameToPB` sets `Fields: fieldValuesToPB(g.Fields())`, and on each copy `Details.Fields = fieldValuesToPB(c.Fields)`.
    - In `toConnectError`, add `errors.As(err, &fv)` for `*field.ValidationError` to the `InvalidArgument` case and `errors.Is(err, field.ErrNotFound)` to `NotFound`.
  - `game_handler.go`:
    - `CreateGame`: `info.Fields = fieldValuesFromPB(req.Msg.Fields)`, and pass `fieldValuesFromPB(d.Fields)` of each copy as `copyFields`.
    - `UpdateGame`: `Fields: fieldValuesFromPB(req.Msg.Fields)`.
    - `AddCopy` / `UpdateCopy`: pass `fieldValuesFromPB(req.Msg.Details.Fields)` (`Details` may be nil: `detailsFromPB` already refuses it first).
    - `AddScannedCopies` keeps passing details only (scans have no field values).
  - `field_handler.go`: a `FieldHandler` over `*fields.Service`, one method per RPC, mapping errors with `toConnectError`, with `var _ gamevaultv1connect.FieldServiceHandler = (*FieldHandler)(nil)`.
  - Register it in `server.go` next to the others (look how `Valuation` is registered), and build it in `main.go` with `fields.NewService(settingsRepo, games, db)`.

Run the test and `task test`. Expected: green.

- [ ] **Step 4: Lint, commit**

```bash
git add proto internal/gen web/src/gen internal/adapters/inbound/rpc cmd/gamevault
git commit -m "API: FieldService and custom field values on games and copies"
```

---

### Task 6: Web data and pure helpers

**Files:**
- Create: `web/src/lib/fields.ts`, `web/tests/fields.test.mjs`
- Modify: `web/src/state/AppData.tsx`, `web/src/api/client.ts`, `web/src/lib/model.ts`

**Interfaces:**
- Consumes: Task 5 generated types.
- Produces:
  - `useAppData().fields: FieldDefinition[]` and `reloadFields(): Promise<void>`
  - `fieldClient` in `api/client.ts`
  - `gameInfo(g)` now includes `fields`
  - `lib/fields.ts`:
    - `fieldText(def, value, fmt) → string`, where `fmt` is `{ money(amount, currency), date(d), lang }`
    - `parseNumber(input, decimals) → bigint | null` and `numberInput(value, decimals) → string`
    - `searchableText(game, defs) → string`
    - `type FieldFilter = Record<string, string[]>`
    - `matchesFieldFilters(game, defs, filter) → boolean`
    - `filterOptions(games, def) → {key, count}[]`

- [ ] **Step 1: Failing tests** (`web/tests/fields.test.mjs`). They use plain objects shaped like the generated messages: `{ fields: { id: { value: { case: 'text', value: 'x' } } }, copies: [{ details: { kind: 3, fields: {…} } }] }`, and definitions `{ id, name, type, scope, kinds, decimals, unit, currency, choices }`. Cover:
  - `fieldText` per type:
    - `number` with 2 decimals and unit: 45050n → "450.50 g";
    - `duration`: 750 minutes → "12 h 30 min";
    - `bool`: "Yes";
    - `multilist`: names joined by ", " in field order;
    - `date`: "2024-02" through `fmt.date`;
  - `parseNumber`: "12.5" with 2 decimals → 1250n; "12,50" → 1250n (comma accepted); "1.234" with 0 decimals → null; "" → null;
  - `numberInput(1250n, 2)` → "12.50";
  - `searchableText` includes text and long-text values of the game and its copies, and nothing else;
  - `matchesFieldFilters`:
    - a list filter with a choice id matches;
    - `""` matches a game without a value;
    - a bool filter `'yes'` / `'no'` / `''`;
    - a copy-field filter matches when any copy matches;
    - an empty filter list for a field matches everything;
  - `filterOptions` counts games per choice and "(empty)".

Run: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/fields.test.mjs`. Expected: FAIL (`Cannot find module …/lib/fields.ts`).

- [ ] **Step 2: Write `web/src/lib/fields.ts`.** It uses structural types only: no imports from `gen/` or `model.ts`.

```ts
// Custom fields in the web: formatting, number input, search and filters. Structural types only,
// so Node tests run it as is (no generated code, no TypeScript enums).

export interface Choice { id: string; name: string }
export interface Definition {
  id: string; name: string; type: string; scope: string; kinds: number[];
  decimals: number; unit: string; currency: string; choices: Choice[];
}
export interface Money { amountMinor: bigint; currency: string }
export type Value = { value:
  | { case: 'text'; value: string } | { case: 'bool'; value: boolean } | { case: 'number'; value: bigint }
  | { case: 'money'; value: Money } | { case: 'date'; value: string } | { case: 'minutes'; value: bigint }
  | { case: 'choice'; value: string } | { case: 'choices'; value: { ids: string[] } } | { case: undefined; value?: undefined } };
export type Values = Record<string, Value>;
interface CopyLike { details?: { kind: number; fields: Values } }
interface GameLike { fields: Values; copies: CopyLike[] }
export interface Formatters { money: (amountMinor: bigint, currency: string) => string; date: (d: string) => string; yes: string; no: string; hours: string; minutes: string }

/** Filter values: choice ids, 'yes' / 'no', or '' for "no value". */
export type FieldFilter = Record<string, string[]>;

export function numberInput(v: bigint, decimals: number): string {
  if (decimals !== 2) return v.toString();
  const neg = v < 0n;
  const abs = neg ? -v : v;
  return `${neg ? '-' : ''}${abs / 100n}.${(abs % 100n).toString().padStart(2, '0')}`;
}

/** The value typed in a number input, in hundredths when the field has 2 decimals; null when invalid or empty. */
export function parseNumber(input: string, decimals: number): bigint | null {
  const s = input.trim().replace(',', '.');
  if (!s) return null;
  const m = decimals === 2 ? /^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(s) : /^(-?)(\d+)$/.exec(s);
  if (!m) return null;
  const whole = BigInt(m[2]!);
  const cents = decimals === 2 ? BigInt((m[3] ?? '').padEnd(2, '0') || '0') : 0n;
  const v = decimals === 2 ? whole * 100n + cents : whole;
  return m[1] ? -v : v;
}

/** A value as text for the sheet and the copy card. */
export function fieldText(def: Definition, v: Value | undefined, fmt: Formatters): string {
  const val = v?.value;
  if (!val || val.case === undefined) return '';
  switch (val.case) {
    case 'text': return val.value;
    case 'bool': return val.value ? fmt.yes : fmt.no;
    case 'number': return [numberInput(val.value, def.decimals), def.unit].filter(Boolean).join(' ');
    case 'money': return fmt.money(val.value.amountMinor, val.value.currency);
    case 'date': return fmt.date(val.value);
    case 'minutes': {
      const h = val.value / 60n; const m = val.value % 60n;
      return [h > 0n && `${h} ${fmt.hours}`, m > 0n && `${m} ${fmt.minutes}`].filter(Boolean).join(' ') || `0 ${fmt.minutes}`;
    }
    case 'choice': return def.choices.find((c) => c.id === val.value)?.name ?? '';
    case 'choices': return def.choices.filter((c) => val.value.ids.includes(c.id)).map((c) => c.name).join(', ');
  }
}

const textOf = (v?: Value) => (v?.value.case === 'text' ? v.value.value : '');

/** The text of a game's (and its copies') text and long-text fields, for the search box. */
export function searchableText(g: GameLike, defs: Definition[]): string {
  const texts = defs.filter((d) => d.type === 'text' || d.type === 'longtext');
  return texts.flatMap((d) => (d.scope === 'game' ? [textOf(g.fields[d.id])] : g.copies.map((c) => textOf(c.details?.fields[d.id]))))
    .filter(Boolean).join(' ');
}

/** The filter keys a value matches: choice ids, 'yes' / 'no', or '' when it has none. */
function keysOf(def: Definition, v: Value | undefined): string[] {
  const val = v?.value;
  if (!val || val.case === undefined) return [''];
  if (val.case === 'choice') return [val.value];
  if (val.case === 'choices') return val.value.ids.length ? val.value.ids : [''];
  if (val.case === 'bool') return [val.value ? 'yes' : 'no'];
  return [''];
}

function gameKeys(g: GameLike, def: Definition): string[] {
  if (def.scope === 'game') return keysOf(def, g.fields[def.id]);
  const copies = g.copies.filter((c) => c.details && (!def.kinds.length || def.kinds.includes(c.details.kind)));
  return copies.length ? copies.flatMap((c) => keysOf(def, c.details!.fields[def.id])) : [''];
}

/** A game matches when, for every filtered field, one of its keys is among the chosen ones. */
export function matchesFieldFilters(g: GameLike, defs: Definition[], filter: FieldFilter): boolean {
  return defs.every((d) => {
    const chosen = filter[d.id];
    if (!chosen?.length) return true;
    return gameKeys(g, d).some((k) => chosen.includes(k));
  });
}

/** How many games each filter option matches, in display order ('' last). */
export function filterOptions(games: GameLike[], def: Definition): { key: string; count: number }[] {
  const keys = def.type === 'bool' ? ['yes', 'no'] : def.choices.map((c) => c.id);
  return [...keys, ''].map((key) => ({ key, count: games.filter((g) => gameKeys(g, def).includes(key)).length }));
}
```

  Adjust `Value`/`Money` to the real generated field names after Task 5 (`grep -n "amountMinor\|case: \"choices\"" web/src/gen/gamevault/v1/game_pb.ts`). Keep the test's plain objects matching them.

- [ ] **Step 3: App data.**
  - Add `fieldClient` to `api/client.ts`, like the other clients.
  - In `AppData.tsx`, add `fields` state, `reloadFields`, loaded with the rest in the first effect (`fieldClient.listFields({})`), and expose both.
  - In `model.ts`, `gameInfo` gains `fields: g.fields`, so every `UpdateGame` keeps the values.

- [ ] **Step 4: Run tests, type check, commit.** Run `task test`. Expected: green.

```bash
git add web/src/lib/fields.ts web/tests/fields.test.mjs web/src/state/AppData.tsx web/src/api/client.ts web/src/lib/model.ts
git commit -m "Web: custom field definitions in the app data; formatting, search and filter helpers"
```

---

### Task 7: The Fields page

**Files:**
- Create: `web/src/features/fields/FieldsPage.tsx`, `web/src/features/fields/FieldDialog.tsx`
- Modify: `web/src/App.tsx`, `web/src/components/Icon.tsx`, `web/src/i18n/locales/en.json`, `es.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 6 (`useAppData().fields`, `reloadFields`, `fieldClient`).

- [ ] **Step 1: Route and navigation.**
  - In `App.tsx`, add `'fields'` to `ROUTES` and `{ route: 'fields', icon: 'fields' }` to `SETTINGS` after `providers`, and render `{route === 'fields' && <FieldsPage />}`.
  - Add a `fields` icon to `Icon.tsx`, a form with lines: `'M4 5h16v4H4zM4 15h16v4H4zM7 7h.01M7 17h.01'`.
  - Add `nav.fields` ("Fields" / "Campos") to both locales.

- [ ] **Step 2: Texts** (`fields` group in both locales):
  - title, intro;
  - `add`, `edit`, `delete`, `confirmDelete` (with `{{name}}`, `{{games}}`, `{{copies}}`), `empty`;
  - `name`, `type`, `scope`, `scopeGame`, `scopeCopy`, `kinds`, `kindsAll`, `decimals`, `unit`, `currency`, `currencyDefault`;
  - `values`, `addValue`, `removeValue`, `mergeInto`, `leaveEmpty`;
  - `moveUp`, `moveDown`;
  - `fixedAfterCreate` ("The type and whether it describes a game or a copy cannot change later.");
  - one key per type under `fields.types.<type>`.

  Write both languages fully. Run `task i18n`.

- [ ] **Step 3: `FieldsPage.tsx`.**
  - A page like `ProvidersPage` (`grep -n "export default function ProvidersPage" -A 30 web/src/features/providers/*.tsx` for the layout classes).
  - Each row shows the name, the type label, "Game" or "Copy" (with its kinds), up/down buttons calling `fieldClient.moveField({ id, index })` and **Edit** (opens `FieldDialog`); **Add field** sits at the top.
  - After each change, `reloadFields()`.
  - Delete lives in the dialog: it first calls `fieldUsage`, then `confirm(t('fields.confirmDelete', …))`, then `deleteField`, then `reloadFields()` and `reloadGames()`, because games change.

- [ ] **Step 4: `FieldDialog.tsx`** (`Modal`, like `SourceDialog`):
  - name;
  - type and scope selects, disabled when editing;
  - copy kinds as checkboxes (copy scope);
  - decimals (0/2) and unit (number);
  - currency (money, placeholder = default currency);
  - values (list/multilist): rows with a name input, up/down and **Remove**. Remove asks "merge into" with a select of the other values plus "Leave empty", then calls `removeChoice` at once. New rows get no id; `updateField` assigns one.
  - **Save** calls `createField` or `updateField` and shows errors in an `Alert`.

- [ ] **Step 5: Type check and commit.** Run `task test`. Expected: green.

```bash
git add web/src/features/fields web/src/App.tsx web/src/components/Icon.tsx web/src/i18n/locales web/src/styles.css
git commit -m "Web: a Fields page to define custom fields"
```

---

### Task 8: Filling the values in

**Files:**
- Create: `web/src/features/fields/FieldInput.tsx`, `web/src/features/fields/FieldList.tsx`
- Modify: `web/src/features/library/GameDetail.tsx`, `web/src/features/library/CopyForm.tsx`, `web/src/features/library/GameSheet.tsx` (or wherever `SheetOverview` lives), `web/src/i18n/locales/*.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 6 `fieldText`, `parseNumber`, `numberInput`; `fieldClient.addChoice`.
- Produces:
  - `FieldInput({ def, value, onChange })`: one control per type;
  - `FieldInputs({ defs, values, onChange })`: a grid of them;
  - `FieldList({ defs, values })`: a definition list of the values set, formatted.

- [ ] **Step 1: `FieldInput.tsx`.** One component, with a branch per type:

| Type | Control | Value |
|---|---|---|
| text | `<input maxLength={250}>` | `{case:'text'}` |
| longtext | `<textarea maxLength={10000}>` | `{case:'text'}` |
| bool | three-state segmented control (Yes / No / —); "—" removes the value | `{case:'bool'}` |
| number | text input `inputMode="decimal"`, with the unit as a suffix | keep the typed string locally; on blur, `parseNumber` sets `{case:'number'}`, or flags the input invalid and keeps the old value |
| money | amount input (same parsing, 2 decimals) + the currency shown (the field's currency or the default from preferences) | `{case:'money'}` |
| date | year input + month select (optional) + day select (optional, only with a month) | `{case:'date'}` as `YYYY`, `YYYY-MM` or `YYYY-MM-DD` |
| duration | hours and minutes number inputs | `{case:'minutes'}` |
| list | a segmented control when there are 4 choices or fewer, else a `<select>` with an empty option | `{case:'choice'}` |
| multilist | chips of the chosen values, and an input with a `datalist` of the others. Enter or a pick adds the value. A new name calls `fieldClient.addChoice`, then `reloadFields()`, then adds the returned id. A chip's × removes it. | `{case:'choices'}` |

  Removing a value sends an empty `{ value: { case: undefined } }`; the parent drops empty values before saving.

- [ ] **Step 2: Game sheet.**
  - Overview: below the existing details, `<FieldList defs={gameDefs} values={game.fields} />` under a `t('fields.more')` heading ("More details" / "Más datos"), only when one has a value.
  - Edit tab: `info` state gains `fields`, and the `dirty` check compares them (`JSON.stringify` of the maps after dropping empties is enough). After the notes, render `<FieldInputs defs={gameDefs} values={info.fields} onChange={…} />`, and save it through `onSave({ …, fields })`.
  - `GamePatch` gains `'fields'`.

- [ ] **Step 3: Copy form and card.**
  - `CopyForm` gets the copy defs that apply to the selected kind (`defs.filter(d => d.scope === 'copy' && (!d.kinds.length || d.kinds.includes(kind)))`). It renders them after the copy's own fields and submits them in `details.fields`. Values of fields that no longer apply to a changed kind are dropped.
  - The copy card in `CopiesTab` shows `FieldList` inline (`className="copy-fields"`) for the copy's values.

- [ ] **Step 4: Type check, commit.** Run `task test`. Expected: green.

```bash
git add web/src/features web/src/i18n/locales web/src/styles.css
git commit -m "Web: fill in custom fields on the game sheet and the copy form"
```

---

### Task 9: Filters, search, docs and browser check

**Files:**
- Modify: `web/src/features/library/FilterPanel.tsx`, `web/src/features/library/LibraryPage.tsx`, `web/src/i18n/locales/*.json`, `docs/technical.md`

- [ ] **Step 1: Filters.**
  - `Filters` gains `fields: FieldFilter` (`NO_FILTERS.fields = {}`), and `activeFilterCount` adds the number of chosen values.
  - `matchesFilters(g, f)` becomes `matchesFilters(g, f, defs)` and ends with `matchesFieldFilters(g, defs, f.fields)`; update its callers.
  - `FilterPanel` renders, after the existing groups, one group per list, multilist and bool field, with `filterOptions(games, def)`. The labels:
    - choice names;
    - `t('fields.yes')` / `t('fields.no')` for bool;
    - `t('fields.noValue')` for `''`.

    It toggles values in `f.fields[def.id]`.

- [ ] **Step 2: Search.** In `LibraryPage`, add `searchableText(g, fields)` to the `hay` array.

- [ ] **Step 3: Docs.** Add a "Custom fields" section to `docs/technical.md`. It covers the nine types, game or copy scope and copy kinds, the limits, what stays fixed, storage (the `fields` setting, values inside the game and copy documents, empty values never stored), the bulk changes (delete, remove/merge a value), filters and search, and that scans never touch the values.

- [ ] **Step 4: Lint and tests.** Run `task lint` and `task test`. Expected: green.

- [ ] **Step 5: Browser check.** `task test-server`, then http://127.0.0.1:8093/?v=<new number>. On desktop:
  - **Fields page:** create one field of each of the nine types, mixing game and copy scope, including a physical-only copy field. Reorder two fields.
  - **A game:** fill every game field in the Edit tab and save. The overview shows "More details" with each value formatted (number with its unit, duration in h/min, money and date localized).
  - **Copies:** a physical copy shows the physical-only field and a key copy does not. Fill them in; the copy card shows them.
  - **Multilist:** type a new value; it is added to the field.
  - **Library:** filter by a list value, by "(empty)" and by Yes/No; search a text typed in a text field.
  - **Fields page again:**
    - rename a list value; the game shows the new name;
    - remove a value with "merge into"; the game holds the target once;
    - delete a field; the confirmation shows the counts and the value disappears.

  Then the mobile preset: the Fields page from the Settings tab, the field dialog, the edit tab and the copy form, all without widening the page. Reset the viewport, then `task test-server:stop`.

- [ ] **Step 6: Commit**

```bash
git add web docs
git commit -m "Custom fields in the library filters and search; documentation"
```
