---
name: gog
description: GOG: Galaxy credentials, login redirects, public box art API
metadata:
  type: reference
---
As of 2026-10:
- Uses GOG Galaxy's public client credentials.
- `embed.gog.com` answers a bad token with a 302 to the login page: don't follow redirects.
- `api.gog.com/v2/games/{id}` is public and gives the box art.
