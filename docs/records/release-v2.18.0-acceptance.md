# Release v2.18.0 acceptance

Published on 2026-11-11 at 06:50:13 UTC from
`c6d8da44a3fc6986156607edd3c671d6e1001394`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev build was `v2.17.0-dev.da5aba8`, which was also `origin/main`. The release is
`da5aba86c` plus its release notes (`31634ea0d`, merged as `c6d8da44a`). Exact-source
[CI 42971582411](https://github.com/sainteye/clawdline/actions/runs/42971582411) passed. In
[Release 42972484957](https://github.com/sainteye/clawdline/actions/runs/42972484957) both
Linux tmux jobs passed on the first attempt. Chrome opened the run's Review deployments dialog,
ticked the `release` environment and pressed Approve and deploy once; the pending deployment
list then read empty and the build, sign and draft job started. No environment protection
settings were changed. The job succeeded and the draft targeted the exact source.

`tools/release/gate.sh v2.18.0` passed on the draft, including provenance, manifest and asset
signatures, SBOMs, Apple signing, timestamps, stapled tickets, Gatekeeper, the DMG delivery set
and the privacy scan (598 files clean). `--publish` repeated every check, passed and published.
The actual DMG passed release-mode installation acceptance with `accepted: true` and
`cleanup: true` (copy to a disposable Applications directory, bundled daemon identity,
shell-owned daemon health, console HTTP 200 and matching `BUILD.json`).

- Manifest SHA-256: `fd9dc665174098550a2e5d55507cda0d52700023fa03d14eb92837c3ad16a90d`.
- DMG SHA-256: `f8947932c98ab46844cf728ef3b68467dcaf3ff5bdeb41003e94f988db18780f`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.18.0)
is neither a draft nor a prerelease and targets the exact source above.
`releases/latest/download/manifest.json` names v2.18.0 and that source.

## Update and recovery transitions

The updater, supervisor, service registration, installer, release reader and signed-release
identity are unchanged since v2.17.0, so no transition matrix applies. The daemon's stop path
now stops the Gemini and Grok hosts before cancelling the Cloud context. There is no store schema
change.

## This machine

`clawdline update --apply --version v2.18.0` replaced `v2.17.0-dev.da5aba8` and reported
`healthy`. `/v1/health` reports v2.18.0 with `ok: true`, `GET /` answers 200 and
`clawdline version` prints v2.18.0. The new App is staged and replaces the running App when it
is quit and reopened.

## Hosted console

Not deployed by this operation. Production `BUILD.json` names `8e5b2063c`; the released source
changes the console after it (Codex credits label, Codex answer refresh, phone Enter check), so
the hosted console needs its own deployment.

## Installation references and website

README download links in English and Taiwan Traditional Chinese (the latter had still named
v2.13.0) and the fixed-version installation example name v2.18.0 and its released source.
`install.sh` is unchanged since the pin `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the official
short URL redirects to that pin, and its bytes and this release's `install.sh` match with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`. The website's download link
lives in the private Cloud service repository and was not changed by this operation.
