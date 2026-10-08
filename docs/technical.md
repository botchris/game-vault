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
cmd/gamevault/        Entry point and composition root (wires adapters into use cases)
config/               Runtime data: database, backups, logs, per-game images (game-data). Back this up. Git-ignored.
proto/gamevault/v1/   API contracts (Protocol Buffers). Source of truth for client and server.
internal/
  domain/             Pure business model, no infrastructure imports
    game/             Game aggregate, Copy entity, value objects, Consolidator domain service
    source/           Source aggregate (scanned accounts), settings schema, sync reports
    settings/         Runtime settings edited from the UI (log level and rotation)
    auth/             Access control: request identity, principals, settings, users, sessions
    provider/         Metadata provider configuration: kind, order, enabled, settings
    schema/           Settings schema shared by sources and providers (fields, secret masking)
  application/        Use cases; declares the ports it needs
    port/             Cross-cutting ports: TxManager, Clock
    catalog/          Browse/edit games and copies, merge, move, mark redeemed
    sync/             Configure sources, scan them, scheduler. Declares the Provider port
    transfer/         CSV import/export. Declares the Codec port
    system/           Status and backups. Declares the DatabaseBackup port
    media/            Provider chains, covers (resolve + cache), Steam store search. Declares CoverProvider & co.
    logs/             Log viewer and rotation settings. Declares the Sink and Files ports
    auth/             Authenticate, setup, login, password, settings. Declares the Hasher port
  adapters/
    inbound/rpc/      Driving adapter: ConnectRPC handlers, proto↔domain mapping, static UI
    outbound/humble/, steam/, epic/, gog/, battlenet/, eaapp/, ubisoft/, xbox/, playstation/
                      Store sources (and their cover providers where the store has public art)
    outbound/browsersession/ Reuse of a website session pasted from the browser (cookies)
    outbound/thegamesdb/ TheGamesDB cover and details providers (platform box art, overview, trailer)
    outbound/cex/, ebay/, upcitemdb/, eansearch/  Barcode databases
    outbound/gamedata/ Per-game asset folders (cover, sheet images, assets.json)
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
```

Dependencies only point inwards. `domain` imports nothing from the project, `application` imports `domain`, and adapters import both.
Only `cmd/gamevault` knows the concrete adapters.

## Domain model

| Concept          | Kind                  | Notes |
|------------------|-----------------------|-------|
| `Game`           | Aggregate root        | Title, Steam AppID, notes, and its copies. Every copy change goes through the game. |
| `Copy`           | Entity inside `Game`  | `kind` is `key`, `library` or `physical`. `status` must be valid for the kind. Holds platform, key, redeem-by date, origin, edition, condition, location. |
| `Source`         | Aggregate root        | A scanned account: type, settings (secrets masked towards clients), interval, last sync report. |
| `Consolidator`   | Domain service        | Merges imported copies into the catalog. It matches by external id, then by Steam AppID, then by normalised title; if nothing matches it creates a new game. |

A key is **redundant** when it is pending (unrevealed or revealed) and the same game already has a `library` copy on the same platform. It is a key you can gift.
Re-scans never move a key you marked as `redeemed` back to pending.

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

The log level and log rotation are set in the UI (**Logs** page) and stored in the database. The defaults are `info`, 10 MB per file, and 5 files kept.

The web client calls the page's own origin. To point it at another backend, build it with `VITE_API_URL=https://host:port`.

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
  nothing: then `git push origin main v0.2.0` and `task docker:publish`.
- Image tags for `1.2.3`: `1.2.3`, `1.2`, `1` and `latest`. For `0.x` there is no major tag
  (`0.2.1`, `0.2`, `latest`), and a pre-release (`1.3.0-rc.1`) only gets its own tag. The logic
  is in `build/release.sh`.

## Sources

How each source reads your library.

- **Humble Bundle:** session cookie `_simpleauth_sess` from your browser. It uses the site's own JSON API, which is undocumented and read-only. Hidden keys are reported as unrevealed and never revealed. Every game key of every order becomes one copy, Humble Choice months included (a chosen game is a key of that month's order). Only keys for a store or console Game Vault knows count as games: software, courses, in-game items and store coupons ("45% off … Coupon") are skipped and listed in the log, and copies of them imported by earlier versions are removed, with their game if nothing else is left in it. Copies are identified by order, game (`machine_name`) and copy number; copies saved by older versions, which identified keys by order and copy number only and so kept one copy per order, are adopted by the right game on the next scan.
- **Steam:** a Web API key plus your profile, given as SteamID64, vanity name or URL. "Game details" must be public.
- **Epic Games Store:** Epic has no public library API, so Game Vault talks to the endpoints the Epic launcher uses, the same way [Legendary](https://github.com/derrod/legendary) and Heroic do (no dependency on them). Open the login link in the source dialog, sign in and paste the `authorizationCode` it shows (or the whole page). The one-time code is exchanged for a session whose refresh token rotates on every use; Game Vault stores it as an internal setting that is never sent to the browser. Scans list the library, read titles from Epic's catalog (cached in memory) and import games only: DLC, add-ons and Unreal Engine assets are skipped. Copies use the platform "Epic Games", so Humble keys for Epic are flagged as spare when you already own the game there. If the session expires after a long time without scans, paste a new code.
- **GOG:** signs in like the GOG Galaxy client, the way Heroic, Playnite and lgogdownloader do (no dependency on them). Open the login link in the source dialog and sign in; GOG then opens an almost blank page on `embed.gog.com/on_login_success?…&code=…`. Paste that address (or just the code). Like Epic, the code becomes a rotating session stored as an internal setting, and scans list your owned games with their titles. Copies use the platform "GOG", the same as Humble's GOG keys.
- **Xbox / Microsoft Store:** signs in like the Xbox app (public Microsoft account client `000000004C12AE6F`, scope `service::user.auth.xboxlive.com::MBI_SSL`), the way Playnite and xbox-webapi do. After signing in, Microsoft opens an almost blank page on `login.live.com/oauth20_desktop.srf?code=…`: paste that address. The code becomes a rotating session; each scan turns it into Xbox Live tokens (user token, then an XSTS token for the Store, relying party `http://mp.microsoft.com/`) and reads the owned games from the Microsoft Store collections: purchases and redeemed codes, without Game Pass ("recurring") entitlements, trials, add-ons or revoked items. Titles come from the public Store catalog, which also gives the **Xbox** cover provider its official portrait posters. Copies use the platform "Microsoft Store / Xbox", the same as Humble's Microsoft keys. Physical Xbox discs are still added by scanning their barcode.
- **PlayStation:** signs in like the PlayStation app (public client of psn-api / PSNAWP): sign in on playstation.com, open `ca.account.sony.com/api/v1/ssocookie` and paste the NPSSO token it shows. It becomes an authorization code and then a session (refresh token) that renews itself; when the refresh token stops working the NPSSO is used again. Scans read the games bought with the account (PS4 and PS5, no PS Plus games) from the library website's GraphQL API, whose queries Sony only runs by registered hash (two known versions are tried). Copies use the platform of each game ("PS4", "PS5"), so their covers come from TheGamesDB's box art for that console, or from Steam when you also own the game there.
- **Keeping browser sessions alive:** Battle.net, EA and Ubisoft reuse a website session, which expires when idle. Game Vault keeps them alive like an open browser tab: a light request roughly every 15 minutes (Battle.net), every hour (EA) and every 2 hours (Ubisoft), saving whatever the site renews (`sync.KeepAliver`). Intervals vary at random by ±30% and the first keep-alive after start-up comes at a random moment within 10 minutes, so requests never follow a fixed rhythm. Scheduled scans vary the same way: ±10% of their interval (±2.4 hours for a daily scan), and sources that are due when the server starts are scanned at random moments within its first 30 minutes. A failed keep-alive is logged; if the site no longer accepts the session, the next scan shows the error and you paste new cookies.
- **Battle.net:** Blizzard's public API has no list of owned games and its sign-in has a captcha, so Game Vault reuses your browser session on account.battle.net, the way Playnite does. Paste the `Cookie` request header of account.battle.net (DevTools → Network → the `overview` request). Scans read the "Games & subscriptions" and "Classic games" data the site loads, one copy per game even if you have it in several regions. When the site session expires, Game Vault renews it through Battle.net's single sign-on (the longer-lived login cookie) and keeps the renewed cookies as an internal setting (`internal/adapters/outbound/browsersession`, shared with the EA app); paste new cookies only when scans say you are signed out.
- **EA app:** the old Origin API is shut down and EA's sign-in has a captcha, so Game Vault reuses your browser session on accounts.ea.com. Paste the `Cookie` request header of the login-check link in the source dialog (it shows `access_token` when you are signed in). Each scan signs in silently with those cookies (EA's web SDK flow, `client_id=ORIGIN_JS_SDK`, `prompt=none`), keeps any cookie EA renews, and reads `me.ownedGameProducts` from the GraphQL API the EA app uses: full PC games from EA's own store, one copy per base game with the product name as the edition ("Revolution"). EA Play subscription games are skipped. Copies use the platform "EA App", the same as Humble's Origin/EA keys.
- **Ubisoft Connect:** no public library API and a captcha on sign-in, so Game Vault reuses the session of connect.ubisoft.com, the way Lutris and the GOG Galaxy plugin do. Sign in with "Remember me" ticked and paste the login data the site keeps in Local Storage (the `PRODrememberMe` entry, which holds `rememberMeTicket`). The login link uses ubisoft.com's own app id: connect.ubisoft.com's app (`314d4fef…`, used by Lutris and the GOG Galaxy plugin) is no longer allowed to sign in on Ubisoft's public gateway. Ubisoft replaces the remember-me ticket every time it is used, so Game Vault spends it as little as possible: it reuses a session opened in the last hour, then the ticket and session id pasted, and only then renews the remember-me ticket, keeping the new one in memory (so "Test" before "Save" does not burn the pasted value) and as an internal setting. Copy it from a private window, so the renewals do not sign your normal browser out. Each renewal exchanges the remember-me ticket for a session (`POST /v3/profiles/sessions`, trying ubisoft.com's app and then the Ubisoft Connect PC client, remembering which works) and keeps the rotated ticket as an internal setting, then reads `viewer.games(filterBy: {isOwned: true})` from the Ubisoft Connect GraphQL API. PC games only: console games linked to the account are skipped; if Ubisoft renames the platform fields, a minimal query without them is used. Copies use the platform "Ubisoft Connect", the same as Humble's Uplay keys.
- **Tarkov and other launchers** have no public library API. Add those games by hand or with a CSV import.

## Metadata providers and covers

For each kind of data, the enabled providers are tried in the order you set on the **Providers** page, and the first one with an answer wins.
Each provider declares which games it applies to. That keeps providers with a quota away from games another provider covers better.

| Cover provider | Applies to | Notes |
|---|---|---|
| Chosen / custom cover | any game with a cover URL | Always first. Set with **Choose cover…** or by pasting a URL |
| TheGamesDB | physical copies, games no store knows, and store games no store had art for | Platform-specific box art (Xbox 360 case, PS3 case…). Needs an API key, which has a monthly allowance. **Test** shows what is left without spending it |
| Steam | games with a Steam AppID | No key, no quota |
| Epic, GOG, Ubisoft, EA, Battle.net, Xbox | games imported from that store | Official box art, no key, no quota (see below) |

`GET /media/covers/{gameId}` returns the cover. The first image found is stored in the game's folder in `config/game-data/`. A miss is remembered for 7 days so quotas are not spent again.
Changing, enabling or reordering a cover provider forgets those misses.
**Choose cover…** in the game page shows every enabled provider's proposals, and pins the one you pick.
Images are plain HTTP rather than RPC so browsers can load and cache them with `<img>`. Everything else goes through Connect.
For games without a Steam AppID (physical games, for example), use **Find on Steam** in the game page, or paste any image URL.

Store covers: **Epic Games Store** and **GOG** give the official box art of games imported from those libraries, with no key and no quota. Epic's comes from its catalog, read with an application token (client credentials, no user session); GOG's from its public product API. **Ubisoft** gives the box art Ubisoft Connect shows (by space id, on Ubisoft's CDN, which has art for most but not all games), else the Ubisoft Store packshot found by title through the store's public search (the search-only key every store visitor's browser gets); it also covers games with a Ubisoft Connect key. **EA** gives the official pack art from EA's catalog (public in the EA app API), looking the game up by slug guessed from its title and product id. **Battle.net**: Blizzard publishes no box art outside its signed-in shop, so games imported from Battle.net are looked up on the Steam store by exact title. TheGamesDB is only asked for physical copies, games no store knows, and, on a second pass, store games no store had art for (World of Warcraft, StarCraft II…), so its monthly allowance is not spent on games stores cover. When the way covers are found improves, "no cover found" markers written before the change are ignored, so those games are retried (see `coverLogicChanged` in `internal/application/media`).

## Browsing the library

- **Sort** by title (A–Z or Z–A), recently added, release year, redeem deadline or number of copies. The choice and the view (covers or list) are remembered per device.
- **A–Z bar**: jump to the games starting with a letter (`#` groups titles starting with a digit or symbol). Letters with no games under the current filters are greyed out.
- **Filters**: kind of copy, platform and genre, plus quick filters for pending keys, keys expiring in 30 days and keys you don't need. Each option shows how many games it matches.
- **Genres and release year** come from the game details. Game Vault downloads the details of Steam-linked games in the background (in the UI language, at a gentle pace), so the genre filter fills in over the first hours; the filter panel says how many games are covered so far.
- **Platform badges** on every cover show where you have the game: the store or console logo (with the generation for consoles: PS3, 360…). Solid badges are copies you own; hollow ones with a key glyph are keys you have not redeemed yet. Sold, gifted and expired copies are not shown. Logos come from [Simple Icons](https://simpleicons.org) (CC0); Xbox and Nintendo, which Simple Icons does not ship, use simple drawn glyphs. Brand logos are trademarks of their owners.
- On phones the sidebar becomes a bottom tab bar, filters open as a bottom sheet and the game sheet takes the full screen.

## Game sheets

Each game page shows a sheet: summary, genres, developer and publisher, release date, age rating, players, Metacritic, trailers and screenshots.
It comes from the **game details** provider chain. The first provider gives the main sheet and the next ones fill the gaps; trailers from all providers are kept.

| Details provider | Applies to | Notes |
|---|---|---|
| Steam store | games with a Steam AppID | In the UI language, free. Trailers are HLS streams, played natively in Safari or with hls.js (loaded on demand) elsewhere |
| TheGamesDB | physical copies or games without a Steam AppID | Platform-specific, in English. Shares the key with TheGamesDB covers. About 2 requests per game; genre and company lists are fetched once per run |

Sheets are fetched when a game is opened and cached in `game_details` for 30 days, per language. **Refresh details** fetches them again.

Every image of a game lives in its own folder, `config/game-data/<title> [<id>]/`. That folder holds `cover.jpg`, the screenshots and their thumbnails, the trailer posters, and `assets.json`, which records where each image came from.
When a sheet is opened, its images are downloaded in the background. From then on the UI loads them from disk through `GET /media/games/{id}/assets/{name}`. A file that goes missing is downloaded again on first request.
Names are derived from the source URL, so a changed image gets a new file and images dropped from the sheet are deleted.
Only trailers stream from the provider; they are hundreds of MB each.
The image proxy is only used when exploring new options (choose cover, scan suggestions). Folders are renamed when a game is renamed and deleted with the game.
Covers from the old flat `config/covers/` layout are moved into `game-data` on first start.
Changing a game's Steam AppID or cover drops its cached sheet. Descriptions are converted to plain text, so third-party HTML is never rendered.

## Scanning physical games

The **Scan** page registers discs quickly. You scan the barcode with the camera, a USB or Bluetooth reader, or by typing it. Then you confirm the game and move on to the next box.

- On phones the camera opens by default (the choice is remembered on the device); on desktop the barcode field has the focus, for readers that type the code and press Enter.
- The result is one card: the cover, the game, its platform, where the code was found ("found in CeX as …") and a single main action ("Add to *game*" when you already have it on another platform, else "Add game"). Enter in the empty barcode field confirms it, and a new code skips it, so a reader never leaves the keyboard. Other covers, "a new game" instead of a copy, and the title/platform fields ("Not this game?") are secondary. When there is nothing to add yet (unknown code, a product with no platform or no matching game) the title search opens on its own and the big title asks "Which game is it?" instead of repeating a database's product name.
- A lookup can take seconds (several barcode databases, CeX country by country), so the moment a code is read it shows: the camera picture freezes on the box with the guide in amber and the code over it, the code fills the field, and a placeholder card ("Looking it up…") takes the place of the result. After you add or skip a box, its code is ignored until it has been out of the camera's sight for a moment, so the box still in your hand is not looked up again.
- What every box of the batch gets (platform, condition, location) is shown on one line under the field and edited in place.

1. If one of your copies already has that barcode, you are told you already own it. Nothing external is asked.
2. Otherwise the **barcode provider chain** is asked: CeX (free, no key), eBay (free developer keys; finds second-hand listings carrying the EAN), UPCitemdb (free, about 100 lookups a day), then EAN-Search (needs a token). Installations that existed before CeX get it at the end of the chain; move it up on the Providers page.
   **CeX** reads the box detail endpoint the CeX website calls (`wss2.cex.{country}.webuy.io/v3/boxes/{ean}/detail`, undocumented; community notes in github.com/Dionakra/webuy-api). It knows many PAL editions the other databases miss. Each country has its own catalog of the editions sold there and a barcode does not say which country it belongs to (the Spanish Dead Space 3 has a UK-registered code and is only in the Spanish catalog), so the countries are asked one at a time, never in parallel, stopping at the first that knows the code. The countries that answered are remembered (an internal provider setting) and asked first, so after the first scans a lookup is usually one request; an unknown code asks every country once. The settings can limit the countries (`uk, es, ie, pt, au, in, mx, it, pl`). Only the product name and the platform (from the category, e.g. "Xbox 360 Juegos") are used: CeX's images and prices are not. Products outside CeX's games category (films, accessories) are not offered as games. If CeX answers with anything but its JSON API (a bot check page), the lookup fails with a clear warning and the next provider is used; it is never worked around.
   The retail product name is cleaned up, e.g. "Assassin's Creed Iii Ed. Special Ps3(sp)" → *Assassin's Creed III*, PS3, Special Edition.
3. The cover providers (TheGamesDB) propose the canonical game and its box art for that platform. The cover you pick is pinned, so no further lookup is spent.
4. If the code is unknown (UPCitemdb knew about half of the PAL boxes we tried), you type the title, pick a suggestion, and the disc is saved **with its barcode**. The next scan of that code is recognised instantly.

If a game with the same title is already in your catalog (e.g. you own it on Steam), the disc can be added to it as one more copy.
Platform, condition and location can be preset for a whole batch.

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

Passwords are stored as bcrypt hashes and session tokens as SHA-256 hashes.
CD keys and source credentials are stored in clear text in `config/gamevault.db`. Treat that directory, and its backups, as secrets.
Do not expose the port to the network directly (`-addr 0.0.0.0`); put an HTTPS reverse proxy in front of it instead.
Logs never contain keys or credentials.

## Working on the code

- Working with an AI coding agent such as Claude Code: `CLAUDE.md` describes the commands, architecture, conventions and safety rules; `.claude/` holds the project memory (`MEMORY.md`), guides for adding integrations and for the UI (`docs/`), helper scripts (a test server on a copy of the config, a translation check) and slash commands (`/verify`, `/new-source`, `/new-cover-provider`).

- Change the API: edit `proto/`, then run `task generate`. This regenerates `internal/gen` and `web/src/gen`.
- Add a UI language: add `web/src/i18n/locales/<code>.json` and register it in `web/src/i18n/index.ts`.
- Schema changes go in a new numbered file in `internal/adapters/outbound/sqlite/migrations/`, which is applied on start.
- `task test` runs the Go tests, the TypeScript type check and the translation check, in the toolchain container. The Go tests cover the domain, SQLite, the providers (against fake servers), CSV, and an end-to-end Connect test.
