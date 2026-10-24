# Release v2.3.0 acceptance

Published on 2026-10-24 at 00:04:37 UTC from `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40040498358) passed before release dispatch.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40040986157) passed after the release environment was approved through Chrome, as explicitly requested by the person. GitHub displayed “The deployments have been approved.”
- The draft passed `tools/release/gate.sh v2.3.0`, then the unchanged gate with `--publish`: signed manifest, archive sizes and digests, SBOMs, build provenance, Developer ID team, hardened runtime, timestamps, notarization, stapled tickets, Gatekeeper, DMG payload and privacy scans.
- DMG SHA-256: `b362d138526d9a6836a65ce27f22e0f672438d2547d30219469e4b0948543365`.

## Actual DMG installation

`tools/release/dmg-install-smoke.sh` ran in release mode against that actual DMG and reported
`accepted: true` and `cleanup: true`. It verified image integrity, a read-only mount, bundle layout
and version, Apple signatures, stapled tickets and Gatekeeper, copying into a disposable
Applications directory, bundled daemon identity, and a shell-owned daemon on its own port.
Health, console HTTP 200 and `BUILD.json` matched v2.3.0 and the released commit. Shell revision:
`3d656e598fc38842ffa12c508c627140c975b17e137ac53efcd1a100e4e0dc23`.

The first smoke run used a disposable directory nested under the OS-account home. Its sandbox
denies that home except the exact test subtree; the daemon could not open its database and
readiness failed. Cleanup succeeded and publication stopped. A second run moved only the owned
working directory to `/private/tmp`, outside the denied home, keeping the same image digest and
sandbox policy. All installation checks passed. Both results were retained; the failed run is
not installation acceptance, and no isolation rule or readiness check was removed.

## Installed release and hosted console

At the person's explicit request, the built-in updater ran with the named version:
`clawdline update --apply --version v2.3.0`. It changed the daemon from `v2.2.0-dev.817fc22` to
v2.3.0 and reported `apply.state: healthy`. The running health response, served `BUILD.json` and
console HTTP 200 were checked. Both pre-existing terminal Session identities remained available.
The updater initially staged the native App; its installed bundle was subsequently read back as
2.3.0 without forcing the App to quit.

The hosted console was separately built from the exact released commit and deployed. Production
`BUILD.json` named that commit, the served main bundle passed `CloudGate` (two matches), and all
nine shipped catalogs were checked. English and Taiwan Traditional Chinese had 100% key coverage;
the other seven languages retained their existing fallback coverage.

The public fixed-commit installer bytes matched the Git object at the released source and were
unchanged from the previous verifier pin. The one-line quick installer continues using the
canonical short URL; download references now select v2.3.0.

## Boundaries

Gemini remains an opt-in personal-account preview; publishing it does not declare the complete
integration backlog finished. No Gemini model request or paid API was made by this release
procedure. Existing free-account and AI Credits-off restrictions remain in place.

No separate macOS account or VM, Finder drag installation, browser download or first launch on a
clean account was exercised. The isolated DMG harness is the native installation evidence.
The person cancelled the additional post-publication independent reviews while away from the
login device. The [review page](https://clawdline.com/ai-review/) retains its earlier valid
rounds; no new v2.3.0 security-review result is claimed. Release integrity and installation
acceptance do not constitute a security certificate.
