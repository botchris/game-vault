---
name: steamgriddb
description: SteamGridDB: official API v2 with personal keys, probe results, the Terms of Use clause, and the maintainer's decision to add it as a cover provider behind a risk consent (2026-10-11)
metadata:
  type: reference
---
As of 2026-10-11:
- **Terms of Use** (https://www.steamgriddb.com/terms, effective 2019-07-31; the page is rendered by
  JavaScript, so curl gets an empty shell) allow only "personal, non-commercial use solely as
  permitted by the normal functionality of the Service" and forbid to "make any automated use of the
  Site, the Service or the related systems". An unattended cover lookup through the API is that.
- **Decision (maintainer, 2026-10-11, after reading the terms):** add it anyway as a full cover
  provider, the same way as [[fanatical]]: required consent (`schema.FieldConsent`) whose notice
  quotes the clause first, the user's own API key, disabled by default, warnings in the README's
  provider list and `docs/technical.md`, an exception line in CLAUDE.md. The wording says the user
  accepts the risk (key revoked, account suspended), not the authors.
- **API v2** (base `https://www.steamgriddb.com/api/v2`, header `Authorization: Bearer <key>`; key from
  Preferences → API, `https://www.steamgriddb.com/profile/preferences/api`, after signing in with
  Steam). Spec: `https://www.steamgriddb.com/static/openapi.yml` (v2.10.0); wrappers: npm
  `steamgriddb` (official), Rust `steamgriddb_api`, .NET, Python. Used by Playnite, Heroic, Lutris,
  Steam ROM Manager.
  - `GET /search/autocomplete/{term}` → `{"success":true,"data":[{"id","name","types":[…],"verified"}]}`.
  - `GET /grids/steam/{appid}` and `GET /grids/game/{id}` with `dimensions=600x900&types=static&nsfw=false&humor=false&epilepsy=false`
    (nsfw/humor/epilepsy default to false anyway; `limit` default 50) →
    `{"success":true,"page","total","limit","data":[{"id","score","style","url","thumb","tags","author":{"name","steam64","avatar"}, …}]}`.
    Real answers also carry `width`, `height`, `nsfw`, `humor`, `epilepsy`, `mime`, `language`,
    `upvotes`, `downvotes` (per the wrappers; not in the spec). Images on `cdn2.steamgriddb.com`
    (`/grid/…`, thumbnails `/thumb/…`).
  - Unknown game: 404 `{"success":false,"errors":[…]}` (spec). No documented rate limit.
- **Probes with a bogus key (2026-10-11, two requests):** `GET /search/autocomplete/example` with
  `Bearer 000…` → HTTP 401 `{"success":false,"errors":["Invalid API key"]}` (`www-authenticate:
  Bearer`, behind Cloudflare); `GET /grids/steam/620` with no header → 401
  `{"success":false,"errors":["Authentication Required"]}`. So paths and auth scheme are right.
- Implementation: `internal/adapters/outbound/steamgriddb` (DefaultOrder 90, after IGDB; fallback pass
  only, never `LinksOnly`; Steam AppID first, else exact-title search, one game; top 3 grids by score,
  labelled community art; throttle 1 req/s, 3 s max wait, queue 3, 1 min back-off after 429/5xx).
  **Awaits a test with a real key**: grid ordering, the 404 body for an unknown AppID, and whether
  `width`/`height` are present were only checked against fakes.
