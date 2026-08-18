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

rm -rf "$APP"
mkdir -p "$(dirname "$BIN")" "$RES"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -O \
  -o "$BIN" \
  Sources/*.swift \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation

cp Resources/iterm.js "$RES/"
cp -R Resources/mascots "$RES/"

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Clawdline</string>
  <key>CFBundleDisplayName</key><string>Clawdline</string>
  <key>CFBundleIdentifier</key><string>dev.sainteye.clawdline</string>
  <key>CFBundleExecutable</key><string>Clawdline</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.5.0</string>
  <key>CFBundleVersion</key><string>0.5.0</string>
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
  echo "✓ done (relaunched, since it was running before)"
else
  echo "✓ done"
fi
echo "  run:    open \"$APP\""
echo "  config: ~/.config/clawdline/config.json"
