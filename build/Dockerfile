# The Game Vault image published to Docker Hub. Built by `task docker:build` / `task docker:publish`
# from what the toolchain already produced (bin/release and web/dist), so nothing is compiled here
# and one build serves every platform without emulation.

# An empty data directory owned by the image's non-root user, so a named volume mounted on it is
# writable. Created on the build machine's platform: no emulated RUN for other architectures.
FROM --platform=$BUILDPLATFORM busybox:1.37 AS dirs
RUN mkdir -p /config

# Static Go binary: distroless has only CA certificates (for the stores' HTTPS), time zone data and
# a non-root user. No shell, no package manager.
FROM gcr.io/distroless/static-debian13:nonroot

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown

LABEL org.opencontainers.image.title="Game Vault" \
      org.opencontainers.image.description="Self-hosted catalog of the games you own: store libraries, keys and physical discs" \
      org.opencontainers.image.source="https://github.com/botchris/game-vault" \
      org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=dirs --chown=nonroot:nonroot /config /config
COPY web/dist /app/web/dist
COPY bin/release/gamevault-${TARGETOS}-${TARGETARCH} /app/gamevault

# Inside a container the server must listen on every interface (publishing the port decides who
# can reach it), and requests never come from 127.0.0.1: the host's own browser arrives from
# Docker's gateway and the home network keeps its addresses. So the private networks are trusted
# until the security settings are saved from the UI, the way Sonarr trusts local addresses.
ENV GAMEVAULT_ADDR=0.0.0.0:8080 \
    GAMEVAULT_CONFIG_DIR=/config \
    GAMEVAULT_UI_DIR=/app/web/dist \
    GAMEVAULT_TRUSTED_NETWORKS=127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,::1/128,fc00::/7,fe80::/10

VOLUME /config
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/gamevault"]
