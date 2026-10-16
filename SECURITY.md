# Security

Game Vault keeps the sign-ins of your game stores and your CD keys, so its security matters. This
page says how to report a problem, what Game Vault stores and how to run it safely.

## Reporting a vulnerability

Please report security problems privately, through GitHub's
[**Report a vulnerability**](https://github.com/botchris/game-vault/security/advisories/new) form
(the Security tab of the repository), not in a public issue. Say what you found, how to reproduce
it and which version you run (System shows it). You will get an answer as soon as possible; this is
a hobby project maintained by one person, so please allow some days.

Fixes go into the next release. Only the latest release is supported.

## What Game Vault stores

Everything lives in one folder (`config/`, or the `/config` volume in Docker):

- **The database** (`gamevault.db`, SQLite): your catalog, your CD keys, and the credentials of the
  stores and services you connect (session cookies, refresh tokens, API keys). These credentials
  are stored **as they are, not encrypted**: whoever can read this folder can use your accounts.
- **Your password**, if you create a user: only a bcrypt hash.
- **Access tokens** for the browser extension: only their SHA-256 hash.
- **Box art, images and the photos of your copies**, and the **logs** (which never contain
  credentials) and **daily backups** (which contain the database, so the same credentials).

Keep this folder private, back it up somewhere only you can read, and never share a backup.

## Who can get in

- **Out of the box**, the program listens only on this computer (`127.0.0.1`), and requests from
  this computer get in without a password.
- **The Docker image** listens on every address of the container and lets in your private networks
  (192.168.x.x, 10.x.x.x…) without a password until you change that in **System → Security**.
- **In System → Security** you create the one user and choose whether a password is required
  everywhere or only outside your trusted networks.
- **Never trusted, whatever the settings:** requests that come through a proxy (they carry
  forwarding headers), and requests that address Game Vault by a host name instead of an IP
  address. The first rule keeps a proxied caller from looking local; the second stops DNS rebinding
  (a web page using your browser to reach Game Vault).
- **Access tokens** (System → Security, for the browser extension) can only ask whether you own a
  game; they cannot read or change anything else.
- **Sessions** are an `HttpOnly`, `SameSite=Lax` cookie, `Secure` over HTTPS. Repeated failed
  sign-ins are slowed down.
- **Forgot the password?** Start Game Vault with `-reset-auth`.

## Running it safely

- **Do not expose Game Vault to the internet without a password.** If you reach it from outside
  your home, make the password required everywhere and put it behind HTTPS: a reverse proxy
  (Caddy, nginx, Traefik…) or a private network such as a VPN or a tailnet.
- **Use HTTPS** when the browser extension or your phone talk to Game Vault from another device: the
  extension sends an access token, and the phone's camera (barcode scanning) only works over HTTPS.
- **Keep certificate validation on** (System → Security) unless a service on your own network needs
  otherwise.
- **Store accounts:** Game Vault signs in to stores the way their own apps or well-known launchers
  do, and only reads. A store can still treat that as unusual activity. Integrations whose terms
  forbid automated access (Fanatical, SteamGridDB) are off until you accept the risk in their
  settings.
