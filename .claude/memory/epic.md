---
name: epic
description: Epic: launcher credentials, catalog without namespace, client-credentials token for covers
metadata:
  type: reference
---
As of 2026-10:
- Uses the Epic launcher's public client credentials (the same as Legendary).
- The user pastes the JSON page with `authorizationCode`, not the code Epic emails.
- Catalog `bulk/items` works without a namespace and with a client-credentials token (no user
  session needed for covers).
