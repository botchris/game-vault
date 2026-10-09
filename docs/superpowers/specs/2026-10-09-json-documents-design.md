# Store the aggregates as JSON documents in SQLite

Status: approved design, waiting for the implementation plan. Branch `feature/json-documents`.

## Why

Adding a field to a game, a copy, a source or a provider means a schema migration today: a new
column, a new `INSERT`/`SELECT` list, a new scan target. The next features (richer physical copies,
photos, market valuations) add many fields to copies, so the aggregates move to JSON documents:
adding or removing a field becomes a code change only, while SQLite keeps transactions, hot backups
(`VACUUM INTO`), the pure-Go build and indexes where queries need them.

Other local document databases were considered and rejected: bbolt and Badger have no queries or
indexes, CloverDB and Chai are small or immature projects without hot backups or multi-collection
transactions, and DuckDB, LiteDB, MongoDB, CouchDB and FerretDB need cgo or a separate server.

## Scope

In:

- `games`: one document per game, with its copies inside (the `copies` table goes away).
- `sources` and `providers`: one document each.

Out (unchanged):

- `users` and `sessions`: fixed, sensitive fields where columns and constraints help.
- `settings` and `game_details`: already JSON.
- The domain, the application services, the Connect API, the UI, the CSV format.
- Full-text search (FTS5): the library searches in the browser today; add it when a server-side
  search exists.

## Storage model

```sql
games(id TEXT PRIMARY KEY, doc TEXT NOT NULL)
sources(id TEXT PRIMARY KEY, doc TEXT NOT NULL)
providers(id TEXT PRIMARY KEY, doc TEXT NOT NULL)
```

Indexes on JSON expressions, only for what is queried:

| Index | Expression | Used by |
| --- | --- | --- |
| `games_title` | `json_extract(doc, '$.title') COLLATE NOCASE` | `GameRepository.List` order |
| `sources_name` | `json_extract(doc, '$.name') COLLATE NOCASE` | `SourceRepository.List` order |
| `providers_kind` | `json_extract(doc, '$.kind'), json_extract(doc, '$.priority')` | `ProviderRepository.List(kind)` |

Queries use exactly the indexed expressions so SQLite can use the indexes.

## Document format

- The documents are the sqlite adapter's own structs (`gameDoc`, `copyDoc`, `sourceDoc`,
  `providerDoc`) with JSON tags, mapped to and from the domain in the adapter. The domain knows
  nothing about JSON, and renaming a Go identifier never changes the stored format.
- Field names are lowerCamelCase and never change once released (like `ExternalID` prefixes).
- Every document has a format version, `"v": 1`.
- Adding a field needs no migration: older documents decode it as its zero value, unknown fields
  are ignored. Changing the meaning of a field bumps `v`; the adapter converts older versions when
  it reads them and writes the current one.
- Empty values are omitted (`omitempty`), so a library copy carries no physical-copy fields.
- Times are RFC 3339 with nanoseconds in UTC, as `formatTime` writes them today; dates stay
  `YYYY-MM-DD` strings.

Game document:

```json
{
  "v": 1,
  "title": "Hades",
  "links": {"steam": "1145360", "epic": "…"},
  "notes": "…",
  "coverUrl": "…",
  "createdAt": "2026-10-01T10:00:00.000000000Z",
  "updatedAt": "2026-10-09T18:30:00.000000000Z",
  "copies": [
    {
      "id": "019…",
      "kind": "physical",
      "platform": "PS4",
      "status": "owned",
      "origin": "…",
      "acquiredOn": "2021-07-11",
      "edition": "…",
      "condition": "…",
      "location": "…",
      "barcode": "5026555…",
      "notes": "…",
      "sourceId": "…",
      "externalId": "…",
      "createdAt": "…",
      "updatedAt": "…"
    }
  ]
}
```

Keys also have `key` and `redeemBy`. Copies keep the order the aggregate holds them in; the
migration writes them by `created_at, id`, the order `List` returns today.

Source document: `v`, `type`, `name`, `enabled`, `syncIntervalSeconds`, `settings` (object),
`lastSync` (the current sync report object, absent when never scanned), `createdAt`, `updatedAt`.

Provider document: `v`, `kind`, `enabled`, `priority`, `settings` (object), `updatedAt`.

## Migration `0009_documents.sql`

Pure SQL, inside the existing migration transaction:

1. `games`: add `doc`, fill it with `json_object(…)`, the copies with
   `json_group_array(json_object(…))` over `copies` ordered by `created_at, id`, and `links` as
   `json(links)`; then drop the `games_title` index and the old columns (`title`, `notes`,
   `cover_url`, `links`, `created_at`, `updated_at`), and drop `copies` with its indexes.
2. `sources` and `providers`: add `doc`, fill it, drop the old columns and the `providers_kind`
   index.
3. Create the three expression indexes.

Tables are altered in place instead of rebuilt: `game_details` references `games(id)` with
`ON DELETE CASCADE`, so dropping `games` would empty the details cache, and foreign keys cannot be
switched off inside the migration transaction. `copies.source_id ON DELETE SET NULL` is not needed:
deleting a source already removes or releases its copies through the domain
(`sync.Service.Delete` → `RemoveCopiesFromSource` / `ReleaseCopiesFromSource`).

Empty strings written by the migration are harmless (they decode as zero values) and disappear the
next time each document is saved.

## Backup before migrating

When `sqlite.Open` finds pending migrations on an existing database, it first writes a full copy
with `VACUUM INTO <backupDir>/pre-migration-<first pending version>.db` (for example
`pre-migration-0009.db`) and only then migrates. If that file exists already (a pre-migration copy
was restored and is being migrated again), the new copy gets a timestamp
(`pre-migration-0009-20261009-183000.db`) instead of blocking the start. `Open` receives the backup directory from the
caller (`cfg.BackupDir()`); a new, empty database is not backed up. A failed backup stops the start
with an error that names the path. The backups page lists every `.db` in that directory, so the
file shows up in System → Backups (where it can be downloaded) and is rotated with the scheduled
backups: it is a safety net for the upgrade, not an archive.

## Repositories

Same interfaces (`game.Repository`, `source.Repository`, `provider.Repository`), so nothing above
the adapter changes:

- `Save`: one `INSERT … ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`. The copy diffing of
  `GameRepository.Save` goes away.
- `Get` / `List`: select `id, doc`, decode, map to the domain with the existing `Rehydrate`
  constructors.
- `Delete`: unchanged (`DELETE … WHERE id = ?`; `game_details` still cascades).
- A document that does not decode is an error naming the table and id; it is never skipped
  silently.

## Testing

- Repository round trips: save, read back, compare the aggregate field by field (a game with one
  copy of each kind, links, notes, a custom cover; a source with settings and a sync report; a
  provider).
- An older document without some fields decodes with zero values; unknown fields are ignored.
- Migration: a database migrated up to `0008`, filled with rows of every shape (copies of the three
  kinds, a copy from a source, links, a source with a report, configured providers, a cached
  details row), then migrated to `0009`. Everything reads back as before through the repositories,
  and the details row is still there.
- Backup: opening a database with a pending migration writes `pre-migration-0009.db`; a new
  database writes none.
- With the user's data, on copies of `config/gamevault.db` only: the `ListGames`, `ListSources`
  and `ListProviders` answers of the current server and of the new one are identical, the
  `pre-migration` file exists, and the library and a game sheet look the same in the browser.
  `List` time is measured on both.

## Delivery

- Branch `feature/json-documents`, one pull request to `main`; CI must pass before merging.
- `docs/technical.md` (persistence) and `.claude/memory/data-model.md` updated; this spec ships in
  the same pull request.
- No version is tagged until the user asks.

## Next

Part 1 of the physical-copy work (grade and contents, purchase price with `Money`, default currency
setting, form and sheet) is designed on top of this and only adds fields to `copyDoc`.
