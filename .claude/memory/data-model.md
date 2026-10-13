---
name: data-model
description: Game owns Copies; how imported copies are consolidated and why ExternalID formats must never change
metadata:
  type: project
---
- A **Game** owns **Copies** (kind key / library / physical). Sources import copies with a stable
  `ExternalID`; the consolidator matches by ExternalID, then any shared store link
  (`Game.Links`, `{"steam": "620"}`; imports fill stores the game has no link to and replace automatic links), then
  `game.MatchKey(title)`. No store is special in the domain: Steam's AppID became the `steam`
  link on 2026-10-09 (migration 0007, CSV column `links`, no backwards compatibility: the old export's column
  names are reported as unknown). `MatchKey` drops "<store> key" for every store, never a bare
  store name ("Epic Mickey", "Steam Prison").
- **Changing an ExternalID format duplicates copies on the next scan**: don't, or set
  `ImportedCopy.PreviousExternalID` so the consolidator adopts the old copy (only when it is in the
  same game; source details are cleared, user notes kept). A source drops a copy it imported
  earlier by returning it `Withdrawn`; games left empty are deleted by the sync.
- Prefixes: `steam:<appId>`, `humble:<gamekey>:<machine_name>:<keyindex>` (was `humble:<gamekey>:<keyindex>`, see [[humble]]), `epic:<catalogItemId>`,
  `gog:<productId>`, `battlenet:<titleId>`, `ea:<productId>`, `ubisoft:<spaceId>`,
  `xbox:<productId>`, `psn:<entitlementId>`, `amazon:<productId>`, `fanatical:<keyId>`. Each library source also links the
  game to its store with the same id (`epic`, `gog`, `battlenet`, `ea`, `ubisoft`, `xbox`, `psn`,
  `amazon`, `steam`; migration 0008 filled existing games), and cover providers read those links,
  never the copies' ids. See [[provider-chains]].
- Platform names are shared between sources so Humble keys are flagged as redundant when the
  game is already in that store: "Steam", "Epic Games", "GOG", "Battle.net", "EA App",
  "Ubisoft Connect", "Microsoft Store / Xbox"; PlayStation copies use "PS4"/"PS5".
  Since 2026-10-13 (audit PR 4) every platform, system and link key is a constant in
  `internal/domain/game/platforms.go`, built into one table (name, system, link key);
  `TestVocabulary_valuesNeverChange` pins the strings and a cmd/gamevault test rejects literals in
  adapters (allowlist `rawVocabulary` for external API values). The EA source's `Origin` stays
  "EA app" (lowercase, as stored) while its platform is "EA App".
- Battle.net and EA items without a store id get `<prefix>:` + `game.TitleIDKey(title)`, and CSV
  rows without `externalId` get `csv:<kind>|<platform lowercased>|<TitleIDKey(title)>|<key>`.
  `TitleIDKey` is a frozen copy of `MatchKey` as of 2026-10-13 (pinned by tests): never `MatchKey`,
  whose changes would re-key those copies.
- **Storage (since migration 0009, 2026-10-09):** games (copies inside), sources and providers are
  JSON documents `(id, doc)`; the adapter's `gameDoc`/`copyDoc`/`sourceDoc`/`providerDoc` field
  names never change once released (like ExternalID prefixes). Since 2026-10-13 (audit PR 6) the
  details cache has its stored form too (`detailsDoc`, Go field names as tags), `testdata/` holds a
  fixture per format that must round-trip byte for byte, and aggregates are rebuilt from one
  `game.Snapshot` / `source.Snapshot` (the v2 cover is its `LegacyCover`). A new field needs no migration;
  changing a field's meaning bumps `"v"`. Opening an older database writes
  `backups/pre-migration-NNNN.db` first.
- **Copies (2026-10-09, part 1 of the physical-copy work):** `Grade`, `Contents` (a bit set, so
  `CopyDetails` stays comparable), `Price Money` (minor units of the currency, `CurrencyDigits` for
  JPY/BHD…). Game docs are v2 (old `condition` converted on read). Default currency in
  `settings.Preferences`. CSV is English only by the user's decision. Next: part 2 photos, part 3
  second-hand valuation (CeX), each with its own spec.
- **Photos (2026-10-09, part 2):** `Copy.Photos` (`PhotoID` = SHA-256 of the stored JPEG, never
  renamed: it is the file name in `config/photos` and in the backups' shared store) and
  `Info.CoverPhoto`; document fields `photos` / `coverPhoto`, game docs still v2. Backups list
  their photos in `.photos` files; prune by reference, one-day grace. Metadata (GPS included) is
  kept on purpose (user's decision). Next: part 3 second-hand valuation (CeX).
- **Prices (2026-10-09, part 3):** physical copies with a barcode have `Estimates` (latest per price
  provider, `provider.KindValuation`) and `NextValuation`; document fields `estimates` /
  `nextValuation`, game docs still v2. Cleared when the barcode or kind changes. Next possible
  source: PriceCharting (official, paid, sold prices, PAL sections, USD).
- **Editions (2026-10-10):** the effective system of a copy is `CopyDetails.System` (the user's
  override, kept by scans), else `Copy.SourceSystem` (`ImportedCopy.System`; PC store sources set
  `game.SystemPC`, PlayStation PS4/PS5), else `SystemOf(platform)`. Editions are derived, one per
  system; the game stores `covers` by system and `mainSystem`. A game without copies keeps its
  chosen cover under the system `""`. Game docs are v3 (`covers`, `mainSystem`, copies' `system`
  and `sourceSystem`); v2 is converted on read (photo to its edition, which becomes the main one;
  URL to the default main edition). Migration 0010 only triggers the pre-migration backup. System
  names (`PC`, console names, `Other`) are stored in documents and cover file names: never rename
  them. The CSV `system` column is an override only when it differs from the copy's effective
  system; an import never clears one. See [[provider-chains]].
- 2026-10-11: `Game.BorrowsPCBox()` (copies, no PC copy, a PC-store link) adds a PC entry to
  `CoverFingerprints` and `editionSystems` without a real edition; `Edition(PC)` stays false.
- 2026-10-11: automatic links (`Game.AddAutoLinks`, from `media.LinkFinder`: IGDB then Steam's title
  search) are marked (`AutoLinks`), rejected when the user removes them (`RejectedLinks`, key+id),
  replaced by imports, and excluded from the consolidator's link matching. Additive doc fields
  (`autoLinks`, `rejectedLinks`, `autoLinkTriedAt`): an older binary reads the game but drops them
  on its next save (automatic links become plain links or vanish with it), so a downgrade loses the
  marks and the rejections. CSV column `autoLinks`; `UpdateGameRequest.links_set` guards links.
- 2026-10-13: removing the backward compatibility in two steps. PR A (branch
  `feature/upgrade-stored-data`) adds a one-time start-up upgrade (`internal/application/upgrade`)
  that rewrites game docs v<3, the old `localBypass` auth row and the pre-edition `cover.<ext>` /
  `cover.missing` files into today's format. PR B deletes the legacy readers (`LegacyCover`,
  `condition`, `localBypass`, `AdoptLegacyCover`…) together with the upgrade, once the maintainer's
  database has been through it.
