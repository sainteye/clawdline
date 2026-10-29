# Release v2.9.0 acceptance

Published on 2026-10-29 at 03:52:06 UTC from
`e3fca4a71d53e04b932bd34b729717bd228b30e3`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40812302119) passed all five jobs before dispatch. Its existing host exclusions and quarantine remain in effect.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40812885226) passed. The person authorized GitHub confirmation in Chrome; Review deployments confirmed approval of the existing release environment.
- The downloaded draft passed the unchanged publication gate: signed manifest, archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked scan covered 370 files.
- DMG SHA-256: `91be872a4c4b80f4a4898446bbdca9a9f42e31e3ada481b3b86fd15d11043534`.
- Manifest SHA-256: `25868838f19cb8eba98379745e48b356f61f14f7d2b62c3a74f5a1fd7b84a3ad`.

`tools/release/gate.sh v2.9.0 --publish` downloaded the draft again and repeated
every publication check before publishing. The public release is neither draft
nor prerelease. The latest public manifest matches the accepted draft bytes and
identifies the source above. The minor version follows the release policy:
model-capacity notifications add an independent preference and capability.

## Actual DMG installation

The actual signed DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`: integrity, an actual read-only mount,
bundle layout and version, Apple trust, copy to disposable Applications, bundled
daemon identity, and isolated shell-owned daemon startup. Health, console HTTP
200 and BUILD matched the release. The isolated port was 51818. Shell revision:
`a9d749da273df3b5a700fee370580ebefacf6884882b9495056aa3c27790039b`.

No clean OS account, Finder drag installation, fresh TCC acceptance, independent
security audit or physical-device push observation was performed. Push-provider
acceptance and local tests do not establish that a person received a notification.

## Hosted console

Pages deployment `62861c2c` serves the exact release source. Served bundle
`assets/main-JZEucmHh.js` passes CloudGate and the complete production declaration
check. All nine hosted catalogs pass the ordinary release gate. English and
Taiwan Traditional Chinese cover all 9,870 current keys; each other shipped
language falls back for 3,003 newer keys.

Chrome loaded the existing paired workspace and seven real local Sessions.
The update banner identified public v2.9.0 and the existing local development
revision. This verifies hosted startup and the existing read path, not every
image gesture or physical-device notification outcome. The task tab was closed.

## Installation references and website

Both README download links and the fixed-version installation reference identify
v2.9.0 and the signed program source above. The installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: `install.sh` is unchanged between that
pin and the release source; public raw and official redirected bytes match the
reviewed Git object. The quick command keeps the official short URL.

Website commit `6015f935438d7898a212fa278aa8a9b8519b7a14` is on its origin/main
and deployed as Pages `3654eaa4`. Only release.json changed in marketing source.
The exact staged tree passed guard and marketing units. Deployment smoke passed
canonical routes, localized content, redirects, 404s and the public release
receipt. Separate reads verified the v2.9.0 DMG and official installer in both
English and Taiwan Traditional Chinese homepage content.

The observational `cloud-health.py --strict --no-publish --json` still reports
the existing stale console-title marker: it expects `<title>clawdline</title>`,
while the application uses `<title>Clawdline Cloud</title>`. Homepage, API and
Relay health contracts passed; independent hosted checks and Chrome startup
passed. No API or Relay deployment was made for this release.

## Operator machine update

After the person separately approved the brief service interruption, the operator
used `clawdline update --apply --version v2.9.0`. The fresh updater reading
reported `apply.state: healthy`, from `v2.8.0-dev.e4a2c34` to `v2.9.0`, with the
exact release source running. Health returned v2.9.0 and API level 32; console
GET `/` answered 200, and the authenticated BUILD matched the release commit.
All eight Sessions present immediately before the update remained afterwards.
The earlier hosted observation had seven; the update comparison uses the fresh
pre-update inventory rather than that earlier count.

The installed native App bundle subsequently identified 2.9.0. No forced App
quit/reopen or manual bundle replacement was performed. This verifies the
running service and installed bundle metadata, not fresh TCC permission behavior
or a clean-account installation.
