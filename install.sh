#!/bin/bash
# Download the latest release and install it, without Homebrew.
#
#   ./install.sh              → /Applications
#   ./install.sh ~/Applications
#
# TsunamiWorks releases are Developer ID signed and notarized. The legacy branch below exists only
# while v0.6.0 and earlier remain downloadable; it can go after the first notarized release lands.
set -euo pipefail

REPO="sainteye/clawdline"
DEST="${1:-/Applications}"
APP="Clawdline.app"
EXPECTED_TEAM_ID="83D62P566Q"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Kept as one bounded seam so the installer can prove the precise app it launches without
# replaying download, replacement, or signature checks. Production always uses macOS `open`;
# the optional argument is only a focused-test probe.
launch_installed_app() {
  local opener="${1:-${CLAWDLINE_OPEN_COMMAND:-/usr/bin/open}}"
  "$opener" "$DEST/$APP"
}

echo "→ looking up the latest release of $REPO"
# The body is fetched rather than piped, so that a failure can be explained rather than just
# ending the script. GitHub allows 60 unauthenticated API calls an hour per address, which a
# shared office, a VPN or a CI runner can exhaust without anybody here doing anything — and the
# first step of an installer is the worst possible place to exit with no reason given.
STATUS=$(curl -sSL -o "$TMP/release.json" -w '%{http_code}' \
  "https://api.github.com/repos/$REPO/releases/latest" || echo 000)
if [ "$STATUS" != "200" ]; then
  echo "  GitHub answered $STATUS."
  if grep -q "rate limit" "$TMP/release.json" 2>/dev/null; then
    echo "  That is its hourly limit for unauthenticated requests from your address, not you."
    echo "  Wait an hour, or download the .zip by hand:"
  else
    echo "  Could not read the release list. Download the .zip by hand:"
  fi
  echo "    https://github.com/$REPO/releases/latest"
  echo "  Or build it yourself, which needs no network at all after the clone:"
  echo "    git clone https://github.com/$REPO && cd clawdline && ./build.sh"
  exit 1
fi

URL=$(/usr/bin/python3 -c '
import json, sys
release = json.load(open(sys.argv[1]))
assets = [a for a in release.get("assets", []) if a["name"].endswith(".zip")]
if not assets:
    sys.exit("no .zip asset on the latest release")
print(assets[0]["browser_download_url"])
' "$TMP/release.json")
VERSION=$(basename "$(dirname "$URL")")
echo "  $VERSION"

echo "→ downloading"
curl -fsSL -o "$TMP/app.zip" "$URL"

echo "→ unpacking"
ditto -x -k "$TMP/app.zip" "$TMP/out"
[ -d "$TMP/out/$APP" ] || { echo "the archive did not contain $APP"; exit 1; }

if [ -e "$DEST/$APP" ]; then
  echo "→ replacing the copy already in $DEST"
  pkill -x Clawdline 2>/dev/null || true
  rm -rf "${DEST:?}/$APP"
fi

echo "→ installing to $DEST"
mkdir -p "$DEST"
ditto "$TMP/out/$APP" "$DEST/$APP"

SIGN_INFO=$(codesign --display --verbose=4 "$DEST/$APP" 2>&1 || true)
case "$SIGN_INFO" in
  *"Authority=Developer ID Application: TsunamiWorks Co., Ltd. ($EXPECTED_TEAM_ID)"*"TeamIdentifier=$EXPECTED_TEAM_ID"*)
    echo "→ verifying TsunamiWorks signature and Apple notarization"
    codesign --verify --deep --strict --verbose=2 "$DEST/$APP"
    spctl --assess --type execute --verbose=2 "$DEST/$APP"
    ;;
  *)
    echo "→ legacy release: clearing quarantine (v0.6.0 and earlier were ad-hoc signed)"
    xattr -dr com.apple.quarantine "$DEST/$APP" 2>/dev/null || true
    ;;
esac

echo
echo "✓ installed $VERSION to $DEST/$APP"
echo "→ opening $DEST/$APP"
if ! launch_installed_app; then
  echo "!! installed successfully, but macOS could not open $DEST/$APP"
  exit 1
fi
echo "  press ⌥Space in iTerm2"
echo "  macOS will ask once whether it may control iTerm2 — it cannot send anything without that"
