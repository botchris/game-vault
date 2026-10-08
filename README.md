# Game Vault

**Know which games you own, and where.**

If you have played on PC and consoles for a while, your games are scattered: Steam, Epic, GOG,
Battle.net, the EA app, Ubisoft Connect, Xbox, PlayStation, a pile of Humble Bundle keys you
never redeemed, and shelves of discs. Before buying a game, or when a bundle hands you a key,
it is hard to answer a simple question: *do I already have it?*

Game Vault puts your whole collection in one catalog that runs on your own computer:

- **One entry per game**, whatever the format. Each game lists every copy you own: the game in a
  store library, a CD key, a physical disc.
- **Your store libraries are imported automatically** and kept up to date.
- **Physical games are added by scanning their barcode** with your phone's camera.
- **Keys are tracked:** which ones you have not redeemed, which expire soon, and which you don't
  need because you already own the game on that platform (so you can gift them). Steam keys get a
  direct "redeem" link.
- **A proper catalog:** box art, search, sort, filters by platform, store, kind of copy and genre,
  and a sheet for every game with its summary, genres, developer, release date, trailers and
  screenshots.
- Works on desktop and phone, in English and Spanish. Your data stays on your machine.

## Supported platforms

| | How it gets in |
|---|---|
| Steam, Epic Games Store, GOG, Battle.net, EA app, Ubisoft Connect, Xbox / Microsoft Store, PlayStation (PS4, PS5) | Imported from your account and rescanned automatically |
| Humble Bundle keys (Steam, Epic, GOG, Ubisoft, EA… keys) | Imported from your Humble Bundle account, with their redeem deadlines |
| Physical games (Xbox 360, PS3, PS4, Switch…) | Barcode scan from the phone, or typed in |
| Anything else (other launchers, older keys…) | Added by hand, or imported from a CSV file |

Game Vault only **reads** your accounts: it never buys, redeems, installs or changes anything.
Stores have no official way to list what you own, so it signs in the same way their own apps or
well-known open-source launchers do; each source's settings dialog walks you through connecting
it, step by step.

## Install and run

You need [Docker](https://www.docker.com/) and [Task](https://taskfile.dev)
(on macOS: `brew install go-task`). Nothing else: everything is built inside a container.

```bash
git clone <this repository> game-vault
cd game-vault
task run
```

The first run takes a few minutes while the build container is prepared. Then open
**http://127.0.0.1:8080**.

1. **Sources** — connect your stores and your Humble Bundle account, then press **Scan all**.
2. **Providers** — box art and game sheets work out of the box; optionally add a free
   [TheGamesDB](https://thegamesdb.net) key for the box art of physical games.
3. **Scan** — add your discs by scanning their barcodes.

To stop it, press `Ctrl+C`. To update, pull the latest code and run `task run` again.

`task build:release` builds ready-to-run binaries for Linux, macOS and Windows into
`bin/release/`, if you prefer to run Game Vault without building it each time.

## Your data

Everything lives in the `config/` folder next to the program: the database, the downloaded box
art and images, the logs and the automatic daily backups (the last 14 are kept). Back up that
folder, and keep it private: it contains your CD keys and the sessions of your accounts. It is
excluded from git.

Game Vault only listens on this computer (`127.0.0.1`), and on this computer you get in without a
password. To use it from other devices, create a user in **System → Security**; there you can also
make the password required everywhere, or let in your home network without one. To scan barcodes
from your phone, open Game Vault through HTTPS (the camera requires it), e.g. behind a reverse
proxy. Forgot the password? Start Game Vault with `-reset-auth`.

## License

Game Vault is **source-available** under the [PolyForm Noncommercial License 1.0.0](LICENSE): you
may use, study, modify and share it for free for personal, hobby, educational and other
non-commercial purposes. Commercial use is not allowed, including selling it, offering it as a
paid service or building it into a commercial product, whether modified or not.

Game Vault is not affiliated with, endorsed or sponsored by Valve, Epic Games, GOG, Blizzard,
Electronic Arts, Ubisoft, Microsoft, Sony, Humble Bundle or any other store or service it works
with. Their names and logos are trademarks of their respective owners and are only used to show
where your games come from. Most stores offer no official way to list what you own: Game Vault
signs in the way their own apps or well-known open-source launchers do, and you are responsible for
using it in line with each service's terms.

## More

- [Technical notes](docs/technical.md): architecture, configuration options, how each source
  and provider works.
- [CLAUDE.md](CLAUDE.md): how to work on the code (commands, conventions, tests), also used by AI
  coding agents such as Claude Code.
