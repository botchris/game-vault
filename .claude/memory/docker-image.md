---
name: docker-image
description: Published Docker image botchrishub/game-vault: how it is built, why it trusts private networks
metadata:
  type: project
---
Decided 2026-10-08: Game Vault is published as `botchrishub/game-vault` on Docker Hub (the user's
personal account), multi-arch `linux/amd64` + `linux/arm64`. Versioning (decided the same day):
SemVer git tags starting at `v0.1.0`, read with `git describe`; `task release -- X.Y.Z` tags,
`task docker:publish` only publishes a tagged clean commit (`build/release.sh`).

- `build/Dockerfile` packages the toolchain's output (`bin/release/gamevault-linux-*`, `web/dist`)
  on distroless `static-debian13:nonroot`; nothing compiles in the image build. `.dockerignore`
  whitelists only those paths so `config/` can never reach a build context.
- In a container no request comes from 127.0.0.1, so with the normal default nobody could create
  the user. The user chose Sonarr-style trust: the image sets `GAMEVAULT_TRUSTED_NETWORKS` to the
  private networks. It only replaces the default until the security settings are saved
  (`sqlite.SettingsRepository.DefaultTrustedNetworks`). The proxy and Host rules in
  [[security-model]] still apply.
- On Docker Desktop every client arrives from the gateway (private), so anyone who can reach the
  published port is "trusted": the README says to set this in System → Security.
- `task docker:publish` pushes public content: only run it when the user explicitly asks, after
  they ran `docker login` themselves.
