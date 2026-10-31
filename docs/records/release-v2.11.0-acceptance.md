# Release v2.11.0 acceptance

Published on 2026-10-31 at 13:47:55 UTC from
`47f1101ff172e6d787105afe4cd2c9be313aabdc`.
The program tree is the requested running Dev `68d3f3ed428e76afea8fe79281f4ebbf971fb75b`
plus the release notes, a documentation fix and a GitHub device-flow deadline fix with
its test. The minor version follows the tool library and Gemini Terminal capabilities.

- Exact-source CI [41204581446](https://github.com/sainteye/clawdline/actions/runs/41204581446) passed before dispatch. The release-notes commit `35c22fdfb` had failed [41203391451](https://github.com/sainteye/clawdline/actions/runs/41203391451) in five `githubissues` tests on Linux and Darwin: the device flow put an expiry computed on the injected test clock (pinned to 2026-10-31 12:00 UTC) onto a runtime-clock deadline, so every flow ended at once after 12:15 UTC. `76d84444c` gives the context the remaining time; `47f1101ff` unpins the fixture and adds a skewed-clock test that fails against the previous code.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/41205182245) passed. Under the person's explicit request, Chrome opened Review deployments, selected the existing release environment and pressed Approve and deploy; the pending deployment list then read empty. Environment protection rules were unchanged.
- The downloaded draft passed the unchanged publication gate, then `--publish` repeated it: signed manifest, archive sizes and digests, SBOMs, provenance attestations, Developer ID signatures, hardened runtime, timestamps, stapled tickets, Gatekeeper, unchanged DMG payload and privacy scans. The final unpacked scan covered 530 files.
- DMG SHA-256: `aed60ffa6241bee63c6877ac782ddcf7d3239cac0cee3dc58b5641b1b3410494`.
- Manifest SHA-256: `563442347f04604da75433708728980882fae49c90fdb85b78ae5173ebccc336`.

The latest public manifest names v2.11.0 and this source.

The release notes as first published omitted `081f78264` (image downloads stay in the
preview). The notes file and the GitHub release body were corrected after publication;
no asset changed.

## Actual DMG installation

The downloaded signed DMG passed the release-mode installation harness with
`accepted: true` and `cleanup: true`: bundle layout and version, Apple signatures,
stapled tickets and Gatekeeper, copy to disposable Applications, bundled daemon
identity, and an isolated shell-owned daemon with health, console HTTP 200 and
matching BUILD. The isolated port was 57244. Shell revision:
`bef4474ceecae1f43ca51c2deb5d07a2a85730b407fa6b0b2cdd50095cdb5f11`.

No clean OS account, Finder drag installation, fresh TCC acceptance or independent
security audit was performed.

## This machine

`clawdline update --apply --version v2.11.0` replaced the running Dev
`v2.9.0-dev.68d3f3e`; `/v1/health` reports `v2.11.0` and `GET /` answers 200. The new
App is staged until the person quits and reopens the existing one.

## Hosted console

Production served `68d3f3ed4` when the release was published, the Dev this release
contains. A console built from `47f1101ff` passed `npm run build:cloud`; its
production deployment was refused by the operator's permission policy and is left
to the person. The console source differs from `68d3f3ed4` only in the release
stamp, so the served behaviour is that release's.

## Installation references

README download links and the fixed-version installation reference identify v2.11.0
and the signed source above. Installer pin `773e04fc70bdb88d864dc2d4e125946e9f0e29a5`
remains unchanged: `install.sh` did not change since v2.10.0, and the release source,
public raw bytes at the pin and the official redirected bytes share SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.

## Website

clawdline.com release data and download links live in the private Cloud service
repository and were not changed by this operation.
