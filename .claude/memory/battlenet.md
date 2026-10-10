---
name: battlenet
description: Battle.net: cookie-based, short sessions, failed SSO renewal, no public box art
metadata:
  type: reference
---
As of 2026-10:
- No public library API; the source reuses account.battle.net cookies.
- The site session is short; SSO renewal from pasted cookies failed once (2026-10-08), so
  keep-alive was added (see [[unattended-requests]]).
- The Blizzard shop requires sign-in even to view products: there is no public box art. The
  cover provider looks games up on Steam by exact title, then TheGamesDB on the fallback pass.
