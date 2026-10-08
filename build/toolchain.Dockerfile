# The Game Vault build toolchain: every task in Taskfile.yml runs inside this image, so building,
# testing and generating code need nothing on the host but Docker (and Task to run the tasks).
#
# Versions are pinned here; change one and `task toolchain` rebuilds the image (its tag is a hash
# of this file).

ARG GO_VERSION=1.26.8
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-bookworm-slim AS node

FROM golang:${GO_VERSION}-bookworm

# Node.js and npm for the web UI (Vite, TypeScript, protoc-gen-es).
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

# Protobuf / Connect code generators (protoc-gen-es comes from web/node_modules).
ARG BUF_VERSION=1.73.0
ARG PROTOC_GEN_GO_VERSION=1.36.12
ARG PROTOC_GEN_CONNECT_GO_VERSION=1.21.0
RUN go install github.com/bufbuild/buf/cmd/buf@v${BUF_VERSION} \
 && go install google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOC_GEN_GO_VERSION} \
 && go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v${PROTOC_GEN_CONNECT_GO_VERSION} \
 && rm -rf /root/.cache/go-build /go/pkg/mod

# Go linter (configuration in .golangci.yml). Built from source with this image's Go, so it always
# understands the Go version the code is written in.
ARG GOLANGCI_LINT_VERSION=2.13.2
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${GOLANGCI_LINT_VERSION} \
 && rm -rf /root/.cache/go-build /go/pkg/mod

# The repository is mounted at /src; its .git (if any) belongs to the host user.
ENV GOFLAGS=-buildvcs=false \
    CGO_ENABLED=0 \
    npm_config_update_notifier=false \
    npm_config_fund=false
WORKDIR /src
