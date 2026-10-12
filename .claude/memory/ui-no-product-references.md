---
name: ui-no-product-references
description: UI text never names or compares with other products (Plex, Sonarr, Playnite, Heroic…); stores Game Vault integrates are fine
metadata:
  type: feedback
---

Text shown in the web UI (and the extension) never refers to other products to explain itself:
no "like Plex agents", "the way Playnite does", "like Sonarr". Name only the services Game Vault
actually connects to (Steam, Epic, GOG, SteamGridDB…), and explain behaviour in its own words.

**Why:** the maintainer asked for it on 2026-10-12 after seeing "Like Plex agents" on the
Providers page.
**How to apply:** when writing en/es strings, describe what Game Vault does; comparisons with other
apps can stay in code comments, docs for contributors or memory, never in UI copy. The README
follows the existing "no product comparisons" rule in CLAUDE.md.
