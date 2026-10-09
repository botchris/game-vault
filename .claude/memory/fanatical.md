---
name: fanatical
description: Fanatical: account API used by the Playnite plugin, probe results, the terms clause, and the user's decision to integrate it with an explicit risk consent (2026-10-09)
metadata:
  type: reference
---
As of 2026-10-09:
- Reference: nosyn00b/playnite-fanatical-plugin (MIT, last push 2024-01). It takes the session
  from the site's Local Storage entry `bsauth` (JSON `{authenticated, token, …}`) and calls
  `GET https://www.fanatical.com/api/user/keys` with header `authorization: <token>`; it checks
  the token with `GET /api/user/refresh-auth`. Items: `_id`, `name`, `type` (game, dlc…), `drm`
  flags (steam, epicgames, gog, uplay, origin…), `status`, `serialExpiry`, `purchased`, `order`,
  `bundles`, `giveaway`, `isFreeProduct`.
- Probes: no header → 401 "Missing authorization header"; bogus token → 401 "Invalid
  authorization header" (text/html, no bot check). robots.txt disallows the account pages
  (/en/account, /en/orders…) and does not mention /api/user.
- **Terms and Conditions** (support.fanatical.com, article 115005056629) list among prohibited
  uses: "Take data from our website for your or third party databases or data harvests without
  our consent". A scheduled scan that copies the order history into Game Vault's database is
  that. Options were put to the user, who chose (2026-10-09) to build it anyway as option C,
  with the risk made unmissable: required consent checkbox (`schema.FieldConsent`), notice
  quoting the clause first in the dialog, manual scans by default (`ManualScans`), one read per
  scan, README warning, and an explicit exception in CLAUDE.md. The project is public: the
  wording must make clear the user accepts the risk, not the authors.
- Not confirmed with a real account: the item shape and how `status` marks revealed/redeemed
  keys; whether the key value itself is in the list (Game Vault does not store it).
