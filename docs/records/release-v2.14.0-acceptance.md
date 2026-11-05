# Release v2.14.0 acceptance

Published on 2026-11-05 at 14:13:34 UTC from
`debcf828b63a9844613ca367e900592c5f528b47`. The person requested the running dev
build as a public release, approval through Chrome in GitHub, and a return to the
published stable release on this machine.

## Source, approval and publication

The running dev source was `ea4eb380112b40764b03c7f1f285322b7e1311e2`.
The release adds only its release notes; application code is unchanged from that dev.
Exact-source [CI 42036821606](https://github.com/sainteye/clawdline/actions/runs/42036821606)
and [Release 42037904177](https://github.com/sainteye/clawdline/actions/runs/42037904177)
passed. Chrome used the existing Review deployments dialog, selected the release
environment and pressed Approve and deploy. GitHub confirmed approval and the pending
list became empty. No environment protection settings were changed.

The initial draft gate stopped at an Apple App ticket query. An explicit verbose
`stapler validate` on the same downloaded App succeeded. The unchanged complete draft
gate then passed; no asset or validation policy was changed. The actual DMG passed
release-mode installation acceptance with `accepted: true` and `cleanup: true` on
isolated port 65399. The unchanged `--publish` gate repeated provenance, manifest and
asset signatures, SBOM, Apple signing, timestamps, tickets, Gatekeeper, DMG and privacy
checks before publication.

- Manifest SHA-256: `d3aa02414f7af8725741e149ae244ea0a7be1361166c2203a01e5c1225dbed5d`.
- DMG SHA-256: `38c6f7e880ea0479fbb2fd0f3ee15ed156804c20ce3bafaeb2d4457c930409e0`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.14.0)
is neither a draft nor a prerelease and targets the exact source above.
The latest stable manifest also names that version and source.

## Update and recovery transitions

Updater, release reader, installer, service registration and daemon executable
identity source did not change since v2.13.0. The signing-script change affects the
dev branch only; the hardened release branch retains its timestamp. The signed-archive
update/recovery transition matrix was therefore not required and was not run.
The actual release-mode DMG acceptance and production update are separate evidence.

## This machine

`clawdline update --apply --version v2.14.0` replaced
`v2.13.0-dev.ea4eb38` and reported `apply.state: healthy`. The running version and
served BUILD identify v2.14.0 and the exact released source; the console answers
HTTP 200. All three pre-switch Session IDs and native identities remain available.
The other active project owner confirmed a safe point; the immediately preceding
inventory showed the other Sessions idle and no held or queued lease. These
point-in-time readings do not prove a freeze on new user work or zero interruption.

The App watcher replaced the bundle without a forced quit. Its bundle version is
2.14.0; all three installed MacOS executables match the downloaded signed App tar.
The App process started after the update, separately from the running managed daemon.
No fresh-account TCC, permission-persistence or paid model experiment was performed.

## Hosted console

Production BUILD names the released source. Its served main bundle passes CloudGate
and the complete production declaration check. All nine catalogs load; English and
Taiwan Traditional Chinese each have 10,728 keys. Seven other languages have 6,867
keys and fall back to English for 3,861 newer keys.

Chrome opened the actual production console and displayed the connected machine and
three existing Session rows. The schedule panel displayed a read warning; schedule
operations were not accepted or changed by this release operation. Cloud API and Relay
were not deployed.

## Installation references and website

README download links and the fixed-version installation example name v2.14.0 and
its released source. The reviewed installer pin stays
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: pinned raw bytes, the official short URL's
final bytes and the release's installer match SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
Website source `ff8b11c9c9488da30258a2818cf61276e7b93683` updates only the version
and signed DMG link after its guard and marketing staged checks passed.
Production website receipt names that exact commit (Pages deployment `6bea9c6d`).
The deployment's public route, localized route, redirect, 404 and console purchase-target
checks passed. Chrome verified the English and Traditional Chinese homepages show
the v2.14.0 signed DMG link, official short installation command and copy button.
