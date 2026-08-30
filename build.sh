#!/bin/bash
# Builds Clawdline.app. No Xcode project, no package manager: a few .swift files and one .js,
# compiled straight by swiftc — one less layer of "what does the build config actually say".
set -euo pipefail
cd "$(dirname "$0")"
. tools/swift-source-manifest.sh
verify_swift_source_manifest production

APP="${CLAWDLINE_APP:-$HOME/Applications/Clawdline.app}"
APP_PARENT="$(dirname "$APP")"
APP_NAME="$(basename "$APP")"
BUNDLE_ID="com.tsunamiworks.clawdline"
SIGN_IDENTITY="${CLAWDLINE_SIGN_IDENTITY:--}"

mkdir -p "$APP_PARENT"
# Build beside the installed app, on the same filesystem. The final rename is then quick and
# cannot turn into a slow copy just when the running app has been stopped.
STAGE_ROOT="$(mktemp -d "$APP_PARENT/.clawdline-build.XXXXXX")"
STAGED_APP="$STAGE_ROOT/$APP_NAME"
BIN="$STAGED_APP/Contents/MacOS/Clawdline"
RES="$STAGED_APP/Contents/Resources"
BACKUP="$STAGE_ROOT.previous"
cleanup_build() {
  if [ "${MAINTENANCE_ACTIVE:-0}" = 1 ] && [ -n "${MAINTENANCE_REQUEST_ID:-}" ] \
      && [ -r "${TOKEN_FILE:-}" ] && [ -n "${PORT:-}" ]; then
    curl -sS --max-time 5 -X DELETE \
      "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null 2>&1 || true
  fi
  rm -rf "$STAGE_ROOT"
}
trap cleanup_build EXIT

echo "→ building staged app for $APP"
mkdir -p "$(dirname "$BIN")" "$RES"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -O \
  -o "$BIN" \
  "${clawdline_production_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network

cp Resources/iterm.js "$RES/"
cp Resources/Clawdline.icns "$RES/"
cp Resources/clawdline-hook.sh "$RES/"
# The default dispatch policy. It is a document people read and edit, so it ships as a file rather
# than as a string literal in the source; Orchestrator writes it out once if the machine has none.
cp Resources/dispatch-policy.md "$RES/"
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
  <key>CFBundleIdentifier</key><string>com.tsunamiworks.clawdline</string>
  <key>CFBundleExecutable</key><string>Clawdline</string>
  <key>CFBundleIconFile</key><string>Clawdline</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.6.0</string>
  <key>CFBundleVersion</key><string>0.6.0</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 TsunamiWorks Co., Ltd.</string>
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
    <key>CFBundleURLName</key><string>com.tsunamiworks.clawdline</string>
    <key>CFBundleURLSchemes</key><array><string>clawdline</string></array>
  </dict></array>
</dict>
</plist>
PLIST

# Local builds stay ad-hoc and cheap. A release supplies the company Developer ID identity and
# receives Hardened Runtime, a trusted timestamp, and only the two resource entitlements the app
# actually uses. The private key never enters this repository.
if [ "$SIGN_IDENTITY" = - ]; then
  codesign --force --sign - --identifier "$BUNDLE_ID" "$STAGED_APP" >/dev/null 2>&1 \
    || echo "  (codesign failed; harmless, but you may be re-asked to authorise iTerm2 after each rebuild)"
else
  signed=0
  for attempt in 1 2 3; do
    if codesign --force --sign "$SIGN_IDENTITY" --identifier "$BUNDLE_ID" \
        --options runtime --timestamp --entitlements Resources/Clawdline.entitlements \
        "$STAGED_APP"; then
      signed=1
      break
    fi
    [ "$attempt" = 3 ] && break
    echo "  Apple timestamp service did not answer; retrying Developer ID signing ($attempt/3)"
    sleep $((attempt * 5))
  done
  [ "$signed" = 1 ] || { echo "!! Developer ID signing failed after 3 attempts"; exit 1; }
fi

# Packaging and CI build beside the installed app but must never inspect, stop, replace, or reopen
# the live Clawdline process. Their caller owns the fresh output path and receives only the bundle.
if [ "${CLAWDLINE_BUILD_ONLY:-0}" = 1 ]; then
  [ ! -e "$APP" ] || { echo "!! build-only destination already exists: $APP"; exit 1; }
  mv "$STAGED_APP" "$APP"
  echo "✓ built $APP"
  exit 0
fi

# Nothing installed or running has been touched until here. Remember the state at the instant
# of replacement — somebody who deliberately quit while a long build was running should not
# have the app reopened against their wishes.
echo "→ installing finished build"
WAS_RUNNING=0
pgrep -x Clawdline >/dev/null 2>&1 && WAS_RUNNING=1

# A dispatched task lives through a restart once it has been briefed: its durable record is on disk,
# its secret has reached the child, and its child is a terminal tab this script does not touch. A
# `briefed` task is therefore evidence of live work, not a restart blocker. In the seconds
# *before* that,
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
    if ! TASK_SNAPSHOT=$(curl -s --max-time 2 \
        "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null); then
      TASK_SNAPSHOT=
    fi
    MIDFLIGHT=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") in ("queued", "spawning"):
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    BRIEFED=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") == "briefed":
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    if [ -n "$BRIEFED" ]; then
      echo "→ briefed task(s) are durable and do not block this restart"
      echo "$BRIEFED"
    fi
    if [ -n "$MIDFLIGHT" ]; then
      # Wait rather than warn. The window is seconds wide and closes on its own, while the thing
      # on the other side of it is somebody's dispatched task dying with a message that blames
      # the app. A printed warning is the right shape for a person and the wrong one here: on a
      # machine where several sessions share a checkout, the one running this is usually another
      # agent, and it will not stop to read a line it did not ask for.
      echo "→ a dispatched task is mid-spawn; waiting for it to be briefed (up to 90s)"
      echo "$MIDFLIGHT"
      for _ in $(seq 1 90); do
        sleep 1
        STILL=$(curl -s --max-time 2 "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
            -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null \
          | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
print("".join("x" for x in t if x.get("state") in ("queued", "spawning")))' 2>/dev/null)
        [ -z "$STILL" ] && { echo "   clear — carrying on"; break; }
      done
      # Ninety seconds is the whole of the patience. Past that the task is not mid-spawn any
      # more, it is stuck, and holding a build hostage to it helps nobody.
      [ -n "$STILL" ] && echo "   still spawning after 90s; restarting anyway"
    fi
  fi
fi

# New runtimes own the replacement boundary: close admission, let already-admitted Apple Events
# drain, persist `ready`, and keep admission closed until the replacement's own complete Session
# inventory reconciles every briefed executor. The first build that installs this protocol is
# necessarily talking to an older runtime; only an exact 404 takes the documented bootstrap path
# above. Every other refusal is typed and stops before the running app is touched.
MAINTENANCE_REQUEST_ID=
MAINTENANCE_ACTIVE=0
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1 && [ -r "${TOKEN_FILE:-}" ]; then
  MAINTENANCE_REQUEST_ID=$(/usr/bin/uuidgen | tr '[:upper:]' '[:lower:]')
  MAINTENANCE_REPLY=$(mktemp "$STAGE_ROOT/maintenance.XXXXXX")
  MAINTENANCE_STATUS=$(curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" -w '%{http_code}' \
      -X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" || true)
  if [ "$MAINTENANCE_STATUS" = 404 ]; then
    echo "→ installed runtime has no restart-maintenance route; using one-time bootstrap preflight"
    MAINTENANCE_REQUEST_ID=
  elif [ "$MAINTENANCE_STATUS" != 200 ]; then
    echo "!! restart maintenance was refused (HTTP ${MAINTENANCE_STATUS:-unreachable})"
    /usr/bin/python3 -c 'import json,sys
try:
    e=json.load(open(sys.argv[1])).get("error",{})
    print("   %s: %s" % (e.get("code","unknown"),e.get("message","no message")))
except Exception: pass' "$MAINTENANCE_REPLY"
    exit 1
  else
    MAINTENANCE_ACTIVE=1
    echo "→ restart maintenance admitted; waiting for the terminal broker to drain"
    for _ in $(seq 1 120); do
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      sleep 1
    done
    if [ "${PHASE:-}" != ready ]; then
      curl -sS --max-time 5 -X DELETE \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
        -H 'Content-Type: application/json' \
        -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
      echo "!! terminal broker did not drain in 120s; maintenance was aborted and nothing was replaced"
      exit 1
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
    if [ "$MAINTENANCE_ACTIVE" = 1 ]; then
      echo "→ replacement is listening; waiting for fresh executor reconciliation"
      RECONCILED=0
      for _ in $(seq 1 180); do
        RESTART_REPLY=$(curl -sS --max-time 5 \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true)
        PHASE=$(printf '%s' "$RESTART_REPLY" | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("restart",{}).get("phase",""))
except Exception: pass')
        if [ "$PHASE" = complete ]; then RECONCILED=1; break; fi
        sleep 1
      done
      if [ "$RECONCILED" != 1 ]; then
        curl -sS --max-time 5 -X DELETE \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
          -H 'Content-Type: application/json' \
          -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
        echo "!! replacement did not reconcile in 180s; maintenance was explicitly aborted"
        exit 1
      fi
      MAINTENANCE_ACTIVE=0
    fi
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
