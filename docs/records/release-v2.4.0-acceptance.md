# Release v2.4.0 acceptance

Published on 2026-10-24 at 05:58:18 UTC from `643bef81822b472217284a127400253b39b81b87`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/40065267663) passed before release dispatch. The preceding entry-only CI was cancelled when this documentation-inclusive source started; the cancelled run is not counted as a pass.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/40065644028) passed after its release environment was approved in Chrome. GitHub displayed “The deployments have been approved.”
- The actual draft passed the unchanged publication gate. After the person explicitly requested publication and a return from Dev to the online release, `tools/release/gate.sh v2.4.0 --publish` repeated the checks and published it. Signed manifest, sizes and digests, SBOMs, build provenance, Apple signatures, hardened runtime, timestamps, stapled notarization tickets, Gatekeeper, DMG payload and privacy scans passed.
- DMG SHA-256: `b7c982be88afbdd095cf8d9274c681759a3e5cfaf114a14057fecc00a4c42b66`.
- Manifest SHA-256: `51a2a195e412498ce2e60656138704076a47fe88a1d18e5e36c8878f80ca7d4c`.

## Actual DMG installation and installed release

The actual DMG passed `tools/release/dmg-install-smoke.sh` in release mode with
`accepted: true` and `cleanup: true`. It verified a read-only mount, bundle layout and
version, Apple trust, a copy to a disposable Applications directory, the bundled daemon
identity, and a shell-owned daemon on its own port. Health, console HTTP 200 and
`BUILD.json` matched v2.4.0 and the exact source. Its shell revision was
`3d656e598fc38842ffa12c508c627140c975b17e137ac53efcd1a100e4e0dc23`.

The built-in updater then installed the published release with the explicit version,
`clawdline update --apply --version v2.4.0`. It changed the running daemon from
`v2.3.0-dev.15259ab` to v2.4.0 and reported `apply.state: healthy`. The running source
stamp and installed console BUILD matched the release, the console answered HTTP 200,
and all five Sessions present immediately before the update remained available.

The native App was initially staged while the Dev shell remained open. Its installed
bundle was subsequently read back as 2.4.0 through the normal update mechanism; no
forced quit or manual bundle replacement was used.

## Boundaries

The integration remains machine-enabled and uses the official Antigravity CLI's personal
account connection with AI Credits off. The console entry is no longer restricted to an
administrator. No model request or paid API call was made by this release procedure.

The native DMG harness is the installation evidence. No clean OS account, Finder drag
installation or separate fresh-account permission-persistence acceptance was performed.
No additional independent security review was commissioned or claimed for this release.

## Hosted console and website

The hosted console was separately built from the released commit. Production
`BUILD.json` names that source, the served main bundle passes CloudGate, and all
nine shipped catalogs passed the normal localization gate.

The website downloads the verified v2.4.0 DMG and publishes the reviewed
instruction-transparency snapshot. Its deployed Cloud repository commit is
`0244b2bbcd2b094bd1dd2b1cd89f130ca7b70e4f`; the public release receipt and download link were read back.
Historical independent AI review pages retain their original reviewed versions.

The official installer redirect still selects the reviewed verifier commit
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`. Its public bytes match the Git object
and are unchanged in the program release. The quick install remains one command;
the fixed-version guide and both README download links now select v2.4.0.
