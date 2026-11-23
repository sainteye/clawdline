#!/bin/bash
# Without the Linux heavy wrapper, native builds use the person's default Go
# cache, which Go never bounds by size. After the command, still inside the
# compile slot, `clawdline go-cache trim` keeps it under the registered budget
# (internal/domain/capacity/heavy_cache.go). The trim never changes the
# command's exit status, never touches an explicit GOCACHE, and keeps every
# entry used within the protection window, so concurrent direct `go test`
# runs keep what they are using.
set -uo pipefail
[ "$#" -gt 0 ] || { echo 'usage: tools/run-heavy-shared-cache.sh <command> [args...]' >&2; exit 2; }
"$@"
status=$?
bin=${CLAWDLINE_GO_CACHE_TRIM:-}
if [ -n "$bin" ] && [ -x "$bin" ] && [ -z "${GOCACHE:-}" ] && command -v go >/dev/null 2>&1; then
  "$bin" go-cache trim || echo "tools/heavy.sh: shared Go cache trim refused or failed (exit $?); nothing unknown was removed" >&2
fi
exit "$status"
