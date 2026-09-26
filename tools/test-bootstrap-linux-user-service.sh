#!/bin/sh
# Run tools/bootstrap-linux-user-service.sh against stub systemctl, loginctl, runuser, getent,
# curl and go, on two machines: a fresh one that never had the system-wide clawdline-next.service,
# and one migrating from it.
#
# A fresh machine used to abort at `systemctl disable clawdline-next.service`, a step that only
# means something when there is a system unit to retire. This holds that both machines finish, that
# a second run is harmless, that a fresh machine is never told a system service was touched or
# restored, and that a real failure still stops the script.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-bootstrap.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
uid=$(id -u)
gid=$(id -g)
stamp=0123abcd

# world <name>: a scratch machine with a staged release, a stub PATH and an empty call log.
# Names stay short because macOS caps a socket path at 104 bytes.
world() {
  w=$tmp/$1
  home=$w/home
  bin=$w/bin
  state=$w/state
  mkdir -p "$bin" "$state" "$home/.local/bin" "$home/.config/clawdline-next" \
    "$home/.local/share/clawdline-next/releases/r1/dist" "$w/r/run/user/$uid"
  printf 'x' >"$home/.local/share/clawdline-next/releases/r1/clawdline"
  chmod +x "$home/.local/share/clawdline-next/releases/r1/clawdline"
  printf '{"stamp":"%s"}\n' "$stamp" >"$home/.local/share/clawdline-next/releases/r1/dist/BUILD.json"
  ln -s releases/r1 "$home/.local/share/clawdline-next/current"
  printf 'token\n' >"$home/.config/clawdline-next/local-token"
  python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$w/r/run/user/$uid/bus"
  : >"$w/calls"

  cat >"$bin/id" <<'EOF'
#!/bin/sh
[ "$1" = -u ] && { echo 0; exit 0; }
exec /usr/bin/id "$@"
EOF
  cat >"$bin/getent" <<EOF
#!/bin/sh
[ "\$1 \$2" = "passwd svc" ] || exit 2
echo "svc:x:$uid:$gid::$home:/bin/sh"
EOF
  cat >"$bin/loginctl" <<EOF
#!/bin/sh
echo "loginctl \$*" >>"$w/calls"
EOF
  cat >"$bin/runuser" <<'EOF'
#!/bin/sh
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
EOF
  cat >"$bin/curl" <<EOF
#!/bin/sh
for a; do url=\$a; done
case \$url in
  */BUILD.json) echo '{"stamp":"$stamp"}' ;;
  *) printf 200 ;;
esac
EOF
  cat >"$home/.local/bin/go" <<'EOF'
#!/bin/sh
echo 5
EOF
  # systemctl answers like systemd does. state/system-unit exists when the system-wide unit
  # file is installed, state/system-active while it runs.
  cat >"$bin/systemctl" <<EOF
#!/bin/sh
st=$state
if [ "\$1" = --user ]; then
  shift
  echo "user \$*" >>"$w/calls"
  [ "\$*" = "start clawdline-next.service" ] && [ -e "\$st/user-start-fails" ] && exit 1
  exit 0
fi
echo "system \$*" >>"$w/calls"
unit=; for a; do unit=\$a; done
case "\$1 \$unit" in
  "daemon-reload "*|"start user@$uid.service") exit 0 ;;
esac
[ "\$unit" = clawdline-next.service ] || { echo "stub: unexpected systemctl \$*" >&2; exit 64; }
case \$1 in
  show)
    if [ -e "\$st/system-unit" ]; then echo loaded; else echo not-found; fi
    exit 0 ;;
  is-active)
    [ -e "\$st/system-active" ] && exit 0
    exit 3 ;;
esac
if [ ! -e "\$st/system-unit" ]; then
  case \$1 in
    disable|enable) echo "Failed to \$1 unit: Unit file clawdline-next.service does not exist." >&2; exit 1 ;;
    *) echo "Failed to \$1 clawdline-next.service: Unit clawdline-next.service not found." >&2; exit 5 ;;
  esac
fi
case \$1 in
  stop) rm -f "\$st/system-active" ;;
  start) touch "\$st/system-active" ;;
  disable) [ -e "\$st/system-disable-fails" ] && { echo "Failed to disable unit: Access denied" >&2; exit 1; } ;;
esac
exit 0
EOF
  chmod +x "$bin"/* "$home/.local/bin/go"
}

# bootstrap: run the script in the current world; its exit status lands in $status.
bootstrap() {
  set +e
  PATH="$bin:$PATH" CLAWDLINE_BOOTSTRAP_SYSROOT="$w/r" \
    "$root/tools/bootstrap-linux-user-service.sh" svc >"$w/out" 2>&1
  status=$?
  set -e
}

fail() {
  echo "$1" >&2
  echo "--- output" >&2; cat "$w/out" >&2
  echo "--- calls" >&2; cat "$w/calls" >&2
  exit 1
}

# 1. A fresh machine finishes, and nothing is done to a system unit that is not there.
world f
bootstrap
[ "$status" -eq 0 ] || fail "fresh machine: bootstrap exited $status, want 0"
grep -Fq 'bootstrap complete' "$w/out" || fail "fresh machine: no completion line"
[ -f "$home/.config/systemd/user/clawdline-next.service" ] || fail "fresh machine: user unit not installed"
grep -Fxq 'user enable clawdline-next.service' "$w/calls" || fail "fresh machine: user unit not enabled"
grep -Fxq 'user start clawdline-next.service' "$w/calls" || fail "fresh machine: user unit not started"
if grep -Eq '^system (stop|disable|enable|start)( --now)? clawdline-next.service$' "$w/calls"; then
  fail "fresh machine: a system unit that does not exist was stopped, disabled or started"
fi
[ ! -e "$w/r/etc/systemd/system/clawdline-next.service.d" ] ||
  fail "fresh machine: a drop-in was written for a system unit that does not exist"

# 2. ...and a second run on it is harmless.
: >"$w/calls"
bootstrap
[ "$status" -eq 0 ] || fail "fresh machine, second run: exited $status, want 0"

# 3. A machine migrating from the running system unit keeps its sessions, starts the user unit
#    before it retires the system one, and ends with the system unit stopped and disabled.
world m
touch "$state/system-unit" "$state/system-active"
bootstrap
[ "$status" -eq 0 ] || fail "migrating machine: bootstrap exited $status, want 0"
grep -Fxq 'KillMode=process' "$w/r/etc/systemd/system/clawdline-next.service.d/90-preserve-sessions.conf" ||
  fail "migrating machine: the drop-in that preserves sessions was not written"
grep -Fxq 'system stop clawdline-next.service' "$w/calls" || fail "migrating machine: system unit not stopped"
started=$(grep -nFx 'user start clawdline-next.service' "$w/calls" | cut -d: -f1)
disabled=$(grep -nFx 'system disable clawdline-next.service' "$w/calls" | cut -d: -f1)
[ -n "$started" ] && [ -n "$disabled" ] && [ "$started" -lt "$disabled" ] ||
  fail "migrating machine: want the user unit started, then the system unit disabled"
[ ! -e "$state/system-active" ] || fail "migrating machine: the system unit is still running"

# 4. ...and a second run on it, with the system unit already stopped and disabled, is harmless.
: >"$w/calls"
bootstrap
[ "$status" -eq 0 ] || fail "migrating machine, second run: exited $status, want 0"

# 5. A real failure still stops the script. On a fresh machine a user unit that will not start is
#    reported as such, with no system service to fall back to or claim to have restored.
world fs
touch "$state/user-start-fails"
bootstrap
[ "$status" -eq 1 ] || fail "fresh machine, user unit fails: exited $status, want 1"
grep -Fq 'user service failed to start' "$w/out" || fail "fresh machine, user unit fails: failure not named"
if grep -Fq 'restored the system service' "$w/out"; then
  fail "fresh machine, user unit fails: claimed to restore a system service that never existed"
fi
grep -Fxq 'user disable --now clawdline-next.service' "$w/calls" ||
  fail "fresh machine, user unit fails: the failed user unit was left enabled"

# 6. A migrating machine whose system unit cannot be disabled stops there.
world md
touch "$state/system-unit" "$state/system-active" "$state/system-disable-fails"
bootstrap
[ "$status" -ne 0 ] || fail "migrating machine, disable denied: exited 0, want a failure"
if grep -Fq 'bootstrap complete' "$w/out"; then
  fail "migrating machine, disable denied: claimed completion"
fi

echo "bootstrap: a fresh machine and a migrating one both finish, twice; real failures still stop it"
