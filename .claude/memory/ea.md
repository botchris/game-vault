---
name: ea
description: EA app: dead Origin API, GraphQL that validates before auth, public game(slug) art
metadata:
  type: reference
---
As of 2026-10:
- The Origin API is shut down.
- The EA app GraphQL (`service-aggregation-layer.juno.ea.com`) validates queries before
  authenticating: field names can be discovered without an account (see [[api-probing]]).
- `game(slug)` is public and has `packArt.aspect9x16Image`.
- Token: `accounts.ea.com/connect/auth?client_id=ORIGIN_JS_SDK…` with the user's cookies.
