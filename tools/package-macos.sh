#!/bin/bash
# Build the macOS app: the Go daemon and the Swift shell in one bundle.
#
# The daemon ships inside the app rather than beside it, so there is one thing
# to install and one thing to remove, and the shell always runs the daemon it
# was built with rather than whichever one is on PATH.
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="Clawdline Next"
ID="com.sainteye.clawdline-next"     # deliberately not the Swift app's id:
                                      # both must be installable at once
APP="dist/$NAME.app"
VERSION="$(git describe --tags --always 2>/dev/null || echo 0.0.1)"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" \
  -o "$APP/Contents/MacOS/clawdline" ./cmd/clawdline
swiftc -O -o "$APP/Contents/MacOS/$NAME" shell/darwin/main.swift

# The console travels with the app. It is read from the Swift checkout at build
# time and copied, never linked: a bundle that depends on a directory outside
# itself is not a bundle.
WEB="${CLAWDLINE_WEB_SOURCE:-$HOME/code/clawdline/Resources/web}"
if [ -d "$WEB" ]; then
  cp -R "$WEB" "$APP/Contents/Resources/web"
else
  echo "warning: no console at $WEB; the app will have nothing to show" >&2
fi

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>$NAME</string>
  <key>CFBundleIdentifier</key><string>$ID</string>
  <key>CFBundleExecutable</key><string>$NAME</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

echo "built $APP ($(du -sh "$APP" | cut -f1))"

# A disk image, because that is how a Mac application arrives.
if [ "${1:-}" = "--dmg" ]; then
  DMG="dist/$NAME-$VERSION.dmg"
  rm -f "$DMG"
  STAGE="$(mktemp -d)"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -quiet -volname "$NAME" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
  rm -rf "$STAGE"
  echo "built $DMG ($(du -h "$DMG" | cut -f1))"
fi
