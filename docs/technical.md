# Game Vault — technical notes

How Game Vault works inside. For what it is and how to run it, see the [README](../README.md);
for working on the code (with or without an AI agent), see [CLAUDE.md](../CLAUDE.md).

- Go backend (hexagonal architecture + DDD) over SQLite, pure Go (no cgo).
- React + TypeScript web client (Vite), in English and Spanish.
- Client and server talk only through **ConnectRPC** with **Protocol Buffers** contracts in
  [`proto/`](../proto/).
- Building, testing and code generation run in a toolchain container
  ([`build/toolchain.Dockerfile`](../build/toolchain.Dockerfile)) through [Task](https://taskfile.dev)
  ([`Taskfile.yml`](../Taskfile.yml)); the server binary is cross-compiled for the host and runs there.

## Repository layout

```
cmd/gamevault/        Entry point: configuration, the real plugins, background jobs, listening
config/               Runtime data: database, backups, logs, per-game images (game-data), photos of copies (photos). Back this up. Git-ignored.
proto/gamevault/v1/   API contracts (Protocol Buffers). Source of truth for client and server.
internal/
  app/                Composition root (wires adapters into use cases), shared by the server and the API's end-to-end tests
  domain/             Pure business model, no infrastructure imports
    game/             Game aggregate, Copy entity, value objects, Consolidator domain service
    source/           Source aggregate (scanned accounts), settings schema, sync reports
    settings/         Runtime settings edited from the UI (log level and rotation)
    auth/             Access control: request identity, principals, settings, users, sessions, access tokens
    provider/         Metadata provider configuration: kind, order, enabled, settings
    schema/           Settings schema shared by sources and providers (fields, secret masking)
  application/        Use cases; declares the ports it needs
    port/             Cross-cutting ports: TxManager, Clock
    catalog/          Browse/edit games and copies, merge, move, mark redeemed; Consolidate merges imported
                      copies (source scans, CSV imports) in one transaction. Declares the CoverCache port
    sync/             Configure sources, scan them, scheduler. Declares the Provider port
    transfer/         CSV import/export. Declares the Codec port
    system/           Status and backups. Declares the DatabaseBackup and PhotoArchive ports
    valuation/        Second-hand price estimates: estimate a copy, the collection's value, the scheduler. Declares the price Provider port
    providers/        The providers registry: configuration of every provider kind (enabled, order, settings), read once
                      into memory; provider status and its health check. Declares the Provider port
    media/            One small service per part, wired by media.New: Covers (resolve, cache, picker, add-ons),
                      Details (sheets, background scan), Assets (sheet images), Identifier (barcodes, typed
                      titles), Stores (link stores and search), Photos, AutoLinker, ImageProxy. Declares
                      CoverProvider, MetadataProvider, PhotoStore & co.
    plugin/           The Plugin type (one external service's source and providers) and its registry;
                      plugintest/ checks every registered plugin
    logs/             Log viewer and rotation settings. Declares the Sink and Files ports
    auth/             Authenticate, setup, login, password, settings. Declares the Hasher port
    ownership/        "Do I own it?": store links, exact titles, current copies (read only)
  adapters/
    inbound/rpc/      Driving adapter: ConnectRPC handlers, proto↔domain mapping, static UI
    outbound/humble/, steam/, epic/, gog/, battlenet/, eaapp/, ubisoft/, xbox/, playstation/,
             amazon/, itch/, fanatical/
                      Store plugins: the source, and cover/details providers where the store has public art
    outbound/apiclient/ Shared HTTP client for plugins (base URL, User-Agent, JSON, forms, GraphQL, status errors)
    outbound/ratelimit/ The one throttle every provider paces its requests with
    outbound/oauthsession/, oauthapp/ OAuth token caches: sources' code-based sessions, applications' tokens
    outbound/example/ A complete plugin for an imaginary store, to copy (tested, never registered)
    outbound/browsersession/ Reuse of a website session pasted from the browser (cookies)
    outbound/thegamesdb/ TheGamesDB plugin: cover and details providers (platform box art, overview, trailer)
    outbound/igdb/    IGDB plugin: cover and details providers (Twitch application credentials)
    outbound/steamgriddb/ SteamGridDB plugin: community cover provider (behind a risk consent)
    outbound/wikipedia/ Wikipedia plugin: details provider (Wikidata facts, article introduction)
    outbound/cex/, ebay/, upcitemdb/, eansearch/  Barcode database plugins (CeX and eBay also give second-hand prices)
    outbound/gamedata/ Per-game asset folders (cover, sheet images, assets.json)
    outbound/photostore/ Content-addressed photos of copies, and the backups' shared photo store
    outbound/sqlite/  Repositories, transactions, migrations, backups
    outbound/logfile/ Size-rotated log files with retention
    outbound/passwordhash/ bcrypt Hasher
    outbound/imagefetch/ HTTP image downloader
    outbound/csvfile/ CSV codec
  config/             Flags and environment variables
  gen/                Generated Go code (buf generate) — do not edit
web/                  React client
  src/gen/            Generated TypeScript code (buf generate) — do not edit
  src/api/            Connect clients
  src/features/       library/, scan/, sources/, providers/, system/, logs/ pages
  src/i18n/           i18next setup and locales (en.json, es.json)
extension/            Browser extension (Game Vault Connector): sign-in recipes (lib/), store labels (ownership/), tests with a fake chrome
```

Dependencies only point inwards. `domain` imports nothing from the project, `application` imports `domain`, and adapters import both.
Only the composition root (`internal/app`, called by `cmd/gamevault` and by the API's end-to-end tests
with fakes) knows the concrete adapters, so the tests run the same wiring as the server.

Every external service is a **plugin**: its package returns a `plugin.Plugin` with the pieces it
offers (a source, cover, details or barcode providers), and `cmd/gamevault/plugins.go` lists them.
The registry checks that ids are unique and hands the sources to the sync service and every provider
(covers, details, barcodes, prices) to the providers registry (`internal/application/providers`),
which keeps their configuration; the media and valuation services run them. Each provider declares
its default place in its chain (`DefaultOrder`); the user's order replaces it once saved. The
configuration is read from the database once, on first use, and kept in memory: only the registry
changes it (the Providers page), writing each change through. A new kind of provider is one entry in
`provider.Kinds()` and one field of `plugin.Plugin`. How to write one: [`docs/plugins.md`](plugins.md).

**Errors** carry their kind (`internal/domain/errkind`): invalid input, not found, already exists,
conflict, unauthenticated, permission denied, precondition, exhausted or unavailable. Sentinel
errors get theirs from `errkind.New`, rule violations are the one `errkind.ValidationError` (made
by `errkind.Invalidf`, whichever domain package checks the rule), and `errkind.Wrap` gives one to an error
that has none (the CSV codec's errors are the file's, so `Invalid`). The RPC layer maps kinds, not
sentinels, to Connect codes in one table (`toConnectError`), so its handlers never map errors
themselves; the media HTTP endpoints (covers, images, photos) answer their few errors with HTTP
statuses of their own. In the RPC layer, an error without a kind is a failure of Game Vault
itself: it is answered `Internal` and logged at error level. Only Game Vault's own sign-in is `Unauthenticated` (the web page signs
the user out on it); a store or provider that rejects its credentials is `Precondition`. A test
lists every exported sentinel of the domain and application packages with its code, and fails for
an adapter sentinel made with a plain `errors.New` (a short allowlist of `apiclient` plumbing aside)
and for an error type with its own `Is` method but no `Kind`, so a new error cannot fall through
to `Internal` unnoticed.

## Domain model

| Concept          | Kind                  | Notes |
|------------------|-----------------------|-------|
| `Game`           | Aggregate root        | Title, links to stores (`{"steam": "620"}`), notes, play status, rating, and its copies. Every copy change goes through the game. |
| `Copy`           | Entity inside `Game`  | `kind` is `key`, `library` or `physical`. `status` must be valid for the kind. Holds platform, key, redeem-by date, origin, edition, purchase date and price (any kind), and for physical copies grade, contents and location, and the system it is played on (see below). |
| `Edition`        | Derived from the copies | One per system of a game's copies (PC, PS3, Xbox 360…). It holds only its chosen cover (a URL or a photo of one of its copies); the game stores those by system and an optional main system. |
| `Source`         | Aggregate root        | A scanned account: type, settings (secrets masked towards clients), interval, last sync report. |
| `Consolidator`   | Domain service        | Merges imported copies into the catalog. It matches by external id, then by any store link the copy shares with a game, then by normalised title (no trademarks, brackets, edition words or "<store> key", so "Hades - GOG Key" is "hades"); if nothing matches it creates a new game. |

The **play status** (`backlog`, `playing`, `finished`, `abandoned`, or not set) and the **rating**
(1 to 5 stars, 0 when unrated) belong to the game, not to a copy: owning a game twice does not mean
playing it twice. Merging games keeps the kept game's status and rating and takes the other's only
where it has none. They are not part of the CSV, which has one row per copy.

### Systems and editions

A copy's **platform** says where it lives (Steam, a Humble key, a PS4 disc). Its **system** says
where it is played: the user's override on the copy, else the system its source states (PlayStation
copies say PS4 or PS5), else the one its platform implies. PC stores and launchers (Steam, Epic
Games, GOG, EA App, Ubisoft Connect, Battle.net, itch.io, Amazon Games, Rockstar, Riot and
Battlestate (Tarkov)), the platform `PC` itself and the ambiguous "Microsoft Store / Xbox" imply
`PC`; console names are their own system; "PlayStation Store" implies PS4 and "Nintendo eShop"
Switch. An unknown platform is its own system and an empty one is `Other`. A source leaves
`ImportedCopy.System` empty when its platform says the system (every PC store), and sets it when the
store says more than its platform (PlayStation's console). A system a source sends that is not a valid
name is ignored (the platform's applies); scans warn about it once per source and system while the
server runs, then only log it at debug level, while a CSV import warns every time. Scans never touch
the override; the form's **System** field clears it when emptied. A system name is stored in game
documents and in cover file names, so existing names never change.

A game has one **edition** per system of its copies. The **main edition** is the one the user chose
(**Use as the game's cover**), else one with a physical copy, then the one with the most copies,
then the first by system. A chosen cover whose system has no copy left is dropped, a photo cover
must be of a copy of that system, and a main system without copies is cleared. A game without
copies keeps its chosen cover under the empty system until its first copy appears: it then moves to
that edition if it has no cover of its own, else it is dropped. Merging games keeps the kept game's
covers and main system and fills the gaps from the other. Redundant keys still compare platforms;
play status, rating, notes and custom fields stay on the game.

A key is **redundant** when it is pending (unrevealed or revealed) and the same game already has a `library` copy on the same platform. It is a key you can gift.
Re-scans never move a key you marked as `redeemed` back to pending.

**Physical copies** have a **grade** (sealed, mint, very good, good, acceptable, damaged, or not
stated) and **contents** (box, manual, disc or cartridge, extras), stored as a set. Any copy can have
a **purchase price**: an amount in the currency's minor unit (cents for EUR, yen for JPY, fils for
BHD) and its ISO 4217 code; Game Vault never converts currencies. The **default currency** (System →
Preferences, stored in the `settings` table) fills new prices and CSV rows without a currency; until
it is saved the UI proposes the browser region's currency. Scans never set or clear grade, contents
or price. Game documents are version 3: the free-text `condition` of version-1 documents is
converted when read (the five texts the form used to suggest, in English or Spanish, become a grade
and contents; anything else is appended to the notes as `Condition: …`), and so are version-2
documents (a cover photo goes to its copy's edition, which becomes the main one; a cover URL goes
to the default main edition, unless the photo took it: when both land on the same edition, the
photo wins and the URL is not kept, so clearing that photo later falls to the providers, not to
the old URL). Migration `0010` only triggers the pre-migration backup.

**CSV** (System → import / export) is English only: `title, platform, kind, status, key, redeemBy,
origin, acquiredOn, edition, grade, contents, location, price, currency, notes, links, externalId,
barcode, system, autoLinks` (`autoLinks` are the game's automatic links, see "Automatic links"). `grade` and `contents` take the values above (`very_good`, `box manual media`), `price` a
dot or comma decimal with at most the currency's decimals. Unknown columns and invalid values are
reported per row; the row is still imported. The export writes each copy's effective system. The
import keeps `system` as the copy's own system only when it differs from the copy's effective one
(for a new copy, the one its platform implies), so re-importing an export pins nothing; empty means
automatic, and an import never clears an override the copy already has.

## Configuration

| Flag                 | Env var                     | Default            |
|----------------------|-----------------------------|--------------------|
| `-addr`              | `GAMEVAULT_ADDR`            | `127.0.0.1:8080`   |
| `-config-dir`        | `GAMEVAULT_CONFIG_DIR`      | `config`           |
| `-ui-dir`            | `GAMEVAULT_UI_DIR`          | `web/dist` (served if it exists; empty disables) |
| `-cors-origins`      | `GAMEVAULT_CORS_ORIGINS`    | none (set it when the UI is hosted on another origin) |
| `-backup-interval`   | `GAMEVAULT_BACKUP_INTERVAL` | `24h` (`0` disables) |
| `-backup-keep`       | `GAMEVAULT_BACKUP_KEEP`     | `14`               |
| `-trusted-networks`  | `GAMEVAULT_TRUSTED_NETWORKS`| this computer: comma-separated networks trusted until the security settings are saved from the UI |
| `-reset-auth`        | —                           | off: on start, stop requiring sign-in on trusted networks (forgotten password) |
| `-no-unattended`     | —                           | off: no scheduled scans, session keep-alives, price estimates or background details scan. For a server on a copy of the config: renewing a credential that rotates there kills the real server's |

The log level and log rotation are set in the UI (**Logs** page) and stored in the database. The defaults are `info`, 10 MB per file, and 5 files kept.

The web client calls the page's own origin. To point it at another backend, build it with `VITE_API_URL=https://host:port`.

### The config directory

`config/` (or the `-config-dir` / `GAMEVAULT_CONFIG_DIR` location) holds all of Game Vault's
state, like `/config` in Sonarr or Radarr. It is git-ignored.

| Path | Contents |
|------|----------|
| `gamevault.db` (and `-wal`, `-shm` while running) | SQLite database: games, copies (CD keys in clear text), sources with their credentials, settings |
| `photos/` | The photos of your copies. They exist nowhere else: back them up with the database |
| `backups/` | Database snapshots and their photo store (see [Backups](#backups)) |
| `logs/` | Server logs, rotated by size (limits set on the Logs page) |
| `game-data/` | One folder per game (`<title> [<id>]`): `cover-<system>-<hash>.jpg` per edition (and `.missing` markers), sheet screenshots, trailer posters and `assets.json` (where each image came from). A `cover.jpg` from before editions is adopted by the main edition on first view when it can only be that edition's (see Covers), else deleted. Downloaded again when missing: safe to delete |

To back up Game Vault by hand, stop it and copy the whole directory; `game-data/` can be left out.
Treat the copy like a password manager's file: the database holds store sessions and CD keys.

## Docker image

`botchrishub/game-vault` on Docker Hub, for `linux/amd64` and `linux/arm64`, tagged with the
release version (see [Releases](#releases)). `build/Dockerfile` only packages what the toolchain built
(`bin/release/gamevault-linux-*` and `web/dist`) on `gcr.io/distroless/static-debian13:nonroot`:
CA certificates, time zone data and a non-root user (uid 65532), no shell. `.dockerignore` lets
nothing else into the build context, so `config/` can never end up in an image.

| Task | What it does |
|---|---|
| `task docker:build` | Image for this machine's architecture as `botchrishub/game-vault:dev`, loaded into the local Docker |
| `task docker:publish` | Only on a commit with a release tag and no uncommitted changes. Asks for confirmation, builds both architectures with a `docker-container` buildx builder (`gamevault`, created once) and pushes the release's tags. Run `docker login` first. `IMAGE=…` publishes elsewhere |
| `task docker:publish:dev` | Any commit with no uncommitted changes. Asks for confirmation, builds both architectures like `docker:publish` and pushes them as `botchrishub/game-vault:dev`, replacing the previous dev image. The app shows the commit's `git describe` version (`0.5.0-3-gabc1234`). Run `docker login` first |

**Dev image.** To try unfinished changes on a real server without releasing a version, run the
**Publish dev image** workflow by hand (GitHub → Actions → Publish dev image → Run workflow, on the
branch to build): the same checks as CI, then `task docker:publish:dev`. The `dev` tag is
overwritten every time, so no versions pile up; version tags and `latest` only come from releases.
A server that follows `botchrishub/game-vault:dev` gets the new build with `docker pull` and a
recreated container. The image `task docker:build` loads locally has the same name: pull again to
go back to the published one.

The image sets `GAMEVAULT_ADDR=0.0.0.0:8080` (inside a container the port mapping decides who can
reach it), `GAMEVAULT_CONFIG_DIR=/config` (a volume) and `GAMEVAULT_UI_DIR=/app/web/dist`.

In a container no request comes from `127.0.0.1`: the host's own browser arrives from Docker's
gateway (on Docker Desktop, every client does). So the image also sets
`GAMEVAULT_TRUSTED_NETWORKS` to the private networks (`127.0.0.0/8`, `10.0.0.0/8`,
`172.16.0.0/12`, `192.168.0.0/16`, `::1/128`, `fc00::/7`, `fe80::/10`), the way Sonarr trusts
"local addresses". They only apply until the security settings are saved from the UI; the proxy
and DNS-rebinding rules still hold, so a request with a host name in `Host` signs in.

```bash
docker run -d --name game-vault -p 8080:8080 -v game-vault-data:/config botchrishub/game-vault
```

A named volume is writable out of the box. For a host folder, run as its owner
(`--user "$(id -u):$(id -g)"`) or give it to uid 65532. Arguments after the image name are
passed to the server, e.g. `… botchrishub/game-vault -reset-auth`.

## Releases

Versions follow [SemVer](https://semver.org) and live only in git tags (`v0.1.0`). Everything
else reads them with `git describe`: the binaries (`-X main.version`, shown in **System**) and the
image get `0.2.0` on a tagged commit, `0.2.0-3-gabc1234` on later commits and a `-dirty` suffix with
uncommitted changes.

- **Patch** (`0.2.1`): fixes. **Minor** (`0.3.0`): new features, e.g. a new store. **Major**:
  updating needs the user to do something (a renamed setting or variable, a different port, a
  migration that cannot be undone). `0.x` until the first stable release.
- `task release -- 0.2.0` checks it runs on a clean `main` and that the version is new and higher
  than the last one, runs `task lint` and `task test`, and creates the annotated tag. It pushes
  nothing: then `git push origin main v0.2.0`, and GitHub Actions publishes the image
  (`.github/workflows/release.yml`). `task docker:publish` does the same by hand.
- Image tags for `1.2.3`: `1.2.3`, `1.2`, `1` and `latest`. For `0.x` there is no major tag
  (`0.2.1`, `0.2`, `latest`), and a pre-release (`1.3.0-rc.1`) only gets its own tag. The logic
  is in `build/release.sh`.

## Sources

How each source reads your library. **Test connection** makes one cheap request instead of a
scan: the request of a scan's first library page, with the scan's own page size, and no more
pages; Battle.net's games overview without the classic games; Steam's game count. It
still signs in and renews what a scan renews, so a rotated credential is saved as after a scan.
Fanatical answers every key in one request, which the test reads whole. What a scan leaves out on
purpose (DLC, subscription games such as Game Pass, PS Plus or EA Play, console games on Ubisoft,
software in bundles) is counted in the server log, one line per reason with its count, not in the
scan's warnings: it happens on every scan. Battle.net and EA items without a store id get one from
their title (`game.TitleIDKey`); one whose title has no Latin letters or digits gives no id, so it
is skipped, and that count is a scan warning, since a game the user owns is left out.

- **Humble Bundle:** session cookie `_simpleauth_sess` from your browser. It uses the site's own JSON API, which is undocumented and read-only. Hidden keys are reported as unrevealed and never revealed. Every game key of every order becomes one copy, Humble Choice months included (a chosen game is a key of that month's order). Only keys for a store or console Game Vault knows count as games: software, courses, in-game items and store coupons ("45% off … Coupon") are skipped and listed in the log, and copies of them imported by earlier versions are removed, with their game if nothing else is left in it. Copies are identified by order, game (`machine_name`) and copy number; copies saved by older versions, which identified keys by order and copy number only and so kept one copy per order, are adopted by the right game on the next scan.
- **Steam:** a Web API key plus your profile, given as SteamID64, vanity name or URL. "Game details" must be public.
- **Epic Games Store:** Epic has no public library API, so Game Vault talks to the endpoints the Epic launcher uses, the same way [Legendary](https://github.com/derrod/legendary) and Heroic do (no dependency on them). Open the login link in the source dialog, sign in and paste the `authorizationCode` it shows (or the whole page). The one-time code is exchanged for a session whose refresh token rotates on every use; Game Vault stores it as an internal setting that is never sent to the browser. Sessions are refreshed one at a time, and a refresh token already spent is never sent again: a scan or a test still holding it gets the session that replaced it (`oauthsession`). Scans list the library, read titles from Epic's catalog (cached in memory) and import games only: DLC, add-ons and Unreal Engine assets are skipped. Copies use the platform "Epic Games", so Humble keys for Epic are flagged as spare when you already own the game there. If the session expires after a long time without scans, paste a new code.
- **GOG:** signs in like the GOG Galaxy client, the way Heroic, Playnite and lgogdownloader do (no dependency on them). Open the login link in the source dialog and sign in; GOG then opens an almost blank page on `embed.gog.com/on_login_success?…&code=…`. Paste that address (or just the code). Like Epic, the code becomes a rotating session stored as an internal setting, and scans list your owned games with their titles. Copies use the platform "GOG", the same as Humble's GOG keys.
- **Xbox / Microsoft Store:** signs in like the Xbox app (public Microsoft account client `000000004C12AE6F`, scope `service::user.auth.xboxlive.com::MBI_SSL`), the way Playnite and xbox-webapi do. After signing in, Microsoft opens an almost blank page on `login.live.com/oauth20_desktop.srf?code=…`: paste that address. The code becomes a rotating session; each scan turns it into Xbox Live tokens (user token, then an XSTS token for the Store, relying party `http://mp.microsoft.com/`) and reads the owned games from the Microsoft Store collections: purchases and redeemed codes, without Game Pass ("recurring") entitlements, trials, add-ons or revoked items. Titles come from the public Store catalog, which also gives the **Xbox** cover provider its official portrait posters. Copies use the platform "Microsoft Store / Xbox", the same as Humble's Microsoft keys. Physical Xbox discs are still added by scanning their barcode.
- **PlayStation:** signs in like the PlayStation app (public client of psn-api / PSNAWP): sign in on playstation.com, open `ca.account.sony.com/api/v1/ssocookie` and paste the NPSSO token it shows. It becomes an authorization code and then a session (refresh token) that renews itself (`oauthsession`: one renewal at a time, an access token the library refuses is renewed once); when the refresh token stops working the NPSSO is used again. The NPSSO stays in the settings: it is not a one-time code. Scans read the games bought with the account (PS4 and PS5, no PS Plus games) from the library website's GraphQL API, whose queries Sony only runs by registered hash (two known versions are tried). That list also holds apps (streaming, music), demos, soundtracks and videos as PS4 / PS5 titles, with nothing in it to tell them apart, so each scan also reads the PlayStation app's played-titles list (`m.np.playstation.com/api/gamelist/v2/users/me/titles`, with the same access token) twice: all played titles, and only the games (`categories=ps4_game,ps5_native_game,pspc_game`). A purchased title that matches a played title (by title id, a title id of its concept, or the concept id) found in the first list but not in the second is not a game. Titles never played stay, and if either list cannot be read the scan goes on without this check (logged as a warning). Names that clearly mark a non-game are left out too: ending with "Demo", "Trial", "Beta", "Soundtrack" / "OST", "Theme", "Avatar(s)", "Art Book" (or the Spanish equivalents), after a space, dash or colon or in trailing parentheses or brackets, and "Behind the Scenes" or "Making of" anywhere; the same words at the start of a name count when a joiner follows ("Banda sonora de …", "Soundtrack of …", "Original Soundtrack: …", "Demo - …"), and ™ / ® are ignored. Excluded titles are returned as withdrawn, so copies imported earlier are removed, and their names are logged. The store catalog would know each title's kind, but playstation.com's terms forbid automated access to its pages, so it is not read. Copies use the platform of each game ("PS4", "PS5"), so their covers come from TheGamesDB's box art for that console, or from Steam when you also own the game there.
- **Amazon Games / Prime Gaming:** signs in as the Amazon Games launcher, the way nile (Heroic's Amazon backend) does (no dependency on it). The sign-in link registers a launcher "device" with PKCE; its serial and challenge are made when Game Vault starts and kept in memory, so the link changes on every start and a code only works with the link of the running server (reload the page if it fails). After signing in, Amazon lands on amazon.com with `openid.oa2.authorization_code=` in the address: paste that address. The code registers the device (`api.amazon.com/auth/register`) and gives a refresh token that does not rotate, so a test copy or a second server cannot invalidate it; Game Vault shows up as an Amazon Games device in the Amazon account, and removing it there signs Game Vault out. Each scan gets an access token and reads the entitlements the launcher uses (`gaming.amazon.com/api/distribution/entitlements`, 50 per page): games bought and games claimed with Prime Gaming, one copy each on the platform "Amazon Games", linked to Steam when Amazon gives the game's Steam page (so covers and sheets come from Steam). Products without a title are skipped with a warning. Prime Gaming codes for other stores (GOG, Epic, Legacy Games) are not entitlements: those games come in when that store is scanned. Amazon has no portrait box art, so there is no Amazon cover provider.
- **itch.io:** the one store here with an official API for this. Paste a personal API key (itch.io → Settings → API keys; such keys are unscoped). Each scan reads `GET https://api.itch.io/profile/owned-keys` with `Authorization: Bearer <key>`, 500 keys per page, until a page comes back empty (the documentation warns that a short page is not the last one). Every download key the account holds (bought, claimed for free, from a bundle or a gift) becomes one library copy per game on the platform "itch.io" (the name Humble's itch.io keys use), linked to the store by the game's numeric id (`itch:<game_id>`, no store page: itch.io pages are addressed by subdomain and slug). A game held through several keys is one copy dated by its earliest key, whose origin says whether it was a purchase (`purchase_id`). Items whose `classification` is not `game` (tools, assets, soundtracks, comics, books…) are counted in a warning and skipped; a missing classification is taken as a game. A bogus key gets HTTP 403 `{"errors":["invalid key"]}`, no key HTTP 401; errors may also come with HTTP 200 in the same `errors` list. itch.io's Lua backend writes an empty list as an empty object, so `owned_keys` is read as an array, an object of entries (in numeric key order) or `{}`. itch.io has no portrait box art (its covers are 315×250), so there is no itch.io cover provider.
- **Fanatical** ⚠️: Fanatical has no API for other apps, and its Terms and Conditions forbid "taking data from our website for your or third party databases or data harvests without our consent". Game Vault still offers it, read-only, the way the Playnite Fanatical plugin works, because the user decides: the settings dialog opens with a risk notice quoting that clause and linking the terms, and the source cannot be tested or saved until "I understand the risk" is ticked (a `consent` setting, `schema.FieldConsent`, checked by the server too). New Fanatical sources scan only when asked (the type sets `ManualScans`); scheduling is the user's choice. Paste the `bsauth` value from the site's Local Storage (or just its `token`); each scan is one `GET /api/user/keys` with `authorization: <token>`, never a reveal or a redeem. Game keys become key copies on the store the key is for (`drm` flags: Steam, Epic Games, GOG, Ubisoft Connect, EA App, Rockstar, Microsoft Store / Xbox, PlayStation Store, Nintendo eShop, or PC for DRM-free), with the key itself once you have revealed it on the site (`key`), the redeem deadline (`serialExpiry`), the purchase date and, for keys that came in a bundle, the bundle's name in the origin ("Fanatical – Overlord: Ultimate Evil Collection"); DLC, software, books, audio and vouchers are counted in a warning and skipped. A bundle bought as one product is listed too, "fulfilled" and without a serial, next to the keys it was split into: it is not a key, so it is not imported (and a copy imported for it earlier is withdrawn). The key's state comes from `status` (revealed, redeemed, else unrevealed); the item shape is confirmed with a real account, the status words of unrevealed and redeemed keys not yet. Fanatical answers a missing or rejected session with a plain 401.
- **Removing imported items:** the trash button on a copy a source imported asks to remove it from that source and not import it again (the store may list things that are not wanted: apps, demos, soundtracks). The copy goes, and its game with it when no copy is left. Its id at the source is added to the source's removed list, stored in the source's document with the title and the date. Every scan turns the listed items into withdrawn copies before consolidating, so they never come back, even when the store changes its id format: an item's old id (`PreviousExternalID`) counts too, unless several items report the same old id (Humble's old ids named an order), in which case only the item with the removed title is skipped. The scan report counts them ("N removed by you"). The source's dialog lists them under "Removed from this source", each with **Import again**, which takes it off the list for the next scan. Copies added by hand are deleted as before. A scan reads the source again before consolidating and before saving its report, so an item removed, a setting changed or a new sign-in made while the store is answering is kept; the session is written only when the store rotated it during the scan. If that final save fails, it is logged as an error naming the source.
- **Keeping browser sessions alive:** Battle.net, EA and Ubisoft reuse a website session, which expires when idle. Game Vault keeps them alive like an open browser tab: a light request roughly every 15 minutes (Battle.net), every hour (EA) and every 2 hours (Ubisoft), saving whatever the site renews (`sync.KeepAliver`). Intervals vary at random by ±30% and the first keep-alive after start-up comes at a random moment within 10 minutes, so requests never follow a fixed rhythm. Scheduled scans vary the same way: ±10% of their interval (±2.4 hours for a daily scan), and sources that are due when the server starts are scanned at random moments within its first 30 minutes. A failed keep-alive is logged; if the site no longer accepts the session, the next scan shows the error and you paste new cookies.
- **Battle.net:** Blizzard's public API has no list of owned games and its sign-in has a captcha, so Game Vault reuses your browser session on account.battle.net, the way Playnite does. Paste the `Cookie` request header of account.battle.net (DevTools → Network → the `overview` request). Scans read the "Games & subscriptions" and "Classic games" data the site loads, one copy per game even if you have it in several regions. When the site session expires, Game Vault renews it through Battle.net's single sign-on (the longer-lived login cookie) and keeps the renewed cookies as an internal setting (`internal/adapters/outbound/browsersession`, shared with the EA app); paste new cookies only when scans say you are signed out.
- **EA app:** the old Origin API is shut down and EA's sign-in has a captcha, so Game Vault reuses your browser session on accounts.ea.com. Paste the `Cookie` request header of the login-check link in the source dialog (it shows `access_token` when you are signed in). Each scan signs in silently with those cookies (EA's web SDK flow, `client_id=ORIGIN_JS_SDK`, `prompt=none`), keeps any cookie EA renews, and reads `me.ownedGameProducts` from the GraphQL API the EA app uses: full PC games from EA's own store, one copy per base game with the product name as the edition ("Revolution"). EA Play subscription games are skipped. Copies use the platform "EA App", the same as Humble's Origin/EA keys.
- **Ubisoft Connect:** no public library API and a captcha on sign-in, so Game Vault reuses the session of connect.ubisoft.com, the way Lutris and the GOG Galaxy plugin do. Sign in with "Remember me" ticked and paste the login data the site keeps in Local Storage (the `PRODrememberMe` entry, which holds `rememberMeTicket`). The login link uses ubisoft.com's own app id: connect.ubisoft.com's app (`314d4fef…`, used by Lutris and the GOG Galaxy plugin) is no longer allowed to sign in on Ubisoft's public gateway. Ubisoft replaces the remember-me ticket every time it is used, so Game Vault spends it as little as possible: it reuses a session opened in the last hour, then the ticket and session id pasted, and only then renews the remember-me ticket, keeping the new one in memory (so "Test" before "Save" does not burn the pasted value) and as an internal setting. Copy it from a private window, so the renewals do not sign your normal browser out. Each renewal exchanges the remember-me ticket for a session (`POST /v3/profiles/sessions`, trying ubisoft.com's app and then the Ubisoft Connect PC client, remembering which works) and keeps the rotated ticket as an internal setting, then reads `viewer.games(filterBy: {isOwned: true})` from the Ubisoft Connect GraphQL API. PC games only: console games linked to the account are skipped; if Ubisoft rejects the query (renamed platform fields), a minimal query without them is used; a rate limit or an outage is not retried that way. Copies use the platform "Ubisoft Connect", the same as Humble's Uplay keys.
- **Tarkov and other launchers** have no public library API. Add those games by hand or with a CSV import.

## Connecting sources with the browser extension

Most sources need a credential from the browser (a session cookie, a value a site keeps, or the
code a store redirects with). The **Game Vault Connector** extension (`extension/`, plain JavaScript,
Manifest V3, Chrome and Firefox; loaded unpacked for now, `task extension:pack` zips it for the
stores) collects it from the store's own sign-in, in the user's own browser, and hands it to the
Game Vault tab that asked. It never signs in for the user and never works around bot protection.

- **Recipes.** The extension knows no store. Each credential field declares a `schema.SignInRecipe`
  (data, never code), sent to the page as `SettingField.sign_in` (JSON; an empty `open` becomes the
  field's help link, which is how Amazon's per-run link is used). A recipe has a version (1), the
  address to open, optional extra `hosts`, `private` (prefer a private window: Ubisoft), a timeout
  (`timeoutSeconds`, default 300, at most 600), an optional readiness condition `when`
  (`urlPrefix`, `contains`, `fetch`) and exactly one capture: `cookie`, `cookies` (as a Cookie
  header), `storage` (a localStorage value, optionally only on a `path`), `redirect` (a query
  parameter of the address the store redirects to) or `fetch` (a JSON field fetched with the
  session). Every address is https and on the opened host or a listed one; Go
  (`SignInRecipe.Validate`, run by `plugintest` for every source) and the extension check it.
  Cookie and storage captures need a readiness condition (anonymous values exist before sign-in).
  A redirect recipe takes no condition (the redirect is one), and a private recipe cannot fetch
  (the extension's requests carry the normal window's session). Prefixes (`urlPrefix`, a storage
  `origin` + `path`, a redirect `prefix`) match only up to a `/`, `?` or `#`: `/ready` does not
  match `/readyX`.
- **Protocol.** A bridge content script, registered only on Game Vault addresses the user enabled,
  relays `window.postMessage` requests (`type: "gamevault-connector"`, `dir: "request"`, ops
  `hello`, `connect`, `cancel`) to the background over a port, and the answers back (`hello` says
  the version, the recipe version and whether private windows are allowed; `result` with
  the value, or `error` with a code: `denied`, `busy`, `invalid`, `unsupported`, `cancelled`,
  `timeout`, `failed`). It checks the message's window and origin; the background checks the
  sender's exact origin (port included) against the enabled list. Only the tab that started a
  sign-in can cancel it. The bridge pings the port every 20 seconds so a long sign-in keeps the
  service worker alive; closing the dialog or the page cancels the sign-in and closes its tab. The
  page gives up a minute after the recipe's own timeout.
- **Permissions.** No host permissions at install. A Game Vault address is enabled from the
  extension's popup (the browser's prompt). The first time an address asks for a store host, the
  extension's own window asks Allow / Deny (and remembers it); Allow also requests the browser's
  permission for that host. The popup lists and revokes both. The extension stores only the enabled
  addresses and confirmed hosts, never credentials.
- **Dialog.** With the extension enabled, each field with a recipe shows **Connect**: the value
  fills the field, Test connection runs, and the source is saved when it passes (otherwise the
  error is shown and the value kept); Test, Save and Delete wait while it runs. A private recipe
  says whether it will open in a private window or, when the extension is not allowed in
  incognito, that the browser will be signed out of the store. Every field has **Open the sign-in page**; without the
  extension, a hint links to `extension/README.md`. Every credential field also accepts what is
  naturally copied (a whole address, the JSON a page shows, a whole `Cookie` header…).

## Do I own it?

One lookup answers "do I already have it?" for store pages, bundles and pasted lists.

- **Lookup.** `OwnershipService/CheckOwnership` (`internal/application/ownership`) takes up to 200
  items, each `{store, id}` (a link key such as `steam` or `gog` and the game's id there), a
  `{title}`, or both, and answers each, in order: **owned** (one game), **possible** (up to five
  candidates) or **not owned**. Matching: a store link equal to `{store, id}` (automatic links
  included), then the exact title (`game.SameTitle`, indexed by `game.TitleKey`), then the title
  without edition and store tags (`game.MatchKey`), which only ever says possible: a single match
  found only that way is "possible", never "owned". Only current
  copies count: sold, given away and expired ones are left out, and a game with no other copy is
  not owned. An owned game comes with its copies (platform, system, kind, pending key) and
  `#/game/<id>`, the address that opens its sheet in the web UI. The lookup only reads (no
  provider or network calls, nothing saved); a request is at most 256 KiB.
- **Bundle checker.** Library → **Do I own it?** cleans a pasted list (`web/src/lib/ownership.ts`:
  one title per line; bullets, numbering, prices, discounts and currency signs removed; lines
  marked as DLC or soundtrack skipped and counted; blank, overlong and duplicate lines dropped),
  checks it 200 titles at a time and lists every title as owned (badges, pending key, a link to
  the sheet), possible (its candidates) or not owned, with "You own N of M" and **Copy the ones I
  don't own** (possible ones are not copied). Nothing is saved; on phones the dialog is full
  screen, the list above the results.
- **Store labels (browser extension).** With **Show what I own on store pages** on, the extension
  asks the browser for `store.steampowered.com`, `www.gog.com`, every `humblebundle.com` site
  (`*.humblebundle.com`, so language sites such as `es.humblebundle.com` work) and
  `www.fanatical.com`, and registers one content script per allowed site (`extension/ownership/`);
  turning it off unregisters them and gives back the sites no sign-in uses. Store scripts
  registered by an older version are replaced on update. The syncs of the scripts (option or
  permission changes, start-up, update) run one at a time, and a store the browser refuses does
  not stop the others (the failure is logged without any setting). What each page shows
  is data in `extension/ownership/stores.js`: Steam product pages give the AppID from
  `/app/<id>` and the title, GOG product pages the product id (`card-product`) and the title,
  Humble and Fanatical product pages the title, and Humble (`/games/…`, and the members' Humble
  Choice month pages, `/membership/home`, read as bundle pages) and Fanatical
  (`/bundle/…`, `/pick-and-mix/…`) bundle pages every game's title. A debounced MutationObserver
  follows single-page navigation and content drawn late. Product pages get a label after the
  title ("In your collection: PS4 · disc, Steam · key not redeemed", a link to the sheet in a new
  tab; "Possible: …"; a quieter "Not in your collection"); bundle pages a mark on every game
  ("Owned on PC", "Key not redeemed (PC)", "Maybe owned", empty when not owned) and "You own N of M" by the heading,
  counting different titles. When Game Vault cannot answer (not running, an error, the token
  refused, not set up) a label says **Game Vault unavailable** with a hint, never "not in your
  collection". The store page must not learn what you own: labels and bundle marks are drawn in
  closed shadow roots with their own styles, and every state has the same size, the same single
  tab stop and the same event handling (long text is ellipsized, the detail is a tooltip inside
  the root), so the page cannot read the answer from the layout, the focus order or events. Only
  the background talks to Game Vault: it sends `{store, id, title}` to the chosen address (one of
  the enabled ones), with the access token, 200 items per request, and reuses answers for a
  minute (changed settings or a refused token clear them). Requests carry no cookies
  (credentials omitted), never follow a redirect (one would carry the token to another address;
  Game Vault then reads as unavailable) and go only to that address. The token is bound to the address it was
  saved for: changing the address clears it and turns the option off. Where the browser supports
  it, extension storage is limited to trusted contexts, so content scripts cannot read the token.
  The popup warns when the address is plain http and not this computer (the token would travel
  unencrypted). Texts follow the browser language
  (en, es). The selectors were checked on the live sites (date in `stores.js`); when a store
  changes its pages, fix `stores.js` and the matching sample in `extension/tests/samples/`.
  On Fanatical the labels only read the page the user is viewing and store nothing.

## Metadata providers and covers

For each kind of data, the enabled providers are tried in the order you set on the **Providers** page, and the first one with an answer wins.
Each provider declares which games it applies to. That keeps providers with a quota away from games another provider covers better.

| Cover provider | Applies to | Notes |
|---|---|---|
| Chosen cover | the edition it was chosen for | Always first. Set with **Choose cover…** (a candidate, or a photo of one of the edition's copies) |
| TheGamesDB | any system, searching that system's box art: physical copies, editions no store knows, and on the fallback pass | Platform-specific box art (Xbox 360 case, PS3 case…). Needs an API key, which has a monthly allowance. **Test** checks the key without spending it, and says so when this month's allowance is used up. Once it is used up TheGamesDB answers every request with HTTP 429; on a 429 Game Vault checks the allowance (for free) and, if none is left, sends nothing more with that key until it renews (the refresh time TheGamesDB reports, or 6 hours when it reports none). Meanwhile game sheets say when it renews, and covers from it are simply not found until then. A new key, or a **Test** that finds allowance left (extra allowance bought), lifts the block at once |
| Steam | the PC edition of games linked to Steam; on the fallback pass, any PC edition without a Steam AppID | No key, no quota. Without an AppID it searches the Steam store by title (one request) and uses the only app with exactly that title (`game.SameTitle`: the same words, ignoring case, accents and punctuation), or, when none has it, the only one with the same `game.MatchKey` ("Mafia" is never taken for "Mafia: Definitive Edition" when "Mafia" exists); an inexact title or two apps give nothing. The AppID found is used for that cover only, never stored as a link. Kept for the fallback pass, so the search is only spent on PC editions nothing else had art for. Steam's store host allows about 200 requests per 5 minutes: its requests (the store search and the app details of Steam game sheets) share one throttle (one request at a time, a burst of 10, then one every 1.5 s; a request that would wait more than 5 s, for its turn or its pace, is answered rate-limited, and after a 429, a 5xx or no answer the store host is left alone for a minute, or longer when its `Retry-After` asks for it), and a search's answer is reused for 10 minutes under a key without case, extra spaces or a trailing "(year)" (Battle.net's covers and Steam's title search of the same game cost one). The art lookups (IStoreBrowseService on the Web API, which has its own higher limits) are not throttled, and when they fail the predictable CDN URLs are used, as before. A 429, a 5xx or no answer counts as a rate limit, so no miss is remembered |
| Epic, GOG, Ubisoft, EA, Battle.net | the PC edition of games linked to that store | Official box art, no key, no quota (see below) |
| Xbox | the PC and Xbox editions of games linked to the Microsoft Store | Official box art, no key, no quota (see below) |
| IGDB | any edition, on the fallback pass | The game's cover from IGDB, the same for every system: a last resort, so a console edition no other provider had art for gets the game's (often PC) box rather than none. Disabled until enabled with the Twitch application set up for IGDB's game sheets (see below); entering the credentials from either tab, or enabling it, retries the editions marked without cover. Twitch or IGDB not answering (5xx, no connection) counts as a rate limit. Last in the chain (after TheGamesDB and the stores) |
| SteamGridDB ⚠️ | any edition, on the fallback pass (never a virtual PC box) | Community-made portrait grids, **at the user's own risk** (see below). Disabled by default; needs the user's own API key and a ticked risk consent. After IGDB, so it is the very last resort |

Covers are resolved per edition, in this order: the chosen photo, the chosen URL, the provider chain (with the fallback pass, which runs whenever the first pass found nothing, for every game: those without store links too), the base game's cover for the same system (add-ons), else "missing". The first image found is stored in the game's folder in `config/game-data/`; a miss is remembered for 7 days per edition so quotas are not spent again. A provider that asks to slow down (HTTP 429, `provider.ErrRateLimited`) says nothing about the game: the edition has no cover this time, no miss is remembered and no base game's box is borrowed, so the next request asks again; the cover address then answers `503` with `Retry-After: 30` and `Cache-Control: no-store`. TheGamesDB's requests (covers and details) go one at a time, at least 500 ms apart, so a library page full of new console editions does not reach it as a burst. A lookup that would wait more than 5 s for its turn (for the pace, or for a slow request under way), or find 8 already waiting, is answered rate-limited at once, and after a `429`, a 5xx or no answer TheGamesDB is left alone for 30 s, or longer when its `Retry-After` asks for it (at most an hour; lookups meanwhile are rate-limited too). A lookup whose caller gave up first (a closed page, a deadline) starts no such rest: the missing answer is not TheGamesDB's. A `429` also checks the key's allowance through `API/Limit`, which costs nothing, while still holding the queue's turn (lookups queued meanwhile wait for the answer), one check at a time and at most once every 5 minutes after a successful one. When it is used up (TheGamesDB answers 429 "Unknown Error Code" then, not 403), TheGamesDB gets no request with that key at all, not even a queued one, until the allowance renews; lookups fail at once with `provider.ErrQuotaExhausted` (a kind of `provider.ErrRateLimited`, so nothing is cached or remembered as missing, but without the 30 s back-off) and a message saying when it renews; the cover address then answers `503` with `Retry-After: 3600`. The state is kept per key, and **Test** updates it. An unknown key (no allowance and a zero refresh timer) gets an unknown-key error instead, which still counts as a rate limit: nothing is remembered from a `429`. **Test** bypasses that queue. Only image downloads take one of the six download slots, not provider lookups, so lookups queued behind TheGamesDB never hold up other providers' covers. A cover from before editions (`cover.jpg`) was resolved for the whole game, so it may show another system's box. The main edition (or a game's cover under the empty system) adopts it on first view instead of downloading again only when the game has at most one system, or when the main edition has a chosen cover (a URL or a photo); otherwise the file is deleted and the main edition resolves its own. Making another edition the main one deletes it too, so the new main edition never adopts the old one's image. An edition's cover address (`/media/covers/{id}/{system}`) serves the cached file without reading the game: a scan or an edit that changes or removes an edition deletes its file. Once the main edition has resolved, and on **Retry games without cover** (or a change to the cover providers), the game's leftover "no cover found" markers are deleted: those of systems it no longer has, and `cover.missing` from before editions.
`GET /media/covers/{gameId}/{system}` (the system URL-escaped) returns an edition's cover and `GET /media/covers/{gameId}` the main edition's; a miss is a 404 with `no-store`. `GameQuery.System` carries the edition's system and each provider's `Applies` decides by it (`ForPC()` for the store providers).
A scan or an edit drops the cached covers only of the editions whose cover inputs changed: their chosen cover, their copies' platforms (or whether one is physical) or the game's store links. Adding a disc or a key to an edition does not refetch its cover, which spares TheGamesDB's quota. Choosing an edition's cover keeps the cached sheet; only deleting the game drops it.
Changing, enabling or reordering a cover provider forgets the misses.
**Choose cover…** in the game sheet shows every enabled provider's proposals for the edition's system, and the photos of that edition's copies, and pins the one you pick. Unlike the automatic resolution, it always asks the providers kept for the fallback pass too (TheGamesDB for a game a store knows, IGDB, SteamGridDB), even when a store had art: the user asked to see every choice. Each provider is asked once per opening: the candidates of the providers that apply to the first pass (the stores' own art) come first, then the fallback ones, each group in chain order. TheGamesDB may cost one or two requests per opening (the search, and the allowance check after a 429); its allowance rules still apply. Only the places where the user picks from a list ask providers to browse (`GameQuery.Browse`): this dialog and the title typed on the scan page. TheGamesDB then also lists the other games its search found ("Halo 3: ODST" for "Halo 3"), after the game's own releases; the automatic resolution and the barcode lookup only ever get the game's own box (the same title, or the same `game.MatchKey`: "Halo 3 (Platinum Hits)"). The dialog keeps each edition's candidates while it is open, so switching back to an edition asks nothing again.
To find one by hand, **Search Google Images** (the title without edition and store tags, the edition's system and "box art", large tall images) and **Search SteamGridDB** open in a new tab; the server never queries either (their terms forbid automated access). An image dragged from that tab, or its address pasted anywhere in the dialog, fills the image address (only http(s) addresses; a Google thumbnail or a `data:` image gets a hint to open the full-size image), and the browser previews it directly, without sending a referrer, before **Use this image** has the server download it.
Images are plain HTTP rather than RPC so browsers can load and cache them with `<img>`. Everything else goes through Connect.
For games no store knows (physical games, for example), link them under **Edit → Store links** (each provider that can search its store offers **Search…**), or paste any image URL.

Store covers: every library source links the games it imports to its store (Steam, Epic, GOG, Battle.net, EA, Ubisoft, Xbox, PlayStation, Amazon), and **Edit → Store links** shows and edits those links. Store cover providers read them, so a game linked by hand gets the art too. **Epic Games Store** and **GOG** give the official box art of games linked to those stores, with no key and no quota. Epic's comes from its catalog, read with an application token (client credentials, no user session); GOG's from its public product API. **Ubisoft** gives the box art Ubisoft Connect shows (by space id, on Ubisoft's CDN, which has art for most but not all games), else the Ubisoft Store packshot found by title through the store's public search (the search-only key every store visitor's browser gets); it also covers games with a Ubisoft Connect key. **EA** gives the official pack art from EA's catalog (public in the EA app API), looking the game up by slug guessed from its title and product id. **Battle.net**: Blizzard publishes no box art outside its signed-in shop, so games imported from Battle.net are looked up on the Steam store with Steam's own title rules (the only app with exactly that title, else the only one with the same `game.MatchKey`, without the year Battle.net adds). TheGamesDB is only asked for physical copies, games no store knows, and, on a second pass, store games no store had art for (World of Warcraft, StarCraft II…), so its monthly allowance is not spent on games stores cover.

**Automatic links.** A game without a Steam link (a console disc, a PlayStation library game) gets its store links found automatically, so Steam's sheet and box fill in without linking it by hand. Providers that implement `media.LinkFinder` are asked among the enabled ones in the user's chain order, the details chain's first (a sheet provider names the ids of the game it matched), then the covers' (a title search only guesses): IGDB first (the Steam id among the external games of the game it matched for the sheet, by a store link it knows or the only game with exactly the title, the main one over its editions; GOG and Epic ids are left out while their formats are unverified), then, when IGDB is not configured or gives no Steam id, Steam's store search (only the one app with exactly the title or the edition's search title, never a looser match; skipped for games whose copies are all on systems older than PS4 / Xbox One / Switch or on handhelds, which Steam rarely sells under the same name). A finder without its settings is skipped; one that asks to slow down, is unavailable, times out or fails in any other way stops the search, which waits an hour in memory: a game is marked as searched, and left alone for 30 days (`Game.AutoLinkTriedAt`, stored without changing the game's update time), only when every enabled finder answered. Searches run in one background worker (`media.AutoLinker.Run`, stopped with the server): a sheet or a cover answered for a game without a Steam link queues it, and so does the background details scanner for the games that are due, without waiting; the queue is a set, so a game is queued once. The worker takes one game at a time, waits while covers are being downloaded, pauses 2 to 4 seconds (jittered) between games and 15 minutes ±20% after a search it ran was stopped by a rate limit or a failure; games still waiting their hour are not queued, and a game deleted after it was queued is skipped quietly. The provider calls run outside any transaction; the catalog then re-reads the game, adds the links and saves it in one (`catalog.AddAutoLinks`, the auto-linker's `GameLinker`), so an edit made meanwhile is never lost. Links found are added only where the game has none (an invalid id is skipped and logged, without losing the valid ones found with it), marked automatic (`Game.AutoLinks`, `Game.auto_links` in the API, an "automatic" badge on the Edit tab), and the game's cached covers and sheet are dropped so the store's replace them. The user wins: a link they set or change is theirs (the API's `UpdateGame` changes links only when `links_set` is true, and the Edit tab reloads the game when opened and, when the game changes while it is open or on save, keeps only the fields and links the user changed in the form and takes the rest from the game as it is now, so a link found meanwhile is never removed by a save and a reload never overwrites what the user typed), and an automatic link they remove is remembered as rejected (store and id) and never added again, unless they set it by hand. A source's import replaces an automatic link with its own. The CSV export writes them in their own `autoLinks` column, and the import adds them back as automatic (an invalid entry in that column is skipped; it no longer drops the valid ones of the same row); merging two games keeps the link that is not automatic when both have one for a store. Automatic links are never used to recognize a game in imports (the consolidator matches by the user's and sources' links only), so a guessed link cannot merge two games. They are stored as additive fields of the version-3 game document (`autoLinks`, `rejectedLinks`, `autoLinkTriedAt`).

**The PC box as a last resort.** A game with copies but no PC edition that is linked to a store selling PC games (`game.IsPCStoreLink`: Steam, GOG, Epic, EA, Battle.net, Ubisoft, Xbox, Amazon, itch.io; `Game.has_pc_store_link` in the API). The Microsoft Store sells for PC and Xbox under one id, so for a game with an Xbox edition an Xbox link alone does not count: its art is that edition's own. Such a game has a virtual PC edition: `GET /media/covers/{id}/PC` resolves it like a real one, cached and marked missing under PC with the same rules, but finds the game by its store links alone (`GameQuery.LinksOnly`): only the providers that apply to a PC edition are asked, in the first pass; none searches by title (Battle.net's Steam search, Ubisoft's store search, Steam's title search, TheGamesDB and IGDB keep out), no fallback pass runs and no console platform reaches the providers. It borrows no base game's cover (add-ons), so it is the store's art or nothing. Its cover inputs are the store links (`CoverFingerprints` has a PC entry while the game borrows the box), so changing or removing the link drops it. Without a PC-store link the address stays 404. The web appends PC as the last cover a console edition tries (`coverOrder`), after its own and the other editions', so a PS4 disc of a game linked to Steam shows Steam's art labelled "PC box" with the hero's borrowed-box note; a game with a real PC edition already tries it and is not changed. A test in `cmd/gamevault` checks that every store whose cover provider has PC art reads a PC-store link key.

**SteamGridDB** ⚠️. SteamGridDB has an official API v2 with personal keys, used by open-source launchers (Playnite, Heroic, Lutris), but its Terms of Use (effective 2019-07-31) allow only personal, non-commercial use "solely as permitted by the normal functionality of the Service" and forbid to "make any automated use of the Site, the Service or the related systems". The maintainer decided on 2026-10-11 to offer it anyway, as a full cover provider, with the risk made unmissable, as for the Fanatical source: the settings dialog opens with a risk notice quoting the clause and linking the terms, and the provider cannot be tested or enabled until "I understand the risk" is ticked (a `consent` setting, `schema.FieldConsent`, checked by the server too); it needs the user's own API key (sign in with Steam on steamgriddb.com → Preferences → API → Generate API Key) and starts disabled. The user accepts the risk, not the authors: SteamGridDB could revoke the key or suspend the account. It applies only on the fallback pass, for any system, and never to a virtual PC edition (`LinksOnly`). A game linked to Steam is looked up by its AppID (`GET /api/v2/grids/steam/{appid}`); one SteamGridDB does not know by its AppID (404), or any other game, by its title (`GET /api/v2/search/autocomplete/{term}`, with `game.SearchTitle`, then the raw title when that finds no match), keeping the only game named exactly like the copy (`game.SameTitle`: the same words, ignoring case, accents and punctuation, so "Example - A Wild Adventure" is "Example: A Wild Adventure" but "Re-Volt" is not "Revolt"), or else like the copy without its store and key tags ("Example Quest (Steam Key)", "[PS2]"); edition words and years are never dropped for the match, so "Example Remastered" or "Doom (2016)" never gets the original's box, and two games with the name give nothing. Then `GET /api/v2/grids/game/{id}`. Grids are asked as `dimensions=600x900&types=static&nsfw=false&humor=false&epilepsy=false`; the three with the highest score are proposed, labelled "SteamGridDB: community art by <author>", from `cdn2.steamgriddb.com` (declared for the image proxy). The key goes in `Authorization: Bearer <key>`, never in the URL. Requests go one at a time, at least 1 s apart, each with an 8 s timeout; a lookup that would wait more than 3 s (for the pace, or for a slow request under way), or find 3 already waiting, is answered rate-limited at once, and after a 429, a 5xx or no answer SteamGridDB is left alone for a minute, or longer when its `Retry-After` asks for it (at most an hour). A JSON 401/403 is a settings problem ("SteamGridDB rejected the API key: check it in the provider settings"), and that key is not sent again for an hour unless the user tests it or enters another; a 429 is a rate limit; a 5xx, no answer, an HTML page (Cloudflare answers 401/403 pages too) or an answer of an unexpected shape is `provider.ErrUnavailable` (the latter with a "report this message" text); so System → Health shows them and no miss is remembered. **Test** sends one search, even with a key rejected earlier; it works on a 200 or the API's own JSON 404. The image proxy accepts the hosts of every compiled-in provider, enabled or not (the store-link search shows thumbnails from stores whose cover provider may be off).

**Which providers search by title.** Most store providers only read a store id the game already has (Epic, GOG, EA, Xbox need their link; Steam its AppID; Ubisoft also covers Ubisoft Connect keys), so a key from Humble or Fanatical for another store, a PC game without a Steam AppID or a console edition had only TheGamesDB, whose allowance can run out. These providers search by title: **TheGamesDB** (any system, first pass for games without store links and physical copies, fallback pass otherwise), **Battle.net** (the Steam store, for games linked to Battle.net) and, on the fallback pass, **Steam** (its store, for PC editions without an AppID), **IGDB** (any edition, the game's cover) and, when the user enabled it, **SteamGridDB** (any edition, community art). The fallback pass skips the providers the first pass asked, so a game without store links, which TheGamesDB already got in the first pass, costs no more of its allowance. The **Choose cover** dialog always asks the fallback providers too, which for a game linked to a store includes TheGamesDB (one request of its allowance on the user's demand, two when the title has edition or store tags and the game's name finds no exact title: the raw title is searched too). Every exact title match (Steam, IGDB, Wikipedia, SteamGridDB, TheGamesDB) uses `game.SameTitle`: the same words, ignoring case, accents and punctuation ("&" is "and"), so separators do not matter but words are never glued together.

**Provider status.** Problems of a provider rather than of a game are not shown on game sheets, in the Choose cover dialog, in the scan page's title suggestions or as a copy's price warning: a rate limit or a busy throttle (`provider.ErrRateLimited`), a used-up allowance (`provider.ErrQuotaExhausted`), a service not answering (`provider.ErrUnavailable`, a 5xx or no connection) and settings the service rejects (`provider.ErrBadSettings`, an unknown key or wrong credentials). The providers registry (`providers.Registry`) keeps each service's last such problem in memory (by settings group, so TheGamesDB's covers and details share one), with its message, since when and until when: the time the error gives (`provider.Resumer`, TheGamesDB's allowance renewal), else 5 minutes for a rate limit or an outage and an hour otherwise. Price providers report to it too (the valuation service calls `ReportResult`, a "not listed" answer counting as success). The next successful answer of that service (a lookup or **Test**), new settings, or that time passing clears it; a provider without its settings answers "settings", never an empty success, so it cannot clear a sibling's status. TheGamesDB's rejected key and server errors, and Wikimedia's refusals, server errors and missing answers, count as provider problems too. So do a 429, a 5xx or no answer from Steam's sheet, the store cover providers (GOG, Epic, EA, Xbox, Ubisoft) and the barcode providers (UPCitemdb, EAN-Search, eBay, CeX), classified by one shared helper (`apiclient.ClassifyError` / `ClassifyStatus`), so an outage of a store never marks its covers missing; UPCitemdb's daily limit (`EXCEED_LIMIT`, not its per-minute `TOO_FAST`) and EAN-Search's used-up credits are a used-up allowance, and a key EAN-Search, UPCitemdb or eBay rejects is a settings problem. EAN-Search takes its token in the URL, so its errors never show the URL's query. The cover picker says "Some providers are unavailable right now" with a link to System → Health when it dropped one. The Providers page reloads its chains on window focus. The Providers page shows them as a badge on the affected providers (`Provider.status` in `ListProviders`), translated ("monthly allowance used up; renews in 27 days"), with the provider's own message, and System → Health lists them (see Health). The caching rules do not change: a sheet a provider was too busy to fill is still not cached, and a failure for one game is still a warning on its sheet.

**Answers that cannot become a marker.** An edition is only marked "no cover found" when no provider asked to slow down. While one is limited (TheGamesDB's allowance used up), the providers that did answer "no cover" are remembered in memory for 24 hours for that edition and not asked again on every view; **Retry games without cover**, a change to the cover providers, a change to the edition's cover inputs or deleting the game forgets them. An add-on (DLC, soundtrack…) borrows its base game's cover before the fallback pass, so a link-less add-on key does not spend title searches first (unless a provider was limited: it may have had the add-on's own art). When the way covers are found improves, "no cover found" markers written before the change are ignored, so those games are retried (see `coverLogicChanged` in `internal/application/media`).

## Browsing the library

- **Sort** by title (A–Z or Z–A), recently added, release year, redeem deadline, number of copies or rating. The choice and the view (covers or list) are remembered per device.
- **A–Z bar**: jump to the games starting with a letter (`#` groups titles starting with a digit or symbol). Letters with no games under the current filters are greyed out.
- **Two views**, chosen with "By platform | By game" next to Covers / List and remembered per device ("By platform" by default). **By platform** shows a card or row per edition: its cover, the game title, the system badge, small store badges on PC, and the pending or redundant badges of that edition's copies; a box borrowed from another edition is labelled "<system> box". The header counts "N editions of M games". **By game** shows the main edition's cover with the badges of all the game's systems. When an edition's cover answers 404, the card borrows the main edition's, then the other editions', and shows the title's initials when none has one. A failed cover is not asked for again for a minute, or until the game's cover inputs change (title, store links, its copies' platforms or a first physical copy, an edition's chosen cover, a new or main edition), so rating a game or changing its play status costs no new requests. An image cannot tell a missing box (404) from a rate-limited answer (503), so after the minute the next render tries once more; nothing retries on a timer. The "+N" chip of a PC edition's store badges names the hidden stores in its tooltip and, for screen readers, in the card button's description; like the badges, a click on it opens the card.
- **Boxes on a blurred copy of themselves.** Every cover takes the same 2:3 slot, so titles and rows line up. The image is shown whole, centred in the slot. When it does not fill the slot (its aspect is more than about 2% off 2:3: a DS case, a Switch case, a landscape banner), the same image, cropped to cover the slot, enlarged slightly, blurred and darkened, fills the gaps behind it, so cases of different systems make an even grid and the sharp box stands out. It is the same address, so the browser does not download it again. Covers that fill the slot (most PC covers) get no backdrop, which keeps blurred layers in the grid few. The backdrop is added once the image has loaded and its shape is known (remembered per address for the session), without a transition: the grid never animates. The slot carries the rounded corners and the shadow, and the badges, flags and the borrowed-box label sit on it. The same slot is used by the game sheet's cover and the **Choose cover…** candidates. A candidate or photo in **Choose cover…** whose image fails to load is left out. The initials placeholder fills the whole slot. List rows, scan thumbnails and the blurred sheet background still crop to fill.
- **Filters**: system, kind of copy, play status, source, platform, genre and the custom list, multilist and yes/no fields, plus quick filters for pending keys, keys expiring in 30 days, keys you don't need and games "On several platforms" (two or more systems, such as Xbox 360 and PS3 or PC and PS4, to spot a game bought twice; two stores of one system, like Steam and GOG, do not count, nor a game without copies). That last one is about the game: in "By platform" it keeps every edition of those games, which the title sort shows side by side, and its count follows the view (editions, or games). The System filter counts editions in "By platform" and games in "By game". In "By platform", copy-level filters (kind, platform, source, copy fields, redundant keys) keep an edition when one of its copies matches; game-level ones (play status, rating, genres, game fields) and the search apply to all of a game's editions. Ties in a sort are broken by system. Other options show how many games they match.
- **Genres and release year** come from the game details. Game Vault downloads the details of Steam-linked games in the background (in the UI language, at a gentle pace: 2 to 4 seconds between games, a minute ±20% after a provider complains, a new round every hour ±10%, starting 30 to 60 seconds after start-up; off with `-no-unattended`), so the genre filter fills in over the first hours; the filter panel says how many games are covered so far.
- **System badges** in "By game" show where you have the game: the store or console logo (with the generation for consoles: PS3, 360…). Solid badges are copies you own; hollow ones with a key glyph are keys you have not redeemed yet. Sold, gifted and expired copies are not shown. Logos come from [Simple Icons](https://simpleicons.org) (CC0); Xbox and Nintendo, which Simple Icons does not ship, use simple drawn glyphs. Brand logos are trademarks of their owners.
- On phones the sidebar becomes a bottom tab bar, filters open as a bottom sheet and the game sheet takes the full screen.
- A `#/game/<id>` address opens that game's sheet; moving to another game in that sheet (‹ ›, ← →) updates the address to the game shown, and closing it returns to `#/library`. Changing the address within a page (a sheet over the library, a card of System) keeps the scroll position; only a new page starts at the top.

## Game sheets

The sheet opens on the edition that was clicked (the main one when opened from a link). With two or
more editions, chips switch between them. The hero shows that edition's cover and, when the box is
borrowed, says so in a note under the cover (the cover itself carries no "<system> box" label there)
and offers **Choose cover…** and **Use a photo**; **Use as the game's cover** makes a non-main
edition the main one. Copies are grouped by system, the current edition first, and the copy form has
a **System** field (the derived system as placeholder; emptied means automatic). When the copy has
an override stored, the API does not expose the source's system, so the hint says "as the source or
the platform says" instead of naming one.

The hero of the sheet holds the **play status** chips and the **rating** stars. Both save on click;
clicking the current one again clears it. The list view shows them next to each game.

Each game page shows a sheet: summary, genres, developer and publisher, release date, age rating, players, Metacritic, trailers and screenshots.
It comes from the **game details** provider chain. The first provider gives the main sheet and the next ones fill the gaps; trailers from all providers are kept.

| Details provider | Applies to | Notes |
|---|---|---|
| Steam store | games linked to Steam | In the UI language, free. Trailers are HLS streams, played natively in Safari or with hls.js (loaded on demand) elsewhere |
| Wikipedia | games not linked to Steam (and Steam games on the fallback pass) that Wikidata identifies without doubt | Enabled by default, no key. Steam's localized summary always wins for a Steam game, so Wikipedia's 4-5 requests are not spent on it (the background scan asks for every game) unless Steam had nothing. The summary is the introduction of the game's article in the UI language's Wikipedia (a regional language such as `pt-BR` uses its primary subtag), else the English one, shown with "Source: Wikipedia" linking to the article and "License: CC BY-SA 4.0" linking to the license (links are only rendered for https addresses). Developers, publishers, genres (named in the UI language, else English) and the publication date come from the game's Wikidata item: the earliest date, a more precise one winning over a bare year or month it falls in. Wikidata's genres ("Puzzle video game", "First-person shooter") join the other providers' in the library's genre filter as they are; they are not mapped to a common list. See below for how the item is found |
| IGDB | every game it identifies | Summary (English: IGDB has no translations), genres, companies, first release date, PEGI rating (else ESRB, else USK), up to 8 screenshots and 4 YouTube trailers. Needs a Twitch application (see below); free for non-commercial use. A few requests per game, then cached |
| TheGamesDB | physical copies, games without store links, and on the fallback pass | Platform-specific (searched on the systems of the game's copies, mapped as for covers: a PlayStation Store copy is a PS4 one), in English. Shares the key with TheGamesDB covers. About 2 requests per game, up to 3 when the title has edition or store tags and the game's name finds no exact title (the raw title is searched too); genre and company lists are fetched once per run. When it asks to slow down, or its monthly allowance is used up, the sheet shows the others' data and is not cached, so the next view asks again; the System and Providers pages say so (and when the allowance renews), not the sheet |

New installs get the chain **Steam → Wikipedia → IGDB → TheGamesDB** (each provider's `DefaultOrder`). With the first provider winning each field, a game linked to Steam gets Steam's localized sheet (Wikipedia is not asked unless the sheet stays sparse and the fallback pass runs), any other game gets Wikipedia's summary (in the UI language when that Wikipedia has the article, else English) before IGDB's English one, and screenshots come from Steam, then IGDB. An existing install keeps its order: new providers are added at the end of its chain (Wikipedia enabled, IGDB disabled until configured), and the **Providers** page reorders them. A summary that needs credit carries it with it: the credit belongs to the provider whose summary won, so a Steam summary never shows "Source: Wikipedia". A YouTube trailer two providers know is listed once. Providers that were asked and returned no game are listed in the sheet's footer ("No clear match on IGDB"), so a missing part is not mistaken for a provider that was not tried. That covers both no result and an ambiguous one: Wikipedia, IGDB and TheGamesDB answer nothing rather than guess between two games with the title. It is not a failure, so the sheet is still cached. A provider that failed for this game shows a warning instead; one that asked to slow down, is not answering or rejects its settings is not listed and shows no warning on the sheet: that is the provider's status (see Provider status). Title searches (IGDB, Wikipedia, TheGamesDB, SteamGridDB, Steam's cover search and link finder) search `game.SearchTerms`: the game's name without edition and store tags (`game.SearchTitle`: "Example West GOTY" → "Example West"), then the raw title only when the name finds no match (a second TheGamesDB request only then); they are filtered to `GameQuery.SearchSystems()` (the edition's system, else the physical copies', else every copy's); results are still matched against the full title (exact first, then `game.MatchKey`, the main game over its editions).

**Wikipedia: finding the game.** The title is searched on Wikidata (`wbsearchentities`) in English and in the UI language; only items whose label or alias is the title (ignoring case, accents and punctuation, or edition tags as in other title matches) are candidates, and of those only video games (instance of Q7889). Of these, the one on one of the game's systems (Wikidata's platform, P400, mapped from Game Vault's systems: PC is Windows, macOS, Linux or "personal computer") is used; with no system known, the only one. Two candidates on the game's systems (a 1993 game and its 2016 reboot, both on PC) give nothing, and so does a lone candidate listed on other systems only: a wrong sheet is worse than none. A lone candidate Wikidata lists no platforms for is used. A disambiguation page is never a summary. Labels come from `wbgetentities` in the UI language, then English, then `mul` (the language-neutral label Wikidata uses for names). Requests go one at a time, 200 ms apart (a request that would wait more than 10 s for its turn is answered rate-limited, and after a 429, a 5xx or a dropped connection the servers are left alone for 30 s, or longer when their `Retry-After` asks for it; a lookup whose caller gave up starts no such rest), with the User-Agent `GameVault/<version> (https://github.com/botchris/game-vault)` the Wikimedia User-Agent policy asks for; HTTP 429 and the API's `maxlag` / `ratelimited` errors are rate limits (the sheet is not cached), other API errors (which come with HTTP 200) are shown with their message. **Test** runs one search.

**IGDB.** IGDB is Twitch's game database, free for non-commercial use under the Twitch Developer Service Agreement. To use it, create an application on the [Twitch developer console](https://dev.twitch.tv/console/apps) (Twitch asks for two-factor authentication first): any name, `http://localhost` as the OAuth redirect URL, category Application Integration, client type Confidential. Paste its Client ID and a new Client Secret into IGDB's settings on the **Providers** page, under Covers or Game details (the two share them). Game Vault exchanges them for an app access token (OAuth client credentials, `POST https://id.twitch.tv/oauth2/token`), keeps it in memory until five minutes before it expires, and asks for a new one once when IGDB answers 401 (only if the cached token is still the one refused; concurrent lookups share one token request). The credentials go in a form body, never in the URL, so no error message or log can carry the secret; a token without a lifetime is kept for an hour. **Test** asks for a new token and runs one minimal query; Twitch answers 400 "invalid client" to an unknown Client ID and 403 "invalid client secret" to a wrong secret, both shown as "check them in the provider settings". Queries (`POST https://api.igdb.com/v4/<endpoint>`, Apicalypse in the body) take one of 8 open slots, then start at least 250 ms apart, IGDB's limits of 4 requests per second and 8 open requests; a lookup that would wait more than 10 s (for a slot or its start) is answered rate-limited, as is IGDB's HTTP 429, after which (or after a 5xx, or a connection dropped without an answer) IGDB is left alone for 30 s, or longer when its `Retry-After` asks for it; a lookup whose caller gave up (a closed page, a deadline) starts no such rest. A game is found by its Steam, GOG or Epic link (`external_games`, one query with the game expanded; a store id listed under a game and its editions describes the game, under two games it is not trusted), else by searching its title on the IGDB platforms of its systems and keeping the only exact match (an edition or add-on with the same title yields to its game; two games with the title give nothing). Images come from `images.igdb.com` (covers `t_cover_big_2x`, screenshots `t_screenshot_big` / `t_1080p`) and are downloaded into the game's folder like the others.

**Fallback pass.** A game only a store without a details provider knows (a PS4 game in the PlayStation library) would otherwise get an empty sheet: its store link keeps TheGamesDB out, and no store provider applies. So when every provider asked answered (none failed or asked to slow down) and the sheet is still *sparse* — no summary and no screenshots, whatever else it has — the chain runs a second time with the fallback flag set. Quota-limited providers that kept out of the game then apply, providers already asked are not asked again, and they only fill the gaps. A provider that asks to slow down in either pass keeps the sheet out of the cache. A sparse sheet whose fallback pass did not complete is marked so when cached, and opening it runs the passes again: the background scan never runs the fallback pass (it must not spend TheGamesDB's quota), and a provider that failed in either pass (a server error) may have had the missing part.

Game sheets stay per game, not per edition. Sheets are fetched when a game is opened and has stayed on screen for 300 ms (the games passed over with ‹ › or the arrow keys are not asked for, which spares TheGamesDB's quota), and cached in `game_details` for 30 days, per language. Each cached sheet records the version of the gathering logic that built it (`detailsLogicVersion` in `internal/application/media/details.go`: 1 since the fallback pass, 2 since Wikipedia and IGDB, 3 since title searches use the game's name). A sparse sheet built by an older version is fetched again once when opened, even one an older server cached after the upgrade; other cached sheets stay until they expire. **Refresh details** fetches them again.

Every image of a game lives in its own folder, `config/game-data/<title> [<id>]/`. That folder holds the covers (one per edition), the screenshots and their thumbnails, the trailer posters, and `assets.json`, which records where each image came from.
When a sheet is opened, its images are downloaded in the background. From then on the UI loads them from disk through `GET /media/games/{id}/assets/{name}`. A file that goes missing is downloaded again on first request.
Names are derived from the source URL, so a changed image gets a new file and images dropped from the sheet are deleted.
Only trailers stream from the provider; they are hundreds of MB each.
The image proxy is only used when exploring new options (choose cover, scan suggestions). Folders are renamed when a game is renamed and deleted with the game.
Covers from the old flat `config/covers/` layout are moved into `game-data` on first start.
The sheet is dropped only when the game is deleted. Descriptions are converted to plain text, so third-party HTML is never rendered.

## Custom fields

**Settings → Fields** defines your own fields for what the catalog does not track: a shelf, the price
you paid, how long a game took, a personal rating… Each field has a name and one of nine types:

| Type | Value | Shown as |
|---|---|---|
| Text | one line, up to 250 characters | as typed |
| Long text | several lines, up to 10,000 characters | as typed |
| Yes / no | a Yes / No / — segmented control (— leaves it empty) | Yes or No |
| Number | whole, or with 2 decimals (stored in hundredths; a dot or a comma is the decimal separator); optional unit | the number in the UI language (`1,25 kg` in Spanish) and its unit |
| Money | an amount in minor units and a currency (ISO 4217; empty means the default currency) | localized amount |
| Date | a year, a year and month, or a full date | localized |
| Duration | hours and minutes (stored in minutes) | `12 h 30 min`, numbers in the UI language |
| List | one of the field's values | the value's name |
| Multi-select list | several of the field's values; typing a new one while filling it in adds it to the field | chips with the names, in the field's order |

A field describes either the **game** or each of its **copies**. A copy field can be limited to some
kinds of copy (for example physical only); with none ticked, every kind has it. Game fields are
filled in on the game sheet's **Edit** tab and shown under **More details** on the overview; copy
fields in the copy form and on the copy card. Only values with something to show appear (a list
value whose choice was removed shows nothing), and **More details** is hidden when none has one.
While a number, amount, year or duration holds text that is not valid, the control says so and
**Save** stays disabled (in the Edit tab and the copy form) until it is fixed or cleared. Fields
appear in the order of the Fields page, where they can be moved up and down with the arrows (touch
and keyboard) or dragged by their grip on desktop.

Limits: 50 fields; 200 values per list; field and value names of 1 to 60 characters (field names
are unique, ignoring case, and adding a list value that already exists returns that one); numbers
within ±1,000,000,000,000 (whole or with 2 decimals); money amounts from 0 to
1,000,000,000,000 in a currency with cents (at most 100,000,000,000,000 minor units, so never
negative); durations up to 100,000 hours.
The type and whether a field describes a game or a copy are fixed once it exists; the name, the
copy kinds, the number and money options and the list values can change. A change that would
corrupt or invalidate stored values is refused, saying how many games are affected: new decimals
while any game or copy holds a value (the stored hundredths would be read at another scale, so the
Fields page disables the control then), and a new currency or new copy kinds that some stored value
no longer fits. Clear or change those values first.

**Storage.** The definitions live in the `fields` setting, as one ordered list; each field and list
value has an id that never changes, so renaming a value renames it everywhere. Values live inside
the game document (game fields) and the copy documents (copy fields), keyed by field id. The server
validates and normalizes them on save (text trimmed, multilist values in the field's order without
repeats, the field's currency filled in) and never stores empty values: clearing a field removes it
from the document. A save that names a field that no longer exists is refused, so an old page cannot
write stale values, and so is a copy value for a field that does not apply to the copy's kind (the
copy form leaves those out when it saves a copy whose kind changed).

**Bulk changes.** Changes that touch every game run in one transaction:

- **Deleting a field** removes it and its values from every game and copy. The confirmation says
  how many games and copies hold a value.
- **Removing a list value** clears it from every game and copy, or, with "merge into", replaces it
  with another value of the same field (a multilist holds the target once). It applies at once,
  without the dialog's Save.

**Filters and search.** Every list, multilist and yes/no field adds a group to the library filters,
with each value, Yes / No, and "No value" for games without one; each option shows how many games
it matches. A copy field matches a game when one of its copies (of the kinds the field applies to)
has the value. Picked values of a field that was deleted, or of a choice that was removed, are
dropped from the filters, so they no longer count in the filter badge. The search box also looks in
the text and long-text fields of the game and its copies.

Scans never touch field values: sources write store data only, so values survive every sync.

## Photos of copies

Any copy (key, library or physical) can have up to 50 photos, in the order the user chooses, each
with an optional caption (at most 200 characters), the date it was taken and the date it was added.
One of them can be the cover of its copy's edition: a cover photo wins over the providers, and an
edition holds either a photo or a URL, never both (choosing one replaces the other). Clearing a
photo cover therefore lets the providers choose: no URL from before the photo is kept to fall back
to. Editing the game keeps it; choosing another cover for the edition replaces it; removing
the photo (or the copy), or moving the copy to another system, clears it when no other copy of that
edition has that photo.

- **Files.** `config/photos/<first two hex>/<sha256>.jpg` and `<sha256>-thumb.jpg`. A photo is named
  after the SHA-256 of its stored JPEG, so the same image is stored once and a file never changes
  (writes go to a temporary file renamed into place). Moving or merging copies keeps their photos.
- **In the browser.** Picked images (the file picker offers the camera on phones) are decoded with
  their orientation applied, reduced to at most 2560 px (JPEG quality 0.85) and given a 400 px
  thumbnail. The original's EXIF segment is copied into the reduced JPEG with the orientation reset
  to upright, so the date, camera and **location** are kept: they are the user's own record. A photo
  file copied out of `config/photos` therefore carries where it was taken. Formats the browser
  cannot decode (HEIC outside Safari) are refused with a message. The pure EXIF helpers
  (`web/src/lib/exif.ts`) have Node tests in `web/tests/`.
- **Upload.** `POST /media/photos`, a multipart form with `photo` and `thumb`, behind the same access
  control as `/media/`, plus a required `X-Gamevault-Upload` header: a page on another site cannot
  add it without a CORS preflight the server never grants, so it cannot upload through the browser
  of someone on a trusted network (where no session cookie is needed and SameSite cookies do not
  help). Both parts must be JPEG; the photo at most 8000 px and 15 MB, the thumbnail at most 512 px.
  The server computes the id, reads `DateTimeOriginal` (or `DateTime`) from the EXIF metadata and
  answers `{"id", "takenAt"}`. EXIF dates have no zone, so `takenAt` is the camera's clock reading
  stored as UTC and shown as a date in UTC. The photo is then attached with `AddCopyPhotos`;
  `UpdateCopyPhoto`, `ReorderCopyPhotos`, `RemoveCopyPhoto` and `SetEditionCover` edit them.
- **Serving.** `GET /media/photos/{id}` and `/media/photos/{id}/thumb`, cached as immutable.
- **Cleanup.** An hour after start and then daily, photo files no copy references are deleted once
  they are a day old, so a photo being attached, or removed by mistake and uploaded again, survives.
  Uploading a stored photo again marks its files as just written for the same reason.

The image viewer (`web/src/components/Lightbox.tsx`) serves screenshots and copy photos: previous /
next buttons, a counter, zoom (buttons, double click or tap, Ctrl/⌘ + wheel, trackpad or touch
pinch, drag to pan), full screen where the browser supports it, swipe on touch screens and the keys
← → Esc + − 0 F. Copy photos add a footer: caption edited in place, date taken, move left / right,
use as cover, delete.

## Second-hand prices

Physical copies with a barcode get second-hand price estimates from the **price providers**
(Providers → Prices), which plugins add like any other provider (`provider.KindValuation`,
`internal/application/valuation`). Every enabled one is asked; their order is only how they are
listed. Keys, library copies and physical copies without a barcode have no estimates (the card
offers to add the barcode).

| Price provider | What the figure is | Settings |
|---|---|---|
| CeX (`cex-prices`, on by default) | What CeX sells the product for, and what it pays in cash or store credit, from `boxes/{EAN}/detail` | Shared with CeX barcode lookups (settings group `cex`): countries to ask, in order (`es` when none is set or valid); the first whose catalog has the EAN answers, in its currency |
| eBay (`ebay-prices`, off by default) | The median asking price of up to 50 used listings with the EAN on the first configured marketplace (shipping excluded; listings in other currencies ignored). eBay keeps sold prices for approved partners | Shares the developer keys and marketplaces of the eBay barcode provider (settings group `ebay`), and its client: one application token (cached by `oauthapp` until shortly before it expires, renewed once when eBay refuses it) serves both |

- **What is kept:** each copy keeps the latest estimate of each provider (amounts in the currency's
  minor units, the product's page, the date), when it was last checked and its next valuation date,
  in the copy document (a copy no source lists says so). Estimating changes only the copy, not the
  game, so the game's cached cover stays.
  Changing the barcode or the kind clears them; scans and CSV imports never set them (a CSV import
  that changes the barcode clears them too). There is no history.
- **When:** each copy has its own date, 20–40 days (uniform) after its last estimate. A copy that
  becomes priceable without a date (first start, a barcode added) gets one within the next 30 days,
  so a collection is spread over the month instead of being asked at once. A background task wakes
  every 3–7 minutes, estimates the due copies one at a time with 20–60 seconds between them (a copy
  priced by hand meanwhile is skipped; the round stops if every provider is disabled), and is off
  with `-no-unattended`. "Update price" on a copy asks now and gives it a new date.
- **Failures:** a provider that fails keeps its previous estimate for that copy; one that no longer
  lists the product loses it; the copy's date always moves forward, so nothing is retried in a loop.
  The copy counts as checked ("Not listed by any source · date") only when every provider answered.
  A provider-level problem (a bot check, a rate limit, rejected keys) is the provider's status on
  System → Health, not a warning: "Update price" then says some providers are unavailable, with a
  link to Health. A provider that is disabled drops out of the next estimate and of the totals. When CeX answers
  with a bot check (Cloudflare), that is its provider status (a provider-level problem, see above)
  and nothing works around it.
- **Collection value** (System): per provider, the sum of its estimates in the default currency and
  how many copies that covers; estimates in other currencies are counted apart. Providers are never
  mixed: a shop's price and an asking price are different things.

## Health

System → Health (at the top of the System page) lists what needs the user's attention, like Sonarr's and Radarr's health checks; with nothing to report it says "No issues found", and when the list cannot be fetched it says so and keeps the last one. Each part of Game Vault registers a check (`internal/application/health`, `health.Check`) that reports its current issues from in-memory state, the local database or a stat, never from a network call. `HealthService/GetHealth` returns them, errors first, then by kind and source; each has a level (error: stopped working until the user acts; warning: works worse or will stop), a kind, the name of what has the problem (empty for the backups and the disk, which the UI names), a reason code with its values (`provider_quota` with `until`, `backup_stale` with `days`, `disk_low` with `free` and `percent`…) that the UI translates, the English message as detail, since when, and where it can be fixed: a source's issue opens that source's dialog (`#/sources/<id>`), the backups' scroll to the Backups card (`#/system/backups`), providers' go to Providers. The app fetches the issues once for every page (`state/Health.tsx`): on start, on window focus, every 3 minutes, and after saving a source or a provider, scanning or making a backup. The sidebar shows a dot on System (on Settings on phones), colored by the worst level and labelled "System: error" / "System: warning"; the Providers page reads its chains again when the providers' issues change.

| Check | Error | Warning |
|---|---|---|
| Providers (`providers.Registry`) | settings rejected (an unknown key, wrong credentials) | allowance used up, rate limited, not answering (see Provider status) |
| Sources (`sync.Service`) | the store signed the source out (`source.ErrSignedOut`, recorded in the scan report as `SignedOut`, or a keep-alive that failed with it): "sign in again in Sources" | the last scan failed (since the first of consecutive failures, `SyncReport.FailingSince`); the last keep-alive failed (in memory, cleared by the next successful keep-alive or scan, by saving the source's settings, or by deleting it); the sources could not be read. Disabled sources are skipped; a report saved before `SignedOut` existed stays a warning until the next scan |
| Backups (`system.Service`) | the last backup failed, automatic or made by hand (cleared by the next one that works) | automatic backups are on but the newest backup is older than twice their interval, three days at least (the server's start counts only while there is no backup yet) |
| Disk (`health.DiskCheck`) | under 200 MB free on the config directory's filesystem | under 1 GB free, or under 5% of the filesystem with a cap of 10 GB (so a large disk does not warn with 199 GB free) |

Sources report a sign-out through `source.SignedOut(msg)`, which matches `source.ErrSignedOut`: Amazon, EA, Battle.net, Epic, Fanatical, GOG, itch.io, Humble Bundle, PlayStation, Steam (a rejected API key), Ubisoft and Xbox do. Disk space comes from `syscall.Statfs` on Linux, macOS and FreeBSD (`internal/adapters/outbound/diskspace`); elsewhere the check reports nothing.

## Backups

A backup is `config/backups/gamevault-<timestamp>.db` (a `VACUUM INTO` snapshot) plus
`gamevault-<timestamp>.photos`, the ids of the photos that catalog references, one per line. The
photos themselves go to one store shared by every backup, `config/backups/photos/` (same layout as
`config/photos`), as hard links: a photo costs no extra space while it is still in use, and it is
stored once however many backups list it. Where hard links are not possible (another file system)
it is copied. Rotation (`-backup-keep`) deletes a backup's `.db` and `.photos` together, then the
store's photos no remaining list names; when a remaining list cannot be read, the store's photos are
left alone until the next backup. Backups without a list (older ones, `pre-migration-*.db`)
count as having no photos. System → Backups shows the photos of each backup and the store's size.
The rules (when to back up, how many to keep, which photos to keep) are `system.Service`'s; the
files and their layout are the `system.BackupStore` port's, implemented by
`internal/adapters/outbound/backupdir`.

To restore:

```bash
# Stop Game Vault first.
cp config/backups/gamevault-YYYYMMDD-HHMMSS.db config/gamevault.db
rm -f config/gamevault.db-wal config/gamevault.db-shm
cp -Rn config/backups/photos/. config/photos/   # -n: photos still in place are the same files
# Start Game Vault again; the daily cleanup removes photos the restored catalog does not use.
```

## Scanning physical games

The **Scan** page registers a shelf of discs without stopping: the camera (or a USB / Bluetooth reader, or typing) keeps reading box after box, every box joins a list, and **Send** adds the ready ones at once. Nothing asks for a confirmation while scanning.

- On phones the camera opens by default (the choice is remembered on the device); on desktop the barcode field has the focus, for readers that type the code and press Enter (each Enter adds the code and leaves the field empty).
- **The camera never pauses.** Each read gives a short vibration and a high beep, flashes the guide green and shows the code with "added to the list" for a moment. A code already in the list gives a double vibration, a low tone, an amber flash and "already in the list": repeats are always ignored, and a second copy of the same box is added with **+1** on its row. A box is ignored while it stays in view. Tones are made with Web Audio and can be muted (remembered on the device).
- **The list** lives only in this browser (`localStorage`, key `gamevault.scanList`, format 1): it survives a reload or a locked phone and reaches the server only when sent. If the browser cannot store it (private mode) it works in memory and says so once. Rows are looked up in the background **one at a time** (`IdentifyBarcode`), so a shelf scanned in a minute never makes many requests at once; rows still looking up when the page closed are looked up again.
- Each row shows its state: **Looking up…**, **Ready** (as a new game, or as a copy of one of yours), **You have it** (a copy already carries that barcode: not sent unless +1), **Review** (unknown code, no platform, or several games of yours with that title) or **Error** (with Retry). A row resolves by itself when a database knows the code with a platform and a game is suggested; exactly one game of yours with that title makes it a copy of that game. Tapping a row opens it in place to change the title, platform, edition, cover, or "copy of …" / "new game"; fixing a Review row makes it Ready. When a barcode database or a cover provider had a provider-level problem during the lookup (an allowance used up, a service not answering), the open row says some providers are unavailable, with a link to System → Health, instead of a warning. Rows are removed with ×, with Undo for a few seconds; **Clear list** empties it, with Undo too.
- **Send** (in a bar fixed at the bottom, with "N ready · N to review · N you have") sends every Ready row, plus "You have it" rows with +1, in one `GameService.AddScannedCopies` request (200 items at most; longer lists go in several). The batch defaults (platform, grade, contents, location) are applied then, so changing them mid-session applies to the whole list; the batch platform wins over the code's. The server saves everything in one transaction: copies for the same game in one save, and copies for new games grouped by the title's match key, so two discs of a game you did not have make one game with two copies. A copy that cannot be saved (its game was deleted meanwhile, invalid details) comes back with its error and stays in the list; saved rows leave it for "Added in this session". Review rows stay for later.
- What every box gets (platform, condition, contents, location) is shown on one line under the field and edited in place.

How a code is identified:

1. If one of your copies already has that barcode, you are told you already own it. Nothing external is asked.
2. Otherwise the **barcode provider chain** is asked: CeX (free, no key), eBay (free developer keys; finds second-hand listings carrying the EAN), UPCitemdb (free, about 100 lookups a day), then EAN-Search (needs a token). Installations that existed before CeX get it at the end of the chain; move it up on the Providers page.
   **CeX** reads the box detail endpoint the CeX website calls (`wss2.cex.{country}.webuy.io/v3/boxes/{ean}/detail`, undocumented; community notes in github.com/Dionakra/webuy-api). It knows many PAL editions the other databases miss. Each country has its own catalog of the editions sold there and a barcode does not say which country it belongs to (the Spanish Dead Space 3 has a UK-registered code and is only in the Spanish catalog), so the countries are asked one at a time, never in parallel, stopping at the first that knows the code. The countries that answered are remembered (an internal provider setting) and asked first, so after the first scans a lookup is usually one request; an unknown code asks every country once. The settings can limit the countries (`uk, es, ie, pt, au, in, mx, it, pl`). Only the product name and the platform (from the category, e.g. "Xbox 360 Juegos") are used: CeX's images and prices are not. Products outside CeX's games category (films, accessories) are not offered as games. If CeX answers with anything but its JSON API (a bot check page), that is its provider status (System → Health) and the row's "some providers are unavailable" notice, and the next provider is used; it is never worked around.
   The retail product name is cleaned up, e.g. "Assassin's Creed Iii Ed. Special Ps3(sp)" → *Assassin's Creed III*, PS3, Special Edition.
3. The cover providers (TheGamesDB) propose the canonical game and its box art for that platform. The cover you pick is pinned, so no further lookup is spent.
4. If the code is unknown (UPCitemdb knew about half of the PAL boxes we tried), the row is to review: you type the title, pick a suggestion, and the disc is saved **with its barcode**. The next scan of that code is recognised instantly. The row offers **Search on EAN-Search** and **Search the web** (a search for the exact code) to find the title by hand: they open in your browser, and Game Vault never reads those pages.

If a game with the same title is already in your catalog (e.g. you own it on Steam), the disc can be added to it as one more copy.

The camera needs a secure context: `localhost`, or HTTPS when Game Vault is opened from another device.
The scanner is loaded only when the camera is turned on. It asks for a 1920×1080 picture (browsers default to 640×480, where a barcode is a few blurry pixels) and continuous focus where the camera has it. It reads codes with the browser's own `BarcodeDetector` when there is one (Chrome: Apple's Vision framework on macOS, Google's on Android), which copes with blur and small codes far better, and with zxing (`TRY_HARDER`) otherwise (Safari, Firefox). A code counts once it is read twice with a valid check digit. Front and laptop cameras are shown mirrored, so the box moves on screen the way it moves in your hand; laptop webcams have a fixed focus, so the hint asks for the box at 20–30 cm.

Provider thumbnails are loaded through `GET /media/proxy?url=…`, because CDNs such as TheGamesDB's reject hotlinked images.
The proxy only fetches https URLs on hosts that a provider declares (`media.ImageHoster`).

## Logs

The server logs to the terminal and to `config/logs/gamevault.log`. When the file reaches the size limit it is archived as `gamevault-YYYYMMDD-HHMMSS-mmm.log`, and only the newest *N* files are kept.
The **Logs** page lists the files and lets you view them (filter, minimum level, follow), download them, delete archived ones, and change the level and limits live.
At `debug` level every RPC call is logged.

## Access control

Like Sonarr and Radarr, there is one user with a password and a few settings, in
**System → Security**:

| Setting | Values | Default |
|---|---|---|
| Authentication | **Required for everyone**, or **not required on trusted networks** | Not required on trusted networks |
| Trusted networks | CIDR prefixes (`192.168.1.0/24`) or addresses, one per line | This computer only (`127.0.0.0/8`, `::1/128`); in the Docker image, the private networks (see [Docker image](#docker-image)) |
| Certificate validation | **Enabled**, **disabled for local addresses**, **disabled** — for Game Vault's outgoing HTTPS connections (stores, providers) | Enabled |

- Signing in opens a session: HttpOnly, SameSite=Lax cookie, 30 days. Failed sign-ins are
  throttled, and only a SHA-256 of the session token is stored.
- **Access tokens** (System → Security → Access tokens) let a program, the browser extension, look
  up what you own from a browser outside the trusted networks. A token is `gvt_` and 32 random bytes
  in base64url; only its SHA-256 is stored, and it is shown once. It is sent as
  `Authorization: Bearer gvt_…` and reaches only `OwnershipService/CheckOwnership`: any other request
  that carries one is refused as unauthenticated, even from a trusted network. The token alone
  identifies the caller (the session cookie and the network's trust are not used with it), so it
  never makes a request trusted, and the proxy and host-name rules are unchanged. Other
  `Authorization` headers, such as an authenticating proxy's, are ignored. Unknown tokens are
  throttled with their own counter, apart from the password's, so a flood of bad tokens cannot lock
  out password sign-ins: after 10 failures in 15 minutes, unknown tokens are told to wait (HTTP 429,
  `Retry-After: 900`). A valid token is always served, even while unknown ones are throttled: the
  token is looked up by its hash first (no timing leak), so junk tokens cannot keep the owner's
  extension out. The last use is written at most once a minute; revoking takes
  effect on the next request (tokens are read from the database on every request).
  Behind an authenticating reverse proxy (single sign-on), the proxy must let
  `POST /gamevault.v1.OwnershipService/CheckOwnership` through when it carries an
  `Authorization: Bearer gvt_…` header: the extension sends no cookies, so it cannot pass a
  sign-in page.
- A request counts as coming from a trusted network only if its TCP peer is in one, it carries no
  proxy headers (`X-Forwarded-For`, `Forwarded`, `X-Forwarded-Host`, `X-Real-IP`), and its
  `Host` is an address or `localhost`. Behind a proxy the peer is the proxy, not the caller; and a
  host name in `Host` could be a malicious site pointing its own name at this machine (DNS
  rebinding) to read your data with your browser. Those requests sign in with the password.
- The user can only be created from a trusted network, so nobody else can claim an empty
  installation. Requiring authentication needs the user to exist (no lock-out). From a trusted
  network the password can be reset without the current one, which signs out every session.
- Forgot the password while authentication is required? Start Game Vault with `-reset-auth`: it
  stops requiring sign-in on trusted networks again (and restores the default network if the list
  is empty).
- Certificate validation is applied to every outgoing connection through Go's default transport
  (`internal/adapters/outbound/tlspolicy`). "Local addresses" are loopback, private and link-local
  IPs, `localhost`, single-label names and names under `.local`, `.lan`, `.home.arpa`,
  `.localhost` and `.internal`.

## Security

Passwords are stored as bcrypt hashes and session and access tokens as SHA-256 hashes.
CD keys and source credentials are stored in clear text in `config/gamevault.db`. Treat that directory, and its backups, as secrets.
Do not expose the port to the network directly (`-addr 0.0.0.0`); put an HTTPS reverse proxy in front of it instead.
Logs never contain keys or credentials.

## Working on the code

- Working with an AI coding agent such as Claude Code: `CLAUDE.md` describes the commands, architecture, conventions and safety rules; `.claude/` holds the project memory (`MEMORY.md`), guides for adding integrations and for the UI (`docs/`), helper scripts (a test server on a copy of the config, a translation check) and slash commands (`/verify`, `/new-source`, `/new-cover-provider`).

- Change the API: edit `proto/`, then run `task generate`. This regenerates `internal/gen` and `web/src/gen`.
- Add a UI language: add `web/src/i18n/locales/<code>.json` and register it in `web/src/i18n/index.ts`.
- Games (with their copies), sources and providers are stored as JSON documents, one row `(id, doc)`
  each, mapped to the domain by the sqlite adapter (`internal/adapters/outbound/sqlite/docs.go`;
  `doctable.go` holds the list, get, save and delete every document table shares, and the settings
  rows' read and write). The domain rebuilds a stored game or source from one `Snapshot`
  (`game.Rehydrate`, `source.Rehydrate`). Adding a field is a code change only: older documents read
  it as empty. Every document carries a format version (`"v": 1`); changing the meaning of a field
  bumps it and the adapter converts older documents when it reads them. Indexes are on JSON
  expressions (title, source name, provider kind and priority). Users, sessions, settings and cached
  details keep their own tables; the cached details are stored as `detailsDoc`, whose field names
  are the Go names the cache always had. `testdata/` holds a fixture of each stored format, which
  `TestStoredFormats_*` read and write back byte for byte.
- Schema changes go in a new numbered file in `internal/adapters/outbound/sqlite/migrations/`, which
  is applied on start. Before migrating an existing database, Game Vault writes a copy to
  `config/backups/pre-migration-<first pending migration>.db` (listed in System → Backups, and
  rotated with the scheduled backups by date). A migrated database cannot be opened by an older
  version (it fails with errors such as `no such column: title`). To go back: stop the server,
  replace `config/gamevault.db` with the pre-migration copy, delete `config/gamevault.db-wal` and
  `config/gamevault.db-shm` (a stale WAL next to a restored file corrupts it), then start the older
  version or image.
- A one-time start-up upgrade (`internal/application/upgrade`, run by `app.Build` before any
  background job) rewrites what older versions stored in a shape the readers only convert: game
  documents before version 3 (read through the repository and saved back unchanged, in batches),
  security settings saved with `localBypass` (saved as the reader converts them), and covers cached
  before editions (`cover.<ext>` / `cover.missing` in a game's folder: adopted by the main edition
  or removed, as its next resolution would). It logs one line per part that did something, a
  warning naming any game it could not rewrite, and nothing once there is nothing left. It is
  removed in the next release, together with the readers of those old shapes.
- `task go -- test ./internal/…/ -run TestName -v` runs one package's tests in the toolchain.
- `task test` runs the Go tests, the TypeScript type check, the Node tests of the web's pure helpers (`web/tests/`) and the translation check, in the toolchain container. The Go tests cover the domain, SQLite, the providers (against fake servers), CSV, and an end-to-end Connect test.
- GitHub Actions (`.github/workflows/ci.yml`) runs `task lint`, checks that `task generate` changes nothing, and runs `task test` on every push to `main` and every pull request, inside the same toolchain image (its layers cached in GitHub Actions, rebuilt only when `build/toolchain.Dockerfile` changes).
- Pushing a version tag (`v1.2.3`) runs `.github/workflows/release.yml`: the same checks, then `task docker:publish` for linux/amd64 and linux/arm64 with the Docker Hub credentials in the repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
