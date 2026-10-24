#!/bin/bash
# Package an existing App archive; never rebuild or re-sign the App.
# usage: dmg.sh pack|verify|fixture-pack|fixture-verify SOURCE DELIVERY VERSION SOURCE_COMMIT PACKAGING_COMMIT WORKFLOW
# SOURCE holds the original manifest, App tar and App SBOM; DELIVERY holds
# exactly the DMG and its two sidecars (or the full standard release).
# pack requires the release identity/notary environment from codesign.sh.
# fixture-* is local-only, ad hoc, has no Apple API calls and marks evidence
# fixture=true, which verify and gate.sh refuse. No environment bypass exists.
# verify checks local bytes and Apple acceptance; gate.sh adds provenance,
# trusted manifest signature, source SBOM verification and privacy scanning.
set -euo pipefail
export LC_ALL=C
umask 022
cd "$(dirname "$0")/../.."
die() { echo "dmg: $*" >&2; exit 1; }
[ "$#" = 7 ] || die "usage: dmg.sh pack|verify|fixture-pack|fixture-verify SOURCE DELIVERY VERSION SOURCE_COMMIT PACKAGING_COMMIT WORKFLOW"
mode=$1 source=$2 delivery=$3 version=$4 source_commit=$5 packaging_commit=$6 workflow=$7
fixture=0
case "$mode" in pack|verify) ;; fixture-pack) mode=pack; fixture=1 ;; fixture-verify) mode=verify; fixture=1 ;; *) die "unknown mode" ;; esac
[ "$(uname -s)" = Darwin ] || die "disk images need macOS"
for tool in hdiutil codesign xcrun python3 node ditto; do command -v "$tool" >/dev/null || die "missing $tool"; done
source=$(cd "$source" && pwd -P)
mkdir -p "$delivery"
delivery=$(cd "$delivery" && pwd -P)
python3 tools/release/dmg-evidence.py source "$source" "$version" "$source_commit"
[[ "$packaging_commit" =~ ^[0-9a-f]{40}$ ]] || die "packaging commit must be a full SHA"
case "$workflow" in .github/workflows/dmg.yml|.github/workflows/release.yml) ;; *) die "untrusted packaging workflow" ;; esac
archive="$source/Clawdline-${version}-macos-arm64.tar.gz"
image="$delivery/Clawdline-${version}-macos-arm64.dmg"
work=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-dmg.XXXXXX")
mounted=0 finished=0
. tools/release/codesign.sh
cleanup() {
  local rc=$?
  if [ "$mounted" = 1 ]; then
    if hdiutil detach "$work/mount"; then mounted=0; else
      echo "dmg: detach failed; preserved mount at $work/mount; detach it before cleanup" >&2
      rc=1
    fi
  fi
  codesign_end
  if [ "$mounted" = 0 ]; then rm -rf -- "$work"; fi
  [ "$finished" = 1 ] || { [ "$rc" != 0 ] || rc=1; }
  exit "$rc"
}
trap cleanup EXIT
python3 tools/release/dmg-evidence.py extract "$archive" "$work/source"
app="$work/source/Clawdline Next.app"
if [ "$fixture" = 1 ]; then
  codesign --verify --deep --strict "$app"
else
  codesign_verify_release "$app"
fi
if [ "$mode" = pack ]; then
  [ ! -e "$image" ] && [ ! -e "$image.delivery.json" ] && [ ! -e "$image.spdx.json" ] || die "delivery already exists; verify it or use a fresh directory"
  if [ "$fixture" = 0 ]; then
    codesign_begin
    codesign_require_developer_id
  fi
  mkdir "$work/stage"
  ditto "$app" "$work/stage/Clawdline Next.app"
  ln -s /Applications "$work/stage/Applications"
  python3 tools/release/dmg-evidence.py instructions "$work/stage/Install.txt"
  hdiutil create -volname "Clawdline $version" -srcfolder "$work/stage" -format UDZO -fs HFS+ "$image"
  if [ "$fixture" = 1 ]; then
    codesign --sign - "$image"
  else
    codesign --sign "$CLAWDLINE_CODESIGN_ID" --timestamp "$image"
    codesign_notarize_dmg "$image"
  fi
fi
hdiutil verify "$image"
if [ "$fixture" = 0 ]; then codesign_verify_dmg "$image"; else codesign --verify --strict "$image"; fi
mkdir "$work/mount"
mounted=1 # attempt detach even if attach partially succeeds and then fails
hdiutil attach "$image" -readonly -nobrowse -noautoopen -mountpoint "$work/mount"
if [ "$fixture" = 0 ]; then codesign_verify_release "$work/mount/Clawdline Next.app"; fi
args=()
[ "$fixture" = 0 ] || args+=(--fixture)
action=verify
[ "$mode" != pack ] || action=generate
python3 tools/release/dmg-evidence.py "$action" "$source" "$delivery" "$work/source" "$work/mount" \
  "$version" "$source_commit" "$packaging_commit" "$workflow" ${args[@]+"${args[@]}"}
if [ "$mode" = verify ] && [ "$fixture" = 0 ]; then
  # Include the mounted payload AND the published sidecars in the private
  # scan. Failure/unknown is fatal. The source gate scans original archives.
  mkdir "$work/privacy"
  ditto "$work/mount" "$work/privacy/payload"
  cp "$image.delivery.json" "$image.spdx.json" "$work/privacy/"
  tools/check-private.sh -dir "$work/privacy"
fi
hdiutil detach "$work/mount"
mounted=0
echo "dmg: $mode completed (fixture=$fixture); no release was published"
finished=1
