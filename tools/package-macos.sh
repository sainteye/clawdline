#!/bin/bash
# Build the macOS app: the Go daemon and the Swift shell in one bundle.
#
# The daemon ships inside the app rather than beside it, so there is one thing
# to install and one thing to remove, and the shell always runs the daemon it
# was built with rather than whichever one is on PATH.
#
# The app is never changed underneath itself. macOS checks a process that
# sends Apple events against the code signature of the bundle it was launched
# from; replace that bundle's files while it runs and the check fails. On
# 2026-09-26 this script did `rm -rf` and rebuilt dist/ under a running app:
# from then on every osascript the daemon sent iTerm2 was refused with -1743,
# `codesign -dvvv +<pid>` answered "host has no guest with the requested
# attributes" for both processes, and the app could read no terminal for 27
# minutes although its Automation switch was on. Quitting and reopening it
# cleared it in 40 seconds.
#
# So the bundle is built and signed in dist/.staging.<pid>/ first, and a
# failed build leaves the installed app as it was. Then, if a process is
# running from this checkout's dist/ (the shell or its daemon; another
# checkout's app is not ours), the shell is asked to quit — SIGTERM to its
# pid, which it treats as an ordinary quit and which, unlike a quit Apple
# event to the bundle id, cannot reach an instance launched from anywhere
# else — and the new bundle goes in only once both are gone. The app is then
# opened again and the script waits for its daemon to answer.
#
# usage: tools/package-macos.sh [--dmg] [--no-restart]
#   --no-restart  if the app is running, stop with an error instead of
#                 restarting it; the new bundle is left in the staging folder
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="Clawdline Next"
ID="com.sainteye.clawdline-next"     # deliberately not the Swift app's id:
                                      # both must be installable at once
APP="dist/$NAME.app"
VERSION="$(git describe --tags --always 2>/dev/null || echo 0.0.1)"

DMG_WANTED=0
RESTART=1
for arg in "$@"; do
  case "$arg" in
    --dmg) DMG_WANTED=1 ;;
    --no-restart) RESTART=0 ;;
    *) echo "usage: $0 [--dmg] [--no-restart]" >&2; exit 2 ;;
  esac
done

# Everything below builds into $BUILD; $APP is only ever replaced whole.
STAGING="dist/.staging.$$"
BUILD="$STAGING/$NAME.app"
KEEP_STAGING=0
cleanup() { local rc=$?; [ "$KEEP_STAGING" = 1 ] || rm -rf "$STAGING"; exit "$rc"; }
trap cleanup EXIT

# running_pids: the processes whose executable is inside this checkout's
# $APP — the shell and `clawdline serve` alike. Matched on the absolute path,
# so an app built in another worktree's dist/ is not counted.
running_pids() {
  local dir
  dir="$(cd "$(dirname "$APP")" && pwd -P)/$(basename "$APP")/Contents/MacOS/"
  ps -axww -o pid= -o command= | awk -v dir="$dir" -v me="$$" '
    { pid = $1; sub(/^[ \t]*[0-9]+[ \t]+/, "") }
    index($0, dir) == 1 && pid != me { print pid }'
}

# wait_gone <seconds>: true once nothing runs from $APP.
wait_gone() {
  local i
  for ((i = 0; i < $1; i++)); do
    [ -z "$(running_pids)" ] && return 0
    sleep 1
  done
  [ -z "$(running_pids)" ]
}

# health_port: the daemon's port from its config, 7727 when it names none.
health_port() {
  local cfg="$HOME/.config/clawdline-next/config.json" port=""
  [ -f "$cfg" ] && port="$(plutil -extract port raw -o - "$cfg" 2>/dev/null || true)"
  case "$port" in ''|*[!0-9]*) port=7727 ;; esac
  echo "$port"
}

rm -rf "$STAGING"
mkdir -p "$BUILD/Contents/MacOS" "$BUILD/Contents/Resources"

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" \
  -o "$BUILD/Contents/MacOS/clawdline" ./cmd/clawdline
swiftc -O -o "$BUILD/Contents/MacOS/$NAME" shell/darwin/*.swift

# The shell's own files — the mascot packs its menu lists. Copied, like the
# console below: nothing in the bundle points outside it.
cp -R shell/darwin/Resources/. "$BUILD/Contents/Resources/"

# The console travels with the app, copied and never linked: a bundle that
# depends on a directory outside itself is not a bundle.
#
# It is this repository's own React console now. The Swift app's console can
# still be used by pointing CLAWDLINE_WEB_SOURCE at it, which is how the two
# get compared on one daemon — but it is no longer what ships, because a build
# that reaches into another checkout is a build that breaks on any machine but
# this one.
if [ -n "${CLAWDLINE_WEB_SOURCE:-}" ]; then
  WEB="$CLAWDLINE_WEB_SOURCE"
else
  echo "building the console…"
  ( cd web && npm install --silent && npm run build --silent )
  WEB="web/console/dist"
fi
if [ -d "$WEB" ]; then
  cp -R "$WEB" "$BUILD/Contents/Resources/web"
else
  echo "error: no console at $WEB; refusing to ship an app with nothing to show" >&2
  exit 1
fi

# The console this app serves must be the daemon's, not the hosted one. They
# are the same source and a different build: `web/console/src/main.tsx`
# branches on VITE_HOSTED_CONSOLE, and a build that has it loads CloudGate and
# refuses every origin but app.clawdline.com. Shipped inside the app that
# serves 127.0.0.1, it draws "this console was built for
# https://app.clawdline.com" and nothing else — which is what the person met on
# 2026-09-20, from an exported variable that outlived the command it was
# written for, or a dist reused through CLAWDLINE_WEB_SOURCE.
#
# Neither the build nor the copy can fail on its own, because nothing is
# missing: a different branch was taken. So the bundle is asked afterwards what
# it actually contains.
if grep -rql 'CloudGate' "$BUILD/Contents/Resources/web/assets" 2>/dev/null; then
  echo "error: the console in $WEB is the hosted build (CloudGate is in it)." >&2
  echo "       That one only talks to app.clawdline.com and cannot serve 127.0.0.1." >&2
  echo "       Unset VITE_HOSTED_CONSOLE and CLAWDLINE_WEB_SOURCE, remove web/console/dist, and build again." >&2
  echo "       docs/hosted-console.md says which build is which." >&2
  exit 1
fi

cat > "$BUILD/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>$NAME</string>
  <key>CFBundleDisplayName</key><string>$NAME</string>
  <key>CFBundleIdentifier</key><string>$ID</string>
  <key>CFBundleExecutable</key><string>$NAME</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>Clawdline</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <!-- Dictation. The console records in the WKWebView and this machine's own
       whisper reads it (POST /v1/voice); nothing about that leaves the
       machine, which is the sentence the system prompt shows. Without this
       key macOS kills the app the first time the page asks for a microphone
       rather than prompting for one, so it ships with the delegate that
       grants it (shell/darwin/Microphone.swift) and not separately. -->
  <key>NSMicrophoneUsageDescription</key><string>Clawdline Next transcribes what you dictate on this Mac, with the model on this Mac. The recording is never uploaded.</string>
  <!-- clawdline-next://open, so any tool can summon the window. Not clawdline://,
       which the Swift app owns and which both apps would then answer. -->
  <key>CFBundleURLTypes</key>
  <array><dict>
    <key>CFBundleURLName</key><string>$ID</string>
    <key>CFBundleURLSchemes</key><array><string>clawdline-next</string></array>
  </dict></array>
</dict></plist>
PLIST

# Signed ad hoc, as one bundle. The linker signs each binary on its own, but
# launch at login (SMAppService) asks about the application, and an unsigned
# bundle is one it may refuse.
codesign --force --sign - "$BUILD/Contents/MacOS/clawdline"
codesign --force --sign - "$BUILD"

echo "built $BUILD ($(du -sh "$BUILD" | cut -f1))"

# Put it in place without pulling the running app's files out from under it.
WAS_RUNNING=0
if [ -n "$(running_pids)" ]; then
  if [ "$RESTART" = 0 ]; then
    KEEP_STAGING=1
    echo "error: $APP is running (pids $(running_pids | paste -sd ' ' -)) and --no-restart was given;" >&2
    echo "       it was left alone. The new bundle is at $BUILD" >&2
    exit 1
  fi
  WAS_RUNNING=1
  echo "$APP is running; quitting it so its files can be replaced…"
  # The shell turns SIGTERM into NSApp.terminate, which stops its daemon.
  # Only the shell is asked: the daemon is the shell's to stop, and a daemon
  # left over without one is sent the same signal.
  for pid in $(running_pids); do
    case "$(ps -o command= -p "$pid" 2>/dev/null)" in
      *"/Contents/MacOS/$NAME"*) kill -TERM "$pid" 2>/dev/null || true ;;
    esac
  done
  # The shell can take over 30 seconds to go, and opening it again before it
  # has gone fails with -609, so both processes are waited for.
  if ! wait_gone 60; then
    for pid in $(running_pids); do kill -TERM "$pid" 2>/dev/null || true; done
  fi
  if ! wait_gone 60; then
    KEEP_STAGING=1
    echo "error: $APP was still running 120 seconds after it was asked to quit" >&2
    echo "       (pids $(running_pids | paste -sd ' ' -)); it was not replaced." >&2
    echo "       The new bundle is at $BUILD" >&2
    exit 1
  fi
fi

rm -rf "$APP"
mv "$BUILD" "$APP"
echo "installed $APP"

if [ "$WAS_RUNNING" = 1 ]; then
  PORT="$(health_port)"
  open "$APP"
  echo "reopened $APP; waiting for its daemon on 127.0.0.1:${PORT}…"
  ok=0
  for ((i = 0; i < 60; i++)); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$PORT/v1/health" || true)" = 200 ]; then
      ok=1; break
    fi
    sleep 2
  done
  if [ "$ok" = 0 ]; then
    echo "error: $APP was reopened but http://127.0.0.1:$PORT/v1/health did not answer 200" >&2
    echo "       within 120 seconds; the daemon's daemon.log says why." >&2
    exit 1
  fi
  echo "the daemon answers on 127.0.0.1:$PORT"
fi

# A disk image, because that is how a Mac application arrives.
if [ "$DMG_WANTED" = 1 ]; then
  DMG="dist/$NAME-$VERSION.dmg"
  rm -f "$DMG"
  STAGE="$(mktemp -d)"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -quiet -volname "$NAME" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
  rm -rf "$STAGE"
  echo "built $DMG ($(du -h "$DMG" | cut -f1))"
fi
