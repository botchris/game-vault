---
name: thegamesdb
description: TheGamesDB key, quota check, and hotlink protection
metadata:
  type: reference
---
As of 2026-10:
- The key lives in the provider settings; the user has roughly 1,000 requests a month.
- `API/Limit` costs nothing and reports the remaining quota; unknown keys answer 200 with 0/0.
- Its CDN rejects hotlinked images: always go through the proxy (see [[local-images]]).
