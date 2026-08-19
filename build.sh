#!/bin/bash
# Builds Clawdline.app. No Xcode project, no package manager: a few .swift files and one .js,
# compiled straight by swiftc — one less layer of "what does the build config actually say".
set -euo pipefail
cd "$(dirname "$0")"

APP="${CLAWDLINE_APP:-$HOME/Applications/Clawdline.app}"
BIN="$APP/Contents/MacOS/Clawdline"
RES="$APP/Contents/Resources"

echo "→ building into $APP"

# Stop a running copy first, or overwriting the executable fails. Remember whether it was
# running so it can be put back: a build that silently leaves you without the app is a
# footgun everyone steps on exactly once, usually while wondering why the hotkey died.
WAS_RUNNING=0
pgrep -x Clawdline >/dev/null 2>&1 && WAS_RUNNING=1
pkill -x Clawdline 2>/dev/null || true
# **`pkill` asks; it does not wait.** The bundle is deleted on the next line, and a process on
# its way out is still reading it — AppKit's teardown calls `DisableWindowServerConnection`,
# which asks CoreFoundation for the bundle identifier. Delete the bundle in that window and it
# reads freed memory and dies of SIGSEGV, leaving a crash report from a build that otherwise
# looked fine. Observed 2026-08-19 08:23:18, in `CFBundleGetIdentifier` under `HIToolbox`.
for _ in $(seq 1 60); do
  pgrep -x Clawdline >/dev/null 2>&1 || break
  sleep 0.1
done

rm -rf "$APP"
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

cat > "$APP/Contents/Info.plist" <<'PLIST'
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
codesign --force --sign - --identifier dev.sainteye.clawdline "$APP" >/dev/null 2>&1 \
  || echo "  (codesign failed; harmless, but you may be re-asked to authorise iTerm2 after each rebuild)"

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
