---
name: api-probing
description: How to discover undocumented APIs safely: bogus-credential probes, GraphQL aliases, lenient decoding
metadata:
  type: feedback
---
- Probe real endpoints with bogus credentials: a clean "invalid code / not authenticated" answer
  proves the client id and request shape without the user's account.
- GraphQL APIs often validate before authenticating: probe field names that way; batch many
  candidates in one request with aliases (`a: game(slug:…) b: …`).
- When the schema is unknown, decode leniently, surface the service's own error verbatim
  ("ubisoft library: …"), and ask the user to paste it after their first real test.
