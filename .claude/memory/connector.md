---
name: connector
description: Game Vault Connector browser extension — spike results per store (2026-10-10), recipes, permission model
metadata:
  type: project
---
- **Spike (2026-10-10, Chrome, the user's accounts): 10/10 stores captured without DevTools.**
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
  prompts); the bridge uses a port so long sign-ins keep the service worker alive.
- Real-store checks: on a config copy only Humble and Steam; the rest on the real server.
