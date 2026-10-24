#!/bin/bash
# Checks that every asset of a release was built by this repository's Release
# workflow from the commit its manifest names
# (docs/security/release-evidence.md).
#
# usage: tools/release/provenance.sh <release-dir> [--dmg] [--bundle-dir D --trusted-root F]
# --dmg verifies only the three delivery assets at their packaging commit;
# gate.sh independently pins that commit and verifies original release evidence.
#
# Each asset (every archive, its SBOM, manifest.json, manifest.sig.json and
# SHA256SUMS) must carry a GitHub artifact attestation (SLSA provenance, signed
# through Sigstore) that `gh attestation verify` accepts for
#   repository     sainteye/clawdline
#   signer         .github/workflows/release.yml on refs/heads/main
#   source commit  the manifest's commit
#   runner         GitHub-hosted
# and whose statement names the asset's sha256 and that commit. The signature
# and certificate checks are gh's; this script adds the policy and re-reads
# the statement so a looser gh default cannot pass an asset.
#
# Online, gh fetches the attestations from GitHub. Offline, --bundle-dir holds
# the bundles `gh attestation download` saved (sha256:<digest>.jsonl) and
# --trusted-root the output of `gh attestation trusted-root`.
#
# The signer is asked for with --cert-identity, the certificate's exact SAN:
# gh turns --signer-workflow into a prefix pattern, which a workflow named
# release.yml-anything.yml also matches. The repository, workflow and ref are
# fixed here, not read from the environment, so nothing in the gate's
# environment can point it at another signer.
#
# CLAWDLINE_GH names the gh to run (the supply-chain test passes a stub).
set -euo pipefail

[ "$#" -ge 1 ] || { echo "usage: tools/release/provenance.sh <release-dir> [--bundle-dir D --trusted-root F]" >&2; exit 2; }
dir=$1; shift
bundle_dir="" trusted_root="" dmg=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dmg) dmg=1 ;;
    --bundle-dir) bundle_dir=${2:?}; shift ;;
    --trusted-root) trusted_root=${2:?}; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
  shift
done
[ -z "$bundle_dir" ] || [ -n "$trusted_root" ] || { echo "--bundle-dir needs --trusted-root" >&2; exit 2; }

gh=${CLAWDLINE_GH:-gh}
repo=sainteye/clawdline
workflow=.github/workflows/release.yml
ref=refs/heads/main
if [ "$dmg" = 1 ]; then
  # Only the two checked-in packaging workflows may attest delivery assets.
  # gate.sh independently checks this metadata against the expected commit.
  metadata=("$dir"/*.dmg.delivery.json)
  [ "${#metadata[@]}" = 1 ] && [ -f "${metadata[0]}" ] || { echo "provenance: expected one DMG delivery record" >&2; exit 1; }
  commit=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1])).packaging_commit' "${metadata[0]}")
  workflow=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1])).packaging_workflow' "${metadata[0]}")
  case "$workflow" in .github/workflows/dmg.yml|.github/workflows/release.yml) ;; *) echo "provenance: untrusted DMG workflow" >&2; exit 1 ;; esac
else
  [ -f "$dir/manifest.json" ] || { echo "provenance: $dir has no manifest.json" >&2; exit 1; }
  commit=$(node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(process.argv[1])).commit||"")' "$dir/manifest.json")
fi
identity="https://github.com/$repo/$workflow@$ref"
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || { echo "provenance: manifest.json names no commit" >&2; exit 1; }

work=$(mktemp -d)
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT

assets=()
if [ "$dmg" = 1 ]; then
  candidates=("$dir"/*.dmg "$dir"/*.dmg.spdx.json "$dir"/*.dmg.delivery.json)
else
  candidates=("$dir"/*.tar.gz "$dir"/*.spdx.json "$dir/manifest.json" "$dir/manifest.sig.json" "$dir/SHA256SUMS")
fi
for f in ${candidates[@]+"${candidates[@]}"}; do
  [ "$dmg" != 0 ] || [[ "$f" != *.dmg.spdx.json ]] || continue
  [ -e "$f" ] || { echo "provenance: $(basename "$f") is missing from $dir" >&2; exit 1; }
  assets+=("$f")
done

for f in ${assets[@]+"${assets[@]}"}; do
  name=$(basename "$f")
  digest=$(shasum -a 256 "$f" | cut -d' ' -f1)
  args=(attestation verify "$f" --repo "$repo" --cert-identity "$identity"
    --source-ref "$ref" --source-digest "$commit" --deny-self-hosted-runners --format json)
  if [ -n "$bundle_dir" ]; then
    [ -f "$bundle_dir/sha256:$digest.jsonl" ] || { echo "provenance: no bundle for $name (sha256:$digest)" >&2; exit 1; }
    args+=(--bundle "$bundle_dir/sha256:$digest.jsonl" --custom-trusted-root "$trusted_root")
  fi
  if ! "$gh" ${args[@]+"${args[@]}"} >"$work/out.json" 2>"$work/err"; then
    echo "provenance: $name has no attestation from $repo/$workflow at $commit:" >&2
    sed 's/^/  /' "$work/err" >&2
    exit 1
  fi
  node - "$work/out.json" "$digest" "$commit" "$repo" "$workflow" "$ref" "$name" "$identity" <<'JS'
const fs = require("fs");
const [file, digest, commit, repo, workflow, ref, name, identity] = process.argv.slice(2);
let results;
try { results = JSON.parse(fs.readFileSync(file)); } catch { console.error(`provenance: gh printed no JSON for ${name}`); process.exit(1); }
if (!Array.isArray(results) || results.length === 0) { console.error(`provenance: no verified attestation for ${name}`); process.exit(1); }
const why = [];
const ok = results.some((r) => {
  const st = (r.verificationResult || {}).statement || {};
  const bd = (st.predicate || {}).buildDefinition || {};
  const wf = ((bd.externalParameters || {}).workflow) || {};
  const san = (((r.verificationResult || {}).signature || {}).certificate || {}).subjectAlternativeName;
  const checks = [
    [san === identity, `signer ${san}`],
    [st.predicateType === "https://slsa.dev/provenance/v1", `predicate ${st.predicateType}`],
    [(st.subject || []).some((s) => (s.digest || {}).sha256 === digest), "subject digest"],
    [wf.repository === `https://github.com/${repo}`, `repository ${wf.repository}`],
    [wf.path === workflow, `workflow ${wf.path}`],
    [wf.ref === ref, `ref ${wf.ref}`],
    [(bd.resolvedDependencies || []).some((d) => (d.digest || {}).gitCommit === commit), "source commit"],
  ];
  const bad = checks.filter(([pass]) => !pass).map(([, label]) => label);
  if (bad.length) why.push(bad.join(", "));
  return bad.length === 0;
});
if (!ok) { console.error(`provenance: ${name}: no attestation names this asset, ${repo}/${workflow} on ${ref} and commit ${commit} (${why.join("; ")})`); process.exit(1); }
console.log(`provenance: ${name} built by ${repo}/${workflow} at ${commit.slice(0, 12)}`);
JS
done
echo "provenance: ${#assets[@]} assets verified"
finished=1
