---
name: thegamesdb
description: TheGamesDB key, quota check, and hotlink protection
metadata:
  type: reference
---
As of 2026-10:
- The key lives in the provider settings; a free key has roughly 1,000 requests a month.
- `API/Limit` costs nothing and reports the remaining quota; unknown keys answer 200 with 0/0.
- Its CDN rejects hotlinked images: always go through the proxy (see [[local-images]]).
- No documented per-second rate, but a burst of parallel lookups (a library page of new console
  editions) got HTTP 429 with `{"status":"Unknown Error Code"}` on 2026-10-10. The adapter now
  sends one request at a time, 500 ms apart (`minGap`), and maps 429 to `provider.ErrRateLimited`,
  which never records a "no cover" marker nor caches a sheet. Waits over 5 s or a queue of 8 fail
  fast as rate-limited; a 429 starts a 30 s back-off; `Test` bypasses the throttle.
