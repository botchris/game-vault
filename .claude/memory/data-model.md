---
name: data-model
description: Game owns Copies; how imported copies are consolidated and why ExternalID formats must never change
metadata:
  type: project
---
- A **Game** owns **Copies** (kind key / library / physical). Sources import copies with a stable
  `ExternalID`; the consolidator matches by ExternalID, then Steam AppID, then
  `game.MatchKey(title)`.
- **Changing an ExternalID format duplicates copies on the next scan**: don't, or set
  `ImportedCopy.PreviousExternalID` so the consolidator adopts the old copy (only when it is in the
  same game; source details are cleared, user notes kept).
- Prefixes: `steam:<appId>`, `humble:<gamekey>:<machine_name>:<keyindex>` (was `humble:<gamekey>:<keyindex>`, see [[humble]]), `epic:<catalogItemId>`,
  `gog:<productId>`, `battlenet:<titleId>`, `ea:<productId>`, `ubisoft:<spaceId>`,
  `xbox:<productId>`, `psn:<entitlementId>`. Cover providers find a game's own art through them
  (`CoverQuery.ExternalIDsWithPrefix`). See [[provider-chains]].
- Platform names are shared between sources so Humble keys are flagged as redundant when the
  game is already in that store: "Steam", "Epic Games", "GOG", "Battle.net", "EA App",
  "Ubisoft Connect", "Microsoft Store / Xbox"; PlayStation copies use "PS4"/"PS5".
