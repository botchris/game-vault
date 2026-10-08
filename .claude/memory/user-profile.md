---
name: user-profile
description: Who the user is and how they work: language, how they run the app, priorities
metadata:
  type: user
---
- Talks in Spanish; wants the code, docs, log and error messages in English, and the UI in
  English + Spanish.
- Runs the real server with `task run` on 127.0.0.1:8080 and restarts it to pick up changes.
  The product must not depend on or mention Tailscale or any other VPN (see [[security-model]]).
- Wants no build dependencies on the host: everything compiles in Docker (only Task + Docker on
  the host). See [[toolchain]].
- Cares about not being flagged as a bot by the stores: unattended requests must be jittered.
  See [[unattended-requests]].
- UI taste: Emil Kowalski's design-engineering principles (`.claude/docs/ui.md`).
- Design for libraries of ~1,000 games spread over every supported store plus scanned discs.
