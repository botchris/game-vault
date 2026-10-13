---
name: unattended-requests
description: Keep-alive and scheduled scans are jittered so stores do not see a fixed rhythm
metadata:
  type: project
---
- Browser-session sources (Battle.net, EA, Ubisoft) are kept alive by `sync.RunKeepAlive`:
  intervals ±30% at random, first run at a random moment within 10 minutes of start-up.
- Scheduled scans vary ±10% of their interval; overdue sources at start-up are spread over the
  first 30 minutes.
- The background details scan (`media.RunDetailsScanner`) waits pause..2×pause between games,
  1 min ±20% after a complaint, 1 h ±10% between rounds; it is off with `-no-unattended`.
- Every loop uses `internal/application/unattended.Rhythm` (`Vary`, `Between`, `Wait`; injectable
  random source and sleep for tests): sync, valuation, the auto-linker and the details scanner. Do
  not write another jitter or context-aware sleep.
- **Why:** the user asked for it to avoid being flagged as automated. Keep it for any new
  unattended request.
