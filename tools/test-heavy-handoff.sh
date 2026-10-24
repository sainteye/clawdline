#!/bin/bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-handoff-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
cat > "$scratch/clawdline" <<'MOCK'
#!/bin/bash
if [ "$#" -eq 0 ]; then echo '|heavy|'; exit 2; fi
if [ "${2:-}" = --help ]; then
  if [ "${OLD_CLI:-}" = yes ]; then echo 'heavy -- command'; else echo 'heavy --handoff -- command'; fi
  exit 2
fi
printf '%s\n' "$@" > "$HANDOFF_CALLS"
exit "${COMMAND_EXIT:-0}"
MOCK
chmod +x "$scratch/clawdline"
export CLAWDLINE=$scratch/clawdline HANDOFF_CALLS=$scratch/calls HEAVY_HANDOFF=1
unset CLAWDLINE_HEAVY
bash "$root/tools/heavy.sh" true 2> "$scratch/new.log"
grep -Fxq -- --handoff "$HANDOFF_CALLS"
! grep -q predates "$scratch/new.log"
OLD_CLI=yes bash "$root/tools/heavy.sh" true 2> "$scratch/old.log"
! grep -Fxq -- --handoff "$HANDOFF_CALLS"
grep -q predates "$scratch/old.log"
if COMMAND_EXIT=17 bash "$root/tools/heavy.sh" true > /dev/null 2>&1; then exit 1; else [ "$?" = 17 ]; fi
bash "$root/tools/heavy.sh" --compile bash build-app.sh
grep -Fxq -- --compile "$HANDOFF_CALLS"
grep -Fxq -- build-app.sh "$HANDOFF_CALLS"
echo 'heavy handoff capability: PASS'
