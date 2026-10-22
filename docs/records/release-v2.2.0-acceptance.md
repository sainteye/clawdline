# Release v2.2.0 acceptance

Published on 2026-10-22 from `eece0f1d49b388ec37f3b37496f856ea27bf9a65`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/39778378777) passed on that commit before the release was dispatched.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/39779056102) passed after the release environment was approved by the person.
- `tools/release/gate.sh v2.2.0 --publish` verified the signed manifest, every archive size and digest, SBOMs, build provenance, Developer ID team, hardened runtime, timestamps, notarization, stapled tickets, Gatekeeper, DMG payload and privacy scans, then published.
- DMG SHA-256: `f9f492b3f7efd928a5917999b9bdad60acc429f50d6f33cf8c6c236d2edee885`.

## The gate's stapled-ticket check

The first two gate runs failed on `stapler validate` for an App copy that holds a ticket: the first
on the App inside the DMG, the second on the App inside the tar archive, which the first run had
accepted. `stapler validate -v` queries `api.apple-cloudkit.com` each time. Five manual runs on the
same tar App and two on the mounted DMG App all passed, and the v2.1.3 DMG App passed the same
check. The third run, with `--publish`, passed every check unchanged. Nothing was relaxed; a retry
around that query was not added.

## Installation evidence

The actual release DMG passed `tools/release/dmg-install-smoke.sh` in release mode (`accepted: true`,
`cleanup: true`): image integrity, read-only mounting, bundle layout and version, Apple signatures,
stapled tickets and Gatekeeper, copying into a disposable Applications directory, bundled daemon
identity, and a shell-owned daemon on its own port answering health, console HTTP 200 and a
`BUILD.json` naming v2.2.0 at the released commit. Shell revision
`3d656e598fc38842ffa12c508c627140c975b17e137ac53efcd1a100e4e0dc23`.

The signed updater changed this machine from the release-based dev build `v2.1.3-dev.72fba5f` to
v2.2.0 with `clawdline update --apply --version v2.2.0`; it reported `healthy`, `/v1/health`
reports v2.2.0 and the console answers 200. A plain `clawdline update --apply` run thirty minutes
after publication installed v2.1.3, because the updater still held its earlier check; naming the
version installed v2.2.0. The native App replacement was staged and waits for the App to quit.

The hosted console was deployed from the same commit; `BUILD.json` names it and the served main
bundle takes the Cloud branch.

The installation examples now select the public v2.2.0 manifest and pin the installer at the same
source commit. The public installer bytes match that Git object, and `install.sh` is unchanged
from the previously pinned revision.

## Explicit boundaries

A separate macOS account or VM was not used: no browser download, Finder drag installation or
first launch on a clean account. The setup guide's real GitHub sign-in, Cloud tab pairing, browser
pairing and phone scan were exercised by the person on this machine during development, not
repeated against the published build.
