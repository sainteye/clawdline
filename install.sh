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

# BEGIN install-focused: release url
# **This step used `/usr/bin/python3` and that is a dependency this script may not have.**
# `/usr/bin/python3` is an xcselect shim — `otool -L` on it names `libxcselect.dylib`, not a Python
# runtime — so on a Mac without the Command Line Tools it opens the "install the command line
# developer tools" dialog and exits non-zero. Under `set -euo pipefail` that ended the install
# here, on the second step of the path the website and both READMEs recommend, **without printing
# a single word about why**. Every other failure in this file says something; this one did not.
# The four commands further down are not affected: `codesign`, `xattr`, `spctl` and `ditto` are
# real Mach-O executables and were checked with `file` rather than assumed.
#
# **Why `awk` over a second endpoint.** `/releases/latest/download/<name>` needs the asset's name
# known in advance, which is the thing that varies, and any other endpoint is a second network
# round trip on the step that already failed for a rate-limited address. The JSON is here; it is
# read with the tools this script already depends on.
#
# **And why the key is the URL rather than the name.** GitHub does not promise a field order, so
# nothing here counts fields or reads a position: it looks for the `browser_download_url` key by
# name and takes its value. An asset name may contain whitespace — inside a URL that arrives
# percent-encoded, so the value cannot contain a space, a quote or a newline, which is what makes
# a `[^"]*` match safe here where it would not be against `"name"`. `\/` is unescaped on the way
# past because JSON permits an escaped solidus even though GitHub does not currently emit one.
#
# `|| URL=""` is not decoration. A command substitution that fails under `set -e` takes the script
# with it, silently, which is the whole defect being repaired — so the failure is caught here and
# answered by the message below.
URL=$(awk '
  {
    line = $0
    gsub(/\r/, "", line)
    if (!found && match(line, /"browser_download_url"[ \t]*:[ \t]*"[^"]*\.zip"/)) {
      found = substr(line, RSTART, RLENGTH)
      sub(/^"browser_download_url"[ \t]*:[ \t]*"/, "", found)
      sub(/"$/, "", found)
      gsub(/\\\//, "/", found)
      print found
    }
  }
' "$TMP/release.json") || URL=""
# The one shape this script is willing to hand to `curl`. A parse that went wrong in a way nobody
# thought of stops here rather than downloading whatever it produced.
case "$URL" in
  https://*.zip) : ;;
  *) URL="" ;;
esac
if [ -z "$URL" ]; then
  echo "  GitHub answered, but this script could not find a .zip download in the reply."
  echo "  That is this script's reading of the answer, not a problem with your Mac —"
  echo "  the release may have no .zip attached yet, or the reply may not be what was expected."
  echo "  Download the .zip by hand:"
  echo "    https://github.com/$REPO/releases/latest"
  echo "  Or build it yourself, which needs no network at all after the clone:"
  echo "    git clone https://github.com/$REPO && cd clawdline && ./build.sh"
  exit 1
fi
# END install-focused: release url
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
    # BEGIN install-focused: legacy integrity
    # **The legacy branch, and what it is allowed to claim.** v0.6.0 and earlier are ad-hoc signed:
    # there is no Developer ID identity and no notarization ticket, so neither the team-id compare
    # above nor `spctl` can say anything about them. Until this landed, the branch did exactly one
    # thing to a bundle that had just come off the network unexamined — strip the quarantine
    # attribute, which is the one guard macOS had left on it. No checksum, no signature check.
    #
    # What can honestly be checked here is the ad-hoc seal itself. `codesign --verify` recomputes
    # the bundle's hashes and compares them with the signature stored in it, so it proves the app
    # has not been altered since it was built. **It proves nothing about who built it**: an ad-hoc
    # signature carries no identity, and somebody who replaced the download can sign their own copy
    # the same way. So the warning below says that in as many words, and the archive's SHA-256 is
    # printed so it can be compared against the release page by a person who wants more than this.
    #
    # A release with a declared checksum would be better and there is nothing to compare against:
    # this repository's releases publish no checksum file, and the digest field GitHub attaches to
    # an asset would arrive over the same connection as the download it describes. Saying so is the
    # point — a line that read "verified" here would be worth less than this paragraph.
    echo "→ legacy release: v0.6.0 and earlier are ad-hoc signed — no Developer ID, no notarization"
    if ! codesign --verify --deep --strict --verbose=2 "$DEST/$APP"; then
      echo "!! the ad-hoc signature on $DEST/$APP does not verify." >&2
      echo "   The bundle has been altered since it was built, or it was never signed at all." >&2
      echo "   Nothing was launched, the quarantine attribute was left in place, and the copy that" >&2
      echo "   was just unpacked has been removed." >&2
      rm -rf "${DEST:?}/$APP"
      echo "   Download the .zip by hand if you want to look at it:" >&2
      echo "     https://github.com/$REPO/releases/latest" >&2
      exit 1
    fi
    echo "  the ad-hoc seal verifies: this copy has not been altered since it was built"
    echo "  sha256 of the archive installed: $(shasum -a 256 "$TMP/app.zip" | awk '{ print $1 }')"
    echo "!! that is an integrity check and not a provenance one. An ad-hoc signature carries no"
    echo "   identity, so it says the bundle is intact — not who made it. Compare the sha256 above"
    echo "   with the release page, or build from source, if that distinction matters to you."
    echo "→ clearing quarantine"
    xattr -dr com.apple.quarantine "$DEST/$APP" 2>/dev/null || true
    # END install-focused: legacy integrity
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
