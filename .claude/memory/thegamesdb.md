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
- 2026-10-10: a key that used up its monthly allowance gets HTTP 429 `{"status":"Unknown Error
  Code"}` on every request, not 403 (the 30 s back-off then looped for the rest of the month,
  every sheet saying "busy"). `API/Limit` still answers 200 with 0 remaining and the refresh timer.
  So a 429 checks `API/Limit` (at most every 5 min); with 0 left the adapter sends nothing until
  now + `allowance_refresh_timer` (6 h when 0/missing) and lookups return
  `provider.ErrQuotaExhausted` (matches `ErrRateLimited`: no marker, no cached sheet) saying when
  it renews; the cover endpoint answers 503 with `Retry-After: 3600`.
