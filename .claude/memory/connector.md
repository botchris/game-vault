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
