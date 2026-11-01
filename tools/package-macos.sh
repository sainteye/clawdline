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
# running from the selected install target (the shell or its daemon; another
# app path is not ours), the shell is asked to quit — SIGTERM to its
# pid, which it treats as an ordinary quit and which, unlike a quit Apple
# event to the bundle id, cannot reach an instance launched from anywhere
# else — and the new bundle goes in only once both are gone. The app is then
# opened again and the script waits for its daemon to answer.
#
# usage: tools/package-macos.sh [--dmg] [--no-restart] [--install-to /absolute/path/Clawdline Next.app]
#                                [--build-only /absolute/dir]
#   --no-restart  if the app is running, stop with an error instead of
#                 restarting it; the new bundle is left in the staging folder
#   --build-only  build and sign the bundle into that directory and stop:
#                 nothing is installed, quit or opened (tools/release/build.sh)
#
# The app carries a tmux of its own in Contents/Helpers/tmux, used only when
# the Mac has no tmux 3.0 or newer (docs/design-decisions.md D72).
# CLAWDLINE_TMUX_DIR names one already built by tools/release/tmux/build.sh;
# without it the one in dist/tmux/darwin-arm64 is used, and built there first
# when it is missing or not the pinned version.
#
# CLAWDLINE_RELEASE_VERSION=vX.Y.Z stamps a release: the daemon is built with
# -trimpath and that version, and BUILD.json and Info.plist carry it.
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="Clawdline Next"
ID="com.sainteye.clawdline-next"     # deliberately not the Swift app's id:
                                      # both must be installable at once
APP="dist/$NAME.app"
# The oldest macOS the app runs on: Info.plist says it, and every binary in the
# bundle is built for it. swiftc without -target builds for the Mac it runs on,
# so v0.11.0 and v1.0.0, built on a macOS 26 runner, carried a shell with minos
# 26.0 under an Info.plist that said 13.0: LaunchServices refused to open it on
# macOS 15 (error -10825) and the installer quietly opened the browser instead.
MACOS_MIN="13.0"
INSTALL_APP=""
VERSION="$(git describe --tags --always 2>/dev/null || echo 0.0.1)"
RELEASE_VERSION="${CLAWDLINE_RELEASE_VERSION:-}"
[ -z "$RELEASE_VERSION" ] || VERSION="${RELEASE_VERSION#v}"
BUILD_ONLY=""

DMG_WANTED=0
RESTART=1
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dmg) DMG_WANTED=1 ;;
    --no-restart) RESTART=0 ;;
    --build-only)
      [ "$#" -ge 2 ] || { echo "--build-only needs an absolute directory" >&2; exit 2; }
      case "$2" in /*) ;; *) echo "--build-only needs an absolute directory" >&2; exit 2 ;; esac
      [ -d "$2" ] || { echo "--build-only directory does not exist: $2" >&2; exit 2; }
      BUILD_ONLY="$2"
      shift ;;
    --install-to)
      [ "$#" -ge 2 ] || { echo "--install-to needs an absolute app path" >&2; exit 2; }
      INSTALL_APP="$2"
      shift ;;
    *) echo "usage: $0 [--dmg] [--no-restart] [--install-to /absolute/path/$NAME.app] [--build-only /absolute/dir]" >&2; exit 2 ;;
  esac
  shift
done
if [ -z "$INSTALL_APP" ]; then
  INSTALL_APP="$(pwd -P)/$APP"
else
  case "$INSTALL_APP" in
    /*) ;;
    *) echo "--install-to needs an absolute app path" >&2; exit 2 ;;
  esac
  [ "$(basename "$INSTALL_APP")" = "$NAME.app" ] || { echo "--install-to must name $NAME.app" >&2; exit 2; }
  [ -d "$(dirname "$INSTALL_APP")" ] || { echo "--install-to parent does not exist" >&2; exit 2; }
  INSTALL_APP="$(cd "$(dirname "$INSTALL_APP")" && pwd -P)/$NAME.app"
fi
INSTALL_DIR="$(dirname "$INSTALL_APP")"

# Everything below builds into $BUILD; $APP is only ever replaced whole.
STAGING="dist/.staging.$$"
BUILD="$STAGING/$NAME.app"
PROMOTION="$INSTALL_DIR/.$NAME.staging.$$"
BACKUP="$INSTALL_DIR/.$NAME.previous.$$"
KEEP_STAGING=0
RELEASE_INSTALL_ATTEMPT=0
cleanup() {
  local rc=$?
  if [ "$rc" -ne 0 ] && [ "$RELEASE_INSTALL_ATTEMPT" = 1 ] && [ "$RESTART" = 1 ]; then
    CLAWDLINE_RELEASE_NOTIFY_BIN="$INSTALL_APP/Contents/MacOS/clawdline" \
      sh tools/notify-release-failure.sh app "$stamp" || true
  fi
  [ "$KEEP_STAGING" = 1 ] || rm -rf "$STAGING"
  [ "$PROMOTION" = "$BUILD" ] || rm -rf "$PROMOTION"
  exit "$rc"
}
# shellcheck source=tools/release/codesign.sh
. "$(dirname "$0")/release/codesign.sh"
# The build's own exit status reaches cleanup, which reads it first.
trap 's=$?; codesign_end; ( exit "$s" ); cleanup' EXIT
codesign_begin

# running_pids: the processes whose executable is inside the requested install
# target — the shell and `clawdline serve` alike. A worktree build names the
# running app explicitly with --install-to, instead of updating only itself.
running_pids() {
  local dir
  dir="$INSTALL_APP/Contents/MacOS/"
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

if [ -n "$RELEASE_VERSION" ]; then
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath \
    -ldflags="-s -w -X main.releaseVersion=$RELEASE_VERSION" \
    -o "$BUILD/Contents/MacOS/clawdline" ./cmd/clawdline
else
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" \
    -o "$BUILD/Contents/MacOS/clawdline" ./cmd/clawdline
fi
cp "$BUILD/Contents/MacOS/clawdline" "$BUILD/Contents/MacOS/clawdline-daemon"
swiftc -O -target "arm64-apple-macos$MACOS_MIN" -o "$BUILD/Contents/MacOS/$NAME" shell/darwin/*.swift

# The tmux the app carries, with the licenses of what it is built from.
TMUX_DIR="${CLAWDLINE_TMUX_DIR:-dist/tmux/darwin-arm64}"
. tools/release/tmux/sources.sh
if ! grep -q "\"version\": \"$TMUX_VERSION\", \"sha256\": \"$TMUX_SHA256\"" "$TMUX_DIR/tmux.json" 2>/dev/null; then
  [ -z "${CLAWDLINE_TMUX_DIR:-}" ] || { echo "error: $TMUX_DIR does not hold the pinned tmux $TMUX_VERSION" >&2; exit 1; }
  echo "building the tmux the app carries (tmux $TMUX_VERSION)…"
  rm -rf "$TMUX_DIR"
  tools/release/tmux/build.sh darwin arm64 "$TMUX_DIR"
fi
mkdir -p "$BUILD/Contents/Helpers" "$BUILD/Contents/Resources/licenses"
cp "$TMUX_DIR/tmux" "$BUILD/Contents/Helpers/tmux"
cp "$TMUX_DIR/tmux.json" "$BUILD/Contents/Resources/tmux.json"
cp "$TMUX_DIR"/licenses/*.txt "$BUILD/Contents/Resources/licenses/"

# The shell's own files — the mascot packs its menu lists. Copied, like the
# console below: nothing in the bundle points outside it.
cp -R shell/darwin/Resources/. "$BUILD/Contents/Resources/"
# This marker describes native shell inputs, excluding the bundled daemon and
# web console. A service install can keep its existing App when only those
# service-owned payloads change, preserving WebKit's keychain identity.
{
  find shell/darwin -type f -print | LC_ALL=C sort | while IFS= read -r file; do
    shasum -a 256 "$file"
  done
  shasum -a 256 tools/package-macos.sh
  printf '%s\n' "$(swiftc --version)" "$MACOS_MIN" "$CLAWDLINE_CODESIGN_ID"
} | shasum -a 256 | awk '{print $1}' > "$BUILD/Contents/Resources/shell-revision"

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
  # Which commit this bundle is, and when it was committed, so its daemon can
  # say whether the cloud's build is later (docs/updates.md).
  stamp=$(git rev-parse HEAD)
  committed_at=$(TZ=UTC git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ "$stamp")
  if [ -n "$RELEASE_VERSION" ]; then
    printf '{"stamp":"%s","committed_at":"%s","version":"%s"}\n' "$stamp" "$committed_at" "$RELEASE_VERSION" >"$BUILD/Contents/Resources/web/BUILD.json"
  else
    printf '{"stamp":"%s","committed_at":"%s"}\n' "$stamp" "$committed_at" >"$BUILD/Contents/Resources/web/BUILD.json"
  fi
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
  <key>LSMinimumSystemVersion</key><string>$MACOS_MIN</string>
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

# No binary may need a newer macOS than Info.plist promises: LaunchServices
# reads the binary, not the promise.
for bin in "$BUILD/Contents/MacOS/$NAME" "$BUILD/Contents/MacOS/clawdline" "$BUILD/Contents/MacOS/clawdline-daemon" "$BUILD/Contents/Helpers/tmux"; do
  minos="$(vtool -show-build "$bin" | awk '$1 == "minos" { print $2; exit }')"
  if ! awk -v a="$minos" -v b="$MACOS_MIN" 'BEGIN {
      if (a == "") exit 1
      split(a, x, "."); split(b, y, ".")
      for (i = 1; i <= 3; i++) { if (x[i] + 0 < y[i] + 0) exit 0; if (x[i] + 0 > y[i] + 0) exit 1 }
      exit 0 }'; then
    echo "error: $bin needs macOS ${minos:-?}, but the app says it runs on macOS $MACOS_MIN" >&2
    exit 1
  fi
done

# Signed as one bundle. The linker signs each binary on its own, but launch at
# login (SMAppService) asks about the application, and an unsigned bundle is
# one it may refuse. With a certificate (tools/release/codesign.sh) the
# bundle keeps one code requirement across updates. WebKit's keychain grant
# additionally requires an Apple-issued identity; official releases enforce
# Developer ID Application in tools/release/build.sh.
codesign_begin
codesign_path "$BUILD/Contents/MacOS/clawdline" "$BUILD/Contents/MacOS/clawdline-daemon" "$BUILD/Contents/Helpers/tmux" "$BUILD"

echo "built $BUILD ($(du -sh "$BUILD" | cut -f1))"

if [ -n "$BUILD_ONLY" ]; then
  [ ! -e "$BUILD_ONLY/$NAME.app" ] || { echo "$BUILD_ONLY/$NAME.app already exists" >&2; exit 1; }
  mv "$BUILD" "$BUILD_ONLY/$NAME.app"
  echo "built $BUILD_ONLY/$NAME.app; nothing was installed"
  exit 0
fi
RELEASE_INSTALL_ATTEMPT=1

# A disposable worktree may build for the app in the primary checkout. Copy
# into a sibling of that app before stopping it, so the final rename stays on
# one filesystem and the running signed bundle is never changed in place.
if [ "$INSTALL_APP" != "$(pwd -P)/$APP" ]; then
  [ ! -e "$PROMOTION" ] || { echo "install staging path already exists: $PROMOTION" >&2; exit 1; }
  cp -R "$BUILD" "$PROMOTION"
else
  PROMOTION="$BUILD"
fi

# Put it in place without pulling the running app's files out from under it.
WAS_RUNNING=0
if [ -n "$(running_pids)" ]; then
  if [ "$RESTART" = 0 ]; then
    KEEP_STAGING=1
    echo "error: $INSTALL_APP is running (pids $(running_pids | paste -sd ' ' -)) and --no-restart was given;" >&2
    echo "       it was left alone. The new bundle is at $BUILD" >&2
    exit 1
  fi
  WAS_RUNNING=1
  echo "$INSTALL_APP is running; quitting it so its files can be replaced…"
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
    echo "error: $INSTALL_APP was still running 120 seconds after it was asked to quit" >&2
    echo "       (pids $(running_pids | paste -sd ' ' -)); it was not replaced." >&2
    echo "       The new bundle is at $BUILD" >&2
    exit 1
  fi
fi

if [ -e "$INSTALL_APP" ]; then
  [ ! -e "$BACKUP" ] || { echo "install backup path already exists: $BACKUP" >&2; exit 1; }
  mv "$INSTALL_APP" "$BACKUP"
fi
if ! mv "$PROMOTION" "$INSTALL_APP"; then
  [ ! -e "$BACKUP" ] || mv "$BACKUP" "$INSTALL_APP"
  echo "could not install $INSTALL_APP; previous bundle restored" >&2
  exit 1
fi
echo "installed $INSTALL_APP"

if [ "$WAS_RUNNING" = 1 ]; then
  PORT="$(health_port)"
  open "$INSTALL_APP"
  echo "reopened $INSTALL_APP; waiting for its daemon on 127.0.0.1:${PORT}…"
  ok=0
  for ((i = 0; i < 60; i++)); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$PORT/" || true)" = 200 ]; then
      ok=1; break
    fi
    sleep 2
  done
  if [ "$ok" = 0 ]; then
    echo "error: $INSTALL_APP was reopened but http://127.0.0.1:$PORT/ did not answer 200" >&2
    echo "       within 120 seconds; the daemon's daemon.log says why." >&2
    exit 1
  fi
  echo "the daemon answers on 127.0.0.1:$PORT"
  token_file="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}/local-token"
  [ -r "$token_file" ] || { echo "error: local token is unavailable for BUILD.json verification" >&2; exit 1; }
  token=$(cat "$token_file")
  served=$(printf 'header = "Authorization: Bearer %s"\n' "$token" |
    curl --config - -fsS --max-time 2 "http://127.0.0.1:$PORT/BUILD.json" || true)
  if [ "$served" != "$(cat "$INSTALL_APP/Contents/Resources/web/BUILD.json")" ]; then
    echo "error: the running app does not serve the installed BUILD.json" >&2
    exit 1
  fi
  CLAWDLINE_RELEASE_NOTIFY_BIN="$INSTALL_APP/Contents/MacOS/clawdline" \
    tools/notify-release-success.sh app "$stamp"
fi
if [ -e "$BACKUP" ]; then rm -rf "$BACKUP"; fi

# A disk image, because that is how a Mac application arrives.
if [ "$DMG_WANTED" = 1 ]; then
  DMG="dist/$NAME-$VERSION.dmg"
  rm -f "$DMG"
  STAGE="$(mktemp -d)"
  cp -R "$INSTALL_APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -quiet -volname "$NAME" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
  rm -rf "$STAGE"
  echo "built $DMG ($(du -h "$DMG" | cut -f1))"
fi
