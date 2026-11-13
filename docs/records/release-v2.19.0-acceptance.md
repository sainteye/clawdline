# Release v2.19.0 acceptance

Published on 2026-11-13 at 18:00:17 UTC from
`69d25d02c653e8ca2848ca1610f384279d72a6e7`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev build was `v2.18.0-dev.43378d4`. Before release the person asked whether
everything shipped had English; three Chinese-only paths found by a scan of every line added
since v2.18.0 were fixed and merged as `a86df69eb`: the voice copilot's briefing and Session
recap prompts fixed their output to Taiwan Traditional Chinese, the call's 38 on-screen tool log
lines were Chinese only, and a suggested reply's note context was Chinese only. The release is
`a86df69eb` plus its release notes (`69d25d02c`). Exact-source
[CI 43454081580](https://github.com/sainteye/clawdline/actions/runs/43454081580) passed.

In [Release 43455428030](https://github.com/sainteye/clawdline/actions/runs/43455428030) both
Linux tmux jobs passed on the first attempt. Each time, Chrome opened the run's Review
deployments dialog, ticked the `release` environment and pressed Approve and deploy once; the
pending deployment list then read empty. No environment protection settings were changed.

- Attempts 1 and 2: the build, sign and draft job was cancelled after about 16 minutes, "not
  acquired by Runner of type hosted even after multiple attempts", with GitHub's notice of
  macOS arm64 capacity constraints. No step ran.
- Attempt 3: every artifact built and signed, then `notarytool submit` failed with HTTP 403,
  "A required agreement is missing or has expired". The person accepted the pending Apple
  Developer agreement.
- Attempt 4 succeeded, and the draft targeted the exact source with 16 assets.

`tools/release/gate.sh v2.19.0` passed on the draft, including provenance, manifest and asset
signatures, SBOMs, Apple signing (team `83D62P566Q`), hardened runtime, timestamps, stapled
tickets, Gatekeeper, the DMG delivery set and the privacy scan (156 and 733 files clean).
`--publish` repeated every check, passed and published.

The actual DMG passed release-mode installation acceptance with `accepted: true` and
`cleanup: true`: image integrity, read-only mount, bundle layout and version, Apple signatures,
stapled tickets and Gatekeeper, copy to a disposable Applications directory, bundled daemon
identity, and a shell-owned daemon's health, console HTTP 200 and matching `BUILD.json`. The
first two invocations exited at once on their inputs (a work directory not yet created, then an
evidence directory the first left behind) and accepted nothing; the third, with a new evidence
directory, is the acceptance.

- Manifest SHA-256: `24e977ea1c02d3c219e7529a3a7716a5ed3af1e89d1182c118485c41ebc24875`.
- DMG SHA-256: `2e9e4a9b7ce7a5b6b029210627d412b9137f32e00671f3bce5583896fb876a9c`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.19.0) is neither a
draft nor a prerelease, is marked Latest and targets the exact source above.
`releases/latest/download/manifest.json` names v2.19.0 and that source.

## Update and recovery transitions

The updater, supervisor, service registration, installer, release reader and signed-release
identity are unchanged since v2.18.0 (checked by path and by `cmd/clawdline/main.go`'s diff), so
no transition matrix applies. The store gains one additive table for the voice copilot's
workspace. The dedicated Chromium now starts with three switches that keep it painting while the
display sleeps, and its browser extension has a new payload identity.

## This machine

`clawdline update --apply --version v2.19.0` replaced `v2.18.0-dev.43378d4` and reported
`healthy`. `/v1/health` reports v2.19.0 with `ok: true` and `GET /` answers 200. The new App is
staged and replaces the running App when it is quit and reopened.

## Hosted console

Not deployed by this operation. Production `BUILD.json` names `a6abf0b0b`, a later `main` that
another Session deployed and that already contains the released source, so deploying the release
commit would have moved production back.

## Installation references and website

README download links in English and Taiwan Traditional Chinese and the fixed-version
installation example name v2.19.0 and its released source. `install.sh` is unchanged since the
pin `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the official short URL redirects to that pin, and
its bytes and this release's `install.sh` match with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.

The website's download link, in the private Cloud service repository, still named v2.17.0. At
the person's request it was moved to v2.19.0 and deployed by the Cloud runbook from a clean
worktree at its exact commit: `https://clawdline.com/.clawdline-release.json` names that commit,
the English and Traditional Chinese homepages link only the v2.19.0 DMG, which answers 200,
`/install.sh` still redirects to the pin above, and `cloud-health.py --strict` reported 4/4
online. The build first refused a stale transparency page; its reviewed snapshot was re-imported
from this repository's `9067df395` after both languages of the one new card were read.

## Not checked

A clean-machine installation from the public URL, the new App's first launch after quit and
reopen, and physical phone acceptance of the release's browser field check.
