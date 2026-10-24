#!/bin/bash
# The release evidence checks fail on what they exist to catch
# (docs/security/release-evidence.md). Builds a synthetic test release from
# this checkout's HEAD and drives sbom.sh, provenance.sh, reproduce.sh, the
# macOS release check and gate.sh through one passing case and each failure.
#
# usage: tools/heavy.sh bash tools/release/supply-chain.test.sh
#
# provenance.sh runs against a stub gh that prints what `gh attestation
# verify --format json` prints; the Sigstore verification itself is gh's and is
# not exercised here. Nothing is downloaded and nothing leaves the machine.
set -euo pipefail
cd "$(dirname "$0")/../.."
root=$PWD

work=$(mktemp -d)
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT

pass=0 fails=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1" >&2; fails=$((fails + 1)); }
# expect_ok <name> <cmd…> / expect_fail <name> <pattern> <cmd…>: a failure
# must also say why, so a script that dies for another reason does not pass.
expect_ok() { local n=$1; shift; if "$@" >"$work/log" 2>&1; then ok "$n"; else bad "$n"; sed 's/^/    /' "$work/log" >&2; fi; }
expect_fail() {
  local n=$1 pat=$2; shift 2
  if "$@" >"$work/log" 2>&1; then bad "$n (passed)"; sed 's/^/    /' "$work/log" >&2
  elif grep -Eq -- "$pat" "$work/log"; then ok "$n"
  else bad "$n (failed for another reason)"; sed 's/^/    /' "$work/log" >&2; fi
}

. tools/release/tmux/sources.sh
version=v0.0.0-test.1
commit=$(git rev-parse HEAD)
committed_at=$(TZ=UTC git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ "$commit")
gobin="$(go env GOROOT)/bin/gofmt" # any Go binary carries the build information an SBOM reads

# make_release <dir>: a test release with one Linux archive laid out as
# build.sh lays it out, an SBOM for it, and an unsigned manifest.
make_release() {
  local d=$1 stage="$work/stage.$RANDOM"
  mkdir -p "$d" "$stage/dist/assets" "$stage/libexec" "$stage/licenses"
  cp "$gobin" "$stage/clawdline"
  printf 'console.log(1)\n' >"$stage/dist/assets/main-abc.js"
  printf '{"stamp":"%s","committed_at":"%s","version":"%s"}\n' "$commit" "$committed_at" "$version" >"$stage/dist/BUILD.json"
  printf '#!/bin/sh\n' >"$stage/libexec/tmux"
  {
    printf '{\n  "goos": "linux",\n  "goarch": "amd64",\n'
    printf '  "tmux": {"version": "%s", "sha256": "%s"},\n' "$TMUX_VERSION" "$TMUX_SHA256"
    printf '  "libevent": {"version": "%s", "sha256": "%s"},\n' "$LIBEVENT_VERSION" "$LIBEVENT_SHA256"
    printf '  "utf8proc": {"version": "%s", "sha256": "%s"},\n' "$UTF8PROC_VERSION" "$UTF8PROC_SHA256"
    printf '  "ncurses": {"version": "%s", "sha256": "%s"},\n' "$NCURSES_VERSION" "$NCURSES_SHA256"
    printf '  "image": "%s",\n' "$ALPINE_IMAGE_AMD64"
    printf '  "binary_sha256": "%s"\n}\n' "$(shasum -a 256 "$stage/libexec/tmux" | cut -d' ' -f1)"
  } >"$stage/libexec/tmux.json"
  printf 'license\n' >"$stage/licenses/tmux.txt"
  COPYFILE_DISABLE=1 tar -C "$stage" -czf "$d/clawdline_${version}_linux_amd64.tar.gz" .
  rm -rf "$stage"
  (cd "$d" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' >SHA256SUMS)
  go run ./tools/release manifest -version "$version" -commit "$commit" -committed-at "$committed_at" \
    -channel beta -base-url "http://127.0.0.1:1/$version" "$d" >/dev/null
  tools/release/sbom.sh generate "$d" >/dev/null
}

# refresh_manifest <dir>: the manifest and SHA256SUMS again after an archive
# was replaced, so only the SBOM is stale.
refresh_manifest() {
  rm -f "$1/manifest.json" "$1/SHA256SUMS"
  (cd "$1" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' >SHA256SUMS)
  go run ./tools/release manifest -version "$version" -commit "$commit" -committed-at "$committed_at" \
    -channel beta -base-url "http://127.0.0.1:1/$version" "$1" >/dev/null
}

echo "== SBOM"
good="$work/good"
make_release "$good"
expect_ok "a fresh SBOM matches its archive" tools/release/sbom.sh verify "$good"
node -e '
const d = JSON.parse(require("fs").readFileSync(process.argv[1]));
const has = (s) => d.packages.some((p) => p.summary === s);
if (!has("artifact-content") || !has("source-inventory")) process.exit(1);
if (!d.packages.some((p) => p.name === "tmux") || !d.packages.some((p) => p.name === "ncurses")) process.exit(1);
if (!d.packages.some((p) => p.SPDXID === "SPDXRef-Console")) process.exit(1);
' "$good/clawdline_${version}_linux_amd64.tar.gz.spdx.json" && ok "the SBOM separates artifact content from source inventory and names tmux, ncurses and the console" || bad "SBOM content"

d="$work/no-sbom"; cp -R "$good" "$d"; rm "$d"/*.spdx.json
expect_fail "a missing SBOM fails" 'has no .*\.spdx\.json' tools/release/sbom.sh verify "$d"

d="$work/wrong-digest"; cp -R "$good" "$d"; printf 'x' >>"$d/clawdline_${version}_linux_amd64.tar.gz"
expect_fail "an archive that is not the manifest's digest fails" "not the manifest's" tools/release/sbom.sh verify "$d"

d="$work/stale"; cp -R "$good" "$d"
(mkdir "$work/re" && cd "$work/re" && tar -xzf "$d/clawdline_${version}_linux_amd64.tar.gz" && printf 'extra\n' >dist/assets/extra.js &&
  COPYFILE_DISABLE=1 tar -czf "$d/clawdline_${version}_linux_amd64.tar.gz" .)
rm -rf "$work/re"
refresh_manifest "$d"
expect_fail "an SBOM that omits a file the archive holds fails" 'does not describe' tools/release/sbom.sh verify "$d"

d="$work/edited"; cp -R "$good" "$d"
node -e '
const fs = require("fs"), f = process.argv[1], s = JSON.parse(fs.readFileSync(f));
s.packages = s.packages.filter((p) => p.name !== "libevent");
fs.writeFileSync(f, JSON.stringify(s, null, 1) + "\n");' "$d/clawdline_${version}_linux_amd64.tar.gz.spdx.json"
expect_fail "an SBOM with a package removed fails" 'does not describe.*Native-libevent' tools/release/sbom.sh verify "$d"

# The tmux pins an SBOM is checked against are the manifest commit's. A
# scratch clone shares this repository's objects; its working tree holds only
# the sbom.sh under test and a sources.sh whose pins have since moved, and a
# second commit moves the pins in history.
pinclone="$work/pin-clone"
git clone -q --shared --no-checkout "$root" "$pinclone"
mkdir -p "$pinclone/tools/release/tmux"
cp tools/release/sbom.sh "$pinclone/tools/release/sbom.sh"
sed -e "s/^TMUX_VERSION=.*/TMUX_VERSION=9.9z/" -e "s/^TMUX_SHA256=.*/TMUX_SHA256=$(printf moved | shasum -a 256 | cut -d' ' -f1)/" \
  tools/release/tmux/sources.sh >"$pinclone/tools/release/tmux/sources.sh"
expect_ok "pins that moved in the checkout do not fail an older release's SBOM" "$pinclone/tools/release/sbom.sh" verify "$good"
moved=$(cd "$pinclone" && export GIT_INDEX_FILE="$work/pin-index" && git read-tree "$commit" &&
  git update-index --add --cacheinfo "100644,$(git hash-object -w tools/release/tmux/sources.sh),tools/release/tmux/sources.sh" &&
  GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid \
    git commit-tree -p "$commit" -m "move the tmux pin" "$(git write-tree)")
d="$work/moved-pin"; cp -R "$good" "$d"
rm -f "$d/manifest.json" "$d/SHA256SUMS" "$d"/*.spdx.json
(cd "$d" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' >SHA256SUMS)
go run ./tools/release manifest -version "$version" -commit "$moved" -committed-at "$committed_at" \
  -channel beta -base-url "http://127.0.0.1:1/$version" "$d" >/dev/null
"$pinclone/tools/release/sbom.sh" generate "$d" >/dev/null
expect_fail "an archive whose tmux is not its commit's pinned tmux fails" 'does not name the pinned tmux 9\.9z' "$pinclone/tools/release/sbom.sh" verify "$d"
rm -rf "$pinclone"

echo "== provenance (stub gh)"
cp "$good/manifest.json" "$work/manifest.copy"
printf '[]\n' >"$good/manifest.sig.json" # provenance needs every asset present; its content is not read here
cat >"$work/gh" <<'STUB'
#!/bin/bash
# What `gh attestation verify --format json` prints for one verified asset,
# shaped by STUB_MODE. It insists on the policy flags provenance.sh owes it,
# and matches the signer identity as gh does: --cert-identity exactly,
# --signer-workflow as a prefix of the certificate's SAN (gh's
# validateSignerWorkflow), so a looser flag passes here as it would there.
set -euo pipefail
file=$3
wf=.github/workflows/release.yml
if [ -f "$(dirname "$file")/manifest.json" ]; then
  want_commit=$(node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(process.argv[1])).commit)' "$(dirname "$file")/manifest.json")
else
  metadata=("$(dirname "$file")"/*.dmg.delivery.json)
  want_commit=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1])).packaging_commit' "${metadata[0]}")
  wf=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1])).packaging_workflow' "${metadata[0]}")
fi
args=" $* "
owner=sainteye/clawdline
san=https://github.com/sainteye/clawdline/$wf@refs/heads/main
case "${STUB_MODE:-}" in
  # Somebody else's genuinely signed attestation: gh accepts it only when it
  # is asked for that repository.
  someone-else) owner=someone-else/clawdline; san=https://github.com/someone-else/clawdline/.github/workflows/release.yml@refs/heads/main ;;
  # A workflow whose path merely starts with release.yml, signing a statement
  # that claims to be release.yml.
  prefix-signer) san=https://github.com/sainteye/clawdline/.github/workflows/release.yml-copy.yml@refs/heads/main ;;
esac
for need in "--repo $owner" "--source-ref refs/heads/main" "--source-digest $want_commit" "--deny-self-hosted-runners" "--format json"; do
  [[ "$args" == *" $need "* ]] || { echo "Error: no attestations found (stub gh: missing $need)" >&2; exit 1; }
done
if [[ "$args" =~ \ --cert-identity\ ([^ ]+)\  ]]; then
  [ "${BASH_REMATCH[1]}" = "$san" ] || { echo "Error: no attestations found (stub gh: SAN $san)" >&2; exit 1; }
elif [[ "$args" =~ \ --signer-workflow\ ([^ ]+)\  ]]; then
  [[ "$san" == "https://github.com/${BASH_REMATCH[1]}"* ]] || { echo "Error: no attestations found (stub gh: SAN $san)" >&2; exit 1; }
else
  echo "stub gh: no signer identity asked for" >&2; exit 1
fi
[ "${STUB_MODE:-}" != gh-refuses ] || { echo "Error: no attestations found" >&2; exit 1; }
digest=$(shasum -a 256 "$file" | cut -d' ' -f1)
repo=https://github.com/$owner commit=$want_commit
case "${STUB_MODE:-}" in
  wrong-repo) repo=https://github.com/someone-else/clawdline ;;
  wrong-commit) commit=0000000000000000000000000000000000000000 ;;
  wrong-digest) digest=$(printf other | shasum -a 256 | cut -d' ' -f1) ;;
  wrong-workflow) wf=.github/workflows/ci.yml ;;
esac
printf '[{"verificationResult":{"signature":{"certificate":{"subjectAlternativeName":"%s"}},"statement":{"predicateType":"https://slsa.dev/provenance/v1","subject":[{"name":"%s","digest":{"sha256":"%s"}}],"predicate":{"buildDefinition":{"externalParameters":{"workflow":{"repository":"%s","path":"%s","ref":"refs/heads/main"}},"resolvedDependencies":[{"uri":"git+%s@refs/heads/main","digest":{"gitCommit":"%s"}}]}}}}}]\n' \
  "$san" "$(basename "$file")" "$digest" "$repo" "$wf" "$repo" "$commit"
STUB
chmod +x "$work/gh"
export CLAWDLINE_GH="$work/gh"
expect_ok "provenance from the release workflow at the manifest's commit passes" tools/release/provenance.sh "$good"
STUB_MODE=wrong-repo expect_fail "an attestation from another repository fails" 'repository https://github.com/someone-else' tools/release/provenance.sh "$good"
STUB_MODE=wrong-commit expect_fail "an attestation of another commit fails" 'source commit' tools/release/provenance.sh "$good"
STUB_MODE=wrong-digest expect_fail "an attestation of another digest fails" 'subject digest' tools/release/provenance.sh "$good"
STUB_MODE=gh-refuses expect_fail "an asset gh finds no attestation for fails" 'has no attestation' tools/release/provenance.sh "$good"
d="$work/no-sig"; cp -R "$good" "$d"; rm "$d/manifest.sig.json"
expect_fail "a release missing an asset fails" 'manifest.sig.json is missing' tools/release/provenance.sh "$d"
STUB_MODE=wrong-workflow expect_fail "an attestation whose statement names another workflow fails" 'workflow .github/workflows/ci.yml' tools/release/provenance.sh "$good"
STUB_MODE=prefix-signer expect_fail "a signer whose workflow path only starts with release.yml fails" 'has no attestation' tools/release/provenance.sh "$good"
CLAWDLINE_PROVENANCE_REPO=someone-else/clawdline CLAWDLINE_PROVENANCE_WORKFLOW=.github/workflows/release.yml STUB_MODE=someone-else \
  expect_fail "the environment cannot point provenance at another repository" 'has no attestation' tools/release/provenance.sh "$good"
unset CLAWDLINE_GH
rm "$good/manifest.sig.json"

echo "== DMG delivery provenance (stub gh)"
dmgs="$work/dmg-provenance"; mkdir "$dmgs"
printf 'fixture image' >"$dmgs/Clawdline-v2.1.2-macos-arm64.dmg"
printf '{}' >"$dmgs/Clawdline-v2.1.2-macos-arm64.dmg.spdx.json"
printf '{"packaging_commit":"%s","packaging_workflow":".github/workflows/dmg.yml"}' "$commit" >"$dmgs/Clawdline-v2.1.2-macos-arm64.dmg.delivery.json"
export CLAWDLINE_GH="$work/gh"
expect_ok "DMG evidence is attested by its packaging workflow and commit" tools/release/provenance.sh "$dmgs" --dmg
STUB_MODE=wrong-commit expect_fail "DMG provenance of another packaging commit fails" 'source commit' tools/release/provenance.sh "$dmgs" --dmg
STUB_MODE=wrong-digest expect_fail "DMG provenance of another image fails" 'subject digest' tools/release/provenance.sh "$dmgs" --dmg
STUB_MODE=wrong-workflow expect_fail "DMG statement of another workflow fails" 'workflow .github/workflows/ci.yml' tools/release/provenance.sh "$dmgs" --dmg
rm "$dmgs/Clawdline-v2.1.2-macos-arm64.dmg.spdx.json"
expect_fail "missing DMG SBOM attestation fails" 'is missing' tools/release/provenance.sh "$dmgs" --dmg
unset CLAWDLINE_GH
bash tools/release/dmg.test.sh --no-disk

echo "== rebuild comparison"
expect_ok "two identical builds compare clean" tools/release/reproduce.sh compare "$good" "$good"
expect_fail "a different payload file is reported" '^differs .*dist/assets/extra.js|only-in-B .*extra.js' tools/release/reproduce.sh compare "$good" "$work/stale"
d="$work/extra-archive"; cp -R "$good" "$d"; cp "$good/clawdline_${version}_linux_amd64.tar.gz" "$d/clawdline_${version}_linux_arm64.tar.gz"
# Named relative to the directory it is run from, as a person types them.
expect_fail "an archive only the second build has is reported, with relative paths too" '^only-in-B +clawdline_.*_linux_arm64' \
  bash -c 'cd "$1" && "$2/tools/release/reproduce.sh" compare good extra-archive' _ "$work" "$root"
if [ "$(uname -s)" = Darwin ]; then
  # A Mach-O file differs; it is signature-only only when the bytes left once
  # both signatures are removed are the same.
  mk_mach() { local d=$1 bin=$2 ident=$3 s="$work/mach.$RANDOM"; mkdir -p "$d" "$s"; cp "$bin" "$s/clawdline"; chmod u+w "$s/clawdline"
    codesign --force --sign - --identifier "$ident" "$s/clawdline" 2>/dev/null; COPYFILE_DISABLE=1 tar -C "$s" -czf "$d/clawdline_${version}_darwin_arm64.tar.gz" .; rm -rf "$s"; }
  other="$(go env GOROOT)/bin/go"
  mk_mach "$work/mach-a" "$gobin" one; mk_mach "$work/mach-b" "$gobin" two; mk_mach "$work/mach-c" "$other" two
  expect_ok "a Mach-O file re-signed with another identity is signature-only" tools/release/reproduce.sh compare "$work/mach-a" "$work/mach-b"
  expect_fail "a Mach-O file with another payload is not called signature-only" '^differs .*:clawdline$' tools/release/reproduce.sh compare "$work/mach-a" "$work/mach-c"
fi

if [ "$(uname -s)" = Darwin ]; then
  echo "== macOS release signature"
  # The toolchain's own signature differs by installer (ad hoc from source,
  # Developer ID on GitHub's runners), so the fixture is re-signed ad hoc and
  # the precondition checked before the case relies on it.
  cp "$gobin" "$work/adhoc"; chmod u+w "$work/adhoc"
  codesign --force --sign - "$work/adhoc" 2>/dev/null
  adhocsig=$(codesign -dvvv "$work/adhoc" 2>&1 || true)
  if grep -q '^Signature=adhoc$' <<<"$adhocsig" && ! grep -q '^Authority=' <<<"$adhocsig"; then
    ok "the ad hoc fixture carries an ad hoc signature and no authority"
  else
    bad "the ad hoc fixture is not ad hoc signed"; sed 's/^/    /' <<<"$adhocsig" >&2
  fi
  expect_fail "an ad hoc signature is not a release signature" 'not signed by a Developer ID' \
    bash -c '. tools/release/codesign.sh; codesign_verify_release "$1"' _ "$work/adhoc"
  # Node's official macOS binary is a Developer ID signature from another
  # team, with the hardened runtime, a timestamp and a notarization ticket:
  # everything a release has except its signer.
  nodebin=$(node -p 'require("fs").realpathSync(process.execPath)')
  nodesig=$(codesign -dvvv "$nodebin" 2>&1 || true)
  if grep -q '^Authority=Developer ID Application: ' <<<"$nodesig"; then
    expect_fail "another team's notarized Developer ID binary is not a release signature" 'not signed by the Clawdline release team' \
      bash -c '. tools/release/codesign.sh; codesign_verify_release "$1"' _ "$nodebin"
    team=$(sed -n 's/^TeamIdentifier=//p' <<<"$nodesig")
    CLAWDLINE_RELEASE_TEAM_ID=$team expect_fail "the environment cannot name another release team" 'not signed by the Clawdline release team' \
      bash -c '. tools/release/codesign.sh; codesign_verify_release "$1"' _ "$nodebin"
  else
    echo "SKIP  another team's Developer ID binary: $nodebin is not Developer ID signed"
  fi
fi

echo "== gate (test release, signed with a throwaway key)"
pub=$(go run ./tools/release keygen "$work/test.key" | awk '{print $3}')
signed="$work/signed"; cp -R "$good" "$signed"
go run ./tools/release sign -key "$work/test.key" "$signed" >/dev/null
export CLAWDLINE_TEST_RELEASE_PUB=$pub
expect_fail "a packaging pin cannot silently gate a source without a DMG" 'requires a DMG delivery' tools/release/gate.sh "$version" --dir "$signed" --packaging-commit "$commit"
# The privacy scan answers 3 (undetermined) on a machine without the private
# word list and 0 with it; either way the evidence checks before it passed.
if tools/release/gate.sh "$version" --dir "$signed" >"$work/log" 2>&1; then rc=0; else rc=$?; fi
if { [ "$rc" = 0 ] || [ "$rc" = 3 ]; } && grep -q 'sbom: every archive has an SBOM' "$work/log"; then ok "the gate accepts a complete test release up to its privacy scan"; else bad "gate on a complete test release ($rc)"; sed 's/^/    /' "$work/log" >&2; fi
d="$work/gate-extra"; cp -R "$signed" "$d"; printf 'x\n' >"$d/notes.txt"
expect_fail "the gate refuses an asset the manifest does not account for" 'does not account for: notes.txt' tools/release/gate.sh "$version" --dir "$d"
d="$work/gate-no-sbom"; cp -R "$signed" "$d"; rm "$d"/*.spdx.json
expect_fail "the gate refuses a release without SBOMs" 'has no .*\.spdx\.json' tools/release/gate.sh "$version" --dir "$d"
d="$work/gate-fixture-dmg"; cp -R "$signed" "$d"; printf 'fixture' >"$d/Clawdline-${version}-macos-arm64.dmg"
expect_fail "a fixture DMG cannot use the test-release gate bypass" 'fixture DMGs cannot pass' tools/release/gate.sh "$version" --dir "$d"
# A -test version may skip provenance and the macOS check only as a local
# --dir release; one drafted on GitHub gets the whole gate or none. The stub
# gh answers as a draft whose assets are this test release.
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<STUB
#!/bin/bash
case "\$1 \$2" in
  "release view") echo true ;;
  "release download") while [ "\$#" -gt 0 ]; do [ "\$1" = --dir ] && { mkdir -p "\$2"; cp "$signed"/* "\$2"/; }; shift; done ;;
  *) echo "stub gh: \$*" >&2; exit 1 ;;
esac
STUB
chmod +x "$work/bin/gh"
PATH="$work/bin:$PATH" expect_fail "a -test version drafted on GitHub is not gated without provenance" 'only with --dir' tools/release/gate.sh "$version"
unset CLAWDLINE_TEST_RELEASE_PUB

echo "supply-chain: $pass passed, $fails failed"
[ "$fails" = 0 ]
finished=1
