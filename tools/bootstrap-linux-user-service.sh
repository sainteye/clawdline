#!/bin/bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ] || [ "$#" -ne 1 ]; then
  echo "usage: sudo tools/bootstrap-linux-user-service.sh <service-user>" >&2
  exit 2
fi

service_user=$1
entry=$(getent passwd "$service_user")
[ -n "$entry" ] || { echo "no account named $service_user" >&2; exit 2; }
service_uid=$(printf '%s' "$entry" | cut -d: -f3)
service_gid=$(printf '%s' "$entry" | cut -d: -f4)
service_home=$(printf '%s' "$entry" | cut -d: -f6)
case "$service_home" in
  /*) ;;
  *) echo "service account has no absolute home" >&2; exit 2 ;;
esac

repo=$(cd "$(dirname "$0")/.." && pwd)
# Only tools/test-bootstrap-linux-user-service.sh sets this, to keep /etc and /run inside a scratch
# directory. sudo's default env_reset drops it from a real run.
sysroot=${CLAWDLINE_BOOTSTRAP_SYSROOT:-}
data_root=$service_home/.local/share/clawdline-next
current=$data_root/current
[ -L "$current" ] && [ -x "$current/clawdline" ] && [ -f "$current/dist/BUILD.json" ] || {
  echo "no staged release; run tools/deploy-linux-user.sh --stage-only as $service_user first" >&2
  exit 2
}

unit_dir=$service_home/.config/systemd/user
install -d -m 0700 -o "$service_uid" -g "$service_gid" "$unit_dir"
install -m 0600 -o "$service_uid" -g "$service_gid" \
  "$repo/tools/systemd/clawdline-next.service" "$unit_dir/clawdline-next.service"

loginctl enable-linger "$service_user"
systemctl start "user-runtime-dir@$service_uid.service" "user@$service_uid.service"
runtime=$sysroot/run/user/$service_uid
[ -S "$runtime/bus" ] || { echo "user service manager did not create $runtime/bus" >&2; exit 1; }

as_user() {
  runuser -u "$service_user" -- env \
    HOME="$service_home" XDG_RUNTIME_DIR="$runtime" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime/bus" \
    systemctl --user "$@"
}

# A machine installed before the user unit existed runs a system-wide clawdline-next.service that
# this retires; a fresh machine never had one, and every step that touches it is skipped there.
# systemctl answers LoadState=not-found for a unit with no file, and fails outright only when it
# cannot ask systemd at all, which stops the script.
migrating=0
load_state=$(systemctl show --property=LoadState --value clawdline-next.service)
[ "$load_state" = not-found ] || migrating=1

if [ "$migrating" -eq 1 ]; then
  # Preserve tmux and assistant processes that the old system service opened.
  dropin=$sysroot/etc/systemd/system/clawdline-next.service.d/90-preserve-sessions.conf
  install -d -m 0755 "$(dirname "$dropin")"
  printf '[Service]\nKillMode=process\n' >"$dropin"
  systemctl daemon-reload
fi

# Undo this run: stop the user unit and, when migrating, give the machine its system unit back.
restore() {
  as_user disable --now clawdline-next.service || true
  if [ "$migrating" -eq 1 ]; then
    systemctl enable --now clawdline-next.service || true
    echo "$1; restored the system service" >&2
  else
    echo "$1; stopped and disabled it" >&2
  fi
  exit 1
}

as_user daemon-reload
as_user enable clawdline-next.service
if [ "$migrating" -eq 1 ] && systemctl is-active --quiet clawdline-next.service; then
  systemctl stop clawdline-next.service
fi
as_user start clawdline-next.service || restore "user service failed to start"
if [ "$migrating" -eq 1 ]; then
  systemctl disable clawdline-next.service >/dev/null
fi

expected=$(sed -n 's/.*"stamp":"\([0-9a-f]*\)".*/\1/p' "$current/dist/BUILD.json")
token=$(cat "$service_home/.config/clawdline-next/local-token")
deploy_health_seconds=$(
  cd "$repo"
  runuser -u "$service_user" -- env HOME="$service_home" \
    PATH="$service_home/.local/bin:/usr/local/bin:/usr/bin:/bin" \
    go run ./tools/deploy-limit
)
healthy=0
deadline=$((SECONDS + deploy_health_seconds))
while [ "$SECONDS" -lt "$deadline" ]; do
  page=$(curl --connect-timeout 1 --max-time 2 -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:7727/ || true)
  build=$(curl --connect-timeout 1 --max-time 2 -fsS -H "Authorization: Bearer $token" http://127.0.0.1:7727/BUILD.json 2>/dev/null || true)
  stamp=$(printf '%s' "$build" | sed -n 's/.*"stamp":"\([0-9a-f]*\)".*/\1/p')
  if [ "$page" = 200 ] && [ "$stamp" = "$expected" ]; then
    healthy=1
    break
  fi
  sleep 1
done
[ "$healthy" -eq 1 ] || restore "user service did not prove its console and exact release"

echo "bootstrap complete: future deploys run as $service_user with tools/deploy-linux-user.sh"
