# Custom fields

Status: approved design, waiting for the implementation plan. Branch `feature/custom-fields`.

## Why

Collectors track things Game Vault has no field for: who recommended a game, whether a disc is
sealed, what shipping cost, a personal review, awards, who a copy was lent or given to. Collection
apps such as CLZ let the user define their own fields, each of a type, and fill them in on every
item. Game Vault gets the same, adapted to its model where a game has several copies.

## Decisions

- **Per game or per copy, chosen per field.** A game field ("My review", "Recommended by") appears
  once on the game; a copy field ("Sealed", "Given to", "Shipping cost") appears on each copy, and
  may be limited to some copy kinds (e.g. physical only).
- **Nine types**, covering CLZ's fifteen without the variants: Text, Long text, Yes/No, Number
  (whole or two decimals, optional unit), Money, Date (full or partial), Duration, List (pick one
  of managed values), Multi-list (pick several, like tags).
- **Filter and search.** The library filters by List, Multi-list and Yes/No fields, and its search
  box finds text in Text and Long text fields. Sorting, list-view columns and CSV come later.
- **Definitions in one JSON document, values inside the game and copy documents**, keyed by the
  field's id: renaming a field or a list value touches no game. No schema migration.

## Field definitions (`internal/domain/field`)

```go
type ID string        // a UUID, never reused
type Scope string     // "game" | "copy"
type Type string      // "text" | "longtext" | "bool" | "number" | "money" | "date" | "duration" | "list" | "multilist"

type Definition struct {
	ID       ID
	Name     string     // 1-60 characters, unique (case-insensitive)
	Type     Type       // fixed after creation
	Scope    Scope      // fixed after creation
	Kinds    []game.Kind // copy fields only: the copy kinds it applies to; empty = all
	Decimals int        // number: 0 or 2
	Unit     string     // number: free text up to 10 characters ("%", "g", "cm"…), may be empty
	Currency string     // money: ISO 4217; empty = the default currency
	Values   []Choice   // list and multilist: the managed values, in order
}

type Choice struct {
	ID   string // stable, so renaming a value changes it everywhere
	Name string // 1-60 characters, unique within the field (case-insensitive)
}
```

- `Set` is the ordered list of definitions (at most 50), with:
  - `Add(d)`, `Update(d)` (name, kinds, unit, decimals, currency and values; never type or scope),
    `Move(id, index)`, `Remove(id)`;
  - `AddChoice(fieldID, name) (Choice, error)`: returns the existing choice when the name is
    already there (case-insensitive);
  - `Validate(id, value) (Value, error)`.
- Limits: 50 fields, 200 values per list, text 250 characters, long text 10,000 characters,
  numbers within ±1e12, durations up to 100,000 hours.

## Values

```go
// Value is one field's value; exactly the member of the field's type is set.
type Value struct {
	Text     string   // text, longtext
	Bool     *bool    // bool
	Number   *int64   // number, in hundredths when Decimals is 2
	Money    *game.Money
	Date     string   // "YYYY", "YYYY-MM" or "YYYY-MM-DD"
	Minutes  *int64   // duration
	Choice   string   // list: a Choice ID
	Choices  []string // multilist: Choice IDs, without repeats, in the field's order
}
```

- `game.Info` gains `Fields map[field.ID]field.Value`; `game.CopyDetails` gains the same map.
- An empty value (empty text, nil pointer, no choices) removes the key: a field without a value is
  absent, never stored as empty.
- Validation lives in the catalog use cases, which load the `Set`. A value that does not fit its
  field (another type, an unknown choice, out of the limits, a game field on a copy, a copy field
  on a copy of a kind it does not apply to) is a validation error that names the field.
  Values of unknown field ids are refused.
- Scans never read or write these maps (like notes): `applyImport` keeps them.
- Moving a copy keeps its values. Merging games keeps the kept game's values and fills the gaps
  from the other's, as with play status and rating.

## Storage

- Definitions: one JSON document in the `settings` table under the key `fields`
  (`{"v":1,"fields":[…]}`), read and written through a `field.Repository` port implemented by
  the SQLite settings repository.
- Values: `fields` objects inside the game document and each copy document
  (`{"<fieldId>": {"text": "…"}}`, only the member that is set), omitted when empty. No migration.

## Use cases (`internal/application/fields`)

- `List`, `Create`, `Update`, `Move`.
- `Delete(id)` removes the definition and, in one transaction, the field's values from every game
  and copy; it returns how many games and copies lost a value. `Usage(id)` gives the same counts
  beforehand, for the confirmation.
- `RemoveChoice(fieldID, choiceID, mergeInto string)`: in one transaction, removes the value and
  either clears it from every game and copy that uses it or replaces it with `mergeInto` (a
  multilist drops the duplicate it may create).
- `AddChoice` is called by the catalog when a value typed in a game or copy form is new
  (multilist "add new"); it returns the choice's id.

## API

- `FieldService`: `ListFields`, `CreateField`, `UpdateField`, `MoveField`, `DeleteField`,
  `FieldUsage`, `RemoveChoice`, `AddChoice`.
- `Game` and `CopyDetails` messages gain `map<string, FieldValue> fields`; `UpdateGame`,
  `CreateGame`, `AddCopy` and `UpdateCopy` accept them (validated as above).
- `FieldValue` mirrors `Value` (a `oneof`).

## Web

- **Fields page** (Settings, next to Sources, Providers, System and Logs; on phones in the
  Settings tab): the fields in order, reorderable (drag on desktop, up/down buttons everywhere),
  **Add field**. The field dialog: name, type, scope, copy kinds (copy fields), decimals and unit
  (number), currency (money), values (list, multilist: add, rename, reorder, remove with "merge
  into …" or "leave empty"). Type and scope are read-only once created. Delete asks with the
  usage counts.
- **Game sheet:** the overview shows a "More details" section with the fields that have a value,
  in order. Values are formatted by type: chips for multilists, "Yes", money and dates in the
  user's locale, "12 h 30 min", "450 g". The edit tab shows every game field with its control.
  The controls per type:
  - text input, textarea, switch;
  - number input with the unit beside it;
  - amount + currency (as the purchase price);
  - year / month / day selects, month and day optional;
  - hours + minutes inputs;
  - a select, or buttons when a list has 4 values or fewer;
  - a tag input with suggestions and "add new".

  They are saved with the rest of the edit.
- **Copy form** (add and edit): the copy fields that apply to the copy's kind, after the copy's own
  details. The copy card shows the copy fields with a value on one compact line.
- **Library:**
  - The filter panel gets a "Fields" section with one filter per List, Multi-list and Yes/No
    field. Lists offer their values plus "(empty)"; Yes/No offers Yes, No and "(no value)".
  - A filter on a copy field keeps games with at least one matching copy.
  - The search box also matches Text and Long text values of the game and its copies.
  - Filters combine with the existing ones and are remembered like them.
- The definitions are loaded once with the rest of the app data and reloaded after edits.
- Filtering and searching are pure functions (`web/src/lib/fields.ts`), tested in Node.
- Every text in `en.json` and `es.json`.

## Testing

- Go:
  - the domain: each type's validation and limits, name and choice uniqueness, the fixed type and
    scope, `AddChoice` reuse;
  - documents: round trip of definitions and of game and copy values;
  - use cases: delete removes values everywhere in one transaction with the counts, and
    `RemoveChoice` clears or merges (multilist duplicates dropped);
  - catalog: an invalid value refused with the field's name, merging games, scans keep values;
  - RPC end to end.
- Node: filtering (list, multilist, yes/no, empty, copy fields) and search.
- Browser, test server on a copy of the config, desktop and mobile:
  - create a field of each type, game and copy;
  - fill them in, see them in the sheet and on a copy card;
  - filter and search;
  - rename a list value, remove one with merge;
  - delete a field.

## Delivery

Branch `feature/custom-fields`, one PR to `main`, CI green before merging. `docs/technical.md`
(custom fields). No version is tagged until the maintainer asks.
