#!/bin/bash
# Download the latest release and install it, without Homebrew.
#
#   ./install.sh              → /Applications
#   ./install.sh ~/Applications
#
# The last step clears the quarantine flag. That is not a trick to get around Gatekeeper —
# the build is ad-hoc signed rather than notarized, so macOS refuses to open the downloaded
# copy at all, and the alternative is a trip through System Settings. If you would rather not
# take that on faith, `git clone` and run ./build.sh instead: an app you compiled yourself is
# never quarantined, and there is nothing to trust.
set -euo pipefail

REPO="sainteye/clawdline"
DEST="${1:-/Applications}"
APP="Clawdline.app"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "→ looking up the latest release of $REPO"
URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | /usr/bin/python3 -c '
import json, sys
release = json.load(sys.stdin)
assets = [a for a in release.get("assets", []) if a["name"].endswith(".zip")]
if not assets:
    sys.exit("no .zip asset on the latest release")
print(assets[0]["browser_download_url"])
')
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

echo "→ clearing quarantine (ad-hoc signed, not notarized)"
xattr -dr com.apple.quarantine "$DEST/$APP" 2>/dev/null || true

echo
echo "✓ installed $VERSION to $DEST/$APP"
echo "  open \"$DEST/$APP\", then press ⌥Space in iTerm2"
echo "  macOS will ask once whether it may control iTerm2 — it cannot send anything without that"
