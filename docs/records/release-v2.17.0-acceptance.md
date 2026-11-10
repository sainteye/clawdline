# Release v2.17.0 acceptance

Published on 2026-11-10 at 06:32:26 UTC from
`da54725171384f395a5fb2149000d306d5bbbdef`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev build was `v2.16.0-dev.55f4c26`; `origin/main` had moved on to `5f5f8ec79`
with this Session's changes, and the person asked for the current dev work to ship, so the
release is `5f5f8ec79` plus its release notes (`da5472517`). Exact-source
[CI 42765912692](https://github.com/sainteye/clawdline/actions/runs/42765912692) passed all
jobs. In [Release 42766902528](https://github.com/sainteye/clawdline/actions/runs/42766902528)
both Linux tmux jobs passed on the first attempt. Chrome opened the run's Review deployments
dialog, ticked the `release` environment and pressed Approve and deploy once; the pending
deployment list then read empty and the build, sign and draft job started. No environment
protection settings were changed. The job succeeded and the draft targeted the exact source.

`tools/release/gate.sh v2.17.0` passed on the draft, including provenance, manifest and asset
signatures, SBOMs, Apple signing, timestamps, stapled tickets, Gatekeeper, the DMG delivery set
and the privacy scan (598 files clean). `--publish` repeated every check, passed and published.
The actual DMG passed release-mode installation acceptance with `accepted: true` and
`cleanup: true` (Apple signatures and Gatekeeper, disposable Applications copy, bundled daemon
identity, shell-owned daemon health, console HTTP 200 and matching `BUILD.json`).

- Manifest SHA-256: `3d9ab52d27fcd9964b8e94200d48a9b77b92228905f02edf3fdb7df0c31455e2`.
- DMG SHA-256: `c1d9e360b28cf0ccbf60ce3de7434eed7fae4a5d9b11006eb945c85565e0f866`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.17.0)
is neither a draft nor a prerelease and targets the exact source above.
`releases/latest/download/manifest.json` names v2.17.0 and that source.

## Update and recovery transitions

The updater, supervisor, service registration, installer, release reader and signed-release
identity are unchanged since v2.16.0, so no transition matrix applies. The store gains one
nullable column, `session_direct_todos.taken_at`, added in place on first start; this
machine's daemon started on it and served its console.

## This machine

`clawdline update --apply --version v2.17.0` replaced `v2.16.0-dev.55f4c26` and reported
`healthy`. `/v1/health` reports v2.17.0, `GET /` answers 200 and `clawdline version` prints
v2.17.0. The new App is staged and replaces the running App when it is quit and reopened.
`/v1/health` answers `ok: false` with `capacity_exhausted`, as the dev build did immediately
before the switch; it is the data condition recorded for v2.16.0, not a release difference.

## Hosted console

Not deployed by this operation. Production `BUILD.json` names `09aefbf55`; the released
source changes the console (folded tool output, the to-do take setting and label), so the
hosted console needs its own deployment.

## Installation references and website

README download links and the fixed-version installation example name v2.17.0 and its released
source. `install.sh` is unchanged since the pin `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the
pinned bytes, the official short URL's bytes and this release's `install.sh` match with
SHA-256 `65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`. The website's
download link lives in the private Cloud service repository and was not changed by this
operation.
