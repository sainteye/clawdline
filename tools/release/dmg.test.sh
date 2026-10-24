#!/bin/bash
# Synthetic/ad hoc only. No Developer ID key, network or Apple API calls.
# --no-disk explicitly skips creation/mounting (restricted sandbox checks).
set -euo pipefail
export LC_ALL=C
cd "$(dirname "$0")/../.."
python3 -B tools/release/test-dmg-evidence.py
if [ "$(uname -s)" != Darwin ]; then echo "SKIP DMG mount test: needs macOS"; exit 0; fi
work=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-dmg-test.XXXXXX")
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT
unset CLAWDLINE_CODESIGN_P12 CLAWDLINE_CODESIGN_ID CLAWDLINE_NOTARY_KEY CLAWDLINE_NOTARY_KEY_ID CLAWDLINE_NOTARY_ISSUER
version=v2.1.2
commit=$(git rev-parse HEAD)
mkdir -p "$work/app/Clawdline Next.app/Contents/MacOS" "$work/source"
cp /usr/bin/true "$work/app/Clawdline Next.app/Contents/MacOS/Fixture"
cat >"$work/app/Clawdline Next.app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleExecutable</key><string>Fixture</string><key>CFBundleIdentifier</key><string>invalid.example.dmg-fixture</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$work/app/Clawdline Next.app/Contents/MacOS/Fixture"
codesign --force --sign - "$work/app/Clawdline Next.app"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$work/app" -czf "$work/source/Clawdline-${version}-macos-arm64.tar.gz" .
python3 - "$work/source" "$commit" <<'PY'
import hashlib, json, pathlib, sys
d, commit = pathlib.Path(sys.argv[1]), sys.argv[2]
name = 'Clawdline-v2.1.2-macos-arm64.tar.gz'
p = d / name
m = {'version': 'v2.1.2', 'commit': commit, 'committed_at': '2026-10-22T00:00:00Z',
     'artifacts': [{'name': name, 'kind': 'app', 'os': 'darwin', 'arch': 'arm64', 'size': p.stat().st_size, 'sha256': hashlib.sha256(p.read_bytes()).hexdigest()}]}
(d / 'manifest.json').write_text(json.dumps(m))
(d / (name + '.spdx.json')).write_text(json.dumps({'documentNamespace': 'https://example.invalid/fixture'}))
PY
args=("$work/source" "$work/delivery" "$version" "$commit" "$commit" .github/workflows/dmg.yml)
expect_fail() {
  local pattern=$1; shift
  if "$@" >"$work/refused.log" 2>&1; then echo "FAIL: rejection passed: $*" >&2; exit 1; fi
  grep -Eq "$pattern" "$work/refused.log" || { cat "$work/refused.log" >&2; exit 1; }
  echo "PASS rejection: $pattern"
}
expect_fail 'not signed by a Developer ID' bash tools/release/dmg.sh verify ${args[@]+"${args[@]}"}
expect_fail 'DMG notarization credentials are missing' bash -c '. tools/release/codesign.sh; codesign_notarize_dmg fixture.dmg'
# Exercise notary rejection with a stub; it must never try to staple a rejected
# image. The actual Apple API and private keys are deliberately absent.
mkdir "$work/bin"
cat >"$work/bin/xcrun" <<'STUB'
#!/bin/bash
if [ "$1 $2" = 'notarytool submit' ]; then printf '{"status":"Invalid","id":"fixture-rejected"}\n'; else echo 'unexpected Apple call' >&2; exit 9; fi
STUB
chmod +x "$work/bin/xcrun"
touch "$work/fixture.p8"
PATH="$work/bin:$PATH" CLAWDLINE_NOTARY_KEY="$work/fixture.p8" CLAWDLINE_NOTARY_KEY_ID=fixture CLAWDLINE_NOTARY_ISSUER=fixture \
  expect_fail 'DMG notarization was Invalid' bash -c '. tools/release/codesign.sh; codesign_notarize_dmg "$1"' _ "$work/delivery/Clawdline-v2.1.2-macos-arm64.dmg"
if [ "${1:-}" = --no-disk ]; then
  echo 'SKIP actual disk creation/mounting: explicitly requested --no-disk'
  finished=1
  exit 0
fi
bash tools/release/dmg.sh fixture-pack ${args[@]+"${args[@]}"}
bash tools/release/dmg.sh fixture-verify ${args[@]+"${args[@]}"}
expect_fail 'already exists' bash tools/release/dmg.sh fixture-pack ${args[@]+"${args[@]}"}
python3 - "$work/delivery/Clawdline-v2.1.2-macos-arm64.dmg.delivery.json" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1]); d = json.loads(p.read_text()); d['packaging_commit'] = '0' * 40; p.write_text(json.dumps(d))
PY
expect_fail 'does not describe' bash tools/release/dmg.sh fixture-verify ${args[@]+"${args[@]}"}
echo 'DMG fixture packaging, read-only mount, evidence, ad hoc rejection and notary rejection passed'
finished=1
