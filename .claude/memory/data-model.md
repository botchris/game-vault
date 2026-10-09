---
name: data-model
description: Game owns Copies; how imported copies are consolidated and why ExternalID formats must never change
metadata:
  type: project
---
- A **Game** owns **Copies** (kind key / library / physical). Sources import copies with a stable
  `ExternalID`; the consolidator matches by ExternalID, then any shared store link
  (`Game.Links`, `{"steam": "620"}`; imports only fill stores the game has no link to), then
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
- **Storage (since migration 0009, 2026-10-09):** games (copies inside), sources and providers are
  JSON documents `(id, doc)`; the adapter's `gameDoc`/`copyDoc`/`sourceDoc`/`providerDoc` field
  names never change once released (like ExternalID prefixes). A new field needs no migration;
  changing a field's meaning bumps `"v"`. Opening an older database writes
  `backups/pre-migration-NNNN.db` first. The user's local `config/` DB is stale (stopped at 0006;
  their real instance runs in Docker), so real-data checks on copies also exercise 0007–0008.
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
