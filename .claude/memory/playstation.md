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
