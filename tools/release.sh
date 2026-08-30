#!/bin/bash
#
# Cut a release: tools/release.sh 0.6.0
#
# **It builds from a clean worktree at HEAD, never from the working tree.** This repository is
# often shared with agents doing other work, so what is on disk is not what is committed — and a
# release built from disk ships somebody's half-finished afternoon under a version number that
# claims to be a commit. The worktree also means the build cannot pick up an untracked file that
# only exists on this machine.
#
# It does not push and it does not publish until the very end, so everything it checks it checks
# before anything is public.
#
# The reason this exists at all: 0.5.0 was cut by hand two hours before the entire remote feature
# landed, and for a day the README described a product that the only downloadable build did not
# contain. Nothing caught it, because nothing was watching.
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="${1:-}"
[ -n "$VERSION" ] || { echo "usage: tools/release.sh <version>   e.g. 0.6.0"; exit 2; }
echo "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo "!! not a version: $VERSION"; exit 2; }

TAG="v$VERSION"
TEAM_ID="83D62P566Q"
SIGN_IDENTITY="${CLAWDLINE_SIGN_IDENTITY:-Developer ID Application: TsunamiWorks Co., Ltd. ($TEAM_ID)}"
NOTARY_PROFILE="${CLAWDLINE_NOTARY_PROFILE:-clawdline-notary}"
TMP="$(mktemp -d)"
WORK="$TMP/src"      # the worktree, removed as soon as the build is done
OUT="$TMP/out"       # **outside it**, or removing the worktree takes the build with it
trap 'rm -rf "$TMP"' EXIT

# --- the checks, all of them before anything leaves this machine ------------------------------

git rev-parse --verify "$TAG" >/dev/null 2>&1 && { echo "!! $TAG already exists"; exit 1; }

IDENTITIES=$(security find-identity -v -p codesigning)
case "$IDENTITIES" in
  *\"$SIGN_IDENTITY\"*) ;;
  *) echo "!! Developer ID identity is not available in this keychain: $SIGN_IDENTITY"; exit 1 ;;
esac

if ! xcrun notarytool history --keychain-profile "$NOTARY_PROFILE" >/dev/null 2>&1; then
  echo "!! notarization credentials are unavailable: keychain profile $NOTARY_PROFILE"
  echo "   create them with: xcrun notarytool store-credentials $NOTARY_PROFILE"
  exit 1
fi

grep -q "<string>$VERSION</string>" build.sh || {
  echo "!! build.sh does not say $VERSION — bump CFBundleShortVersionString first"; exit 1; }

# Uncommitted work is allowed to exist — another agent may be mid-task — but it will not be in
# the release, and saying so is the difference between a surprise and a decision.
if [ -n "$(git status --porcelain)" ]; then
  echo "note: the working tree is dirty; the release is built from HEAD and excludes:"
  git status --short | sed 's/^/      /'
fi

echo "== tests"
./test.sh >/dev/null || { echo "!! tests are red"; exit 1; }

# --- build from what is actually committed -----------------------------------------------------

echo "== worktree at $(git rev-parse --short HEAD)"
git worktree add --quiet --detach "$WORK" HEAD
( cd "$WORK" && CLAWDLINE_APP="$OUT/Clawdline.app" CLAWDLINE_BUILD_ONLY=1 \
    CLAWDLINE_SIGN_IDENTITY="$SIGN_IDENTITY" ./build.sh >/dev/null )
git worktree remove --force "$WORK" 2>/dev/null || true

[ -d "$OUT/Clawdline.app" ] || { echo "!! nothing was built"; exit 1; }
BUILT="$(defaults read "$OUT/Clawdline.app/Contents/Info" CFBundleShortVersionString)"
[ "$BUILT" = "$VERSION" ] || { echo "!! the built app says $BUILT, not $VERSION"; exit 1; }
codesign --verify --strict --verbose=2 "$OUT/Clawdline.app"

SIGN_INFO=$(codesign --display --verbose=4 "$OUT/Clawdline.app" 2>&1)
case "$SIGN_INFO" in
  *"Authority=$SIGN_IDENTITY"*"TeamIdentifier=$TEAM_ID"*) ;;
  *) echo "!! built app is not signed by the expected TsunamiWorks identity"; exit 1 ;;
esac

ZIP="$OUT/Clawdline-$VERSION.zip"
( cd "$OUT" && ditto -c -k --keepParent "Clawdline.app" "Clawdline-$VERSION.zip" )

echo "== notarizing"
NOTARY_JSON="$OUT/notary.json"
xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" \
  --wait --output-format json > "$NOTARY_JSON"
NOTARY_STATUS=$(/usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("status", ""))' "$NOTARY_JSON")
[ "$NOTARY_STATUS" = Accepted ] || { echo "!! Apple notarization status: ${NOTARY_STATUS:-missing}"; exit 1; }

xcrun stapler staple "$OUT/Clawdline.app"
xcrun stapler validate "$OUT/Clawdline.app"
spctl --assess --type execute --verbose=2 "$OUT/Clawdline.app"

# A zip itself cannot carry a stapled ticket. Replace the upload archive with one made from the
# stapled bundle; this exact final artifact is the one attached to the GitHub release.
rm "$ZIP"
( cd "$OUT" && ditto -c -k --keepParent "Clawdline.app" "Clawdline-$VERSION.zip" )
echo "== signed, notarized, and built $(du -h "$ZIP" | cut -f1)"

# --- publish ------------------------------------------------------------------------------------

git tag -a "$TAG" -m "Clawdline $VERSION"
git push origin "$TAG"
gh release create "$TAG" "$ZIP" --title "Clawdline $VERSION" --notes-file "${NOTES:-/dev/stdin}"
echo "→ $(gh release view "$TAG" --json url --jq .url)"
