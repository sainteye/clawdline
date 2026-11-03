# Release v2.7.0 acceptance

Published on 2026-10-28 at 13:40:21 UTC from `dba22ff4bdd83c5fa34c374f31186166a7163bf5`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40693508065) passed before dispatch.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40694481781) passed. The person authorized GitHub confirmation in Chrome. The normal Review deployments dialog approved the existing release environment; no protection rule was changed.
- The draft passed the unchanged `tools/release/gate.sh v2.7.0 --dir <downloaded-assets>`: signed manifest, archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked privacy scan covered 370 files.
- DMG SHA-256: `e1a1d2ed9d8649ac6c8382730018995feadb7f998b578fbbb3fdb10217566d9b`.
- Manifest SHA-256: `2d156c3e57be66c686534b4007f215c60bc20e5fb93db2bbcf967e5b1199254f`.

`tools/release/gate.sh v2.7.0 --publish` downloaded the draft again, repeated every
publication check and published the stable release. Its immediate post-publication
latest-manifest assertion failed after publication had succeeded. Subsequent reads
of both the named release and latest manifest matched the accepted bytes. The public
release is neither a draft nor a prerelease and names the same source. No second
publication was attempted.

## Actual DMG installation and operator update

The actual draft DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`: image integrity, an actual read-only mount,
bundle layout and version, Apple trust, a copy to disposable Applications, bundled
daemon identity, and a shell-owned daemon on an isolated port. Health, console HTTP
200 and BUILD matched the release. Shell revision:
`a9d749da273df3b5a700fee370580ebefacf6884882b9495056aa3c27790039b`.

The operator then ran the built-in updater with `--apply --version v2.7.0`.
A fresh updater read reported `apply.state: healthy`, running v2.7.0 and the exact
release source. Health confirmed v2.7.0; console GET `/` answered 200. All seven
Sessions present immediately before the update remained present afterwards. The
native App was staged while its existing shell remained open; no forced App restart
was performed. An unauthenticated follow-up BUILD read returned 401, so the updater's
fresh running stamp establishes the operator daemon identity; the isolated DMG
harness separately established matching served BUILD.

No clean OS account, Finder drag installation, fresh-account permission persistence
acceptance or independent security audit was performed. Isolated HOME does not prove
fresh TCC behavior.

## Remote Control acceptance boundaries

Real native and production Cloud checks established worker discovery, the Remote
Control marker and controls, explicit permission inspection with no pending request,
a worker-confirmed idle interrupt, and archive/worker exit. Closing the task's empty
worker required explicit person authorization because identity and terminal evidence
were unknown. The selected worker exited first; only then was its task-owned launcher
stopped. No other Session was closed.

The image-test authorization submitted one model prompt. The native transport
accepted the synthetic image, but the worker then reported quota exhaustion with
zero reported token usage and `total_cost_usd: 0`. No prompt was retried after quota.
Successful image inference, interruption during active model work and a real pending
tool allow/deny round trip remain unverified. Billing was not independently audited.

Production acceptance found and fixed two Cloud-only defects before release:
permission inspection needed its own forwarded `info.remote-permission` read intent,
and list fallback needed to retain the RC backend identity. Both regressions were
reproduced with failing tests before correction. The native protocol is undocumented;
compatibility and permission responses remain scoped to the observed worker identity.

## Hosted console and installation references

Pages deployment `b56ecdd3` serves the release source. Served bundle
`assets/main-ZkCWDw25.js` passes CloudGate and complete production-declaration checks.
All nine hosted catalogs pass; English and Taiwan Traditional Chinese cover all
9,810 current keys. Each other shipped language falls back for 2,943 newer keys.
Chrome startup loaded the paired workspace and real machine Session list after the
final deployment. Task-created browser tabs were closed after verification.

Both README download links and the fixed-version installation reference identify
v2.7.0. The reviewed installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: public raw bytes match the Git object,
`install.sh` is unchanged between the pin and release source, and the official
short URL serves those same bytes.

## Website downloads

The website receipt identifies Cloud repository commit
`3ed45c9eeb2b8a38507f80310962913b72b230b4`, deployed as Pages `37216a10`.
Only release.json differs in the marketing tree from the previously serving source.
The exact staged tree passed guard and marketing units. Deployment's public smoke
passed canonical routes, localized content, legacy redirects, 404s and release receipt.
Separate reads verified the v2.7.0 DMG link and official quick command in English and
Taiwan Traditional Chinese homepage content.

The callback's final `cloud-health.py --strict` returned nonzero after deployment had
completed: its web-console probe still requires `<title>clawdline</title>`, whereas the
actual console serves `<title>Clawdline Cloud</title>`. The homepage, API and Relay
health contracts passed; the independent product hosted checker passed for the exact
release source. This stale title probe was recorded, not used to trigger another
website deployment or to claim a new API or Relay deployment.
