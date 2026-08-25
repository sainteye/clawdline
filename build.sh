#!/bin/bash
# Builds Clawdline.app. No Xcode project, no package manager: a few .swift files and one .js,
# compiled straight by swiftc — one less layer of "what does the build config actually say".
set -euo pipefail
cd "$(dirname "$0")"

APP="${CLAWDLINE_APP:-$HOME/Applications/Clawdline.app}"
APP_PARENT="$(dirname "$APP")"
APP_NAME="$(basename "$APP")"

mkdir -p "$APP_PARENT"
# Build beside the installed app, on the same filesystem. The final rename is then quick and
# cannot turn into a slow copy just when the running app has been stopped.
STAGE_ROOT="$(mktemp -d "$APP_PARENT/.clawdline-build.XXXXXX")"
STAGED_APP="$STAGE_ROOT/$APP_NAME"
BIN="$STAGED_APP/Contents/MacOS/Clawdline"
RES="$STAGED_APP/Contents/Resources"
BACKUP="$STAGE_ROOT.previous"
trap 'rm -rf "$STAGE_ROOT"' EXIT

echo "→ building staged app for $APP"
mkdir -p "$(dirname "$BIN")" "$RES"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -O \
  -o "$BIN" \
  Sources/*.swift \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network

cp Resources/iterm.js "$RES/"
cp Resources/Clawdline.icns "$RES/"
cp Resources/clawdline-hook.sh "$RES/"
cp -R Resources/mascots "$RES/"
# The web interface, served by RemoteServer when it is switched on.
[ -d Resources/web ] && cp -R Resources/web "$RES/"

cat > "$STAGED_APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Clawdline</string>
  <key>CFBundleDisplayName</key><string>Clawdline</string>
  <key>CFBundleIdentifier</key><string>dev.sainteye.clawdline</string>
  <key>CFBundleExecutable</key><string>Clawdline</string>
  <key>CFBundleIconFile</key><string>Clawdline</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.6.0</string>
  <key>CFBundleVersion</key><string>0.6.0</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <!-- Menu bar resident, no Dock icon -->
  <key>LSUIElement</key><true/>
  <!-- Without this line macOS kills the app the first time it talks to iTerm2 -->
  <key>NSAppleEventsUsageDescription</key>
  <string>Clawdline needs to control iTerm2 so it can put what you type into Claude Code.</string>
  <!-- Asked for only when the microphone button is pressed. Recognition runs on this Mac when
       the dictation language is installed, and goes to Apple when it is not — the bar says
       which, while it is listening. -->
  <key>NSMicrophoneUsageDescription</key>
  <string>Clawdline uses the microphone only while you hold a dictation session open, so you can talk into the prompt instead of typing.</string>
  <key>NSSpeechRecognitionUsageDescription</key>
  <string>Clawdline turns your speech into text. It uses this Mac when the dictation language is installed, and Apple's service when it is not.</string>
  <!-- clawdline://open so any tool can summon it, not just the built-in hotkey -->
  <key>CFBundleURLTypes</key>
  <array><dict>
    <key>CFBundleURLName</key><string>dev.sainteye.clawdline</string>
    <key>CFBundleURLSchemes</key><array><string>clawdline</string></array>
  </dict></array>
</dict>
</plist>
PLIST

# Ad-hoc signature. Unsigned, TCC forgets "may control iTerm2" on every rebuild.
codesign --force --sign - --identifier dev.sainteye.clawdline "$STAGED_APP" >/dev/null 2>&1 \
  || echo "  (codesign failed; harmless, but you may be re-asked to authorise iTerm2 after each rebuild)"

# Nothing installed or running has been touched until here. Remember the state at the instant
# of replacement — somebody who deliberately quit while a long build was running should not
# have the app reopened against their wishes.
echo "→ installing finished build"
WAS_RUNNING=0
pgrep -x Clawdline >/dev/null 2>&1 && WAS_RUNNING=1

# A dispatched task lives through a restart once it has been briefed: its secret is on disk as a
# hash and its child is a terminal tab this script does not touch. In the seconds *before* that,
# it does not — the plaintext secret is only in the old process's memory, so a child whose tab has
# opened but whose first message has not been typed can never be briefed, and comes back as
# `spawn_failed: the app restarted before the child was briefed`.
#
# That window is about a second wide per task, which is small until several people share this
# working copy: one session runs ./build.sh, another one's grandchildren die, and the message it
# gets back blames the app rather than the person who rebuilt it. So look, and say so. Not a
# refusal — this is somebody's own machine and their own build — just the one fact that turns a
# baffling failure into an obvious one.
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1; then
  PORT=$(/usr/bin/python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/clawdline/config.json"))).get("remote_port",7717))' 2>/dev/null || echo 7717)
  TOKEN_FILE="$HOME/.config/clawdline/orchestrator-token"
  if [ -r "$TOKEN_FILE" ]; then
    MIDFLIGHT=$(curl -s --max-time 2 "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null \
      | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") in ("queued", "spawning"):
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    if [ -n "$MIDFLIGHT" ]; then
      echo "!! a dispatched task is mid-spawn — restarting now will kill it:"
      echo "$MIDFLIGHT"
      echo "   (a briefed task survives a restart; one still opening its tab does not)"
    fi
  fi
fi

pkill -x Clawdline 2>/dev/null || true
# **`pkill` asks; it does not wait.** Moving the bundle while a process on its way out is still
# reading it can make AppKit teardown ask CoreFoundation for files that have just moved away.
# Wait for the old process to be genuinely gone before the two quick renames below.
for _ in $(seq 1 60); do
  pgrep -x Clawdline >/dev/null 2>&1 || break
  sleep 0.1
done

# Keep the old bundle recoverable until the staged one is in its final name. Both moves are on
# the same filesystem; if the second one fails, put the first one back before reporting failure.
HAD_OLD=0
if [ -e "$APP" ]; then
  if ! mv "$APP" "$BACKUP"; then
    echo "!! could not move the existing app aside; it has not been changed"
    [ "$WAS_RUNNING" = "1" ] && open "$APP"
    exit 1
  fi
  HAD_OLD=1
fi
if ! mv "$STAGED_APP" "$APP"; then
  echo "!! could not install the finished build"
  if [ "$HAD_OLD" = "1" ]; then
    if mv "$BACKUP" "$APP"; then
      echo "   restored the previous app"
    else
      echo "   previous app is still recoverable at: $BACKUP"
    fi
  fi
  [ "$WAS_RUNNING" = "1" ] && [ -d "$APP" ] && open "$APP"
  exit 1
fi
[ "$HAD_OLD" = "1" ] && rm -rf "$BACKUP"

if [ "$WAS_RUNNING" = "1" ]; then
  open "$APP"
  # **Say it came back only if it came back.** This used to print "relaunched" the instant
  # `open` returned, which says nothing: `open` hands the request to Launch Services and exits.
  # A build that killed the app and failed to start it again reported success, and the person
  # watching saw their bar vanish with no reason given — which reads as a crash, not as a build.
  for _ in $(seq 1 50); do
    pgrep -x Clawdline >/dev/null 2>&1 && break
    sleep 0.1
  done
  if pgrep -x Clawdline >/dev/null 2>&1; then
    echo "✓ done (relaunched, since it was running before)"
  else
    echo "!! it was running before and did not come back — start it with:"
    echo "   open \"$APP\""
    exit 1
  fi
else
  echo "✓ done (it was not running, so it was left closed)"
fi
echo "  run:    open \"$APP\""
echo "  config: ~/.config/clawdline/config.json"
