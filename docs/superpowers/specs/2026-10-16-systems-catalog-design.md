# Systems catalog: a hundred game systems, regions on copies

Status: design for review. Branch `feature/systems-catalog`.

## Why

Game Vault knows 20 systems: PC and 19 PlayStation, Xbox and Nintendo consoles. A collector of
physical games owns far more: the NES and the Mega Drive, the Game Boy family, the Neo Geo, home
computers like the ZX Spectrum or the Amiga, and new ones like the Switch 2. Today the list lives in
a Go table, is copied by hand into several web arrays, and every provider (IGDB, TheGamesDB,
Wikidata) and the barcode name parser keeps its own partial version, so growing it means touching a
dozen places and the parser already confuses "Switch 2" with "Switch" and knows no PS1, N64 or Game
Boy. Collectors also care about a copy's region (PAL, NTSC-U, NTSC-J), which the copy cannot say.

## Decisions

- **Scope:** consoles and handhelds of every generation plus classic home computers, about 110
  systems. Regional names (Mega Drive / Genesis, PC Engine / TurboGrafx-16, Famicom / NES) are one
  system each with aliases; the region is a field of the copy.
- **One catalog in the domain**, served by the API, with each system's maker, kind, year and aliases.
  The web stops keeping its own lists. Provider ids stay in each adapter's table, and tests require
  every system to be mapped or explicitly excluded by every provider.
- **Names:** the 20 stored system names never change. New systems use the maker's own model name,
  untranslated ("NES", "SNES", "Game Boy Advance", "Mega Drive / Genesis", "ZX Spectrum").
- **Selector:** a searchable picker grouped by maker, with the user's own systems on top.

## The catalog

`internal/domain/game/systems.go`, one entry per system:

| Field | Meaning |
| --- | --- |
| `Name` | the stored name; never renamed (pinned by `TestVocabulary_valuesNeverChange`) |
| `Maker` | Nintendo, Sony, Microsoft, Sega, Atari, NEC, SNK, Bandai, Commodore, Sinclair, Amstrad, Apple, Philips, Panasonic (3DO), Mattel, Coleco, Magnavox, Valve, Panic, Blaze, Analogue, Ouya, Acorn, Dragon Data, Sharp, Fujitsu, MSX, PC (for PC), Other |
| `Kind` | home console, handheld, computer; PC is its own |
| `Year` | first release year; within a maker, home consoles newest first, then handhelds, then computers |
| `Aliases` | regional names, abbreviations, spellings ("Genesis", "Mega Drive", "MD", "Sega Genesis"; "SNES", "Super Nintendo", "Super Famicom", "SFC"; "PlayStation 4", "PS 4") |
| `Modern` | of the PS4 generation or later (replaces Steam's `modernSystems` list for auto-linking) |
| `Short` | the badge label when it differs from the name ("MD", "GBA", "PCE") |

Matching a name or alias ignores case, spaces, hyphens and dots ("ps-4", "P.S. 4", "megadrive").
An alias belongs to one system only; ambiguous short forms ("MD", "GB", "GC", "PC Engine" vs
"PC-98") are aliases only where unique, and the barcode parser treats the short ones carefully
(below).

**Initial list** (about 110), grouped by maker:

- **Nintendo:** Switch 2, Switch, Wii U, Wii, GameCube, N64, SNES, NES (Famicom), Famicom Disk
  System; 3DS, DS, Game Boy Advance, Game Boy Color, Game Boy, Virtual Boy, Pokémon Mini,
  Game & Watch.
- **Sony:** PS5, PS4, PS3, PS2, PS1; PS Vita, PSP.
- **Microsoft:** Xbox Series, Xbox One, Xbox 360, Xbox.
- **Sega:** Dreamcast, Saturn, 32X, Mega-CD / Sega CD, Mega Drive / Genesis, Master System,
  SG-1000; Game Gear, Nomad, Pico.
- **Atari:** Jaguar, Jaguar CD, 7800, 5200, 2600; Lynx; Atari ST, Atari 8-bit.
- **NEC:** PC-FX, PC Engine CD / TurboGrafx-CD, PC Engine / TurboGrafx-16, SuperGrafx; PC-98,
  PC-88.
- **SNK:** Neo Geo CD, Neo Geo AES; Neo Geo Pocket Color, Neo Geo Pocket.
- **Bandai:** WonderSwan Color, WonderSwan.
- **Others (home):** 3DO, CD-i, Amiga CD32, ColecoVision, Intellivision, Vectrex, Odyssey 2,
  Channel F, Ouya, Evercade VS, Intellivision Amico.
- **Others (handheld):** Evercade, Playdate, Analogue Pocket, N-Gage, Gizmondo, Tiger Game.com.
- **Computers:** ZX Spectrum, ZX81, Amstrad CPC, Commodore 64, VIC-20, Amiga, Commodore 16/Plus4,
  MSX, MSX2, Apple II, Mac (classic), BBC Micro, Acorn Electron, Dragon 32/64, Sharp X68000,
  FM Towns.
- **PC:** PC, as today (Steam Deck is a PC; an alias, not a system).

The exact list is finalised in the plan against IGDB's and TheGamesDB's platform lists; any system
either both know or that collectors commonly list goes in.

## Region of a copy

`CopyDetails.Region`, one of: `PAL`, `NTSC-U`, `NTSC-J`, `NTSC-K`, `NTSC-C`, `Asia`, `Region free`,
or empty (not set). Stored in the copy document (additive field, no version bump), in the API
(`CopyDetails.region`), in the CSV (`region` column), editable on physical copies (also on keys and
library copies, where it is rarely used), filterable in the library ("Region"), and shown as a small
label on the copy ("PAL", "JP"). A barcode lookup may fill it when the provider knows it (CeX is a
PAL marketplace; an EAN's country prefix is not used, since it does not tell the region). The Scan
page remembers the last region chosen.

## Providers and name recognition

- **Each provider keeps its own table**, keyed by catalog names: IGDB platform ids, TheGamesDB
  platform names, Wikidata QIDs. A shared test helper (`game.CheckProviderCoverage`) fails when a
  catalog system has neither an entry nor an explicit exclusion in a provider's table, so adding a
  system forces a decision everywhere. IGDB results for a known platform map back to the catalog
  (an upcoming "Switch 2" release keeps its system instead of being dropped).
- **Name recognition** (`game.ParseProductName`, used by CeX, UPCitemdb, EAN-Search; also CSV import
  and hand-typed systems): the 17 hand-written regexes are replaced by matching the catalog's names
  and aliases as whole words, the longest first ("Switch 2" before "Switch", "Game Boy Advance"
  before "Game Boy", "3DS" before "DS"). Short ambiguous aliases (two or three letters: "MD", "GB",
  "GC", "DS") count only as a standalone token. Tested with real box and listing names.
- **Sources:** PlayStation's Sony code map gains PS1 and PS2 classics; other sources are unchanged.
- **SteamGridDB's title cleaning** builds its platform-word pattern from the catalog.
- **Steam auto-linking** reads `Modern` instead of its own list.
- **Unchanged:** eBay and CeX valuation, ITAD, PC stores, the CSV's columns other than `region`.

## API and UI

- A new `CatalogService.ListSystems` returns the catalog:
  name, maker, kind, year, aliases, short label. The web loads it once at start; the hand-kept web
  arrays and their parity test go away (store platforms stay as they are).
- **`SystemPicker`**, a searchable combobox replacing the datalists in the copy form, the new-game
  dialog and Scan:
  - without typing: "In your collection" (the user's systems by game count), then every system
    grouped by maker (home consoles newest first, handhelds, computers);
  - typing filters by name and alias, ignoring case, spaces and hyphens; a match by alias shows it
    ("SNES · Super Famicom");
  - keyboard (arrows, Enter, Escape) and screen-reader semantics of a combobox; a bottom sheet with
    the search on top on phones;
  - free text not in the catalog can still be used ("Use “…”"), as today.
- **Badges:** each system takes its maker's colour and its short label from the catalog; the four
  big families keep their icons; unknown systems keep the neutral badge.
- **Library filters:** only the systems the user has, grouped by maker when there are many; a new
  "Region" filter.
- Every string in en and es; system names are not translated.

## Compatibility

No stored name changes. A system typed by hand that is not in the catalog keeps working as free
text with its own edition; one that matches an alias is stored as the catalog's name from the next
save of that copy (existing copies are not rewritten in bulk).

## Testing

- Domain: stored names never change; aliases unique and non-overlapping; matching rules
  (case, spaces, hyphens, dots); ordering; `Modern`.
- Providers: coverage of every catalog system (mapped or excluded) for IGDB, TheGamesDB and
  Wikidata; IGDB maps platforms back.
- Name recognition: a table of real box and listing names, including the formerly broken cases
  ("Nintendo Switch 2 Edition", "PlayStation 1", "Game Boy Color", "Sega Mega Drive", "SNES PAL").
- Region: copy round trip, CSV, filter, Scan's remembered value.
- Web: picker search by alias, grouping, order, free text; Node tests for the pure helpers.
- Bogus-credential probes of IGDB and TheGamesDB for response shapes; the picker and the region
  filter checked on a test server, desktop and phone.

## Out of scope

Hardware variants as systems (PS4 Pro, New 3DS, Game Boy Pocket), collecting hardware and
accessories, per-region box art choice.

## Delivery

Branch `feature/systems-catalog`, one PR to `dev`. Docs: `docs/technical.md` (catalog, aliases,
region, picker), `docs/plugins.md` (provider coverage of the catalog), memory (`data-model.md`).
