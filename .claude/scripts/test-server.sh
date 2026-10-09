#!/usr/bin/env bash
# Starts a throwaway Game Vault on http://127.0.0.1:8093 with a copy of the real config, so changes
# can be checked against real data without touching config/ or the user's server on :8080.
#
# Usually run through `task test-server` (which builds first).
#
#   .claude/scripts/test-server.sh          copy the database only (covers are fetched again)
#   .claude/scripts/test-server.sh --full   also copy config/game-data (cached covers and images)
#   .claude/scripts/test-server.sh --stop   stop the test server
#
# The copy lives in $GAMEVAULT_TEST_DIR (default: a temp dir) and the log in its server.log.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
dir="${GAMEVAULT_TEST_DIR:-${TMPDIR:-/tmp}/gamevault-test}"
addr="127.0.0.1:8093"

pkill -f -- "-addr $addr" 2>/dev/null || true
if [[ "${1:-}" == "--stop" ]]; then
  echo "test server stopped"
  exit 0
fi

[[ -x "$root/bin/gamevault" ]] || { echo "build first: task build" >&2; exit 1; }

rm -rf "$dir" && mkdir -p "$dir"
# A consistent copy even while the real server is writing (WAL), with the sqlite3 that ships with
# macOS and most Linux systems. Without it, copy the files: the copy recovers its WAL on open.
# (Never open the live database from a container: SQLite's shared memory does not cross the VM.)
if command -v sqlite3 >/dev/null; then
  sqlite3 "$root/config/gamevault.db" ".backup '$dir/gamevault.db'"
else
  cp "$root/config/gamevault.db" "$dir/"
  [[ -f "$root/config/gamevault.db-wal" ]] && cp "$root/config/gamevault.db-wal" "$dir/"
fi
if [[ "${1:-}" == "--full" && -d "$root/config/game-data" ]]; then
  cp -R "$root/config/game-data" "$dir/game-data"
fi

cd "$root"
# -no-unattended: a keep-alive or scheduled scan here would renew credentials that rotate on every
# use (Ubisoft, Epic, GOG, Xbox…) in the copy, and the real server's would stop working.
nohup ./bin/gamevault -addr "$addr" -config-dir "$dir" -backup-interval 0 -no-unattended > "$dir/server.log" 2>&1 &
for _ in $(seq 1 30); do
  if grep -q "game vault started" "$dir/server.log" 2>/dev/null; then
    echo "test server on http://$addr (data: $dir, log: $dir/server.log)"
    exit 0
  fi
  sleep 0.2
done
echo "the test server did not start; see $dir/server.log" >&2
tail -5 "$dir/server.log" >&2
exit 1
