# Release v3.2.0 acceptance

Published on 2026-11-22 at 16:33:56 UTC from
`a0fc429d2c3df3b50366b37ef26a03ff61550b83`. The installed Dev source was
`7b4e686bcb35295578e3843f8a07f1a7cec0a1e3`. The operator approved
`ed72b9df19635208717fb8567c3afcba57775e80`, which includes four subsequent
product commits. Only the v3.2.0 release notes differ between the approved tree
and the signed commit. Exact-source CI passed all five jobs.
[CI 45041305350](https://github.com/sainteye/clawdline/actions/runs/45041305350)

## Approval and publication

After both Linux tmux builds passed, the operator selected the `release`
environment in Chrome under **Review deployments** and pressed **Approve and
deploy**. GitHub recorded the approval and an empty pending-deployment list.
The workflow passed and created a draft with 16 assets at the expected commit.
[Release workflow 45042050296](https://github.com/sainteye/clawdline/actions/runs/45042050296)

The draft and publication gates both passed. They verified the signed manifest,
archives, SBOMs, GitHub provenance, Developer ID signatures, notarization,
Gatekeeper, DMG delivery evidence, and privacy scans. The public latest manifest
and signature matched the accepted draft byte for byte. The published tag points
at the signed commit. [Public release](https://github.com/sainteye/clawdline/releases/tag/v3.2.0)

- Manifest SHA-256 `a4ffddac82b596afa321aa8f0e1c3d9f09af66dd4d10c7b1f32181c3fd5302a9`.
- DMG SHA-256 `d64f585609ebf04d857e733d9c0322bc98ea73d34e5ed98754bb02089bb5c046`.
- App archive SHA-256 `628121bab8ba8b676bd65d726be4a8736202fd93abdf8ba112eac80184a85672`.

The release-mode test mounted the downloaded public DMG read-only, copied the
App to a disposable Applications directory, checked Apple trust, and launched
its isolated daemon and console. It verified the release identity, HTTP 200,
and matching BUILD stamp, with `accepted: true` and `cleanup: true`.

## Update to the public release

The built-in updater explicitly selected v3.2.0. Its final state was healthy,
the running daemon reported API 111 and v3.2.0, and the console returned HTTP 200
and the exact release BUILD stamp. All four existing Session IDs remained visible.
The staged App was applied and its process was running at v3.2.0. All three
installed executable hashes matched the signed public App archive.

The updater, installer, macOS shell, and release tools were unchanged from
v3.1.0. Acceptance covered the installed Dev-to-stable transition and isolated
public DMG. Fresh-account macOS permissions and physical phones were outside this
check.

## Hosted console and website

Console Pages deployment `1339f2bd` serves the exact release commit. A fresh
production fetch verified the CloudGate declaration and matched the served main
JavaScript and both stylesheets byte for byte to the build. All nine advertised
catalogs passed the production check. Chrome loaded the paired machine's Session
list.

Website Pages deployment `68eb9bb9` serves source
`c15f1d2b3b2d39fd45e61a2d59f60ea7d3fdd529`. The initial preflight named the
wrong Pages project and stopped before uploading. The corrected run passed all
83 live checks. Both homepage languages and both installation guide languages
link to the v3.2.0 DMG. The official installer redirects to the existing reviewed
pin and matches the release source with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
Homepage, console, API, and relay health probes passed.
