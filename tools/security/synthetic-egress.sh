#!/bin/bash
# Reproduce synthetic application-body evidence without production services.
set -euo pipefail
cd "$(dirname "$0")/../.."
if [ "$#" -ne 0 ]; then
  echo "usage: bash tools/security/synthetic-egress.sh" >&2
  exit 2
fi
printf 'Source: '
git rev-parse HEAD
go version
node --version
git --version
# The caller can supply a task-owned GOCACHE and TMPDIR.
# Heavy must wait in place: a detached callback cannot be evidence of a pass.
synthetic_state=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-synthetic-egress.XXXXXX")
trap 'rm -rf -- "$synthetic_state"' EXIT
# The broker wrapper uses its normal identity; only the tests get empty state.
HEAVY_HANDOFF=0 tools/heavy.sh env CLAWDLINE_NEXT_DIR="$synthetic_state" go test -count=1 -v \
  ./internal/transport/cloud ./internal/adapters/push ./internal/adapters/release \
  -run '^TestSyntheticEgress(Cloud|Push|Update)$'
