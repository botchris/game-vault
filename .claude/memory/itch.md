---
name: itch
description: itch.io: official server-side API with a personal key, owned-keys endpoint, what the probes returned (2026-10-10)
metadata:
  type: reference
---
As of 2026-10-10 (https://itch.io/docs/api/serverside):
- `GET https://api.itch.io/profile/owned-keys?page=N&per_page=500` (max 500) with
  `Authorization: Bearer <key>`. Answer `{"page","per_page","owned_keys":[{id, game_id,
  purchase_id (only for purchases), downloads, created_at, updated_at, game:{id, title,
  classification, type, url, cover_url…}}]}`. A short page is NOT the last one: read until empty.
- Personal API keys (itch.io/user/settings/api-keys) are unscoped: they reach every endpoint.
- Errors are an `errors` string list, possibly with HTTP 200. Probed with a bogus key: HTTP 403
  `{"errors":["invalid key"]}`; without a key: HTTP 401 `{"errors":["authentication required"]}`
  (both for `/credentials/info` and `/profile/owned-keys`).
- `classification` values (go-itchio): game, tool, assets, game_mod, physical_game, soundtrack,
  other, comic, book. Only `game` (or missing) is imported.
- Dates are documented as RFC 3339; the importer also accepts "2006-01-02 15:04:05".
- Covers are 315×250 (landscape): no cover provider; games get covers from other links or by hand.
- Not yet confirmed with a real account: the exact `game` object and whether `classification`
  is always present. See [[status-integrations]].
