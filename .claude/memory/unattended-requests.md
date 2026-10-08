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
- **Why:** the user asked for it to avoid being flagged as automated. Keep it for any new
  unattended request.
