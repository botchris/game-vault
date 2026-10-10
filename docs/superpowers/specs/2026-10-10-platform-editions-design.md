# Platform editions

Status: approved design, waiting for the implementation plan. Branch `feature/platform-editions`.

## Why

A game owns copies on several platforms, but it has a single cover. When the same game is owned on
PS3 and Xbox 360, the library card can only show one box, so it misrepresents at least one of the
copies. The game aggregate mixes two things:

- the **work** (synopsis, developer, release, play status, rating, notes), the same on every
  platform;
- the **edition on a system** (its box art), different per system.

Removing the game and listing every copy on its own would fix the cover but lose what works today.
Store consolidation would break: one PC game in Steam, GOG and a Humble key would become three
identical cards. Redundant-key detection, play status, rating and game-level custom fields also
depend on the game.

The model gains a middle level instead, like Discogs' master and release:

**Game** (the work) → **Edition** (one per system, with its own cover) → **Copies**.

## Decisions

- **The system is derived from the copy's platform and can be corrected per copy.** The copy's
  platform keeps saying where it lives (Steam, Humble key, PS4 disc…). A domain table maps it to
  the system it is played on (PC, PS4, Switch…). A source may state the system when it knows it.
  The user can override it on the copy, and scans never touch the override.
- **An edition holds only its cover.** Play status, rating, notes and custom fields stay on the
  game or the copy. Per-edition status or an "edition" field scope can come later without reworking
  this.
- **Missing box art borrows another edition's cover, marked.** A PS4 edition without its own box
  shows, for example, the PC cover with a small "PC box" label. The edition's sheet says so and
  offers to choose another or use a photo.
- **"By game" view shows a main edition.** It is chosen by the user, or by default:
  1. an edition with a physical copy;
  2. then the one with the most copies;
  3. then alphabetical by system.

## Domain (`internal/domain/game`)

### System of a copy

- `SystemOf(platform string) string` maps a platform to its system:
  - **PC stores and launchers** become `PC`: Steam, Epic Games, GOG, EA App, Ubisoft Connect,
    Battle.net, itch.io, Amazon Games, Rockstar, Riot, "Battlestate (Tarkov)" and "PC" itself.
  - **Console names** are their own system: PS5, PS4, PS3, PS2, PS1, PSP, PS Vita, Xbox Series,
    Xbox One, Xbox 360, Xbox, Switch, Wii U, Wii, GameCube, N64, 3DS, DS, Game Boy.
  - **Ambiguous stores** take their most likely system: "PlayStation Store" → PS4,
    "Nintendo eShop" → Switch, "Microsoft Store / Xbox" → PC.
  - **Other values:** an unknown platform is its own system (canonical casing, trimmed); an empty
    platform is `Other`.
- `CopyDetails` gains `System string`, the user's override (empty = derived). It is kept by scans
  like the copy's notes.
- `ImportedCopy` gains an optional `System`. A source that knows the exact system sets it, and it
  wins over the derived one but not over the user's override. The PlayStation source sets PS4/PS5.
- `(Copy).System() string` returns the effective system, in this order:
  1. the user's override;
  2. the source's system;
  3. `SystemOf(platform)`.

### Editions

- Editions are derived: `(*Game).Editions() []Edition` has one entry per effective system of the
  game's copies, in a stable order (main edition first, then alphabetical by system).
  - `Edition{System string; Cover EditionCover; Main bool}`;
  - `EditionCover{URL string; Photo PhotoID}`, empty when the providers choose.
- The game stores:
  - `Covers map[string]EditionCover`: chosen covers, keyed by system;
  - `MainSystem string`: empty means the default rule.
- A game without copies keeps its chosen cover under the empty system `""` (served at the main
  address). When its first edition appears, that cover moves to the edition if it has no cover of
  its own, else it is dropped. `SetEditionCover("")` is valid only for a game without copies.
- An entry in `Covers` whose system has no copy left is dropped, the way `dropOrphanCover` drops a
  photo cover today. A photo cover must belong to a copy of that system. Moving a copy to another
  system (an override) drops a photo cover that no longer fits. `MainSystem` is cleared when its
  edition disappears.
- Methods:
  - `SetEditionCover(system, cover, now) error`: an unknown system is a validation error, and so
    is a photo from another system's copy;
  - `SetMainSystem(system, now) error`;
  - `MainEdition() Edition`, which applies the default rule when `MainSystem` is empty:
    1. an edition with a physical copy;
    2. then the most copies;
    3. then alphabetical.
- **Merging games:** editions follow from the union of the copies. Chosen covers and the main
  system of the kept game win; the other game's fill the gaps.
- **Unchanged:**
  - consolidation (ExternalID, store links, `MatchKey`);
  - redundant keys, which still compare the copy's platform;
  - play status, rating, notes, custom fields.

### Storage and migration

- Game documents become `v: 3`:
  - `covers` (`{"PS3": {"url": "…"}}` or `{"photo": "…"}`) and `mainSystem` replace `coverUrl` and
    `coverPhoto`;
  - copies gain `system` (the user's override) and `sourceSystem`;
  - empty values are omitted.
- Reading a `v: 2` document converts it:
  - **`coverPhoto`** goes to the edition of the system of the copy that holds the photo;
  - **`coverUrl`** goes to the main edition by the default rule, and that system is stored as
    `mainSystem`, so the "by game" view looks exactly as before;
  - when both exist, the photo wins for the main edition, as today. When the photo's edition is
    another one, the photo's system becomes the main system (the by-game view is unchanged) and the
    URL goes to the edition the default rule would have chosen.
- No identifier changes: ExternalIDs, PhotoIDs and game ids stay as they are. Opening an older
  database writes the usual pre-migration backup.

## Covers (`internal/application/media`)

- **Queries:**
  - covers are resolved per (game, system);
  - `CoverQuery` gains `System`, and `Platforms` holds the platforms of that edition's copies;
  - `QueryFor(g, system)` builds it.
- **Which providers apply,** each provider's `Applies` deciding:
  - store providers (Steam, GOG, Epic, EA, Battle.net, Ubisoft, Amazon) apply only to `PC`;
  - Xbox applies to `PC` and the Xbox systems;
  - TheGamesDB applies to any system and searches that system's box art.

  Its quota rules stay (physical copies, no store link, the fallback pass), counted per edition.
- **Resolution order** for an edition:
  1. its chosen photo;
  2. its chosen URL;
  3. the provider chain with the fallback pass;
  4. the base game's cover for the same system (add-ons);
  5. otherwise "missing" for that edition.
- **Storage:** the cover cache keys images and "missing" markers by game and system.
  `coverLogicChanged` is bumped, so every old miss is retried.
- **Serving:**
  - `GET /media/covers/{gameId}/{system}` (URL-escaped system) serves an edition's cover;
  - `GET /media/covers/{gameId}` serves the main edition's;
  - a miss is a 404 with `no-store`, as today.
- **Choosing a cover:** the candidates for "Choose cover…" are searched with the edition's system,
  and the offered photos are those of that edition's copies.
- **Invalidation:** a sync invalidates the cached covers of the editions whose copies changed.

## API

- **Copies:** `CopyDetails` gains `system` (the user's override). `Copy` gains `effective_system`
  (read only).
- **Games:** `Game` gains `repeated Edition editions` (system, chosen cover URL or photo id, main)
  and `main_system`. `cover_url` and `cover_photo` are removed: the web is the only client and
  ships with the server.
- **Requests:**
  - `SetEditionCover(game_id, system, url | photo_id | clear)` replaces the game-level cover
    request;
  - `SetMainEdition(game_id, system)` is new;
  - the cover-candidates request takes `system`.
- **Errors:** an unknown system or a photo from another system is `InvalidArgument` with a message
  that names the system.

## Web

### Library

- **View selector** "By platform | By game", next to Covers / List. It is remembered in the
  browser (`localStorage`, wrapped in try/catch); the default is "By platform".
- **By platform:** one card or row per edition, with:
  - the edition's cover and the game title;
  - the system badge;
  - on PC, the store badges, small;
  - the pending or redundant badges counted from that edition's copies.

  The header counts both, e.g. "320 editions of 300 games".
- **By game:** as today, with the main edition's cover and the badges of all the game's systems.
- **Filters:**
  - a new **System** filter (PC, PS4, Switch…), with options counted by editions in the
    platform view and by games in the game view;
  - **Platform** stays (Steam, Humble…);
  - in the platform view, copy-level filters (kind, platform, source, copy custom fields, the
    redundant-keys quick filter) keep an edition when one of its copies matches;
  - game-level filters (play status, rating, genres, game custom fields, search) apply to all of
    a game's editions.
- **Sorting:** the same sorts as today. Ties are broken by system, and the copies sort counts the
  edition's copies.
- **Missing box art:** when an edition's cover URL answers 404, the card tries the main edition's
  cover, then the other editions'. It labels the borrowed image ("PC box", translated), and the
  edition's own system badge stays prominent.

### Game sheet

- It opens on the edition that was clicked, through component state (the app has no per-game route,
  so there is no `?system=` parameter); a link opens the main edition.
- The header shows the current edition's cover and system. When there are two or more editions,
  chips switch between them (`PC · PS3 · Xbox 360`).
- When the cover is borrowed, the header says so, with "Choose cover…" and "Use a photo" at hand.
- The copies tab groups copies by system, the current edition first.
- Edition actions:
  - **Choose cover…**, for this edition;
  - **Use as the game's cover**, shown when this edition is not the main one.
- Copy form: a **System** field. It shows the derived system as a placeholder, accepts another
  value from the system list or free text, and goes back to automatic when emptied.

### Strings and docs

- Every text in `en.json` and `es.json`.
- Filtering and grouping by edition are pure functions in `web/src/lib/editions.ts`, tested in Node.

## CSV

- A new column `system`. Export writes the effective system.
- Import stores the value as the copy's override only when it differs from the system derived from
  the row's platform. An empty value means automatic.

## Testing

- **Go, domain:**
  - `SystemOf` for every known platform, the ambiguous stores, unknown and empty values;
  - effective system precedence (override, source, derived);
  - editions and their order;
  - the main edition's default rule and an explicit choice;
  - orphan covers and an orphan main system dropped;
  - an override that moves a copy away from its photo cover;
  - merging games;
  - the v2→v3 conversion for each case (URL only, photo only, both on the same or on different
    editions).
- **Go, media:**
  - each provider's `Applies` by system;
  - per-edition cache and missing markers;
  - both cover endpoints;
  - add-ons borrowing the base game's cover for the same system.
- **Go, sync:**
  - a source system is kept and an override survives a scan;
  - changed editions are invalidated.
- **Go, RPC:** `SetEditionCover`, `SetMainEdition` and the errors, end to end.
- **Go, CSV:** round trip of `system`, and no override stored when it equals the derived one.
- **Node:** grouping into editions, filters in both views, the System filter, edition counts and
  the fallback order for covers.
- **Browser,** on a test server with a copy of the config, desktop and mobile:
  - both views;
  - a game with editions on two consoles, each with its own box;
  - a borrowed cover with its label;
  - switching editions in the sheet;
  - choosing an edition's cover;
  - changing the main edition;
  - a system override on a copy;
  - the System filter.

## Delivery

- Branch `feature/platform-editions`, one PR to `main`, CI green before merging.
- `docs/technical.md`: model, covers, library and CSV.
- `README.md`: one line, since every platform now shows its own box.
- The data-model and provider-chains memories are updated.
- No version is tagged until the maintainer asks.
