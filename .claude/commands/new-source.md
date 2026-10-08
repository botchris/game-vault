---
description: Add a new store/account as a Game Vault source (research, adapter, tests, UI texts, docs)
argument-hint: <store name>
---
Add $ARGUMENTS as a new source of Game Vault, following `.claude/docs/integrations.md`
("A new source") and the safety rules in `CLAUDE.md`.

1. Research how open-source launchers and libraries read this store's library (sign-in flow,
   public client ids, endpoints) and whether its terms or bot protection rule an integration out.
   If they do, stop and explain the options to the user instead of working around them.
2. Probe the real endpoints from the shell with bogus credentials to confirm the client id,
   request shape and error format. Write findings into `.claude/memory/<store>.md` and add it to the
   index in `.claude/MEMORY.md` (External services).
3. Implement the adapter in `internal/adapters/outbound/<name>/` with tests against a fake server,
   register it in `cmd/gamevault/main.go`, add the en/es texts (help as numbered steps) and a
   bullet in `docs/technical.md` (Sources) plus a row in the README's platforms table. Add a cover provider too if the store has public box art.
4. Run `/verify` (everything in containers via Task), then test "Test connection" in the dialog
   with bogus credentials.
5. Tell the user (in Spanish) how to connect, what is imported and skipped, what was verified
   against the real service and what only against fakes, and which error to paste back.
