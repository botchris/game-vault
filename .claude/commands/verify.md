---
description: Lint, test, build and check the app on a test server with a copy of the config
---
Verify the current state of Game Vault (everything runs in the toolchain container through Task),
stopping at the first failure and reporting it:

1. `task lint` (golangci-lint + buf lint). For blank-line, comment-period or spelling issues run
   `task lint:fix` and re-run; fix the rest by hand following the `write-go` skill.
2. `task test` (go vet, Go tests, TypeScript type check, translations).
3. `task test-server` (builds web + server, then serves a copy of the config on :8093). Open
   http://127.0.0.1:8093/?v=<new number> in the browser pane and check the pages touched by the
   recent changes on desktop and with the mobile preset, then reset the viewport to desktop.
4. `task test-server:stop`.

Summarise in Spanish what passed, what failed and what you checked visually. $ARGUMENTS
