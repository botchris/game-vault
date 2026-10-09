# Game Vault Connector: connecting sources in one click

Status: approved design, waiting for the implementation plan. Branch `feature/connector-extension`.
The feasibility spike lives on branch `spike/connector-extension` (not merged).

## Why

Most sources need the user to dig a session cookie, a storage value or a sign-in code out of the
browser by hand: DevTools, Application, Network, copying headers. A web page cannot read another
site's cookies or see where a store redirects after sign-in, so Game Vault cannot do it itself.
A browser extension can. The user still signs in on each store's own page, in their own browser
(2FA and captchas included); the extension only collects the result and hands it to Game Vault.
Nothing signs in for the user and nothing works around bot protection.

## Spike results (2026-10-10, Chrome, the user's own accounts)

Every source that needs a browser credential was captured without DevTools:

| Source | Captured | How |
|---|---|---|
| Humble | cookie `_simpleauth_sess` on `www.humblebundle.com` | cookie |
| Battle.net | all cookies for `account.battle.net/overview`, as a `Cookie` header | cookies |
| EA | cookies for `accounts.ea.com` (and `access_token` returned by the auth check) | cookies + fetch |
| PlayStation | cookie `npsso` on `ca.account.sony.com` (also returned by `/api/v1/ssocookie`) | cookie |
| Ubisoft | localStorage `PRODrememberMe` on `connect.ubisoft.com`, read when `/ready` loads after the connect login (its home page redirects to the marketing site) | storage |
| Fanatical | localStorage `bsauth` on `www.fanatical.com` | storage |
| Epic | `authorizationCode` from `www.epicgames.com/id/api/redirect?clientId=…` fetched with the session | fetch |
| GOG | `code` from `embed.gog.com/on_login_success` | redirect |
| Xbox | `code` from `login.live.com/oauth20_desktop.srf` | redirect |
| Amazon | `openid.oa2.authorization_code` on `www.amazon.com` after the per-run sign-in link | redirect |

Steam needs only an API key and a profile, nothing from the browser.

## Decisions taken with the user

- **Distribution:** the extension ships in the repository and is loaded unpacked for now; the code
  is ready to publish on the Chrome Web Store and Firefox Add-ons later (a packing task, a privacy
  text), when the user decides.
- **Hand-over:** through the open Game Vault tab. The extension returns what it captured to the tab
  that asked; the page fills the field and saves through the RPC it already uses. No new server
  endpoint, pairing or token.
- **Generic extension:** the extension knows nothing about stores. Game Vault sends it declarative
  **recipes** (JSON data, never code) built from a few primitives; a new store whose sign-in fits
  them needs no extension update. Code sent from outside is not run: Manifest V3 and the extension
  stores forbid remote code, and it would turn any script injection in the Game Vault page into
  access to every allowed session.
- **Ubisoft:** opened in a private window when the user allowed the extension in incognito mode;
  otherwise in a normal tab, with a warning that Game Vault's use of the ticket will sign that
  browser out of Ubisoft.

## Decisions made while planning

- **Readiness (`when`):** recipes gain an optional condition (`urlPrefix`, `contains`, `fetch`).
  Cookies and storage can exist before sign-in; without it the extension would capture anonymous
  values and close the tab first.
- **Enabling from the popup:** a page cannot make the extension ask the browser for a permission,
  so "Enable on this address" lives in the extension's popup; the dialog explains it.
- **Timeout:** `timeoutSeconds` (int) in the recipe's JSON.
- **Ports:** the bridge talks to the background over a port, so a long sign-in keeps the service
  worker alive; if it drops, the page gets an error instead of hanging.
- **Packing:** `task extension:pack` uses a small Go tool (`tools/zipdir`); the toolchain has no zip.

## Recipes

### Shape (`internal/domain/schema`)

```go
// SignInRecipe tells the browser extension how to collect a setting's value from the store's own
// sign-in: what to open and what to capture once the user is signed in. It is data, never code.
type SignInRecipe struct {
	Version int    // primitives this recipe needs; the extension refuses newer ones (1)
	Open    string // HTTPS address to open; empty: the field's HelpURL (e.g. Amazon's per-run link)
	Capture Capture
	Private bool          // prefer a private window when the extension is allowed in one
	Hosts   []string      // other hosts the capture may read (EA's accounts.ea.com); each one is confirmed by the user
	Timeout time.Duration // 0: five minutes
}

// Capture is exactly one of its members.
type Capture struct {
	Cookie   *CookieCapture   // one cookie's value
	Cookies  *CookiesCapture  // every cookie for an address, as a Cookie header
	Storage  *StorageCapture  // a localStorage value of a page of the site
	Redirect *RedirectCapture // a query parameter of the address the store redirects to
	Fetch    *FetchCapture    // a JSON field of an address fetched with the user's session
}

type CookieCapture struct{ URL, Name string }
type CookiesCapture struct{ URL string }
type StorageCapture struct{ Origin, Key, Path string } // Path: read when this page loads ("" any)
type RedirectCapture struct{ Prefix, Param string }
type FetchCapture struct{ URL, Field string }
```

- `schema.Field` gains `SignIn *SignInRecipe`: the field the captured value fills.
- Validation (`Recipe.Validate`, run by `plugintest` for every source): version known, every
  address `https://`, exactly one capture, the capture's host equal to the opened host or listed in
  the recipe's own `Hosts` (EA's `accounts.ea.com`, PlayStation's `ca.account.sony.com`), timeout at
  most 10 minutes.

### Primitives the extension runs (version 1)

- `open(url)`: a new tab (or a private window when `Private` and allowed).
- **Capture is retried** each time a page of the opened host finishes loading in that tab, and
  once right after opening, so a user already signed in is captured in about a second:
  - `cookie(url, name)`: the cookie's value (HttpOnly ones included);
  - `cookies(url)`: `name=value; …` for every cookie sent to that address;
  - `storage(origin, key, path)`: `localStorage.getItem(key)` in the tab, when its address is on
    `origin` (and `path`, if set);
  - `redirect(prefix, param)`: when the tab commits an address starting with `prefix`, its `param`;
  - `fetch(url, field)`: `fetch(url, {credentials: 'include'})`, the JSON field (or `field=` in the
    text).
- The tab closes once the value is captured. Errors: `cancelled` (tab closed), `timeout`,
  `denied` (the user refused a permission or the confirmation), `unsupported` (newer version).

## Protocol between the page and the extension

- **Bridge:** a content script the extension registers only on Game Vault addresses the user allowed
  (`chrome.scripting.registerContentScripts`), relaying `window.postMessage` ↔ `runtime.sendMessage`,
  checking `event.source === window` and the page's origin.
- **Messages** (page → extension, all with `type: "gamevault-connector"`):
  - `{op: "hello"}` → `{op: "hello", version, recipeVersion}`;
  - `{op: "connect", id, source, field, recipe}` → `{op: "result", id, value}` or
    `{op: "error", id, code, message}`; `id` is a random single-use request id.
- **Values** go only to the tab that asked, in the reply; the extension never stores or logs them.

## Extension (`extension/`)

- Plain JavaScript, Manifest V3, no build step and no dependencies; one manifest valid for Chrome
  and Firefox (MV3 `browser_specific_settings` for Firefox).
- Permissions: `cookies`, `webNavigation`, `scripting`, `tabs`, `storage`. **No host permissions at
  install**: every Game Vault and store address is an optional host permission, asked when first
  needed (the browser's own prompt).
- Files: `manifest.json`; `background.js` (recipe validation and engine); `bridge.js`;
  `lib/*.js` (pure functions: validate a recipe, match a redirect, build a Cookie header, check an
  origin); `popup.html/js` (allowed Game Vault addresses and confirmed store hosts, each revocable;
  version); `confirm.html/js`.
- **Confirmation:** the first time a Game Vault address asks for a store host, the extension opens
  its own small window: "Game Vault at 192.168.1.10 wants your session on humblebundle.com" with
  Allow / Deny and "Remember". Remembered answers live in `storage.local` as `origin → hosts`.
- **First use:** in a source dialog, "Enable the extension on this address" asks the browser for the
  Game Vault origin permission and registers the bridge there.
- **Limits:** one sign-in at a time per Game Vault tab; HTTPS only; the recipe's hosts only.
- `task extension:pack` writes `dist/game-vault-connector-<version>.zip`. `extension/README.md`
  (install unpacked, first use, permissions, revoking) and `extension/PRIVACY.md` (what it reads,
  that it sends it only to the user's own Game Vault tab, that it stores no credentials).

## Game Vault

### Sources and API

- Each source's credential field declares its recipe in Go next to its descriptor (the table above).
  Amazon's recipe has no `Open`: the server puts the current per-run sign-in link (its `HelpURL`)
  into the description it sends.
- The source description (`SourceTypeDescriptor` → `SettingField`) gains `sign_in` as the recipe's
  JSON. No new endpoint; saving and testing credentials are unchanged.

### "Paste anything"

Every credential field accepts what a user would naturally copy, normalized server-side before the
value is used: a full address (GOG, Xbox, Amazon codes), Epic's and PlayStation's JSON, a `Cookie`
header containing the needed cookie (Humble's `_simpleauth_sess`, Fanatical's `bsauth` JSON), quotes
and whitespace. Several sources already do part of this; it becomes consistent, with a test per
format.

### Source dialog (web)

- The page asks `hello` on load; with the extension present and the address enabled, each field with
  a recipe shows **Connect**. With the extension present but this address not enabled: **Enable the
  extension on this address**. Without it: a short note linking to `extension/README.md`; the
  current steps stay as the fallback.
- Connect: "Sign in in the tab that opened…" with Cancel; on the value, fill the field, run Test
  connection, save when it passes (otherwise show the error and keep the value); errors from the
  extension shown in words (cancelled, timed out, permission denied, update the extension).
- Every field also gets a clear **Open sign-in page** button (the D quick win), with or without the
  extension.
- Fanatical: Connect stays disabled until the risk consent is ticked, as saving is today.
- Ubisoft: the dialog shows the private-window note or the sign-out warning.
- Every text in `en.json` and `es.json`.

## Testing

- Go: recipe validation; `plugintest` on every source's recipe; "paste anything" per source and
  format; the description carries recipes with Amazon's current link.
- Extension: Node tests (`web/tests`-style, run by `task test`) for the pure functions: recipe
  validation, redirect matching, Cookie header, origin and host checks; `node --check` on every file.
- Dialog: on the test server with a simulated extension injected from the browser (it answers the
  protocol): detection, enable, Connect, fill + test + save, cancel, timeout, errors; desktop and
  mobile.
- Real stores (the user): load the extension unpacked and connect each source. On a **copy** of the
  config only Humble and Steam; the rest on the user's real server after merging (their sessions
  rotate or change on use).

## Delivery

Branch `feature/connector-extension`, one PR to `main`, CI green before merging, nothing published
to extension stores. `extension/README.md`, `extension/PRIVACY.md`, `docs/technical.md` (recipes,
protocol), `docs/plugins.md` (declaring a recipe), one README line, and a project memory with the
spike results. No version is tagged until the user asks.
