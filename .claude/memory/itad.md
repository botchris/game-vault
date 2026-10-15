---
name: itad
description: IsThereAnyDeal API v2 — deals list, Steam lookup, key in a header, 403 on a bad key; terms (personal use, never alter data, never compete, cache, credit)
metadata:
  type: reference
---
As of 2026-10-15 (plugin `internal/adapters/outbound/itad`, provider `itad`, kind `deals`; see
[[provider-chains]], [[unattended-requests]]):
- **Terms (docs.isthereanydeal.com, read 2026-10-15):** private personal use is allowed with one's
  own key (commercial use only for publicly available apps; private non-personal use needs
  approval). Never alter the data: no removing affiliate tags, no changing prices (partial use and
  adding one's own data are fine). Never compete with ITAD or help a competitor: no price alerts or
  waitlist of Game Vault's own (alerts stay ITAD's). Cache rather than maxing the limit. Linking to
  IsThereAnyDeal or crediting its API is recommended (a "SHOULD"). Access may be revoked any time.
- **Privacy:** ITAD receives no collection data: only deal-list requests with the country, and
  lookups of its own game ids.
- **Limits:** 1000 requests per 5 minutes per account with a verified email; beyond, HTTP 429 with
  `Retry-After`; working around limits can lead to a ban. The adapter sends one request a second,
  rests a minute (or `Retry-After`) after a 429 or an outage, and a refresh is 4 requests every
  6 h ±10 %.
- **Auth:** key as `key` query parameter or `ITAD-API-Key` header; the adapter uses the header
  (request errors name URLs) and refuses redirects (Go forwards custom headers to any host). Users get a key by registering an app at
  https://isthereanydeal.com/apps/my/ (redirects to /apps/ when signed out). OAuth with PKCE exists
  for user data (collection, waitlist): out of scope so far.
- **Probed with a bogus key (2026-10-15):** `GET /deals/v2` → HTTP 403
  `{"status_code":403,"reason_phrase":"Invalid or expired api key"}` (the key is checked before the
  parameters); no key → 403 `{"status_code":403,"reason_phrase":"Missing api key"}`;
  `POST /lookup/shop/61/id/v1` → the same 403, except that an empty array answers 400 before the key
  is checked (never probe the lookup with `[]`); unknown path → 404 JSON. A bad key is 403, not 401.
  The adapter maps 401/403 to a bad key only when the body is that JSON; a 403 HTML page is a
  passing trouble. `Test` sends the featured request with limit 40 (the refresh's own size).
- **Country:** the deals request takes a `country` (ISO alpha-2). Empty setting: the default
  currency's country (EUR → ES, by the maintainer's choice), else the adapter sends US itself
  (`settingsOf`; ITAD's own default when the parameter is missing was not checked).
- **Deals list:** `GET /deals/v2` with `country`, `offset`, `limit` (1–200, default 20), `sort`
  ("same as in deals list on the website"; documented: `-cut`, `price`), `nondeals`, `mature`
  (default false), `shops`, `filter` (lz-compressed base64 or URL-encoded JSON, semantics
  undocumented). Answer `{nextOffset, hasMore, list: [{id, slug, title, type ("game", "dlc", null…),
  mature, assets {boxart, banner145…600 on assets.isthereanydeal.com}, deal {shop {id, name},
  price/regular/storeLow/historyLow/historyLow_1y/historyLow_3m {amount, amountInt, currency}, cut,
  voucher, flag ("H" in the sample at a historical low; null), drm, platforms, timestamp, expiry
  (RFC 3339 with offset, or null), url (https://itad.link/…)}}]}`. Featured = no `sort`; historical
  lows = `sort=-cut&limit=200`, kept when `flag == "H"` or price ≤ historyLow. Only `type == "game"`
  is kept. `assets` is decoded leniently (an empty array is plausible for a game without art).
  The docs name the asset fields (all URL strings) without sizes or optionality, and a sample leaves
  `banner600` out; the card frame is a wide banner, so the adapter prefers banner400 → 300 → 600 →
  145 and falls back to boxart (shown contained, not cropped).
- **Steam ids:** `POST /lookup/shop/61/id/v1` with a JSON array of ITAD ids →
  `{"<id>": ["app/…", "sub/…"] | null}`; the first `app/` is the Steam app.
- **Not verified with a real key yet:** the order of `/deals/v2` without `sort`, the meaning of
  `flag` beyond the sample, `assets` for games without art. Ask the user for the first real
  refresh's result before relying on them.
