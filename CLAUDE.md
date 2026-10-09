# Game Vault

Self-hosted catalog of the games you own: keys (Humble Bundle), store libraries (Steam, Epic, GOG,
Battle.net, EA app, Ubisoft Connect, Xbox / Microsoft Store, PlayStation) and physical discs
(barcode scanning from the phone). Go backend + React web UI, one binary, data in `config/`.
The README presents the product (what it is, supported platforms, install and run) and stays short
and non-technical; `docs/technical.md` has the technical reference; this file is how to work on the code.

The project memory index is imported below; each line points to a file in `.claude/memory/`.
Read the files relevant to the task (e.g. the store's file before touching its integration), and
record new lessons as described at the top of the index.

@.claude/MEMORY.md

## Commands

Everything that compiles, tests or generates code runs in the toolchain container
(`build/toolchain.Dockerfile`, Go 1.26 + Node 24 + buf + generators) through **Task**
(`Taskfile.yml`). Do not rely on Go, Node or other tools installed on the host, and do not add host
dependencies: the host only needs Docker and Task.

| Task | Command |
| --- | --- |
| List tasks | `task` |
| Build everything (web, then server binary for this machine) | `task build` |
| Run (real data, port 8080) | `task run` — the user's own server; do not start or stop it |
| Go vet + tests, TS type check, i18n check | `task test` |
| Format / lint Go (golangci-lint) + buf lint | `task fmt` / `task lint` |
| Apply the safe automatic lint fixes (incl. one struct field per line) | `task lint:fix` |
| Regenerate API code after editing `proto/` | `task generate` |
| Test server on a copy of the config (port 8093) | `task test-server` (`-- --full` copies images), `task test-server:stop` |
| Translations check only | `task i18n` |
| One-off command in the toolchain (one package, a tool…) | `task shell`, or `docker run` with the volumes from `Taskfile.yml` (`DOCKER_RUN`) |
| Release binaries (linux, darwin, windows) | `task build:release` |
| Tag a release (SemVer, from a clean main) | `task release -- X.Y.Z` — creates a local tag only |
| Docker image: try locally / publish a tagged release | `task docker:build` / `task docker:publish` — publishing asks first; never run it, or push tags, without the user's go-ahead |

The server binary is cross-compiled for the host (`CGO_ENABLED=0`, pure-Go SQLite) and runs on
the host: access without a password from trusted networks relies on the real client address, which
a container would hide behind Docker's network. For development, never run the server inside a container, and never open
`config/gamevault.db` from a container (SQLite's shared memory does not cross the Docker VM). The published Docker image
(`build/Dockerfile`) is for users: it trusts the private networks instead (see "Docker image" in `docs/technical.md`).

Go 1.26, Node 24 + Vite 8, TypeScript, React 19. The host shell is fish/zsh on macOS (see gotchas
in `.claude/memory/shell-gotchas.md`).

## Layout (hexagonal + DDD)

```
cmd/gamevault/            wiring: repositories, services, HTTP server; plugins.go lists the plugins
proto/gamevault/v1/       ConnectRPC API (buf); generated code in internal/gen and web/src/gen
internal/domain/          game (Game, Copy, consolidation, MatchKey), source, provider, schema (settings), auth
internal/application/     use cases: catalog, sync (sources, scheduler, keep-alive), media (covers, details,
                          barcodes, provider chains), transfer (CSV), system (backups), logs, auth; port/
internal/adapters/inbound/rpc/      Connect handlers, media HTTP endpoints, auth middleware, SPA serving
internal/adapters/outbound/<name>/  one package per external system: plugins (stores, game databases,
                          barcode lookups: Plugin()), sqlite, files; apiclient is the shared JSON client
web/src/                  React app: features/<page>, components, state/AppData, i18n/locales/{en,es}.json
build/                    toolchain.Dockerfile: the container every task builds in
config/                   runtime data (DB, game-data images, logs, backups) — never commit, never edit
```

Rules of the architecture:
- Domain packages import nothing from application or adapters. Application defines ports
  (interfaces); adapters implement them; `cmd/gamevault/main.go` wires them.
- Every external service is a plugin (`internal/application/plugin`, guide in `docs/plugins.md`):
  its package returns a `plugin.Plugin` with its source and media providers, listed in
  `cmd/gamevault/plugins.go`. Pieces of a plugin do not depend on each other at run time (they
  meet through the game's store links); a dependency on another plugin is a `Plugin()` parameter.
  `plugintest` checks every registered plugin in `task test`.
- Optional capabilities are separate small interfaces checked with a type assertion
  (`sync.Preparer`, `sync.KeepAliver`, `media.ImageHoster`, …), not flags. Checking the
  settings is not optional: every source and media provider implements `Test(ctx, settings) error`
  (one cheap request to the real service; nil means it works, the error says why not).
- Adapters talk to external services through a base URL field so tests point them at `httptest`.
- SQLite: one writer (`MaxOpenConns 1`), transactions via `TxManager.WithinTx`; schema changes
  are new numbered files in `internal/adapters/outbound/sqlite/migrations/`, never edits to old ones.

## Conventions

- Everything in the repository is English: code, comments, docs, log messages, error messages.
  The user talks to you in Spanish; answer in Spanish.
- The UI is translated: every string goes through `t()` with keys in both
  `web/src/i18n/locales/en.json` and `es.json`. Run `task i18n` after touching them.
- Dependencies: change `web/package.json` / `go.mod` from inside the toolchain (`task shell`, then
  `npm install …` in `web/` or `go get …`), never with host tools. Tool versions are pinned in
  `build/toolchain.Dockerfile`; editing it rebuilds the image (its tag is a hash of the file).
- Comments explain why, in full sentences, at the density of the surrounding code. Doc comments on
  every exported identifier.
- Errors shown to the user say what happened and what to do ("… paste a new code"), in English.
  Sentinel errors (`ErrSignedOut`, …) so callers use `errors.Is`.
- Each new external integration gets a test against a fake server reproducing the real API's
  shape and failure modes (see the existing `provider_test.go` files).
- Keep the docs current: `docs/technical.md` for how things work (sources, providers, UI
  behaviour, configuration); `README.md` only when the product changes for the user (a new
  supported platform, a new install step). No implementation details or product comparisons in
  the README.

## How to verify a change

1. `task lint` and `task test` pass (golangci-lint, buf lint, vet, Go tests, TS types, translations).
2. `task test-server` (builds web + server first) and look at the result in the browser pane on
   http://127.0.0.1:8093 (desktop, then the mobile preset; reset the viewport afterwards), then
   `task test-server:stop`.
4. For an external service, probe the real endpoint with bogus credentials (expect a clean
   "invalid code / not signed in" error, not a 404 or a schema error) before telling the user it
   is ready, and say plainly which parts could only be tested against fakes.

## Safety rules (non-negotiable)

- Never read, print or send the user's credentials: cookies, tokens, API keys and sessions live
  in `config/gamevault.db`. Debug with a copy (`test-server.sh` makes one) and never echo secret
  values in output or chat.
- Never modify `config/` or the user's server on port 8080. Test servers use port 8093 and a
  copy of the config in the scratchpad, started with `-no-unattended` (`test-server.sh` does it).
  On a copy, never scan or test sources whose credentials rotate on use (Ubisoft, Epic, GOG, Xbox,
  PlayStation, Battle.net, EA): renewing them there invalidates the user's real ones. Humble and
  Steam are safe.
- Never scrape a site whose terms forbid automated access, never bypass captchas or bot
  protection. Store integrations reuse the user's own session the way open-source launchers do
  (Heroic, Legendary, Playnite, Lutris…), and only read.
  The one exception, decided by the user on 2026-10-09, is Fanatical (its terms forbid taking
  site data into databases): it requires a ticked risk consent (`schema.FieldConsent`), warns in
  the UI and the README, scans manually by default and only reads. Any other store like it needs
  the user's explicit decision first; never add one on your own.
- Requests that run unattended (keep-alives, scheduled scans) are jittered so they never follow a
  fixed rhythm (see `internal/application/sync`).
- The server listens on 127.0.0.1 by default. Requests through a proxy or with a host name in `Host`
  are never treated as coming from a trusted network (proxies hide the caller; host names allow
  DNS rebinding). Keep both rules in any change to `internal/domain/auth`.

## Detailed guides

- Writing Go (style, lint rules, layering, tests): the `write-go` skill, `.claude/skills/write-go/SKILL.md`
- Adding a plugin (source, cover, metadata or barcode provider): `docs/plugins.md` (contributor
  guide), then `.claude/docs/integrations.md` (research, probing, memory, checklist)
- Web UI, design system and motion rules: `.claude/docs/ui.md`
