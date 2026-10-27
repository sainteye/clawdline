# Release v2.5.0 acceptance

Published on 2026-10-27 at 06:03:53 UTC from `4b195d1d1d67af72e57fd4e3fcc9caf64dbd7d23`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40456029302) passed before release dispatch.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40456659520) passed after the existing release environment was approved through GitHub's authenticated review API for the requested release. Its source matched the CI commit.
- The draft passed `tools/release/gate.sh v2.5.0`: signed manifest, archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled notarization tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked privacy scan covered 355 files.
- DMG SHA-256: `7c0c19157b3d7c546d5b655f5d32dbfe5cd37ceede4cddb4df927f80463064f7`.
- Manifest SHA-256: `293c94665b662f6ab922e21c22c145c23faa37176e08cac150d2f80720e3e9cf`.

The unchanged `tools/release/gate.sh v2.5.0 --publish` repeated every check,
passed its final privacy scan and published the stable release. The public
Release is neither a draft nor a prerelease and names the same source. The
public latest-manifest bytes match the accepted manifest.

## Actual DMG installation

The actual draft DMG passed `tools/release/dmg-install-smoke.sh` in release mode
with `accepted: true` and `cleanup: true`. It verified image integrity, a read-only
mount, bundle layout and version, Apple trust, a copy to a disposable Applications
directory, the bundled daemon identity, and a shell-owned daemon on its own port.
Health, console HTTP 200 and BUILD matched the exact release source. The shell
revision was `3d656e598fc38842ffa12c508c627140c975b17e137ac53efcd1a100e4e0dc23`.

No clean OS account, Finder drag installation or separate fresh-account permission
persistence acceptance was performed. No independent security audit or paid model
request was commissioned by this release procedure.

## Installed release

The built-in updater installed the explicit public version with
`clawdline update --apply --version v2.5.0`, returning the daemon from
`v2.4.1-dev.2287a64` to v2.5.0. Its result was `apply.state: healthy`. The running
stamp and authenticated served BUILD matched the release source; GET `/` answered
200, and all seven Sessions observed immediately before the switch remained
available afterward.

The verification callback initially failed when it used the orchestrator
credential on the static BUILD route, which correctly refused that credential's
scope. Publication and installation had already succeeded. Read-only verification
with the documented local script credential then passed; neither publication nor
installation was repeated.

The native App watcher consumed the staged bundle without a forced quit. The
installed bundle reported 2.5.0, no staged App remained, and both installed
executables matched their members in the signed App archive byte-for-byte by
SHA-256.

## Hosted console and installation references

Production served the newer hosted console `69a5e917529ee644e33a9feabeede1d3b5fa5dc0`.
Git proves it contains the release source, and its served main bundle passed
CloudGate. All nine catalogs passed the hosted localization check. The earlier
release console was therefore not deployed over the newer one.

The official installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`. The public installer bytes match that
Git object, and `install.sh` is unchanged between that pin and this program
release. The quick command remains on the official short URL; both README
macOS download links and the advanced fixed-version reference select v2.5.0.

## Website downloads

The website's production receipt identifies Cloud repository commit
`def51ebde25b0363db8a90ed082a5e40a409faf1`. Its exact staged tree passed the
guard and marketing units. Deployment completed and its built-in public smoke
checked the canonical pages, both localized page sets, legacy redirects, 404s
and release receipt. English and Taiwan Traditional Chinese homepages were read
back with the v2.5.0 DMG link and the unchanged quick install command.

A supplementary Python urllib probe returned HTTP 403 after that successful
deployment. Independent curl reads confirmed both localized pages and their
links; this did not require a second deployment. The freshness gate also
required refreshing two source citation line numbers and one source hash in
the transparency snapshot. Its English and Traditional Chinese text and its
model remained unchanged.
