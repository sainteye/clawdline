# Release v3.0.0 acceptance

Published on 2026-11-20 at 17:29:42 UTC from
`0d952b75bf407340f6b62eebf816af3880c02866`. The initial running Dev commit was
`5b56309bb`. The approved source was the newer `main` at `3efd2cb4e`, which included
the Bot creation flow and the mobile Session close animation. The release commit adds only
the v3.0.0 release notes to that tree. Exact-source CI passed all five jobs.
[CI 44776441589](https://github.com/sainteye/clawdline/actions/runs/44776441589)

## Approval and publication

The two Linux tmux jobs passed. In Chrome, the operator opened **Review deployments**, selected
the `release` environment, and pressed **Approve and deploy**. GitHub showed the approval and
returned an empty pending deployment list. The workflow succeeded and the draft named the
expected commit with all 16 assets.
[Release workflow 44778792439](https://github.com/sainteye/clawdline/actions/runs/44778792439)

The draft gate and the publication gate both passed. They checked the signed manifest, archive
contents, SBOMs, GitHub provenance, Apple Developer ID signatures, notarization, Gatekeeper, the
DMG delivery evidence and the privacy scans. The public latest manifest and signature matched the
accepted draft byte-for-byte. The public DMG served a successful range request.
[Public release](https://github.com/sainteye/clawdline/releases/tag/v3.0.0)

The release-mode DMG test verified and mounted the image read-only, copied the App to a disposable
Applications directory, and started its own daemon. The console returned HTTP 200 with the
matching BUILD. The test recorded `accepted: true` and `cleanup: true`.

- Manifest SHA-256 `a65aa426fac80b238f5fc75b19249ba057d6386ca6e0139fba6dbff17fc01511`.
- DMG SHA-256 `d2839280e103eddf0af5c2a5c016832344b7662aa89a59f1fa11c6afb451d523`.

## Updater and installed Mac

The updater now registers a download before publishing its first visible state. Its focused
regression and the native launchd fixture passed. A separate eight-case Darwin run used the
signed v2.19.0 and v3.0.0 archives, independent temporary launchd jobs, and the signed releases'
actual daemons and supervisor executable. The test used the previous release's updater source
for preparation, with a transport that read the signed draft assets locally. Manifest verification
and archive digest checks ran against their original bytes.

Successful updates ran with the legacy and named LaunchAgent executable paths. Bootstrap and
health failures restored the previous running daemon and console BUILD. Interruptions in
switching and rolling back exercised supervisor resumption. The v3.0.0 supervisor also completed
a forced transition to the signed v2.19.0 release and handled a failed second transition by
restoring v3.0.0. All eight cases passed and unloaded their temporary jobs.

On the maintainer's installed service, `clawdline update --apply --version v3.0.0` moved
`v3.0.0-dev.5b56309` to the public signed release and reported `healthy`. The running daemon
reported API 101 and v3.0.0, the console returned HTTP 200, and its BUILD matched the release
commit and version. All four Session IDs present immediately before the switch remained visible.
The App watcher applied the staged bundle and reopened it. The running shell and all three
installed App executables matched the signed App archive.

## Hosted console and website

The hosted console ended on `c71b39e1f`, a descendant of the release commit. The first check
returned mismatched HTML and CSS paths during overlapping deployments. A fresh read of production
verified the descendant's main bundle and production CloudGate declaration, both stamp-named
stylesheets, and all nine advertised language catalogs. Chrome reloaded the console and showed the
paired machine's Session list.

The website receipt names `8dd09e348775dd3d72451d8fdeb5e54a49baf4df` and Pages deployment
`4170e07b`. The staged guard and marketing suites and live routes passed. Both homepage languages
and both installation guide languages link to the v3.0.0 DMG. The official installer redirects to
the reviewed verifier pin and matches the release source with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.

Fresh-account macOS permission prompts and physical phone acceptance remain outside this
verification.
