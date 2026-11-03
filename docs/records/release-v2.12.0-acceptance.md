# Release v2.12.0 acceptance

Published on 2026-11-01 at 04:27:00 UTC from
`ea945e4940344f69b2e0278051759c5c6ebea495`.
The person selected the latest main, including the macOS background service rename,
and requested installation of the published release after publication.

## Source, approval and publication

- Exact-source [CI 41271302028](https://github.com/sainteye/clawdline/actions/runs/41271302028) passed before Release dispatch. The preceding notes-only candidate `b972db41a` failed [CI 41270845983](https://github.com/sainteye/clawdline/actions/runs/41270845983) in Linux's `TestASourceOffersOnlyWhatGitDoesNotCarry`: Git appended a filesystem-boundary explanation, and the adapter shortened away the non-repository diagnostic. Native Git at `/dev` reproduced the same classification failure. `8043b7a5b` preserves a typed repository fact before shortening the message; the new regression failed before the fix and the complete projectsync package and local commit gate passed afterward. Missing-directory errors retain their unreadable classification.
- [Release workflow 41271714082](https://github.com/sainteye/clawdline/actions/runs/41271714082) passed. Under the person's explicit instruction, Chrome opened Review deployments, selected the existing release environment and pressed Approve and deploy. GitHub displayed that the deployments had been approved, and the pending-deployment list became empty. Environment protection rules were unchanged.
- The downloaded draft passed the unchanged publication gate. The actual DMG passed release-mode installation acceptance before publication. `--publish` then repeated the publication gate: signed manifest, archive sizes and SHA-256, SBOMs, provenance attestations, Developer ID signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, DMG payload equivalence and privacy scans.
- Manifest SHA-256: `e25a434ae6a76d5b6c54eb4605e1d6d7ae467ca109a62396772e0c43d34a4171`.
- DMG SHA-256: `cb93164ecda1774ea0960116df863080002fb23af706795d184761ab7fc068f9`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.12.0)
is not a draft and its target is the exact source above. The stable updater subsequently
reported that same signed release and source as current.

## Actual DMG installation

The downloaded DMG passed the release-mode harness with `accepted: true` and
`cleanup: true`: image integrity, actual read-only mounting, bundle layout and
version, Apple signatures, stapled tickets and Gatekeeper, copy into disposable
Applications, bundled daemon identity, and isolated native-shell startup with
daemon health, console HTTP 200 and matching BUILD. The isolated port was 56873.
Shell revision: `c21caf1f83a8fab34a073bb99dfd61c3f9a72db21c2a1a0bb403c5ff2315becd`.

No clean OS account, Finder drag installation, fresh TCC grant, real-device push
acceptance or independent security audit was performed.

## This machine

`clawdline update --apply --version v2.12.0` replaced the running
`v2.11.0-dev.e5fbd72` and reported `healthy`. `/v1/health` reports v2.12.0 at API
level 39; authenticated `/BUILD.json` names v2.12.0 and the exact release source;
`GET /` returns 200. All 10 Session IDs observed immediately before the switch
were present afterward.

The modern native watcher completed the staged App swap without a forced quit.
The installed bundle is 2.12.0, and its three MacOS executables match the signed
App archive by SHA-256. The actual service process runs from
`service/clawdline-daemon`; the App process started after the update.

Before applying, the live owned Gemini turn had become idle. A separate already
uncertain owned record had no live original PID. Both original native identities,
history generations and the existing 142 and 133 entries were retained after the
switch. The already uncertain record was not declared recovered: no prompt was
replayed, no conversation was forced closed, and no model call was made to test it.
This is a point-in-time maintenance observation, not a freeze on new user work.
iTerm2 scanning remained disabled; this acceptance did not grant permissions or
claim fresh folder-access persistence under the renamed service.

## Hosted console

The clean exact-source `npm run build:cloud` passed. Pages deployment `3e704396`
serves the release source in production BUILD.json. Served
`assets/main-UL6BJH65.js` passes CloudGate and the complete production declaration
check. All nine hosted catalogs validate: English and Taiwan Traditional Chinese
each contain 10421 keys; the other seven languages use English fallback for 3554
newer keys. Chrome reached the real Cloud sign-in screen. No new sign-in, pairing
or authenticated browser Session interaction was performed during this check.

No Cloud API or Relay deployment was made by this operation.

## Installation references and website

README download links and the fixed-version installation example name v2.12.0 and
the signed source above. The reviewed installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: its public raw bytes, the official
redirected bytes and this release's installer match, with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.

The website's separate source `e6311e8cad3e1d9d89a615dfc8a4cd955970e330`
is deployed as Pages `e3f4c56d`. Its staged guard and marketing units passed,
followed by the deployment's public routes, locale assets, legacy redirects,
branded 404s and exact public release receipt. Chrome verified the English and
Taiwan Traditional Chinese homepage download links to the v2.12.0 DMG and the
visible official quick-install command with its copy button. The earlier probe
of a nonexistent `/_page-locales/en/` returned 404; English is served at the
canonical `/`, which was checked afterward. It was a probe-path error and did
not trigger a redeployment.
