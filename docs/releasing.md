# Releasing

A release is what a person who never cloned this repository installs and updates: signed
archives on a GitHub Release of `sainteye/clawdline`. This page is how one is cut, checked and
published, and how the signing key is kept. What a machine does with a release is
[updates.md](updates.md); the install itself is [user/install.md](user/install.md).

## What a release is

| Asset | Holds |
| --- | --- |
| `clawdline_<v>_<os>_<arch>.tar.gz` (linux amd64/arm64, darwin arm64/amd64) | `clawdline` and `dist/` (the local console, with `BUILD.json`) at the archive root — the layout `~/.local/share/clawdline-next/current` points at |
| `Clawdline-<v>-macos-arm64.tar.gz` | `Clawdline Next.app` (tar, so modes, symlinks and the signature survive) |
| `SHA256SUMS` | manual checksum comparisons and compatibility with older installers; not a trust root |
| `manifest.json` | version, commit, `committed_at`, channel, `min_version`, `non_additive_migration`, and each artifact's os, arch, kind, URL, size and sha256 |
| `manifest.sig.json` | a list of `{key_id, sig}` Ed25519 signatures over `manifest.json`'s exact bytes |
| `<archive>.spdx.json` | the SPDX 2.3 SBOM of each archive (`tools/release/sbom.sh`) |

Every asset also carries a GitHub artifact attestation of its build provenance, and the macOS
binaries and app are notarized. What that evidence claims, how anyone verifies it, and what it
does not prove is [security/release-evidence.md](security/release-evidence.md). The manifest
schema is unchanged: the SBOMs are bound to the signed archives by the digest each one names, and
to the workflow by their attestation, so older installers and updaters read releases as before.

A daemon follows a manifest only when one signature in the list is by a key compiled into it
(`internal/adapters/release/keys.go`), and installs an artifact only when its size and sha256 are
the manifest's. The current `install.sh` verifies the manifest with trusted local OpenSSL 3
and its pinned release public keys, parses it with Python 3, and checks the signed archive size
and hash before unpacking or executing the release. The script's pinned keys must match
`internal/adapters/release/keys.go`; the installer policy test holds that trust set and its
copied release bounds to the Go reader and capacity register. A key rotation ships both keys
in both places before an old key is removed.

First installation still trusts the reviewed installer script and local tools. A compromised
release host cannot substitute an unsigned archive, but a compromised installer or signing key
remains a trust failure. Older installer revisions executed the downloaded binary before it
checked its own signature; fixing `main/install.sh` does not change those historical scripts.

Versions are `vX.Y.Z` on the `stable` channel and `vX.Y.Z-beta.N` on `beta`. The stable pointer is
`https://github.com/sainteye/clawdline/releases/latest/download/manifest.json`, which GitHub serves
from the newest published non-prerelease. `min_version` is the oldest version a release may be
upgraded from and rolled back to; a release whose store change an older binary cannot read sets
`non_additive_migration` and raises it.

The README and quick installation use `https://clawdline.com/install.sh`, which redirects over
HTTPS to a reviewed installer commit and selects the latest signed stable release. The advanced
install guide retains the fixed-script, fixed-version path. The marketing repository pins the
redirect target in `marketing/marketing-claude/scripts/write-localized-404.mjs`; promote it only
after comparing the public script bytes with the reviewed commit and verifying the signed release.
A changing official entry point is not an immutable script pin. The installer revision may be
newer than the program release; do not present them as the same source tree.

## A release from start to finish

The sections below explain each mechanism. This is the order a root follows, with what proves
each step done. v2.2.0 was cut this way ([acceptance record](records/release-v2.2.0-acceptance.md)).

1. **Choose the commit and version.** Everything to ship is merged on `main` and passed
   `tools/check.sh`; read the gate's `check:` line in its log, not the exit code of a wrapper
   around it. A new feature is a minor version (`vX.Y.0`), fixes only a patch.
2. **Write the release notes** at `docs/release-notes/<version>.md` and push them to `main`. List
   everything since the previous tag, not only your own work:
   `git log --oneline --first-parent <previous-tag>..HEAD`, then check which commits the previous
   tag already contains with `git merge-base --is-ancestor`.
3. **Wait for CI on that exact commit**: `gh run watch <run-id> --exit-status`, and confirm the
   run's `headSha` is the commit you will release.
4. **Dispatch the Release workflow**: `gh workflow run release.yml --ref main -f version=<version>`.
   It builds the Linux tmux first, then stops for the `release` environment. Watch
   `repos/sainteye/clawdline/actions/runs/<run>/pending_deployments`; when it is not empty, the
   person must approve it in GitHub. Send them the run link with `clawdline notify`: they are
   often away from the conversation.
5. **Wait for the draft**: the run concludes `success`, and `gh release view <version>` shows a
   draft whose `targetCommitish` is the released commit.
6. **Gate, then publish** (step 3 of *Cutting a release*): `tools/release/gate.sh <version>`, then
   `--publish`, which repeats every check and publishes only if all pass. `stapler validate`
   queries Apple (`api.apple-cloudkit.com`) on every run; if it reports *no stapled notarization
   ticket*, run `xcrun stapler validate -v` on the App it names a few times. A ticket that
   validates there makes the failure a failed query: run the gate again unchanged. A ticket that
   never validates is a real failure; do not publish.
7. **Install the actual DMG**: `tools/release/dmg-install-smoke.sh --dmg <file> --version <version>
   --commit <commit> --work-dir <dir> --evidence-dir <new-dir> --mode release`. Its `result.json`
   must say `"accepted": true` and `"cleanup": true`. It runs on its own port and never touches
   the person's daemon.
8. **Deploy the hosted console** from the released commit
   ([hosted-console.md](hosted-console.md)), and verify `BUILD.json` and the `CloudGate` check.
9. **Move this machine to the release**: `clawdline update --apply --version <version>`. Name the
   version: a plain `--apply` shortly after publication can still act on the updater's earlier
   check and install the previous release. Confirm `/v1/health` reports the version and `GET /`
   answers 200. The new App is staged until the person quits and reopens it.
10. **Refresh installation references**: update the README download badges and the fixed-version
    alternative in `docs/user/install.md`. Keep the quick command on the official short URL.
    Review and promote its verifier pin in the marketing repository. Check the public installer bytes
    (`raw.githubusercontent.com/.../<commit>/install.sh`) against `git show <commit>:install.sh`,
    and whether `install.sh` changed since the previous pin. Add
    `docs/records/release-<version>-acceptance.md` with the evidence and the boundaries not
    checked, and link it under *Recent acceptance records*.
11. **Update clawdline.com**: the website's download links and anything it marks as a preview of
    this version live in the private Cloud service repository; its owner changes them, and the
    live page is checked, not the repository.

Report each destination separately at the end: GitHub, `app.clawdline.com`, this machine, the
public Release and `clawdline.com` can each be ahead of or behind the others.

## Cutting a release

1. Make sure `main` is green in CI and the commit is the one you mean.
2. Run the **Release** workflow (`.github/workflows/release.yml`) from `main` with the version.
   It waits for approval of the `release` environment, which holds the signing key, then tests,
   builds every artifact with `tools/release/build.sh` (macOS ones signed with the hardened
   runtime and notarized, every archive with its SBOM), signs with `tools/release sign`, verifies
   against the keys compiled into that commit, attests the provenance of every asset with the
   pinned `actions/attest`, checks those attestations with `tools/release/provenance.sh`, and
   creates a **draft** Release. Nothing built
   from this repository is uploaded as an Actions artifact: a public repository's artifacts are
   public. The one exception is the Linux tmux the archives carry: a macOS runner has no Docker,
   so `.github/workflows/tmux.yml` builds it on an Ubuntu runner of each architecture from the
   pinned, hash-checked tarballs in `tools/release/tmux/sources.sh`, checks it is a static binary
   that serves a session, and hands it over as an artifact kept one day. The release job builds
   the darwin tmux itself, and `build.sh --tmux-from` refuses any binary whose sha256 is not the
   one its `tmux.json` records. Each Linux tmux is attested by the tmux job, and the release job
   refuses one without that attestation for its own commit. The same workflow runs alone on every change to
   `tools/release/tmux/`, so a broken build shows in the pull request.
3. On a machine with the private word list (`.git/info/private-words`), run the gate:

   ```sh
   tools/release/gate.sh vX.Y.Z            # download, verify, evidence, unpack, privacy scan
   tools/release/gate.sh vX.Y.Z --publish  # the same, then un-draft it
   ```

   Before the privacy scan the gate refuses any asset the manifest does not account for, checks
   every SBOM against its archive, every asset's provenance attestation (repository, release
   workflow, `refs/heads/main`, the manifest's commit, a GitHub-hosted runner), and, on a Mac,
   each macOS binary's Developer ID signature, hardened runtime and timestamp and the app's
   stapled ticket and Gatekeeper verdict. Any missing or wrong piece fails the gate; there is no
   warning-only mode. A `-test.N` release read with `--dir` skips provenance and the macOS check and
   says so; a `-test.N` version is never gated from a GitHub draft.

   The scan reads every unpacked file and the printable strings of each binary. In a binary only
   a private word or this machine's home directory is a finding: a binary carries every literal of
   its dependencies, which the source-file exemptions were not written for. CI does not run this
   scan; its green result is not publication approval.
4. Install it from the public URL on a clean machine ([user/install.md](user/install.md)).

Release notes come from `docs/release-notes/<version>.md` when that file exists, otherwise from
GitHub's generated notes.

## An Apple Silicon DMG from an already published App

The DMG is a delivery container, not a new updater artifact kind. Keep the published App tar,
`manifest.json`, `manifest.sig.json`, `SHA256SUMS` and their original attestations unchanged.
The three additional assets are `Clawdline-<version>-macos-arm64.dmg`, its `.delivery.json`, and
its `.spdx.json`. The delivery record names both the **App source commit** and the **packaging
workflow commit**. The latter does not claim to be the source of the App. No Intel App is offered.
See [release-evidence.md](security/release-evidence.md#dmg-delivery-evidence) for the evidence chain.

For v2.1.2 the App source is `239adc8ad4020f31509bff017bd4e5a3ff913197` and the input is
`Clawdline-v2.1.2-macos-arm64.tar.gz`. `.github/workflows/dmg.yml` downloads the published archives
and their evidence, verifies the trusted manifest, original SBOMs and original release provenance,
and packages the existing App. It verifies the App before and after packaging and never re-signs
or rebuilds it. It signs the DMG with Developer ID, submits the DMG itself to Apple, staples and
validates it, and checks Gatekeeper before generating and attesting the final bytes.

The workflow uses the existing `release` environment and existing certificate/notary secrets;
the environment reviewer owns that approval. It has no Ed25519 release-key input and cannot
rewrite the original manifest through this procedure. Its only Release write is a **separate
draft prerelease**, `dmg-<version>-<run-id>-<run-attempt>`, targeted at the packaging commit.
It never uploads this repository's output to Actions artifacts, publishes that draft or changes
the existing public release. Attestations disclose digests and build metadata as the ordinary
Release workflow already does; the unreviewed delivery bytes stay in the draft.

Root owns integration, workflow dispatch/approval, local acceptance, attachment and rollback.
After the packaging change has landed on main, from a clean checkout of that commit:

```sh
version=v2.1.2
source_commit=239adc8ad4020f31509bff017bd4e5a3ff913197
gh workflow run dmg.yml --repo sainteye/clawdline --ref main \
  -f version="$version" -f source_commit="$source_commit"
gh run list --repo sainteye/clawdline --workflow dmg.yml --limit 5
# Select the run just dispatched; approve its release environment in GitHub.
run=RUN_ID_FROM_THE_LIST_ABOVE
gh run watch "$run" --repo sainteye/clawdline --exit-status
packaging_commit=$(gh run view "$run" --repo sainteye/clawdline --json headSha -q .headSha)
attempt=$(gh api "repos/sainteye/clawdline/actions/runs/$run" --jq .run_attempt)
stage="dmg-${version}-${run}-${attempt}"
[ "$(gh release view "$stage" --repo sainteye/clawdline --json isDraft -q .isDraft)" = true ]
```

The job is bounded by its 90-minute workflow timeout, including Apple's wait. A rejected,
unreachable or timed-out notary submission, invalid staple, signature or provenance stops the
job; there is no warning-only publication path. Investigate a failure before explicitly starting
a new run. An interrupted job can leave no draft or an incomplete draft; never publish it.
Cancel a wrong-input or unapproved run with `gh run cancel "$run" --repo sainteye/clawdline`.

On a Mac with the private word list and Go, Node and Python 3.11+ installed, fetch the source
commit if needed and download into fresh task-owned directories. Nothing below installs or starts
the App on the maintainer's running machine:

```sh
review=$(mktemp -d)
mkdir "$review/source" "$review/dmg"
gh release download "$version" --repo sainteye/clawdline --dir "$review/source" \
  --pattern '*.tar.gz' --pattern '*.tar.gz.spdx.json' \
  --pattern manifest.json --pattern manifest.sig.json --pattern SHA256SUMS
gh release download "$stage" --repo sainteye/clawdline --dir "$review/dmg"
python3 tools/release/dmg-evidence.py source "$review/source" "$version" "$source_commit"
tools/release/gate.sh "$version" --dir "$review/source" \
  --dmg-dir "$review/dmg" --packaging-commit "$packaging_commit"
```

The gate verifies **all original release evidence**, the original macOS binaries, the exact DMG
contents, source App equivalence, Developer ID team, timestamp, staple, Gatekeeper, the DMG SBOM,
the three delivery attestations and both privacy scans. A missing private word list is unknown
and fails. `fixture=true` evidence and ad hoc signatures cannot pass. For offline attestation
verification, the gate's existing `--bundle-dir` / `--trusted-root` also cover the delivery set;
Apple signature/notarization checks still require their normal access.

For an App with changed bytes, exercise the user journey in a disposable Mac account or VM:
download the staged DMG through a browser, open it normally, drag the App to Applications and
open that copy. Verify its version, console and session behavior there. Capture the
Gatekeeper/stapler output for **the downloaded image and copied App**, and never replace the
maintainer's running installation just to test packaging.

A supplemental DMG may instead establish packaging acceptance with the real-image release-mode
harness when the gate proves that its App is byte-identical to the already-published, signed and
notarized App. This requires actual read-only mounting, copying to a disposable Applications
directory, isolated native-shell startup, matching daemon/console identities, Apple trust checks
and cleanup. This narrow case changes the container, not the App, its entitlements or startup
code. Record unavailable Finder/LaunchServices or fresh-account checks explicitly; do not claim
that isolated HOME proves fresh TCC permissions. Synthetic fixtures never establish acceptance. A local fixture test is `bash tools/release/dmg.test.sh`; it uses no private
key or Apple API. A sandbox that cannot create or mount disk images must report that gap and
have root rerun the same test outside that sandbox.

Only after all checks and the installation acceptance pass, attach the three accepted files.
This mutates the existing public Release, but never its tag, tar archives, checksums or manifest:

```sh
name="Clawdline-${version}-macos-arm64.dmg"
# Stop if any of these names already exists. Do not use --clobber.
gh release view "$version" --repo sainteye/clawdline --json assets -q '.assets[].name'
gh release upload "$version" --repo sainteye/clawdline \
  "$review/dmg/$name" "$review/dmg/$name.spdx.json" "$review/dmg/$name.delivery.json"
mkdir "$review/public-dmg"
gh release download "$version" --repo sainteye/clawdline --dir "$review/public-dmg" \
  --pattern "$name" --pattern "$name.spdx.json" --pattern "$name.delivery.json"
cmp "$review/dmg/$name" "$review/public-dmg/$name"
cmp "$review/dmg/$name.spdx.json" "$review/public-dmg/$name.spdx.json"
cmp "$review/dmg/$name.delivery.json" "$review/public-dmg/$name.delivery.json"
tools/release/gate.sh "$version" --dir "$review/source" \
  --dmg-dir "$review/public-dmg" --packaging-commit "$packaging_commit"
```

The final gate is the post-upload health check. On failure stop distribution/link promotion,
inspect partial uploads, and remove **only newly added assets whose bytes were verified as this
run's output**, using `gh release delete-asset "$version" <asset-name> --repo sainteye/clawdline
--yes` for each of the three. Never delete or overwrite a pre-existing/unknown asset. The original
App tar and updater remain available throughout. Report the failure to the release owner and
retain local evidence. After success, root may delete the staging draft with `gh release delete
"$stage" --repo sainteye/clawdline --yes`; do not publish it or delete the program's version tag.
Root records the run, both commits, asset digests and acceptance outputs before cleaning its
task-owned review directory.

Future ordinary Releases create the same DMG inside `build.sh`, using `release.yml` and the same
source/packaging commit. The ordinary Release workflow attests and drafts all three extra assets;
`gate.sh` recognizes only that exact delivery set and verifies it before publication. The updater
manifest and `SHA256SUMS` continue to describe the tar archives; DMG digest verification uses its
independently attested delivery record. A historical supplementary DMG passed to a later local
gate requires its actual `--packaging-commit`, not the old App source commit.

## A local test release

For testing the installer and the updater without the real key:

```sh
go run ./tools/release keygen /somewhere/outside/the/repo/test.key     # prints the public key
tools/release/build.sh v0.10.0-test.1 /tmp/rel/v0.10.0-test.1 --channel beta --no-app \
  --skip-tests --test-key <public key> --base-url http://127.0.0.1:18080/v0.10.0-test.1
go run ./tools/release sign -key /somewhere/outside/the/repo/test.key /tmp/rel/v0.10.0-test.1
(cd /tmp/rel && python3 -m http.server 18080)
```

A test release also carries SBOMs, so `tools/release/gate.sh <v> --dir <dir>` checks them on it.
The evidence checks themselves are tested, with a synthetic release and a stub `gh`, by
`tools/heavy.sh bash tools/release/supply-chain.test.sh`: a missing SBOM, a wrong digest, an
attestation from another repository, workflow, signer or commit, an environment that names another
repository, a `-test.N` draft gated without `--dir`, and a Mach-O payload difference reported as
signature-only must each fail. `tools/check.sh` runs it whenever `tools/release/` or the release
workflows change, and in every full check.

`--test-key` compiles one extra trusted key into those binaries. It is accepted only for a
`-test.N` version, the workflow refuses such a version, and `--no-app` is required, so no
published binary trusts a key that is not in `keys.go`.

## The code-signing certificate

Official macOS releases require a Developer ID Application identity
(`tools/release/codesign.sh`). The earlier self-signed identity preserved some
code requirements but did not preserve WebKit's keychain grant: securityd
rejected the client as not Apple-signed and added a new cdhash ACL entry for
each App replacement (observed 2026-10-17). A release build now fails before
compilation if the selected identity is not Developer ID Application.

- **It is not the release key.** It proves only that two builds are the same program to macOS; the
  Ed25519 key below is what an install trusts an update by.
- **Credential transition.** Replace the release environment's old self-signed
  signing secret with an authorized Developer ID Application certificate and
  private key. Dev builds that must replace the native App need the same
  identity. Verify certificate ownership before exporting or uploading it.
- **Where it lives**: the `release` environment's `CLAWDLINE_CODESIGN_P12_BASE64` and
  `CLAWDLINE_CODESIGN_PASSWORD` secrets, which the Release workflow requires; and, on the
  maintainer's Mac, `<state dir>/signing/codesign.p12` with `codesign.password` beside it (mode
  0600 in a 0700 directory), which `clawdline dev install` signs dev builds with so a dev build
  and a release stay one program. Keep an offline copy of the .p12 and its password.
- **Notarization**: an official build signs every macOS binary with `--options runtime
  --timestamp` and no entitlements, and submits them with `notarytool`
  (`codesign_notarize` in `tools/release/codesign.sh`); the app is stapled. It needs an App
  Store Connect API key in the `release` environment's `CLAWDLINE_NOTARY_KEY_P8_BASE64`,
  `CLAWDLINE_NOTARY_KEY_ID` and `CLAWDLINE_NOTARY_ISSUER` secrets; without them the build fails
  before compiling. Dev builds are neither hardened nor notarized. The gate accepts only the
  release team `83D62P566Q`, fixed in `tools/release/codesign.sh`, and asks Apple online for
  each binary's notarization ticket; changing that team is a reviewed commit
  ([release-evidence.md](security/release-evidence.md#the-release-team-and-changing-it)).
- **Without it**, a build signs ad hoc as before: a clone builds and runs with no one's certificate.
- **Replacing it** changes the identity once: macOS can ask for the keychain
  password and Automation permission once after the transition. Validate two
  successive signed builds on one Mac before claiming prompts are gone.

## The signing key

- **Where it lives**: the base64 Ed25519 seed is the `CLAWDLINE_RELEASE_KEY` secret of the GitHub
  environment `release`, whose required reviewer is the maintainer, so no workflow run reads it
  without that approval. One offline copy is kept by the maintainer. It is never in this
  repository, a shell history or a build log.
- **Creating one**: `go run ./tools/release keygen <file outside the repo>`; put the printed
  public key in `releaseKeys` (`internal/adapters/release/keys.go`), the seed in the environment
  secret, and the file in offline storage; then delete the file.
- **Rotating**: add the new public key to `releaseKeys` and ship a release signed by **both** keys
  (`tools/release sign` adds a signature and keeps the ones already in `manifest.sig.json`). Once
  installs have that release, a later one may drop the old key.
- **If the key leaks**: rotate as above at once, signing with the new key only, and say so in the
  release notes. Installs older than the release that added the new key cannot verify the new
  releases and need the installer run again.

## Recent acceptance records

- [v2.2.0 installation and publication evidence](records/release-v2.2.0-acceptance.md).
- [v2.1.3 installation and publication evidence](records/release-v2.1.3-acceptance.md), including the explicitly accepted fresh-account boundary.
