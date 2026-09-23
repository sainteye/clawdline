#!/bin/bash
set -euo pipefail

usage() {
  echo "usage: tools/deploy-linux-user.sh [--stage-only]" >&2
  exit 2
}

stage_only=0
case "${1:-}" in
  "") ;;
  --stage-only) stage_only=1 ;;
  *) usage ;;
esac
[ "$#" -le 1 ] || usage
[ "$(uname -s)" = Linux ] || { echo "this deploy path is Linux-only" >&2; exit 2; }
[ "$(id -u)" -ne 0 ] || { echo "run the daily deploy as the service user, not root" >&2; exit 2; }

root=$(git rev-parse --show-toplevel)
cd "$root"
commit=$(git rev-parse HEAD)
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

token=$(cat "${CLAWDLINE_NEXT_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}/local-token")
healthy=0
deploy_health_seconds=$(go run ./tools/deploy-limit)
deadline=$((SECONDS + deploy_health_seconds))
while [ "$SECONDS" -lt "$deadline" ]; do
  page=$(curl --connect-timeout 1 --max-time 2 -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:7727/ || true)
  build=$(curl --connect-timeout 1 --max-time 2 -fsS -H "Authorization: Bearer $token" http://127.0.0.1:7727/BUILD.json 2>/dev/null || true)
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
echo "deployed $commit; daemon pid $pid; console 200"
