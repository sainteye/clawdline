# Release v2.4.1 acceptance

Published on 2026-10-25 at 07:40:24 UTC from `f7c8a3fe09bd217d9ab4a140c34683f08ce4456f`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40171892119) passed before release dispatch. The preceding implementation-only run was cancelled by the documentation-inclusive source; cancellation is not counted as a pass.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40172220226) passed after the release environment was approved in Chrome. GitHub displayed “The deployments have been approved.” The person explicitly authorized public release and returning this machine from Dev to the published release.
- The draft passed `tools/release/gate.sh v2.4.1`: signed manifest, all archive sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled notarization tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked privacy scan covered 240 files.
- DMG SHA-256: `d164cbdbefb3663da1ac496709d025d63b962c5a71881cce7cb6a5b35b8b4bbd`.
- Manifest SHA-256: `ad9313cc7a06581749606232332e94fa0d76f1c184cd77c57f1eeb5ace9a7931`.

The unchanged `tools/release/gate.sh v2.4.1 --publish` repeated all checks,
passed its final privacy scan, and published the stable release. The public
Release is not a draft or prerelease and names the same source.

## Actual DMG installation

The actual draft DMG passed `tools/release/dmg-install-smoke.sh` in release mode
with `accepted: true` and `cleanup: true`. It verified image integrity, a read-only
mount, bundle layout and version, Apple trust, a copy to a disposable Applications
directory, the bundled daemon identity, and a shell-owned daemon on its own port.
Health, console HTTP 200 and BUILD matched the exact release source. The shell
revision was `3d656e598fc38842ffa12c508c627140c975b17e137ac53efcd1a100e4e0dc23`.

## Delivery notification timing

The implementation commit is `0f588380249358ca0243ce757c3b639154790b4d`.
It was installed and verified as a Dev build before this release. The running
stamp and served BUILD matched, the console answered HTTP 200, and all three
Sessions present immediately before that switch remained available. The native
App bundle also advanced to that Dev version.

A delivery receipt is recorded before the final answer. Its notification now
waits for a current post-report idle reading of that same conversation. The
regression first failed on the immediate-send path and then passed with a
ten-minute working answer, stale/unknown readings, restart recovery and concurrent
recovery. A race-enabled concurrency check also passed. This record does not
claim that a push was delivered before the reporting turn's final answer ended.

## Boundaries

The native DMG harness is the installation evidence. No clean OS account,
Finder drag installation or separate fresh-account permission-persistence
acceptance was performed. No additional independent security review or model
request was commissioned by this release procedure.

## Return from Dev to the published release

The built-in updater installed the explicit published version with
`clawdline update --apply --version v2.4.1`, returning the daemon from
`v2.4.0-dev.0f58838` to v2.4.1. Its result was `apply.state: healthy`; the running
stamp and authenticated served BUILD matched the release source, `GET /`
answered HTTP 200, and all three Sessions present immediately before the switch
remained available.

The native App watcher consumed the staged bundle. Both installed executables
were compared byte-for-byte by SHA-256 with their members in the published App
archive, whose size and SHA-256 matched the signed manifest. Both matched,
the installed bundle reported 2.4.1, and no staged App remained. No forced quit
or manual bundle replacement was used.

## Hosted console and website

During the signing run, another Session deployed the newer hosted console
`f6486c930871baea81302c1ee10ecc9a64d690a5`. Its production BUILD identifies
that commit, Git proves it contains the release source, and the served main
bundle passed CloudGate. All nine catalogs passed the normal localization gate.
The older release build was therefore not deployed over it.

The website's production receipt names Cloud repository commit
`64955b81bc316fdb99f6c77fb10bc02b9a8bc568`. The English and Taiwan Traditional
Chinese homepages were read back with the verified v2.4.1 DMG link and the quick
install command. The website's staged gate passed its guard and marketing units,
and the deployment callback completed successfully.

The official installer pin remains
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`. Its public script bytes match the Git
object, and `install.sh` is unchanged between v2.4.0 and this program release.
The quick command remains on the official short URL; the advanced fixed-version
reference and both README download links now select v2.4.1.
