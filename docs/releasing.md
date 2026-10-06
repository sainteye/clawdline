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
| `SHA256SUMS` | for `install.sh`, which cannot read JSON |
| `manifest.json` | version, commit, `committed_at`, channel, `min_version`, `non_additive_migration`, and each artifact's os, arch, kind, URL, size and sha256 |
| `manifest.sig.json` | a list of `{key_id, sig}` Ed25519 signatures over `manifest.json`'s exact bytes |

A daemon follows a manifest only when one signature in the list is by a key compiled into it
(`internal/adapters/release/keys.go`), and installs an artifact only when its size and sha256 are
the manifest's. The first install trusts HTTPS to github.com for the binary that then checks the
signature; every later update trusts only the signature.

Versions are `vX.Y.Z` on the `stable` channel and `vX.Y.Z-beta.N` on `beta`. The stable pointer is
`https://github.com/sainteye/clawdline/releases/latest/download/manifest.json`, which GitHub serves
from the newest published non-prerelease. `min_version` is the oldest version a release may be
upgraded from and rolled back to; a release whose store change an older binary cannot read sets
`non_additive_migration` and raises it.

## Cutting a release

1. Make sure `main` is green in CI and the commit is the one you mean.
2. Run the **Release** workflow (`.github/workflows/release.yml`) from `main` with the version.
   It waits for approval of the `release` environment, which holds the signing key, then tests,
   builds every artifact with `tools/release/build.sh`, signs with `tools/release sign`, verifies
   against the keys compiled into that commit, and creates a **draft** Release. Nothing is
   uploaded as an Actions artifact: a public repository's artifacts are public.
3. On a machine with the private word list (`.git/info/private-words`), run the gate:

   ```sh
   tools/release/gate.sh vX.Y.Z            # download, verify, unpack, privacy scan
   tools/release/gate.sh vX.Y.Z --publish  # the same, then un-draft it
   ```

   The scan reads every unpacked file and the printable strings of each binary. In a binary only
   a private word or this machine's home directory is a finding: a binary carries every literal of
   its dependencies, which the source-file exemptions were not written for. CI's fixed-rule scan
   is not publication approval (`tools/check-private-ci.sh`).
4. Install it from the public URL on a clean machine ([user/install.md](user/install.md)).

Release notes come from `docs/release-notes/<version>.md` when that file exists, otherwise from
GitHub's generated notes.

## A local test release

For testing the installer and the updater without the real key:

```sh
go run ./tools/release keygen /somewhere/outside/the/repo/test.key     # prints the public key
tools/release/build.sh v0.10.0-test.1 /tmp/rel/v0.10.0-test.1 --channel beta --no-app \
  --skip-tests --test-key <public key> --base-url http://127.0.0.1:18080/v0.10.0-test.1
go run ./tools/release sign -key /somewhere/outside/the/repo/test.key /tmp/rel/v0.10.0-test.1
(cd /tmp/rel && python3 -m http.server 18080)
```

`--test-key` compiles one extra trusted key into those binaries. It is accepted only for a
`-test.N` version, the workflow refuses such a version, and `--no-app` is required, so no
published binary trusts a key that is not in `keys.go`.

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
