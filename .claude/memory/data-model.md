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
  link on 2026-10-09 (migration 0007, CSV column `links`, no backwards compatibility).
- **Changing an ExternalID format duplicates copies on the next scan**: don't, or set
  `ImportedCopy.PreviousExternalID` so the consolidator adopts the old copy (only when it is in the
  same game; source details are cleared, user notes kept). A source drops a copy it imported
  earlier by returning it `Withdrawn`; games left empty are deleted by the sync.
- Prefixes: `steam:<appId>`, `humble:<gamekey>:<machine_name>:<keyindex>` (was `humble:<gamekey>:<keyindex>`, see [[humble]]), `epic:<catalogItemId>`,
  `gog:<productId>`, `battlenet:<titleId>`, `ea:<productId>`, `ubisoft:<spaceId>`,
  `xbox:<productId>`, `psn:<entitlementId>`, `amazon:<productId>`, `fanatical:<keyId>`. Cover providers find a game's own art through them
  (`CoverQuery.ExternalIDsWithPrefix`). See [[provider-chains]].
- Platform names are shared between sources so Humble keys are flagged as redundant when the
  game is already in that store: "Steam", "Epic Games", "GOG", "Battle.net", "EA App",
  "Ubisoft Connect", "Microsoft Store / Xbox"; PlayStation copies use "PS4"/"PS5".
