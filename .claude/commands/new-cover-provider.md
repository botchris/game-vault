---
description: Add a cover provider (box art source) to the cover chain
argument-hint: <source of box art>
---
Add $ARGUMENTS as a Game Vault cover provider, following `.claude/docs/integrations.md`
("A new cover provider"):

- `Applies` decides without network calls; quota-limited providers respect `HasStoreLink` and
  `Fallback`.
- Candidates best first (portrait before landscape), labelled with where they come from;
  `ImageHosts` declared for the proxy.
- Register in `media.Providers.Covers`, bump `coverLogicChanged` (current UTC time), add the
  `providers.<id>.description` texts (en/es), a test against a fake server, and `docs/technical.md`.
- Measure coverage on the user's real games with the test server (copy of the config, port 8093)
  and report how many games got a cover, from which provider, and which did not.
