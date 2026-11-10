# Release v2.16.0 acceptance

Published on 2026-11-10 at 04:13:44 UTC from
`0cf345f6abebf72aa24f2d13558488fca2a4f8c2`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev source was `513bfeeff` (`v2.15.1-dev.513bfee`), equal to `origin/main`
when the request arrived. The release adds only its release notes.
Exact-source [CI 42751154433](https://github.com/sainteye/clawdline/actions/runs/42751154433)
passed. In [Release 42751798241](https://github.com/sainteye/clawdline/actions/runs/42751798241)
both Linux tmux jobs passed on the first attempt. Chrome opened the run's Review deployments
dialog, selected the `release` environment and pressed Approve and deploy once; GitHub showed
"The deployments have been approved". No environment protection settings were changed. The
build, sign and draft job succeeded and the draft targeted the exact source.

`tools/release/gate.sh v2.16.0` passed on the draft, including provenance, manifest and asset
signatures, SBOMs, Apple signing, timestamps, stapled tickets, Gatekeeper, the DMG delivery set
and the privacy scan (598 files clean). `--publish` repeated every check, passed and published.
The actual DMG passed release-mode installation acceptance on isolated port 50587 with
`accepted: true` and `cleanup: true`.

- Manifest SHA-256: `3ba3a48f0350ecf465f35dae7f06658c34bcf1bdc08ec62faf428641fe36e6df`.
- DMG SHA-256: `70e7b5e2860a6aaf00d07834e0069a254c2012542f2b1a9bc4945d7edc004434`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.16.0)
is neither a draft nor a prerelease and targets the exact source above.
`releases/latest/download/manifest.json` names v2.16.0 and that source.

## Update and recovery transitions

The updater changed since v2.15.1: `Env.Fetch` now downloads `manifest.sig.json` concurrently
with `manifest.json` (bounded by the registered `ReleaseMetadataConcurrency`, two reads), with
manifest-error precedence preserved. Supervisor, service registration, installer, release
reader and signed-release identity did not change, and no store migration was added.

- **Native launchd drill** on the candidate checkout
  (`CLAWDLINE_TEST_LAUNCHD_RECOVERY=1 ... -run '^TestLaunchdUpdateRecovery$'`) passed both the
  legacy-CLI and named-daemon previous cases: injected first-bootstrap failure, then deliberate
  unload and rollback, each verified by console HTTP and BUILD. This is fixture acceptance.
- **Real published manifest through the new fetch.** This machine's updater was the dev build
  `513bfee`, which already contains the changed fetch; it verified and applied the published
  v2.16.0 manifest (below).

Not performed on signed binaries: an isolated update from the signed v2.15.1 archive, candidate
health and bootstrap failure, supervisor interruption and resume, and a second update *from*
v2.16.0 (no later release exists). The v2.15.1 updater's fetch is unchanged from earlier
releases; the changed fetch first governs updates from v2.16.0. Those cases remain covered by
the portable failure-injection suite and the fixture drill.

## This machine

`clawdline update --apply --version v2.16.0` replaced `v2.15.1-dev.513bfee` and reported
`healthy`. `/v1/health` reports v2.16.0 at API level 42, `GET /` answers 200, and installed
`BUILD.json` names `0cf345f6a`. All 4 Session IDs listed immediately before the switch were
present afterward. The new App is staged and replaces the running App when it is quit and
reopened.

After the switch `/v1/health` answered `ok: false` with `capacity_exhausted`: the
`work.documents_per_item` evidence row is full (one writable Board item holds 32 of 32
documents). It is a person-evicted data condition, not a code difference — the release tree
equals the dev source plus release notes. Nothing was removed.

## Hosted console

Not deployed by this operation. Production `BUILD.json` names `09aefbf55`; the released source
changes only `web/contract/src/generated.ts` under `web/` since then.

## Installation references and website

README download links and the fixed-version installation example name v2.16.0 and its released
source. `install.sh` is unchanged since the pin `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the
pinned raw bytes, the official short URL's bytes and this release's `install.sh` match with
SHA-256 `65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`. The website's
download link lives in the private Cloud service repository and was not changed by this
operation.
