#!/bin/bash
# Proves that quitting the macOS shell stops the daemon it started — and shows
# what the shell did before 2026-09-21, side by side.
#
# It builds this checkout's daemon and a probe shell compiled from
# shell/darwin/Daemon.swift (tools/shellprobe/main.swift), starts the probe on
# a port nobody uses with an empty state directory, ends it the two ways a
# shell is ended from outside — a quit Apple event (`osascript quit`, which
# the daemon's operator used that morning) and SIGTERM (`kill`, `killall`,
# launchd) — and asks the process table whether the daemon it started is
# still there. Each daemon is stopped by the pid the probe printed, never by
# name.
#
# usage: tools/prove-shell-stops-daemon.sh [work-dir]
set -euo pipefail
cd "$(dirname "$0")/.."

WORK="${1:-$(mktemp -d)}"
mkdir -p "$WORK"
ID="com.clawdline.shell-probe.$$"
APP="$WORK/ShellProbe.app"

go build -o "$WORK/clawdline" ./cmd/clawdline
mkdir -p "$APP/Contents/MacOS"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>$ID</string>
  <key>CFBundleExecutable</key><string>ShellProbe</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST
swiftc -o "$APP/Contents/MacOS/ShellProbe" shell/darwin/Daemon.swift tools/shellprobe/main.swift

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'
}

# gone <pid> <seconds>: true once the process table has no such pid
gone() {
  local i
  for ((i = 0; i < $2 * 10; i++)); do
    kill -0 "$1" 2>/dev/null || return 0
    sleep 0.1
  done
  return 1
}

FAILED=0
trial() { # mode (new|legacy), how (quit|sigterm)
  local mode=$1 how=$2 port dir out shell daemon
  port=$(free_port)
  dir=$(mktemp -d "$WORK/dir.XXXX")
  out="$WORK/probe-$mode-$how.out"
  local args=()
  [ "$mode" = legacy ] && args=(--legacy)
  env CLAWDLINE_NEXT_PORT="$port" CLAWDLINE_NEXT_DIR="$dir" CLAWDLINE_NEXT_STANDALONE=1 \
    PROBE_DAEMON="$WORK/clawdline" "$APP/Contents/MacOS/ShellProbe" ${args[@]+"${args[@]}"} >"$out" 2>&1 &
  shell=$!
  daemon=""
  for _ in $(seq 1 100); do
    daemon=$(sed -n 's/^daemon: started pid \([0-9]*\).*/\1/p' "$out")
    [ -n "$daemon" ] && curl -s -o /dev/null "http://127.0.0.1:$port/v1/health" && break
    sleep 0.1
  done
  if [ -z "$daemon" ]; then
    echo "$mode/$how: the probe never started a daemon"; cat "$out"; FAILED=1; return
  fi
  if [ "$how" = quit ]; then
    # Bounded: an Apple event that waits on a consent prompt must not hang this.
    perl -e 'alarm 20; exec @ARGV' osascript -e "quit app id \"$ID\"" || echo "(osascript: exit $?)"
  else
    kill -TERM "$shell"
  fi
  gone "$shell" 10 || { echo "$mode/$how: the probe did not end"; kill -KILL "$shell"; }
  wait "$shell" 2>/dev/null || true
  if gone "$daemon" 3; then
    echo "$mode/$how: shell $shell ended; daemon $daemon is gone"
  else
    echo "$mode/$how: shell $shell ended; daemon $daemon is STILL RUNNING:" \
      "$(ps -o ppid=,command= -p "$daemon" | sed 's/^ */ppid /')"
    [ "$mode" = new ] && FAILED=1
    kill "$daemon"
    gone "$daemon" 10 || kill -KILL "$daemon"
  fi
  sed 's/^/    /' "$out"
}

for how in quit sigterm; do
  for mode in legacy new; do
    trial "$mode" "$how"
  done
done
exit $FAILED
