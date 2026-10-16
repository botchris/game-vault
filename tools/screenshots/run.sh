#!/usr/bin/env bash
# Rebuilds the README's screenshots (docs/images) from a demo catalog (catalog.csv: well-known games,
# no one's real library). Starts a throwaway server on 127.0.0.1:8094 with an empty config, creates a
# demo user, imports the catalog, and lets Chromium in the Playwright image photograph it. Covers come
# from the providers that need no key (Steam and the stores' public art). Needs Docker Desktop
# (host.docker.internal) and a built server (task build). Usually run through `task screenshots`.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
addr="127.0.0.1:8094"
image="mcr.microsoft.com/playwright:v1.55.0-noble@sha256:b27e719ecbfef153e13fd24e8341736733bf2658b229677eb21ff57ff5d7fb29"
dir="$(mktemp -d)"
trap 'pkill -f -- "-addr $addr" 2>/dev/null || true; rm -rf "$dir"' EXIT

[[ -x "$root/bin/gamevault" ]] || { echo "build first: task build" >&2; exit 1; }

pkill -f -- "-addr $addr" 2>/dev/null || true
"$root/bin/gamevault" -addr "$addr" -config-dir "$dir" -backup-interval 0 -no-unattended > "$dir/server.log" 2>&1 &
for _ in $(seq 1 50); do grep -q "game vault started" "$dir/server.log" 2>/dev/null && break; sleep 0.2; done

rpc() { curl -fsS -X POST -H 'Content-Type: application/json' -d "$2" "http://$addr/gamevault.v1.$1" > /dev/null; }
rpc AuthService/Setup '{"username":"demo","password":"demo-screenshots-1"}'
rpc SystemService/ImportCsv "{\"content\":\"$(base64 < "$root/tools/screenshots/catalog.csv" | tr -d '\n')\"}"

mkdir -p "$root/docs/images"
docker run --rm --init --ipc=host \
  -v "$root/tools/screenshots:/opt/shots:ro" -v "$root/docs/images:/out" \
  -v gamevault-e2e-node-modules:/opt/e2e/node_modules -w /opt/e2e \
  -e BASE="http://host.docker.internal:${addr##*:}" -e OUT=/out \
  "$image" sh -c '
    cp /opt/shots/package.json . &&
    [ -d node_modules/playwright-core ] || npm install --no-audit --no-fund playwright-core@1.55.0 &&
    ln -sf /opt/e2e/node_modules /node_modules &&
    node /opt/shots/shoot.mjs'
