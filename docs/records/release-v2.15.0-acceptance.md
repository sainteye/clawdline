# Release v2.15.0 acceptance

Published on 2026-11-08 at 07:57:36 UTC from
`74f84f357cfa3bcd651bbb180f9351c69718e756`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev source was `a709efeaea62a7d76adbeb53cf7131ee72d81a50`.
The release adds only its release notes; application code is unchanged from that dev.
Exact-source [CI 42458204143](https://github.com/sainteye/clawdline/actions/runs/42458204143)
and [Release 42458709088](https://github.com/sainteye/clawdline/actions/runs/42458709088)
passed. Chrome used the existing Review deployments dialog, selected the release
environment and pressed Approve and deploy. GitHub recorded the approval by the
repository owner and the pending list became empty. No environment protection settings
were changed.

The draft gate passed completely. The first `--publish` run stopped at an Apple App
ticket query ("no stapled notarization ticket"). Three explicit verbose
`stapler validate` runs on the same downloaded App succeeded, so the failure was the
query; the unchanged `--publish` gate then repeated provenance, manifest and asset
signatures, SBOM, Apple signing, timestamps, tickets, Gatekeeper, DMG and privacy
checks and published. No asset or validation policy was changed. The actual DMG passed
release-mode installation acceptance with `accepted: true` and `cleanup: true` on an
isolated port.

- Manifest SHA-256: `e2c1d2ffca1477b6a20fc2b012fc924cc0fae3bd291f1f6fa2e518ef22ad852e`.
- DMG SHA-256: `e11358d05ff896fbc4f2d701a9a1a60d5dbd98b058928bfe1b9fed46a9bfd4f6`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.15.0)
is neither a draft nor a prerelease and targets the exact source above.
The latest stable manifest names that version and source.

## Update and recovery transitions

Updater, release reader, installer, service registration and daemon executable
identity source did not change since v2.14.0 (empty diff over
`internal/adapters/release`, `internal/adapters/supervisor` and `install.sh`). The
signed-archive update/recovery transition matrix was therefore not required and was
not run. The actual release-mode DMG acceptance and production update are separate
evidence.

## This machine

`clawdline update --apply --version v2.15.0` replaced `v2.14.0-dev.a709efe` and
reported healthy. `/v1/health` reports v2.15.0 and the console answers HTTP 200.
The new App is staged and replaces the running App when it is quit and reopened.
No fresh-account TCC, permission-persistence or paid model experiment was performed.

## Hosted console

Not yet deployed from the released source at the time of this record: the deployment
from `74f84f357` was built and verified (`BUILD.json` names it) but the production
deploy awaited the person's approval. Production served `33cc3dd87`. Application code
in the hosted bundle differs from the release only by commits up to `a709efeae`.

## Installation references and website

README download links and the fixed-version installation example name v2.15.0 and
its released source. `install.sh` is unchanged since v2.14.0, so the reviewed installer
pin stays `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: pinned raw bytes, the official
short URL's final bytes and the release source's installer match SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
The website's download link lives in the private Cloud service repository and was not
changed by this operation; its owner updates it and the live page is checked separately.
