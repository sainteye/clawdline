# Release v2.8.0 acceptance

Published on 2026-10-28 at 15:51:23 UTC from `73ff7732feb2672ca906015828b12387e3a65861`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40714972563) passed before dispatch.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40716170290) passed. The person authorized GitHub confirmation in Chrome. The existing release environment was approved through Review deployments; its protection rules were unchanged.
- The downloaded draft passed the unchanged `tools/release/gate.sh v2.8.0 --dir <assets>`: signed manifest, archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked privacy scan covered 370 files.
- DMG SHA-256: `31a60636612061317f22c1cd70d7e23861e24ef2250b319941c4a760df2b1b19`.
- Manifest SHA-256: `070a3726a0672b1ce2506319ecd12a56982085418af834c67649628f84d7c621`.

`tools/release/gate.sh v2.8.0 --publish` downloaded the draft again, repeated all
publication checks and published the stable release. The public release is neither
a draft nor a prerelease and names the exact source above. The latest public
manifest matches the accepted draft bytes.

## Actual DMG installation and operator update

The actual draft DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`: image integrity, an actual read-only mount,
bundle layout and version, Apple trust, a copy to disposable Applications, bundled
daemon identity, and a shell-owned daemon on an isolated port. Health, console HTTP
200 and BUILD matched the release. Shell revision:
`a9d749da273df3b5a700fee370580ebefacf6884882b9495056aa3c27790039b`.

After separate permission to interrupt the running daemon, the operator used
`clawdline update --apply --version v2.8.0`. The fresh updater reading reported
`apply.state: healthy`, running v2.8.0 and the exact release source. Health confirmed
v2.8.0 and API level 32; console GET `/` answered 200. Seven Sessions were listed
at the initial observation, but the old daemon's inventory already contained six
at 15:52:32 UTC, before update handoff at 15:52:53 UTC. All six were present after
the switch. The cause of the earlier disappearance was not established; this does
not claim preservation of all seven initial Sessions. No Session was closed by
this release work.

The native App was not forced to quit or reopen. Its installed bundle still names
2.7.0 and has the same shell revision as the new release. The updater retains the
existing App when that revision is unchanged; the service runs v2.8.0 and serves
the current console. Bundle version metadata alone does not identify the running
service. No clean OS account, Finder drag installation, fresh TCC acceptance or
independent security audit was performed.

## Doctor and hosted console

Pages deployment `18ce9a5a` serves the exact release source. Served bundle
`assets/main-BkLfHRhG.js` passes CloudGate and complete production-declaration
checks. All nine hosted catalogs pass; English and Taiwan Traditional Chinese
cover all 9,868 current keys. Each other shipped language falls back for 3,001
newer keys.

Chrome loaded the existing paired workspace and real Session list. On the selected
updated machine, Settings → Status showed current and latest v2.8.0. Doctor
completed through Cloud and displayed the release/platform identity, installed
Claude Code and Codex versions, observed quota states, history and evidence limits.
The private-submission consent remained unchecked; no support report or screenshot
was uploaded to support. This verifies the deployed read path, not upstream
account authentication or every report import/submission failure mode. The task's
browser tab was closed after verification.

## Installation references and website

Both README download links and the fixed-version installation reference identify
v2.8.0. The reviewed installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: public raw bytes match the Git object,
`install.sh` is unchanged between the pin and release source, and the official
short URL serves those same bytes.

The website receipt identifies Cloud repository commit
`f40207f6f71a2ae780be8a1b52679b1a5d1332c8`, deployed as Pages `a652b89b`.
Only release.json changed in the marketing source. The exact staged tree passed
the guard and marketing units. Deployment's public smoke passed canonical routes,
localized content, redirects, 404s and the release receipt. Separate reads verified
the v2.8.0 DMG and official quick installer in English and Taiwan Traditional
Chinese homepage content.

The synchronization callback returned nonzero after both deployments succeeded:
its final extra check read the legacy `/zh-TW/` redirect without following it.
The deployment smoke had already checked the current
`/_page-locales/zh-TW/` content; a separate read of that path passed both download
and installer assertions. Neither deployment was repeated.

`cloud-health.py --strict --no-publish --json` still reports the console's stale
HTML-title marker as missing: it expects `<title>clawdline</title>`, while the
served application uses `<title>Clawdline Cloud</title>`. Homepage, API and Relay
health contracts passed. The product's independent hosted checker and real Chrome
startup passed for the exact release source. This known probe mismatch is not a
new API or Relay deployment; their serving versions were unchanged by this release.
