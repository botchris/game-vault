---
name: connector
description: Game Vault Connector browser extension — spike results per store (2026-10-10), recipes, permission model
metadata:
  type: project
---
- **Spike (2026-10-10, Chrome, real accounts): 10/10 stores captured without DevTools.**
  Humble cookie `_simpleauth_sess`; Battle.net all cookies of `account.battle.net/overview`; EA the
  cookies of `accounts.ea.com` (auth check returns `access_token`); PlayStation `npsso`
  (`ca.account.sony.com/api/v1/ssocookie`); Ubisoft localStorage `PRODrememberMe`, read when
  `connect.ubisoft.com/ready` loads after the connect login (its home page redirects to the
  marketing site, so reading there fails); Fanatical localStorage `bsauth`; Epic
  `authorizationCode` fetched from `/id/api/redirect` with the session; GOG / Xbox / Amazon the code
  in the redirect address (Amazon's link opens on amazon.com and lands on www.amazon.com).
- **Design (user's decisions):** generic extension running declarative recipes (data, never code:
  MV3 forbids remote code); hand-over through the Game Vault tab (no server endpoint); loaded
  unpacked, ready to publish later (not published). Ubisoft prefers a private window.
- **Rules:** cookie and storage captures need a readiness condition (`when`), or anonymous values
  are captured before sign-in; hosts outside the opened one must be listed and are confirmed by the
  user; enabling a Game Vault address happens in the popup (a page cannot trigger permission
  prompts); a port alone does not keep an MV3 service worker alive, so the bridge pings it every
  20 s. Redirect recipes take no `when`; private recipes cannot fetch (the extension's requests use
  the normal window's cookies). Prefixes match at a `/`, `?` or `#` boundary. "Remember" is never
  offered on plain-http network addresses (anyone on the network could reuse it).
- The background is tested in Node against a fake `chrome` (`extension/tests/background.test.mjs`):
  the engine tests alone missed that the background never gave the engine its poll timer.
- Real-store checks: on a config copy only Humble and Steam; the rest only on a main server.
- **Store labels (2026-10-12):** what store pages show is data in `extension/ownership/stores.js` (Steam AppID from `/app/<id>`, GOG `card-product`, Humble and Fanatical titles; bundles on Humble `/games/…` and the members' Choice month pages `/membership/home`, Fanatical `/bundle/…` and `/pick-and-mix/…`); Humble matches every humblebundle.com site (language sites such as es.); one content script per allowed store, registered dynamically, resynced on permission changes, and scripts of older versions replaced; labels and marks in closed shadow roots, same size, focus and events whatever the answer, every bundle game gets a mark (empty when not owned); only the background calls `OwnershipService/CheckOwnership` (token, credentials omitted, only the chosen address, 200 items per request, one-minute cache); the token is bound to its address (changing it clears the token and turns the option off) and storage is limited to trusted contexts where supported; the popup warns on plain http that is not loopback; "unavailable" is never "not owned". Fanatical's selectors are checked by the user only (its terms forbid automated access), and its labels only read the page the user is viewing and store nothing. The extension's DOM tests use happy-dom from `web/node_modules`. See [[security-model]].
- **Open tabs (2026-10-14):** a registered content script (`registerContentScripts`) only reaches pages loaded after the registration; Chrome never injects it into open tabs, and Enter in the address bar on a `#fragment` address (Humble Choice) is a same-document navigation, not a reload. After an extension reload or update the old scripts stay in the page, orphaned (`chrome.runtime.id` and `chrome.i18n` gone), and a new `executeScript` gets a new isolated world. So every store-script sync injects into the store's open tabs that do not run the scripts yet (probe `__gvOwnershipStarted` first), `label.js` ignores a second load, and orphaned scripts stop (`alive()`). A "Game Vault Connector" context on a page only exists once something of the extension ran there (store scripts, or `executeScript` such as the popup's "Enable on <this site>" injecting `bridge.js`).
- **End-to-end test:** `task extension:e2e` (Playwright image, `extension/tests/e2e/`): headless Chromium runs extensions; `--load-extension` works in Chromium but not in branded Chrome 137+ (there `Extensions.loadUnpacked` over `--remote-debugging-pipe` with `--enable-unsafe-extension-debugging`); permission prompts cannot be answered headless (the copy declares host permissions; under Xvfb, `xdotool key Tab Return` accepts the prompt); reloading needs developer mode (`developerPrivate.updateProfileConfiguration`), else the reloaded extension comes back disabled; the worker exists before its `chrome.*` APIs do. Checked 2026-10-14 on Chromium 140 and Chrome 155: same results.
- **Store sites are never Game Vault addresses (2026-10-14):** `extension/lib/storesites.js` (store pages + the stores' sign-in/account/community domains, data) is refused by `enableOrigin`, `originAllowed`, `activeSettings` and the popup offer; `forgetStoreSites` migrates old profiles on install/start-up (drops them from `origins`/`grants`, unregisters their bridges, clears the token if the option pointed at one). A new store or sign-in domain goes there too.
