# Release evidence

What a published Clawdline release carries besides its signed manifest, how anyone can check it,
and what it does not prove. Cutting and publishing a release is [releasing.md](../releasing.md).

## What each release carries

| Evidence | Asset or record | Answers |
| --- | --- | --- |
| Signed manifest | `manifest.json`, `manifest.sig.json` | These archives, with these sizes and sha256 digests, are the ones the release key signed. The installer and the updater check it before unpacking anything. |
| SBOM | `<archive>.spdx.json`, one per archive (SPDX 2.3) | What the archive holds and what it was built from. |
| Build provenance | a GitHub artifact attestation for every asset (SLSA provenance v1, signed through Sigstore) | The asset with this digest was produced by `.github/workflows/release.yml` of `sainteye/clawdline`, run on `refs/heads/main` on a GitHub-hosted runner, from the commit the manifest names. |
| macOS notarization | the app's stapled ticket; Apple's notary record for the daemon archives, looked up online by each binary's code directory hash | Apple scanned these Developer ID-signed, hardened-runtime binaries of the release team `83D62P566Q` and accepted them. |

Releases drafted before this evidence existed, including v2.0.0, have none of the last three. They
are not re-described as having it, and the publication gate cannot pass them.

## DMG delivery evidence

An Apple Silicon DMG supplements the App tar archive; it never replaces an updater artifact or
rewrites an already published signed manifest. Its `<image>.delivery.json` binds the image name,
size and SHA256, the exact mounted filesystem inventory (file bytes, modes, directories and
symlink targets), App source commit, packaging commit/workflow, and digests of the original App
tar, signed manifest bytes and App SBOM. The manifest's signature is checked separately with
the release's trusted keys, rather than treating JSON fields as signature evidence.

`<image>.spdx.json` is SPDX 2.3: it inventories every mounted file/link and identifies the image
digest. An external document reference binds the **original** App SBOM by namespace and digest;
`GENERATED_FROM` identifies the input archive. Its component and source-inventory claims remain
the original SBOM's, checked by `sbom.sh verify` against the original archive and source commit.
The DMG does not claim to contain the tar container. Added installation instructions and the
Applications link are included in both the image inventory and its SBOM.

All three delivery assets have GitHub provenance for the **packaging** commit. The verifier accepts
only the exact SAN of `.github/workflows/dmg.yml` or `.github/workflows/release.yml` on main in
`sainteye/clawdline`, with GitHub-hosted runners, matching subject digest and source dependency.
`provenance.sh <delivery-dir> --dmg` verifies those attestations; the original assets retain their
original `release.yml` provenance at the **App source** commit. `gate.sh --packaging-commit` pins
the expected packaging commit independently of the delivery record. The supplemental workflow
uses a separate draft, never Actions artifacts or a direct public upload.

The gate mounts the image read-only, verifies the release-team Developer ID container signature
and timestamp, its stapled Apple ticket and Gatekeeper's `open` assessment, then verifies the
contained App with the ordinary hardened-runtime, release-team and notarization policy. It
compares App files, modes and links with the original archive and regenerates both sidecars from
the actual image. Any addition, deletion or substitution fails. It scans the complete mounted
payload and sidecars with the private word list, in addition to the original release scan.

These checks establish packaging integrity, not a clean-machine installation journey. That
journey and actual Apple submission must be recorded for the real staged DMG before attachment
([releasing.md](../releasing.md#an-apple-silicon-dmg-from-an-already-published-app)). Local tests use
only synthetic/ad hoc fixtures; a passing fixture does not mean a DMG has been notarized.

## What an SBOM entry claims

Each package in an SBOM has `summary` set to one of two kinds, and they mean different things:

- **`artifact-content`** is read from the unpacked archive: every file with its sha256, the Go
  modules linked into each Go binary (from the build information `go version -m` reads out of the
  binary itself), and the carried tmux with the libevent, utf8proc, ncurses and musl it statically
  links (from the archive's `tmux.json`, whose versions must equal the pins in
  `tools/release/tmux/sources.sh` **at the manifest's commit**; a pin that moved later does not
  fail an older release, and the file is parsed as `NAME=value` lines, never sourced).
- **`source-inventory`** is read from the commit: the console's npm packages from
  `web/package-lock.json`, runtime entries as `DEPENDS_ON` and build-only entries as
  `BUILD_DEPENDENCY_OF`. The console is a minified bundle; nobody traced each listed package into it,
  and the SBOM does not claim that every runtime entry is present in the bundle.

The darwin tmux links the system's libncurses and libSystem dynamically; they are not in the
archive and are listed as a relationship comment only. Licenses are `NOASSERTION` unless a pin
states one. The SBOM is an inventory, not an analysis: it does not say the listed code is free of
defects.

## Checking a release yourself

You need a `gh` whose `gh attestation verify --help` lists `--source-digest` (2.97.0 was used here), Node.js, Go and a clone of this repository that has the release's
commit (`git fetch origin`). On a Mac, `codesign`, `spctl` and `xcrun` come with the system.

```sh
v=vX.Y.Z
gh release download "$v" -R sainteye/clawdline --dir "rel-$v"
go run ./tools/release verify "rel-$v"   # Ed25519 manifest signature, sizes and digests
tools/release/sbom.sh verify "rel-$v"    # each SBOM still describes its archive
tools/release/provenance.sh "rel-$v"     # each asset's attestation, repository, workflow and commit
```

`sbom.sh verify` regenerates each SBOM from the downloaded archive and the commit and requires an
exact match, so a file, module or package added to or removed from either side fails it.
`provenance.sh` runs `gh attestation verify` with `--repo`, `--cert-identity`, `--source-ref`,
`--source-digest` and `--deny-self-hosted-runners` for every asset, and then re-reads the verified
certificate identity and statement itself. `--cert-identity` is the certificate's exact SAN,
`https://github.com/sainteye/clawdline/.github/workflows/release.yml@refs/heads/main`; gh turns
`--signer-workflow` into a prefix pattern instead (`validateSignerWorkflow` in gh's
`pkg/cmd/attestation/verify/policy.go`), which a workflow named `release.yml-anything.yml` also
matches. The repository, workflow and ref are fixed in the script; no environment variable changes
them. A single asset can also be checked by hand:

```sh
gh attestation verify "rel-$v/manifest.json" --repo sainteye/clawdline \
  --cert-identity https://github.com/sainteye/clawdline/.github/workflows/release.yml@refs/heads/main \
  --source-ref refs/heads/main --source-digest "$(jq -r .commit "rel-$v/manifest.json")"
```

**Offline.** While online, save the bundles and Sigstore's trusted root, then verify later without
the network:

```sh
for f in rel-$v/*; do gh attestation download "$f" -R sainteye/clawdline; done   # writes sha256:<digest>.jsonl
gh attestation trusted-root > trusted_root.jsonl
tools/release/provenance.sh "rel-$v" --bundle-dir . --trusted-root trusted_root.jsonl
```

**On a Mac**, for the app and the darwin daemon:

```sh
tar -xzf "rel-$v/Clawdline-${v}-macos-arm64.tar.gz" -C app
codesign --verify --deep --strict "app/Clawdline Next.app"
codesign -dvvv "app/Clawdline Next.app" 2>&1 | grep -E 'Authority=Developer ID Application|flags=.*runtime|Timestamp='
codesign --verify --strict -R='anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] and certificate leaf[field.1.2.840.113635.100.6.1.13] and certificate leaf[subject.OU] = "83D62P566Q"' "app/Clawdline Next.app"
spctl --assess --type execute -vv "app/Clawdline Next.app"   # source=Notarized Developer ID
xcrun stapler validate "app/Clawdline Next.app"
tar -xzf "rel-$v/clawdline_${v}_darwin_arm64.tar.gz" -C daemon
codesign --verify --strict --check-notarization -R=notarized daemon/clawdline daemon/libexec/tmux
```

The `-R` requirement is checked by `codesign` against the certificate chain: a Developer ID
Application leaf, under Apple's Developer ID intermediate, issued to team `83D62P566Q`. Any other
team's Developer ID signature fails it, however valid.

A bare command-line binary cannot have a ticket stapled to it. `--check-notarization` asks Apple
online whether it holds a ticket for exactly that code directory hash, so the answer is Apple's,
not a status the build wrote down; without a network connection it fails. `tools/release/gate.sh`
runs all of the above (`codesign_verify_release` in `tools/release/codesign.sh`) and refuses on
any failure.

### The release team, and changing it

`CLAWDLINE_RELEASE_TEAM_ID` in `tools/release/codesign.sh` is fixed in the script. It is not read
from the environment, a manifest or the artifact under test, so neither a crafted release nor the
machine running the gate can name another team. It was established on 2026-10-21, read-only, from
the public signatures of the published v2.0.0 `clawdline_v2.0.0_darwin_arm64.tar.gz` (its sha256
matched the signed manifest; `clawdline` and `libexec/tmux` both `TeamIdentifier=83D62P566Q`,
strict verification passing), and it agrees with this project's dev builds. No private key,
keychain or secret was read. If the maintainer's signing team ever changes, the change is a commit
to that line and to this paragraph, with the new team's evidence, reviewed like any other code; it
is never a build or gate input.

## Rebuilding a release

The build pins its inputs: the Go toolchain from `go.mod`, Go modules by `go.sum`, npm packages by
`web/package-lock.json` (`npm ci --ignore-scripts`), and the tmux sources and Alpine image by
digest. Go builds use `-trimpath -buildvcs=true` and the version and commit in `-ldflags`.

To compare a rebuild with a published release, check out the manifest's commit in a clean clone,
build it the way the workflow does (`tools/release/build.sh`; an official build needs the signing
identities, a rebuild without them signs ad hoc), and compare:

```sh
tools/release/reproduce.sh compare "rel-$v" <rebuild-dir>   # paths relative to where it is run
```

It reports each archive whose bytes differ (`archive-bytes`: tar and gzip record times and owners,
which this procedure does not fix), and for each file inside: identical, `signature-only` (a
Mach-O file identical once both signatures are removed; a Developer ID signature carries a
timestamp and is never reproduced), `differs`, or present on one side only. An identical unsigned
payload is not a claim that the signed archive is bit-for-bit reproducible.

`tools/release/reproduce.sh smoke` is the limited, runnable instance: it builds the Linux amd64
daemon twice, each with an empty Go build cache, and compares the two. It does not cover the
console bundle, tmux or the app, and both builds run on the same machine and toolchain. A full
rebuild of every platform in two independent environments has not been run; until it has, no
release claims to be reproducible.

## Initial macOS signing rollout (2026-10-21)

Observed on 2026-10-21 on a development build of this Mac, read-only: the binaries were signed by a
Developer ID Application identity with a secure timestamp, without the hardened runtime
(`flags=0x0(none)`), and Gatekeeper rejected the app as `Unnotarized Developer ID` with no
stapled ticket. Official release builds now sign with `--options runtime --timestamp` and no
entitlements, and the build fails before compiling when the notary credentials are missing.
At that point the first notarized build was still needed to verify App behavior under the
hardened runtime. This is a historical development-build observation, not the status of later
published Apps or of a separately packaged DMG.

The release environment needs these secrets, beside the existing code-signing and release-key
secrets: `CLAWDLINE_NOTARY_KEY_P8_BASE64` (an App Store Connect API key with access to the notary
service), `CLAWDLINE_NOTARY_KEY_ID` and `CLAWDLINE_NOTARY_ISSUER`. The Apple Developer Program
membership behind them belongs to the maintainer.

## Initial rollout evidence gaps (2026-10-21)

- At that point no build with the hardened runtime had been notarized, so the gate had not yet
  seen a release pass the notarization check. Its refusal was observed on the published v2.0.0 daemon
  (`source=Unnotarized Developer ID`), and the online check was observed passing on an unrelated
  notarized Developer ID command-line binary; both 2026-10-21. The first official release was
  the required acceptance; DMG acceptance remains a separate check of the actual staged image.
- The notary submission record (`CLAWDLINE_NOTARY_LOG`: submission id and status per archive) is
  not yet published as a release asset or included in the attestation. It would be an audit trail
  only: the gate's notarization answer comes from Apple's own record, not from it.

## What this does not prove

- A compromised release key, signing identity, GitHub account or workflow run can still produce
  evidence that verifies. The evidence narrows *where* a substitution could happen; it does not
  rule one out.
- Build provenance names the workflow and commit; it does not say the commit is free of defects or
  that the toolchain is.
- The installer script and the local tools that check a download remain trusted, as
  [releasing.md](../releasing.md) says.
