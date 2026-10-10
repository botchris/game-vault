---
name: playstation
description: PlayStation: NPSSO sign-in, registered GraphQL hashes, CSRF header
metadata:
  type: reference
---
As of 2026-10:
- NPSSO from `ca.account.sony.com/api/v1/ssocookie` (the user must be signed in on
  playstation.com).
- The library GraphQL only runs registered query hashes, and checks the hash before auth (so
  hashes can be verified without an account).
- Requests need `Content-Type: application/json` (CSRF guard).
- PS Store covers dropped (2026-10-10): PS games already get covers from Steam or TheGamesDB, and playstation.com's website terms (section 10) forbid automated access, so store
  pages are never read. The library query's `image.url` (square MASTER art) is still unused.
- The library lists apps, demos, betas, soundtracks and videos as "PS4" with `CUSA…` ids: nothing
  tells them apart. They are removed by hand with "Remove and do not import again" (source exclusions).
