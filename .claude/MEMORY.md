# Project memory — index

One line per memory; the details are in `.claude/memory/`. Read a file only when the task touches
it. To add a memory: create `.claude/memory/<slug>.md` (frontmatter `name`, `description`,
`metadata.type`: user | project | reference | feedback; link related ones with `[[slug]]`) and add
one line here. Update a file instead of adding a duplicate; date facts about external services.

## Working rules
- [Public project](memory/public-project.md) — public repo: no personal data (own library, counts, accounts, test results) in PRs, commits, docs, specs, comments or memory
- [Multilingual](memory/multilingual.md) — English default, Spanish optional, more later: language-dependent features follow the UI language, fall back to English, never Spanish-first
- [UI copy](memory/ui-no-product-references.md) — never name or compare with other products (Plex, Playnite…) in UI text; integrated services are fine
- [Go style feedback](memory/go-style-feedback.md) — always load `write-go` before Go; blank line between documented members; every interface method documented; one struct field per line; functional options for constructors

## Status
- [Open threads](memory/open-threads.md) — offered, not done: cover photo recognition (paused)

## Decisions
- [License](memory/license.md) — PolyForm Noncommercial 1.0.0: source-available, no commercial use; never say "open source"
- [Project name](memory/project-name.md) — stays "Game Vault" despite Phalcode's GameVault; no more rename proposals
- [Data model](memory/data-model.md) — Game owns Copies, store Links (no store special) and editions (a cover per system); ExternalID prefixes and PhotoIDs must never change; shared platform names; copy photos
- [Provider chains](memory/provider-chains.md) — chain order (metadata: Steam → Wikipedia → IGDB → TheGamesDB), covers per edition (`Applies` by system), TheGamesDB quota rules + fallback pass, bump `coverLogicChanged`
- [Credentials](memory/credentials.md) — `FieldState` for rotating secrets, `Preparer` code cache, always hand back the latest token; never two servers on one rotating credential (test copies: `-no-unattended`)
- [Unattended requests](memory/unattended-requests.md) — keep-alive ±30%, scans ±10%, spread start-up
- [Local images](memory/local-images.md) — downloads into `config/game-data`, proxy only for declared hosts
- [Security model](memory/security-model.md) — one user, trusted networks, cert validation, `-reset-auth`; never trust proxied or host-name requests; access tokens reach only the ownership lookup
- [Docker image](memory/docker-image.md) — `botchrishub/game-vault`, amd64+arm64, SemVer git tags from v0.1.0, `dev` tag overwritten for tests, trusts private networks until saved; publish only on request
- [Connector extension](memory/connector.md) — generic browser extension running sign-in recipes; spike 10/10 stores; when-conditions, per-host confirmation, not published; store labels and bundle marks from selector data
- [Cover recognition](memory/cover-recognition.md) — no paid LLMs: OCR + title matching + CLIP tie-break; spike 4/4, logo digits never OCR'd; paused for a 30-50 photo test set

## External services (as of 2026-10)
- [Humble Bundle](memory/humble.md) — keyindex is not a key id (use machine_name); non-game keys withdrawn by platform; one-copy-per-order bug fixed 2026-10-08
- [Steam](memory/steam.md) — public store APIs, ~200 req / 5 min, Humble AppIDs that don't exist
- [TheGamesDB](memory/thegamesdb.md) — free quota check, unknown keys look valid, CDN blocks hotlinks, 429 on bursts (throttled) and on a used-up allowance (checked via API/Limit, then no requests until renewal)
- [IGDB](memory/igdb.md) — Twitch app Client ID/Secret → client-credentials token cached in memory, 4 req/s & 8 open, new `external_game_source`/`rating_category` fields; awaits a real test
- [Wikipedia](memory/wikipedia.md) — Wikidata search + entities, labels need `mul`, errors with HTTP 200, descriptive User-Agent, CC BY-SA credit under the summary, strict one-match rule
- [Epic](memory/epic.md) — Legendary's credentials, paste the JSON code, catalog without namespace
- [GOG](memory/gog.md) — Galaxy credentials, 302 to login on bad token, public box art
- [Battle.net](memory/battlenet.md) — cookies, short sessions, SSO renewal failed once, no public box art
- [EA](memory/ea.md) — Origin dead; GraphQL validates before auth; public `game(slug)` pack art
- [Ubisoft](memory/ubisoft.md) — app `314d4fef…` blocked; `PRODrememberMe` rotates on every use; limit 50
- [Xbox](memory/xbox.md) — Xbox app client; collections need `"beneficiaries": []`; public display catalog
- [PlayStation](memory/playstation.md) — NPSSO; registered GraphQL hashes; CSRF header; store pages forbidden (no PS Store covers); non-games withdrawn via played-titles categories + name markers
- [Amazon Games](memory/amazon.md) — launcher device sign-in (nile), refresh token does not rotate, entitlements API, no portrait art
- [itch.io](memory/itch.md) — official API, personal key, `/profile/owned-keys` until an empty page, errors list even with 200
- [Fanatical](memory/fanatical.md) — `/api/user/keys` with the `bsauth` token; terms forbid it, built only behind a required risk consent (user's decision)
- [SteamGridDB](memory/steamgriddb.md) — official API v2 with a personal key; terms forbid automated use, built only behind a required risk consent (maintainer's decision 2026-10-11); fallback pass, 1 req/s
- [CeX](memory/cex.md) — free EAN box-detail API with PAL coverage; barcode lookups keep name and platform; prices of own copies allowed (2026-10-09); search behind Cloudflare; no images
- [EAN-Search](memory/ean-search.md) — never scrape the public site

## Gotchas
- [Shell](memory/shell-gotchas.md) — zsh `path` / `GID`; no raw BOM in source
- [Go](memory/go-gotchas.md) — `filepath.Glob` and `[id]` folder names
- [Browser pane](memory/browser-pane.md) — cached `index.html`: use `?v=N`
- [Toolchain](memory/toolchain.md) — Go version for buf; golangci-lint and never a bare `--fix`; why `dev:web` is a watch build; node_modules volume
- [Probing APIs](memory/api-probing.md) — bogus-credential probes, GraphQL aliases, lenient decoding
