# Game Vault Connector

A browser extension that connects your stores to **your own** Game Vault in one click. You sign in on
each store's own page, in your own browser (2FA and captchas included); the extension collects the
result (a session cookie, a value the site keeps, or the code the store redirects with) and hands it
to the Game Vault tab that asked. It never signs in for you, never works around bot protection, and
never sends anything anywhere else.

The extension knows no store: Game Vault sends it a **recipe** (data, never code) for each source,
so new stores need no extension update. See "Connecting sources with the browser extension" in
`docs/technical.md`.

## Install (unpacked, for now)

- **Chrome / Edge / Brave:** open `chrome://extensions`, turn on **Developer mode**, press **Load
  unpacked** and choose this `extension` folder.
- **Firefox (128 or later):** open `about:debugging`, **This Firefox**, **Load Temporary Add-on…**
  and choose `manifest.json` in this folder (temporary add-ons are removed when Firefox closes).

## First use

1. Open your Game Vault, click the extension's icon and press **Enable on …** (your Game Vault's
   address). The browser asks for permission on that address.
2. In Game Vault, open a source's settings and press **Connect** next to its credential.
3. The first time for each store, the extension asks whether your Game Vault may read your session
   there (**Allow** / **Deny**), and the browser asks for permission on that store.
4. Sign in on the store's page that opened. The tab closes by itself and Game Vault tests and saves
   the source.

**Ubisoft:** Game Vault renews Ubisoft's remember-me ticket every time it uses it, which signs the
browser it came from out of Ubisoft. To keep your normal session, allow the extension in incognito
(`chrome://extensions` → Details → Allow in Incognito): Ubisoft then opens in a private window.

## Permissions and revoking

The extension asks for no website access at install. Each Game Vault address and each store is
allowed when you first use it. The popup lists the enabled addresses and the stores each may read;
remove any of them there.

## Packing for the stores

`task extension:pack` writes `dist/game-vault-connector-<version>.zip`, ready for the Chrome Web Store
or Firefox Add-ons. Nothing is published yet. Privacy: see `PRIVACY.md`.
