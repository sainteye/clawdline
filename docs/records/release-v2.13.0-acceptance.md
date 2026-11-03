# Release v2.13.0 acceptance

Published on 2026-11-02 from `da84190039b5dbc5c90dae48dace0b341ee2ec9a`.
The person asked for the running dev build to be released, for Chrome to approve the
release environment in GitHub, and for this machine to move to the published release.

## Source, approval and publication

- The dev build this machine ran was `0e1f48a65`. Its CI
  ([41440551830](https://github.com/sainteye/clawdline/actions/runs/41440551830)) failed on
  macOS: `TestAskRunsAnIsolatedControlEndpoint` waited 30 s because Ask's stderr copy held
  `Wait` open while a stand-in's child kept the pipe, and `TestABeatThatExitsIsRestarted`
  removed its TempDir before the broker stopped. `f4c618417` fixes both; the released commit
  adds `docs/release-notes/v2.13.0.md` on top. Exact-source
  [CI 41450418120](https://github.com/sainteye/clawdline/actions/runs/41450418120) passed before
  Release dispatch.
- [Release workflow 41451534473](https://github.com/sainteye/clawdline/actions/runs/41451534473)
  passed. Under the person's explicit instruction, Chrome opened Review deployments, selected the
  existing release environment and pressed Approve and deploy; the pending-deployment list then
  became empty. Environment protection rules were unchanged.
- `tools/release/gate.sh v2.13.0` passed on the draft, including provenance, Apple signatures,
  stapled tickets, Gatekeeper, DMG evidence and the privacy scan (543 files).
- The actual DMG passed release-mode installation acceptance with `accepted: true` and
  `cleanup: true` on isolated port 61259. `--publish` repeated the whole gate and published.
- Manifest SHA-256: `f07e7f6e3aafac1812212b5d4455f7e535d914a129009e91182d2893ef1d32ba`.
- DMG SHA-256: `8580e581bcdee67bdb96b9588930a96f55b2fa7abaf7223425a3f99a16853566`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.13.0) is not a draft,
targets the exact source above, and `releases/latest/download/manifest.json` names it.

## Update and recovery transitions

No file of the updater, release reader, installer or service registration changed since
v2.12.1, so the transition matrix was not required and was not run on signed archives.

## This machine

`clawdline update --apply --version v2.13.0` replaced the running `v2.12.1-dev.0e1f48a` and
reported `healthy`. `/v1/health` reports v2.13.0 at API level 40; installed `BUILD.json` names
the released source; `GET /` returns 200. All 8 Session IDs recorded immediately before the
switch were present afterward. The new App is staged and replaces the installed one when it quits.

## Hosted console

Not redeployed. Production `BUILD.json` names `0e1f48a65`; the two commits after it change
nothing under `web/`.

## Installation references and website

README download links and the fixed-version example name v2.13.0 and the source above.
`install.sh` is unchanged since v2.12.1, so the reviewed pin stays
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the pinned raw bytes, the official redirect's bytes
and this release's `install.sh` match with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
clawdline.com's download links live in the private website source and were not changed here.
