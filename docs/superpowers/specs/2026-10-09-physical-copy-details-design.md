# Richer physical copies: grade, contents and purchase price

Status: approved design, waiting for the implementation plan. Branch `feature/physical-copy-details`.
Part 1 of the physical-copy work; part 2 (photos) and part 3 (second-hand valuation) get their own
specs. Builds on the JSON document storage (`2026-10-09-json-documents-design.md`).

## Why

Collectors describe a physical copy the way CLZ does: how good it looks, what is in the box, where it
is kept, and what they paid. Today a copy has a free-text `condition` (with five suggestions),
`location`, `acquiredOn` and `edition`. This part makes condition structured, adds the purchase price
and a default currency, and suggests the locations already in use.

## Scope

In:

- Domain: `Grade`, `Contents`, `Money`; `CopyDetails.Condition` replaced by `Grade` + `Contents`;
  `CopyDetails.Price`.
- A default-currency preference.
- Storage: new copy-document fields, document version 2 with the conversion of the old `condition`.
- API (`proto`), CSV (English only), the copy form, the copy line in the game sheet, a Preferences
  section in System.

Out: library filters and totals, photos, valuations, prices filled in by sources.

## Domain (`internal/domain/game`)

### Grade

```go
type Grade string

const (
	GradeSealed     Grade = "sealed"
	GradeMint       Grade = "mint"
	GradeVeryGood   Grade = "very_good"
	GradeGood       Grade = "good"
	GradeAcceptable Grade = "acceptable"
	GradeDamaged    Grade = "damaged"
)
```

The empty grade means "not stated". Any other value is a validation error.

### Contents

```go
type Content string

const (
	ContentBox    Content = "box"
	ContentManual Content = "manual"
	ContentMedia  Content = "media"  // the disc or cartridge
	ContentExtras Content = "extras" // map, poster, figure, art book…
)

type Contents []Content
```

Normalized as a set: no duplicates, always in the order box, manual, media, extras, so two copies
with the same contents compare equal. Unknown values are a validation error. Empty means "not stated".

### Money

```go
// Money is an amount in minor units (cents) of an ISO 4217 currency. The zero value means no price.
type Money struct {
	Amount   int64  // 2995 for 29.95
	Currency string // "EUR"
}
```

Normalization: the currency is trimmed and upper-cased. With `Amount == 0` the currency is cleared
(no price). With `Amount > 0` the currency is required and must be three letters A–Z. A negative
amount is a validation error. Game Vault does not convert between currencies.

### CopyDetails

- `Condition string` is removed; `Grade Grade` and `Contents Contents` replace it.
- `Price Money` is added.
- `Grade`, `Contents` and `Location` are physical-only: `normalize` clears them for other kinds, as it
  already clears `Barcode`. `Price` applies to every kind.
- `applyImport` (scans): sources never report grade, contents or price, and an import never clears
  them (an empty imported value keeps what the user set), like `Location` and `Notes` today.

### Default currency

`settings.Preferences{Currency string}` in `internal/domain/settings`, validated like
`Money.Currency` (empty allowed: "not chosen"). `settings.Repository` gains
`Preferences(ctx) (Preferences, error)` and `SavePreferences(ctx, Preferences) error`.

## Storage (`internal/adapters/outbound/sqlite`)

- `copyDoc` gains `grade`, `contents` (array of strings), `priceAmount` (integer) and
  `priceCurrency`, all omitted when empty, and loses `condition`.
- `docVersion` becomes 2. Version-1 game documents are converted when read: each copy's `condition`
  is mapped, case-insensitive and trimmed, from the five texts the form used to suggest, in English or
  Spanish (the language of the UI when it was picked):

  | Stored text | Grade | Contents |
  | --- | --- | --- |
  | `Sealed`, `Precintado` | sealed | box, manual, media |
  | `Complete (case + manual)`, `Completo (caja + manual)` | — | box, manual, media |
  | `Case and disc`, `Caja y disco` | — | box, media |
  | `Disc only`, `Sólo disco` | — | media |
  | `Damaged`, `Dañado` | damaged | — |
  | any other non-empty text | — | — and `Condition: <text>` is appended to the notes |

  The converted document is written as version 2 the next time the game is saved; the database is
  not rewritten. Sources and providers stay at version 1 (`checkVersion` is per document kind).
- The preference is stored in the existing `settings` table (key `preferences`, JSON value), like
  the logging settings.

## API (`proto/gamevault/v1`)

- `CopyDetails`: field 9 (`condition`) becomes `reserved 9; reserved "condition";`. New:

  ```proto
  enum CopyGrade {
    COPY_GRADE_UNSPECIFIED = 0;
    COPY_GRADE_SEALED = 1;
    COPY_GRADE_MINT = 2;
    COPY_GRADE_VERY_GOOD = 3;
    COPY_GRADE_GOOD = 4;
    COPY_GRADE_ACCEPTABLE = 5;
    COPY_GRADE_DAMAGED = 6;
  }

  enum CopyContent {
    COPY_CONTENT_UNSPECIFIED = 0;
    COPY_CONTENT_BOX = 1;
    COPY_CONTENT_MANUAL = 2;
    COPY_CONTENT_MEDIA = 3;
    COPY_CONTENT_EXTRAS = 4;
  }

  // Money is an amount in minor units (cents) of an ISO 4217 currency; empty when there is no price.
  message Money {
    int64 amount_minor = 1;
    string currency = 2;
  }
  ```

  and in `CopyDetails`: `CopyGrade grade = 13; repeated CopyContent contents = 14; Money price = 15;`.
- `SystemService` gains `GetPreferences` and `UpdatePreferences`, with
  `message Preferences { string currency = 1; }` (empty: never chosen).

## CSV (`internal/adapters/outbound/csvfile`)

English only, no backwards compatibility:

- The `condition` column goes (a file that has it gets the usual "not a Game Vault column" warning).
- New columns: `grade` (`sealed`, `mint`, `very_good`, `good`, `acceptable`, `damaged`), `contents`
  (space-separated: `box manual media extras`), `price` (decimal with a dot or a comma: `29.95`,
  `29,95`; at most two decimals) and `currency` (three letters).
- Every Spanish header alias (`titulo`, `juego`, `nombre`, `plataforma`, `tipo`, `estado`, `clave`,
  `fechalimite`, `caducidad`, `origen`, `tienda`, `fechacompra`, `edicion`, `condicion`,
  `estadofisico`, `ubicacion`, `notas`, `enlaces`, `vinculos`, `codigobarras`, `codigo`) and every
  Spanish kind value (`clave`, `biblioteca`, `fisico`, `físico`, `disco`) is removed. English aliases
  stay. The import help text in the UI no longer mentions Spanish names.
- Invalid values (grade, content, price, currency) are a warning for that row and are ignored; the
  row is still imported.
- A row with a price and no currency gets the default currency. The codec leaves the currency empty;
  `transfer.Service.Import` fills it from the preference before consolidating (and warns once when no
  default currency is set, dropping those prices).
- Export writes the four columns; the price with a dot and two decimals (`29.95`).

## Web UI

- **Copy form:**
  - Grade (physical only): a select with "Not stated" and the six grades, translated.
  - Contents (physical only): four toggle buttons: Box, Manual, Disc/cartridge, Extras.
  - Location (physical only): the same text field with a `datalist` of the distinct locations already
    used, taken from the games the app has loaded.
  - Purchase price (every kind): an amount field typed in the UI language's format (`29,95` in Spanish,
    `29.95` in English) and a currency select defaulting to the preference. An amount that is not a
    number marks the field invalid and blocks saving.
- **Copy line in the game sheet:** only what is set, separated by `·`: grade, contents, price
  (formatted with `Intl.NumberFormat` in the UI language and the copy's currency), location. For
  example "Very good · Box, manual, disc · €29.95 · Living room shelf".
- **System → Preferences:** a default-currency select listing `Intl.supportedValuesOf('currency')`,
  with the currency of the browser's region first. While nothing is saved that region's currency is
  preselected (`es-ES` → EUR, `en-US` → USD, `en-GB` → GBP); saving stores it.
- Every new text goes through `t()` in `en.json` and `es.json`; the old `copy.condition*` keys go.

## Testing

- Domain: `Grade`, `Contents` and `Money` validation and normalization; physical-only fields cleared
  on other kinds; an import keeping the user's grade, contents and price.
- Storage: copy-document round trip with the new fields; a version-1 document with each of the
  ten condition texts (and an unknown one) converted as in the table; written back as version 2.
- Preferences: repository round trip; RPC get and update; an invalid currency refused.
- CSV: the new columns in, out and round trip; a comma decimal; invalid values warned; a missing
  currency filled from the preference; Spanish headers now reported as unknown columns.
- Connect end-to-end: create a physical copy with grade, contents and price, read it back.
- UI: TypeScript types; the test server on a copy of the user's data (the old conditions shown
  converted), desktop and mobile.

## Delivery

Branch `feature/physical-copy-details`, one PR to `main`, CI green before merging. `docs/technical.md`
and the data-model memory updated. No version is tagged until the user asks.
