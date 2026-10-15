---
name: igdb
description: IGDB (Twitch) endpoints, client-credentials token, limits, probed error shapes, terms; awaits a real test
metadata:
  type: reference
---
As of 2026-10-11 (plugin `internal/adapters/outbound/igdb`, covers `igdb` + details `igdb-details`,
settings group `igdb`):
- **Terms:** free for non-commercial use under the Twitch Developer Service Agreement; caching
  allowed. The user creates their own Twitch application (dev.twitch.tv/console/apps, 2FA required,
  client type Confidential) and pastes Client ID (`FieldText`) + Client Secret (`FieldSecret`).
- **Token:** `POST https://id.twitch.tv/oauth2/token?client_id=…&client_secret=…&grant_type=client_credentials`
  → `{"access_token","expires_in" (~60 days),"token_type":"bearer"}`. Kept in memory per
  (id, secret) hash until 5 min before expiry; a 401 from IGDB drops it and retries once. Nothing
  rotates, so no `FieldState` and test copies are safe.
- **Probed with bogus credentials (2026-10-11):** unknown Client ID → HTTP 400
  `{"status":400,"message":"invalid client"}`; IGDB with a bogus token → HTTP 401 JSON
  `{"message":"Authorization Failure. Have you tried:", "Tip 1": …}`. A wrong secret for a real
  Client ID (documented 403 `invalid client secret`) could not be probed without an application.
- **API:** `POST https://api.igdb.com/v4/<endpoint>`, headers `Client-ID` + `Authorization: Bearer`,
  Apicalypse body (`apiclient.Request.Text`). Limits: 4 req/s, 8 open; 429 when exceeded
  (→ `provider.ErrRateLimited`). Adapter: `ratelimit` with a 250 ms gap, 8 slots, waits > 10 s (slot included) fail as busy; a 429 or 5xx rests it 30 s (or `Retry-After`) since 2026-10-13.
- **Deprecated fields (docs, 2026-10):** `external_games.category` → `external_game_source`
  (same ids: 1 Steam, 5 GOG, 26 Epic, 11 Microsoft, 36 PlayStation Store US, 20 Amazon ASIN);
  `age_ratings.category/rating` → `organization` (expand `.name`) and `rating_category` (expand
  `.rating`, a string). The adapter only asks for the new ones; rating strings are normalized
  leniently (PEGI words "Twelve" → 12, ESRB "E10" → E10+), because their exact values could not be
  seen without an account.
- **Matching:** Steam/GOG/Epic links via one `external_games` query with `game.*` expanded; the uid
  formats for GOG (product id) and Epic (catalog item id?) are unverified. Else `search "…"` with
  `where platforms = (…)` (PC = 6, 14, 3) and only an exact title match, editions
  (`version_parent`) and add-ons (`parent_game`) yielding to the game; several → nothing.
- **Automatic links (2026-10-11):** `FindLinks` returns only the Steam link (source 1); GOG/Epic
  uids stay out until their formats are verified. No Steam id → empty answer, so Steam's exact
  title search still runs.
- **Review fixes (2026-10-11):** credentials go in a form body (Twitch accepts it: probed, same
  400 "invalid client"); transport errors to Twitch are not wrapped (the URL used to carry the
  secret); one token request per credentials at a time (singleflight), renewed after a 401 only if
  the cached token is the refused one; missing `expires_in` = 1 h; limiter takes the slot before the
  start time; a store id listed under several games keeps the main one, else nothing.
- Images: `https://images.igdb.com/igdb/image/upload/t_<size>/<image_id>.jpg` (cover_big,
  cover_big_2x, screenshot_big, 1080p); removed images stay 30 days.
- **Releases (2026-10-15):** `igdb-releases` (kind `releases`, off until turned on). Anticipated: one
  `games` query, `first_release_date` within 60 days, `hypes > 0`, `version_parent = null &
  parent_game = null`, `sort hypes desc; limit 40;`. Sequels: `external_games` by Steam uid
  (source 1; IGDB receives Steam AppIDs, not IGDB ids) in batches of 500 with
  `game.franchise,game.franchises,game.collections`, then `games` with `collections = (…) |
  franchises = (…) | franchise = (…)` and `first_release_date > now`, 500 ids a batch, paged while
  full. Games without a Steam link contribute no series; queries scale with Steam-linked games (one
  lookup per 500). Bundles or episodes may show as sequels until a `game_type` filter is added.
  `hypes` = follows before release; `follows`, `category` and `collection` are deprecated; `limit`
  max 500. Only the documentation was read: no real query has run yet. IGDB receives the Steam ids
  of owned games (privacy note in `docs/technical.md`).
- **Awaits a test with a real Twitch application**; only the error shapes above were seen live,
  everything else against the fake in `fake_test.go`. If the first real sheet fails, ask for the
  warning text (IGDB's own `[{"title":…,"cause":…}]` is passed through).
