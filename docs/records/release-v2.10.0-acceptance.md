# Release v2.10.0 acceptance

Published on 2026-10-30 at 10:42:16 UTC from
`7876864176a063c89f84975d391da0ce28528f25`.
The program tree is the requested running Dev `2578ab2a736514bb4e2595bccecf11fb72ba9293`
plus the release notes. The minor version follows the new workflow, graph and
output-navigation capabilities.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/41039320674) passed all five jobs before dispatch. Its named host exclusions and quarantine remain in effect.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/41039824445) passed. Under the person's explicit authorization, Chrome used Review deployments, selected the existing release environment, and pressed Approve and deploy. GitHub displayed that the deployments had been approved. Environment protection rules were unchanged.
- The downloaded draft passed the unchanged publication gate: signed manifest, archive sizes and digests, SBOMs, provenance attestations, Developer ID signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked scan covered 390 files.
- DMG SHA-256: `f2e9a26cb8befe6695656e60e5f0b194c37bd586af30d5ad958d55fbbdbcc0c0`.
- Manifest SHA-256: `f5a179f164d1bb7066b90d932a2714cfa5164d8eb1fa5fac7771b81d51f2f940`.

`tools/release/gate.sh v2.10.0 --publish` downloaded the draft again and repeated
every publication check. The public release is neither draft nor prerelease;
the latest public manifest matches the accepted draft bytes and names this source.

The first acceptance callback stopped on a GitHub HTTP 502 while reading workflow
metadata after the successful build. It had not downloaded or published the draft.
The operator independently confirmed workflow success and the exact draft source,
then resumed acceptance of that draft without rebuilding. Transient metadata reads
received bounded retries; publication gates were not weakened.

## Actual DMG installation

The actual signed DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`: integrity, actual read-only mounting,
bundle layout and version, Apple trust, copy to disposable Applications, bundled
daemon identity, and isolated shell-owned daemon startup. Health, console HTTP
200 and BUILD matched the release. The isolated port was 57956. Shell revision:
`4556adeda77cd2526b011a5c9c39b3687910d07e987fe1389ed5cec54ab15055`.

No clean OS account, Finder drag installation, fresh TCC acceptance or independent
security audit was performed. The isolated harness did not replace the operator's
running installation.

## Hosted console

Production serves `633683e3de1b2965c4f32e6e52edef93d72eeb09`, a descendant of
the release that includes a separately authorized video-card and quota-layout fix.
It was preserved instead of deploying the older release console over it.
The served `assets/main-L9YP27N8.js` passes CloudGate and the complete production
declaration check. All nine catalogs pass the ordinary hosted release gate.
English and Taiwan Traditional Chinese cover all 10,144 current keys; the other
seven shipped languages fall back for 3,277 newer keys.

The fix owner separately reported real Chrome video playback and closure on
desktop and at a 390 CSS-pixel mobile viewport, with no horizontal page overflow
and no overlapping provider quotas. That owner's observation is distinct from
this release operator's independent stamp, ancestry, bundle and catalog checks.
No Cloud API or Relay deployment was made by this release operation.

## Installation references and operator choice

README download links and the fixed-version installation reference identify
v2.10.0 and the signed source above. Installer pin
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5` remains unchanged: its `install.sh`
matches the release source, public raw bytes and the official redirected bytes.
SHA-256: `65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.

After two Sessions identified conflicting installation requests, the person
explicitly chose to publish v2.10.0 and keep the operator machine on a newer Dev
containing the video and quota fixes. The release operator therefore did not
install v2.10.0 first. The fix owner owns the single necessary Dev switch and its
maintenance and preservation evidence, after isolated DMG acceptance completed.
The public signed release and operator Dev remain separate destinations.

## Website

Website commit `d98e5f1a3efc95606f7b643f0198d1d7ec058fda` is on its origin/main
and deployed as Pages `7049a7e3`. The exact staged tree passed the guard and
marketing units, including documentation and share-image checks. The release
data names v2.10.0. Transparency snapshot refresh changed only the product source
stamp and the two source hashes affected by Gemini assignment support; English
and Taiwan Traditional Chinese transparency copy and the model were identical.

Deployment smoke passed canonical routes, localized content, redirects, 404s
and the public release receipt. Separate HTTP reads and Chrome verified the
v2.10.0 DMG and unchanged official installer command on both language homepages.
The task browser tab was closed after verification.

The observational `cloud-health.py --strict --no-publish --json` reports the
existing console-title marker mismatch: it expects `<title>clawdline</title>`,
while the application uses `<title>Clawdline Cloud</title>`. Homepage, API and
Relay health contracts passed; the independent hosted bundle, configuration and
catalog checks passed. This probe limitation was not treated as deployment failure
or permission to redeploy an older console.
