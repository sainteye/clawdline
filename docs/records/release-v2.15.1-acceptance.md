# Release v2.15.1 acceptance

Published on 2026-11-08 at 11:25:01 UTC from
`4b106f223f1a632c7817d8140085b0a9a079d60f`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev source was `c66417948` (v2.15.0 plus the Gemini CLI 1.5.0 help fix
`f453ca744` and documentation). The release adds only its release notes.
Exact-source [CI 42461727171](https://github.com/sainteye/clawdline/actions/runs/42461727171)
passed. [Release 42470294225](https://github.com/sainteye/clawdline/actions/runs/42470294225)
failed three times before approval in the Linux tmux job: GitHub release-asset downloads
of the pinned tmux sources answered 504, 503 and dropped connections, reproduced from this
machine with the same URL. No release job ran and nothing was drafted. After the three
GitHub-hosted source tarballs downloaded completely in ten consecutive rounds, the failed
jobs were re-run unchanged and passed. Chrome used the existing Review deployments dialog,
selected the release environment and pressed Approve and deploy (the first press did not
register; the second did). GitHub recorded the owner's approval and the pending list became
empty. No environment protection settings were changed.

The draft gate stopped at an Apple App ticket query. The same command then ran the
`--publish` gate, which repeated provenance, manifest and asset signatures, SBOM, Apple
signing, timestamps, tickets, Gatekeeper, DMG and privacy checks, all passed, and
published. The documented order is to validate the ticket before re-running; three verbose
`stapler validate` runs on the published App succeeded afterwards. The actual DMG passed
release-mode installation acceptance with `accepted: true` and `cleanup: true`.

- Manifest SHA-256: `4cdbf57318b6de6a185d3c0b41e524f47b6d957e3aa53107a0e96ce0c55931b8`.
- DMG SHA-256: `e13fb476ca240194a999b36073fab2761f40cea2f3665fe60c0d94e50e72bdcb`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.15.1)
is neither a draft nor a prerelease and targets the exact source above.
The latest stable manifest names that version and source.

## Update and recovery transitions

Updater, release reader, installer, service registration and daemon executable
identity source did not change since v2.15.0. The transition matrix was not required
and was not run.

## This machine

`clawdline update --apply --version v2.15.1` replaced `v2.15.0-dev.c664179` and
reported healthy. `/v1/health` reports v2.15.1 and the console answers HTTP 200.
The new App is staged and replaces the running App when it is quit and reopened.

## Hosted console

Not deployed from the released source by this operation: the production deploy awaited
the person's approval. Production served `33cc3dd87`.

## Installation references and website

README download links and the fixed-version installation example name v2.15.1 and
its released source. `install.sh` is unchanged since v2.15.0; the installer pin stays
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`. The website's download link lives in the
private Cloud service repository and was not changed by this operation.
