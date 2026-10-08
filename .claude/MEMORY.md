# Project memory — index

One line per memory; the details are in `.claude/memory/`. Read a file only when the task touches
it. To add a memory: create `.claude/memory/<slug>.md` (frontmatter `name`, `description`,
`metadata.type`: user | project | reference | feedback; link related ones with `[[slug]]`) and add
one line here. Update a file instead of adding a duplicate; date facts about external services.

## User
- [User profile](memory/user-profile.md) — Spanish chat / English code, runs `task run`, no host deps, anti-bot jitter, Emil design taste, no VPN dependency in the product

## Status (update when the user reports results)
- [Integration status](memory/status-integrations.md) — confirmed: Humble, Steam, Epic, GOG, EA, Ubisoft, Xbox; watch Battle.net keep-alive; PlayStation and the security redesign untested by the user
- [Open threads](memory/open-threads.md) — offered, not done: PS Store covers, itch.io, Amazon, EAN-Search button, CI, config README
- [Known gaps](memory/known-gaps.md) — games without any cover; project not in git yet

## Decisions
- [License](memory/license.md) — PolyForm Noncommercial 1.0.0: source-available, no commercial use; never say "open source"
- [Project name](memory/project-name.md) — stays "Game Vault" despite Phalcode's GameVault; no more rename proposals
- [Data model](memory/data-model.md) — Game owns Copies; ExternalID prefixes must never change; shared platform names
- [Provider chains](memory/provider-chains.md) — chain order, TheGamesDB quota rules + fallback pass, bump `coverLogicChanged`
- [Credentials](memory/credentials.md) — `FieldState` for rotating secrets, `Preparer` code cache, always hand back the latest token
- [Unattended requests](memory/unattended-requests.md) — keep-alive ±30%, scans ±10%, spread start-up
- [Local images](memory/local-images.md) — downloads into `config/game-data`, proxy only for declared hosts
- [Security model](memory/security-model.md) — one user, trusted networks, cert validation, `-reset-auth`; never trust proxied or host-name requests
- [Docker image](memory/docker-image.md) — `botchrishub/game-vault`, amd64+arm64, trusts private networks until saved; publish only on request

## External services (as of 2026-10)
- [Steam](memory/steam.md) — public store APIs, ~200 req / 5 min, Humble AppIDs that don't exist
- [TheGamesDB](memory/thegamesdb.md) — free quota check, unknown keys look valid, CDN blocks hotlinks
- [Epic](memory/epic.md) — Legendary's credentials, paste the JSON code, catalog without namespace
- [GOG](memory/gog.md) — Galaxy credentials, 302 to login on bad token, public box art
- [Battle.net](memory/battlenet.md) — cookies, short sessions, SSO renewal failed once, no public box art
- [EA](memory/ea.md) — Origin dead; GraphQL validates before auth; public `game(slug)` pack art
- [Ubisoft](memory/ubisoft.md) — app `314d4fef…` blocked; `PRODrememberMe` rotates on every use; limit 50
- [Xbox](memory/xbox.md) — Xbox app client; collections need `"beneficiaries": []`; public display catalog
- [PlayStation](memory/playstation.md) — NPSSO; registered GraphQL hashes; CSRF header
- [EAN-Search](memory/ean-search.md) — never scrape the public site

## Gotchas
- [Shell](memory/shell-gotchas.md) — zsh `path` / `GID`; no raw BOM in source
- [Go](memory/go-gotchas.md) — `filepath.Glob` and `[id]` folder names
- [Browser pane](memory/browser-pane.md) — cached `index.html`: use `?v=N`
- [Toolchain](memory/toolchain.md) — Go version for buf; golangci-lint and never a bare `--fix`; why `dev:web` is a watch build; node_modules volume
- [Probing APIs](memory/api-probing.md) — bogus-credential probes, GraphQL aliases, lenient decoding
