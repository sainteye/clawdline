# Release v2.6.0 acceptance

Published on 2026-10-27 at 18:20:55 UTC from `b25a774b464181057e7a07e8fc0287dc67e3cb64`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40554574119) passed before release dispatch.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40555177052) passed. The person explicitly authorized approving the release in Chrome; the existing release environment was approved through its normal Review deployments dialog, and the page confirmed approval.
- The draft passed `tools/release/gate.sh v2.6.0 --dir <downloaded-assets>`: signed manifest, archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked privacy scan covered 335 files.
- DMG SHA-256: `762049d7feb7f1d42bbd789036ca779b631a2838056cc178d5c87160164ea4aa`.
- Manifest SHA-256: `54ae1022dac5b29be91ab70e45946abd573d35b159bbad58e23a04bc6d16008d`.

The unchanged `tools/release/gate.sh v2.6.0 --publish` downloaded the draft again,
repeated every publication check and published the stable release. The public
Release is neither a draft nor a prerelease and names the same source. The
public latest-manifest bytes match the accepted manifest.

## Actual DMG installation

The actual draft DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`. Checks covered image integrity, an actual
read-only mount, bundle layout and version, Apple trust, a copy to a disposable
Applications directory, the bundled daemon identity, and a shell-owned daemon
on an isolated port. Health, console HTTP 200 and BUILD matched the release.
The shell revision was `a9d749da273df3b5a700fee370580ebefacf6884882b9495056aa3c27790039b`.

No clean OS account, Finder drag installation or fresh-account permission
persistence acceptance was performed. No paid model request or independent
security audit was commissioned. The operator's running installation was not
restarted or replaced; isolated DMG acceptance does not claim that machine
is running this release.

## Hosted console and installation references

Cloudflare Pages deployment `162d7520` serves the release source. Its served
`main-BecbPseY.js` passes CloudGate and complete production-declaration checks.
All nine catalogs passed the hosted checker. English and Taiwan Traditional
Chinese cover every current key; the other seven languages each fall back to
English for 2,594 newer keys.

Chrome acceptance reused the existing paired Cloud tab. After reloading, the
Admin question inbox loaded and the workspace displayed the actual Session
list. The tab was returned to its original Admin route; no duplicate viewer
was created. The GitHub approval tab was closed after approval.

The official installer pin remains `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`.
Its public raw bytes match the Git object, and `install.sh` is unchanged between
that pin and this release source. The official short URL redirects to that
reviewed installer. Both README download links and the fixed-version install
reference now identify v2.6.0.

## Website downloads

The website's public receipt identifies Cloud repository commit
`70a3c855bf91c904161fafeea4e7a0ae6aacf6db`, deployed as Pages `b40a594c`.
The exact staged tree passed the guard and marketing units. Deployment's
public smoke passed the canonical routes, localized page sets, legacy
redirects, 404s and release receipt. Separate curl reads verified the v2.6.0
DMG link and unchanged quick installation command in both English and Taiwan
Traditional Chinese homepage content.

The callback's extra probe initially asserted against `/zh-TW/` without
following its intentional legacy redirect. Deployment had already completed;
the deployment receipt and smoke passed. Reading the documented localized
homepage at `/_page-locales/zh-TW/` verified the Traditional Chinese content.
No second deployment was performed for that probe error.
