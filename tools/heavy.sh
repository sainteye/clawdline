#!/bin/bash
# tools/heavy.sh <command> [args…]
#
# Runs a build, a test suite or anything else that needs much of the machine
# through `clawdline heavy`: after the machine's one compile slot and enough
# available memory, at a lower priority, and as the first thing the kernel
# kills if memory still runs out (cmd/clawdline/heavy.go says why).
#
# Its exit status is the command's own, 75 when --max-wait passed before the
# slot and memory were had, or 76 when HEAVY_HANDOFF=1 queued it as a callback.
#
# Sessions do not have `clawdline` on PATH, so this finds the installed one.
# When there is none, or it predates `heavy`, the command runs directly and
# this says so: a missing wrapper is never a reason not to build.
set -euo pipefail

[ "$#" -gt 0 ] || { echo "usage: tools/heavy.sh <command> [args…]" >&2; exit 2; }

cl=""
for cand in "${CLAWDLINE:-}" "$(command -v clawdline 2>/dev/null || true)" \
    "${XDG_DATA_HOME:-$HOME/.local/share}/clawdline-next/current/clawdline"; do
  if [ -n "$cand" ] && [ -x "$cand" ]; then cl=$cand; break; fi
done

if [ -n "$cl" ] && { "$cl" 2>&1 || true; } | grep -q '|heavy|'; then
  heavy_flags=()
  if [ -n "${HEAVY_REASON:-}" ]; then heavy_flags+=(--reason "$HEAVY_REASON"); fi
  if [ "${HEAVY_HANDOFF:-}" = 1 ]; then
    if "$cl" heavy --help 2>&1 | grep -q -- '--handoff'; then
      heavy_flags+=(--handoff)
    else
      echo "tools/heavy.sh: this clawdline predates --handoff; waiting in place" >&2
    fi
  fi
  exec "$cl" heavy ${heavy_flags[@]+"${heavy_flags[@]}"} -- "$@"
fi
echo "tools/heavy.sh: no clawdline with \`heavy\` found; running without the compile slot" >&2
exec "$@"
