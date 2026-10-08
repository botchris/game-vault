---
name: toolchain
description: Toolchain image Go version, golangci-lint and its unsafe --fix, why dev:web is a watch build, node_modules volume
metadata:
  type: feedback
---
- The toolchain image needs a Go new enough for the pinned buf (buf 1.73 requires Go ≥ 1.26.7);
  go.mod's `go 1.26.3` is only the minimum.
- A Vite dev server in a container cannot reach the API as "local" (requests come from the
  Docker network, so the server answers 401): that is why `task dev:web` is a watch build served
  by the host server.
- `web/node_modules` lives in a Docker volume (Linux binaries); the host's old
  `web/node_modules` is unused and can be deleted.
- golangci-lint (v2.13.2, 2026-10) is built from source in the image with its Go, so it always
  understands the code's Go version. Its cache is the `gamevault-golangci` volume.
- Never run a bare `golangci-lint run --fix`: staticcheck's SA9004 fix typed every constant in
  mixed `const (ID provider.ID = …; settingToken = "token")` blocks and broke four adapters.
  `task lint:fix` limits fixes to wsl_v5, godot and misspell. See the `write-go` skill.
