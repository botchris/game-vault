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
   address; in other languages the button is translated). The browser asks for permission on that address.
2. In Game Vault, open a source's settings and press **Connect** next to its credential.
3. The first time for each store, the extension asks whether your Game Vault may read your session
   there (**Allow** / **Deny**), and the browser asks for permission on that store.
4. Sign in on the store's page that opened. The tab closes by itself and Game Vault tests and saves
   the source.

**Ubisoft:** Game Vault renews Ubisoft's remember-me ticket every time it uses it, which signs the
browser it came from out of Ubisoft. To keep your normal session, allow the extension in incognito
(`chrome://extensions` → Details → Allow in Incognito): Ubisoft then opens in a private window.

## What you own, on store pages

1. Only if this browser is not on one of Game Vault's trusted networks: in Game Vault, open
   **System → Security → Access tokens** and create one (it is shown once).
2. In the extension's menu, under **What you own, on store pages**, choose your Game Vault address,
   paste the token, and turn on **Show what I own on store pages**. The browser asks for Steam, GOG,
   Humble Bundle and Fanatical.
3. Open a game on one of those stores: a label next to its title says whether it is in your
   collection (click it to open the game in Game Vault). Humble and Fanatical bundles mark each game
   you own and say how many you have.

"Game Vault unavailable" means Game Vault did not answer or refused the token: hover over it to see
why. Turning the option off removes the labels and gives the store sites' access back. The token is
bound to the address it was saved for: changing the address clears it and turns the option off. If
the extension talks to Game Vault from another device, use HTTPS or a private tunnel: the popup warns
when the address is plain http. Humble pages on any humblebundle.com site (language sites such as
es.humblebundle.com included) are covered. On Fanatical the labels only read the page you are viewing
and store nothing.

## Permissions and revoking

The extension asks for no website access at install. Each Game Vault address and each store is
allowed when you first use it. The popup lists the enabled addresses and the stores each may read;
remove any of them there.

## Packing for the stores

`task extension:pack` writes `dist/game-vault-connector-<version>.zip`, ready for the Chrome Web Store
or Firefox Add-ons. Nothing is published yet. Privacy: see `PRIVACY.md`.
