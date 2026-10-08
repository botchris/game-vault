---
name: status-integrations
description: Which store integrations the user has confirmed with real accounts and which are untested (2026-10-08)
metadata:
  type: project
---
Confirmed by the user with real accounts:
- Humble Bundle, Steam, Epic (paste the JSON with `authorizationCode`, not the emailed code),
  GOG, EA app, Ubisoft Connect (after the fixes in [[ubisoft]]), Xbox (after adding
  `"beneficiaries": []`, see [[xbox]]).
- Battle.net works, but its session expired once and needed new cookies; keep-alive was added
  afterwards. **Not yet confirmed** that keep-alive is enough: if it fails again, the error says
  where the SSO renewal ended — use that to look for another way. See [[battlenet]].

Implemented but **not yet tested by the user**:
- PlayStation: NPSSO → session, purchased PS4/PS5 games, PS Plus filtered by `membership`.
  Unknown: the real response shape and how PS Plus games are marked. See [[playstation]].
- Xbox Game Pass filtering assumes `acquisitionType: "Recurring"`; not confirmed.

Security redesign (Sonarr-style, see [[security-model]]): built and tested on the test server
(user creation, required authentication, sign-in, `-reset-auth`); **not yet confirmed** by the
user on their real server.

**How to apply:** when the user reports a result, update this file (and the store's file).
