#!/bin/bash
set -euo pipefail

usage() {
  echo "usage: tools/deploy-linux-user.sh [--stage-only] [--rev <commit>]" >&2
  exit 2
}

# --rev builds that commit instead of HEAD. The build already runs in an
# ephemeral checkout at the commit, so the shared checkout's HEAD never moves;
# `clawdline update --apply` uses it to deploy the cloud's latest stamp
# (docs/updates.md).
stage_only=0
rev=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --stage-only) stage_only=1; shift ;;
    --rev) [ "$#" -ge 2 ] && [ -n "$2" ] || usage; rev=$2; shift 2 ;;
    *) usage ;;
  esac
done
[ "$(uname -s)" = Linux ] || { echo "this deploy path is Linux-only" >&2; exit 2; }
[ "$(id -u)" -ne 0 ] || { echo "run the daily deploy as the service user, not root" >&2; exit 2; }

root=$(git rev-parse --show-toplevel)
cd "$root"
if [ -n "$rev" ]; then
  commit=$(git rev-parse --verify --quiet "$rev^{commit}") || {
    echo "not a commit in this checkout: $rev (run git fetch origin main first)" >&2
    exit 2
  }
else
  commit=$(git rev-parse HEAD)
fi
data_root=${XDG_DATA_HOME:-$HOME/.local/share}/clawdline-next
release=$data_root/releases/$commit
mkdir -p "$data_root/releases"

tools/check-worktrees.sh --ephemeral --rev "$commit" -- \
  tools/build-linux-user-release.sh "$release" "$commit"

if [ "$stage_only" -eq 1 ]; then
  current=$data_root/current
  if [ ! -e "$current" ] && [ ! -L "$current" ]; then
    next=$data_root/.current-$commit-$$
    ln -s "$release" "$next"
    mv -Tf "$next" "$current"
  fi
  echo "release staged for the one-time service bootstrap: $release"
  exit 0
fi

unit=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/clawdline-next.service
[ -f "$unit" ] || {
  echo "the user service is not installed; run tools/deploy-linux-user.sh --stage-only, then the one-time bootstrap as root" >&2
  exit 2
}

runtime=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
[ -S "$runtime/bus" ] || {
  echo "the user service manager is unavailable; the one-time loginctl linger bootstrap is still required" >&2
  exit 2
}
export XDG_RUNTIME_DIR=$runtime
export DBUS_SESSION_BUS_ADDRESS=unix:path=$runtime/bus

current=$data_root/current
previous=""
if [ -L "$current" ]; then
  previous=$(readlink "$current")
fi
next=$data_root/.current-$commit-$$
ln -s "$release" "$next"
mv -Tf "$next" "$current"

rollback() {
  if [ -n "$previous" ]; then
    restore=$data_root/.rollback-$$
    ln -s "$previous" "$restore"
    mv -Tf "$restore" "$current"
    systemctl --user restart clawdline-next.service || true
    echo "deployment failed; restored $previous" >&2
  else
    echo "deployment failed and there was no previous release to restore" >&2
  fi
}
trap rollback ERR

systemctl --user daemon-reload
systemctl --user restart clawdline-next.service

# The port and state directory the unit gives the daemon, which is what it
# listens on; a unit that names neither uses the defaults.
unit_env=$(systemctl --user show clawdline-next.service -p Environment --value || true)
unit_value() {
  printf '%s\n' "$unit_env" | tr ' ' '\n' | sed -n "s/^\"\{0,1\}$1=\(.*\)/\1/p" | sed 's/"$//' | tail -n 1
}
port=$(unit_value CLAWDLINE_NEXT_PORT)
port=${port:-7727}
state_dir=$(unit_value CLAWDLINE_NEXT_DIR)
state_dir=${state_dir:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}
token=$(cat "$state_dir/local-token")
healthy=0
deploy_health_seconds=$(go run ./tools/deploy-limit)
deadline=$((SECONDS + deploy_health_seconds))
while [ "$SECONDS" -lt "$deadline" ]; do
  page=$(curl --connect-timeout 1 --max-time 2 -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:$port/ || true)
  build=$(curl --connect-timeout 1 --max-time 2 -fsS -H "Authorization: Bearer $token" http://127.0.0.1:$port/BUILD.json 2>/dev/null || true)
  stamp=$(printf '%s' "$build" | sed -n 's/.*"stamp":"\([0-9a-f]*\)".*/\1/p')
  if [ "$page" = 200 ] && [ "$stamp" = "$commit" ]; then
    healthy=1
    break
  fi
  sleep 1
done
[ "$healthy" -eq 1 ] || false
trap - ERR

pid=$(systemctl --user show clawdline-next.service -p MainPID --value)
echo "deployed $commit; daemon pid $pid; console 200 on port $port"
