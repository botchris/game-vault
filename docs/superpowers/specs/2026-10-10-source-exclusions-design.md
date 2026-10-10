# Removing imported items for good

Status: approved design, waiting for the implementation plan. Branch `feature/source-exclusions`.

## Why

Sources import whatever the store lists, and some of it is not wanted in the catalog. A
PlayStation library, for example, lists next to the games:

- streaming and video apps;
- demos, betas and tech tests, some of them listed under the full game's name;
- soundtracks, making-of videos and digital art books.

Nothing Sony returns tells them apart: every item says "PS4" and has a `CUSA…` product id.
Deleting an imported copy today is useless, because the next sync brings it back.

## Decisions

- **The person decides; nothing is guessed.** A per-source exclusion list replaces filters based on
  names or ids, which are unreliable (other languages, demos listed under the full game's name).
- **Generic: "Remove and do not import again".** It applies to anything unwanted, game or not, and
  to every source, not only PlayStation.
- **PlayStation Store covers are dropped.** PS games already get covers from Steam or TheGamesDB,
  and PlayStation's website terms (section 10) forbid automated access, so store pages
  cannot be used. The open-threads memory is updated.

## What the user sees

- **The trash button on a copy a source imported** opens a new confirmation: "Remove «Netflix»
  from PlayStation and do not import it again". It explains that the next sync skips it and that
  it can be undone from the source's settings. When the copy has photos or saved price estimates,
  the confirmation says they will be deleted.
  - Confirming removes the copy. If the game is left without copies, it is removed too, as when a
    store withdraws an item.
  - Copies the user added by hand are deleted exactly as today.
- **The source dialog** gets a section "Removed from this source (N)", listing each item's title
  and the date it was removed. Each has an **Import again** button, which takes it off the list;
  the next sync brings it back. The section is hidden while the list is empty.

## Domain (`internal/domain/source`)

```go
// Exclusion is an item the user removed from a source's imports.
type Exclusion struct {
	ExternalID string    // the copy's id at the source, e.g. "psn:EP4350-CUSA00127_00-NETFLIXPOLLUX001"
	Title      string    // the title it had, to list it
	At         time.Time // when it was removed
}
```

- `Source` gains `exclusions []Exclusion`, sorted by `At` (newest first), with these methods:
  - `Exclusions() []Exclusion` (a copy);
  - `Exclude(Exclusion)`: excluding an id already listed keeps the first entry;
  - `Include(externalID string) bool`: false when the id was not listed;
  - `Excludes(externalID string) bool`.
- An exclusion without an `ExternalID` is a validation error.
- Stored in the source's JSON document as `exclusions` (array of `{externalId, title, at}`),
  omitted when empty; no migration. A deleted source takes its list with it.

## Sync (`internal/application/sync`)

- Before consolidating, every imported copy whose `ExternalID` or `PreviousExternalID` is excluded
  is turned into a withdrawn copy (`Withdrawn: true`). The existing withdraw path then removes an
  old copy of it (and its game, if it is left empty) and never adds a new one.
- The sync report gains `Excluded int` (shown in the source's last-sync line as "N removed by
  you"). It counts the imported items skipped this way.

### Use cases

- `ExcludeCopy(ctx, gameID, copyID) (*game.Game, error)`, in one transaction:
  1. load the game and the copy; a copy without a `SourceID` or `ExternalID` is refused with
     `ErrNotImported` ("this copy was added by hand: delete it instead");
  2. load its source and add `Exclusion{ExternalID, Title: game title, At: now}`; a source that
     no longer exists is `source.ErrNotFound`;
  3. remove the copy, and the game if it has no copies left;
  4. return the updated game, or nil when it was deleted.
- `IncludeCopy(ctx, sourceID, externalID) error` takes the id off the list (`ErrNotExcluded` when
  it was not listed). Nothing is imported until the next sync.
- Both live in the sync service (it already owns sources and the catalog consolidation). They run
  in the same transaction manager as scans, so excluding during a sync is safe: at worst that sync
  brings the copy back and the next one removes it.

## API

- `GameService.ExcludeCopy(game_id, copy_id) → { Game game }` (`game` absent when the game was
  deleted). A copy added by hand gives `FailedPrecondition`.
- `SourceService.IncludeCopy(source_id, external_id) → {}`.
- `Source` gains `repeated Exclusion exclusions` (`external_id`, `title`, `at`).
- `SyncReport` gains `int32 excluded`.

## Web

- `GameDetail.tsx`: the trash button of a copy with a `sourceId` asks the exclusion question and
  calls `ExcludeCopy`. The page then updates the game or drops it (as when the game is deleted).
  Manual copies keep today's delete.
- `SourceDialog.tsx`: the "Removed from this source" section with **Import again** (`IncludeCopy`,
  then reload the source).
- The sources list's last-sync line mentions `excluded` when it is not zero.
- Every text in `en.json` and `es.json`.

## Testing

- Domain: exclude, include, no duplicates, invalid empty id, document round trip.
- Sync:
  - an excluded item is not imported;
  - an existing copy of an excluded item is removed, with its game when empty;
  - a `PreviousExternalID` match counts;
  - the report counts the excluded items.
- Use cases:
  - a manual copy is refused;
  - the copy is removed and the id is listed, in one transaction;
  - an emptied game is deleted;
  - include takes the id off and the next sync imports it.
- RPC end to end: `ExcludeCopy` and `IncludeCopy`.
- Browser, test server on a copy of the config, desktop and mobile:
  - remove a Humble or Steam copy (the only sources safe to sync on a copy);
  - sync and check it does not come back;
  - import it again from the dialog;
  - sync and check it is back.
- On a main server (PlayStation cannot be synced on a config copy): remove some apps, demos and
  soundtracks from a PlayStation library.

## Delivery

Branch `feature/source-exclusions`, one PR to `main`, CI green before merging. `docs/technical.md`
(sources and sync), the open-threads memory (PS Store covers dropped, with the reason). No version
is tagged until the user asks.
