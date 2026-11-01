# Release v2.12.1 acceptance

Published on 2026-11-01 from `db9007cfdc2ad611b75397197cddcde1a8b0d40e`.
The person asked for the running dev build to be released, for Chrome to approve the
release environment in GitHub, and for this machine to move to the published release.

## Source, approval and publication

- The released commit adds only `docs/release-notes/v2.12.1.md` to `47e971898`, the dev build
  this machine ran. Exact-source [CI 41288614165](https://github.com/sainteye/clawdline/actions/runs/41288614165)
  passed before Release dispatch.
- [Release workflow 41289046465](https://github.com/sainteye/clawdline/actions/runs/41289046465)
  passed. Under the person's explicit instruction, Chrome opened Review deployments, selected the
  existing release environment and pressed Approve and deploy; the pending-deployment list then
  became empty. Environment protection rules were unchanged.
- The first gate run failed with `No space left on device` (664 MiB free). With the person's
  approval the shared Go build cache was cleared (`go clean -cache`, about 11 GiB); no worktree
  or other Session's file was removed. The unchanged gate then passed on the draft.
- The actual DMG passed release-mode installation acceptance with `accepted: true` and
  `cleanup: true` on isolated port 51659. `--publish` repeated the whole gate and published.
- Manifest SHA-256: `a8fb22188b23715eac6df8f47b77afd704262a845cb622e74d02ae74934f274c`.
- DMG SHA-256: `f89b899e057f7f74d025346407a24d20263c44f2dd57d0497b278d19201f1986`.

The [public release](https://github.com/sainteye/clawdline/releases/tag/v2.12.1) is not a draft,
targets the exact source above, and `releases/latest/download/manifest.json` names it.

## Update and recovery transitions

This release changes the updater's launchd recovery, so the
[transition matrix](daemon-launchagent-recovery-20261101.md#required-release-acceptance) applies.

- **Native launchd drill** on the candidate checkout
  (`CLAWDLINE_TEST_LAUNCHD_RECOVERY=1 ... -run '^TestLaunchdUpdateRecovery$'`) passed both the
  legacy-CLI and named-daemon previous cases: injected first-bootstrap failure, then deliberate
  job unload and rollback, each verified by console HTTP and BUILD. This is fixture acceptance.
- **Signed archives, after publication.** A previous published updater can only fetch a published
  manifest, not a draft, so this part ran after publication and before this machine moved.
  Each case installed the actual signed previous archive with its own `setup` into isolated
  `CLAWDLINE_NEXT_INSTALL_ROOT`, `BIN_DIR`, `DIR` and `APPS_DIR`, a unique LaunchAgent label
  and a non-7727 port, with terminal discovery and iTerm2 disabled, then ran that release's own
  `clawdline update --apply --version v2.12.1`:
  - v2.12.0 (named `service/clawdline-daemon` job, port 52163): `healthy`; health v2.12.1,
    `GET /` 200, BUILD stamp `db9007cf…`, listener `service/clawdline-daemon`.
  - v2.11.0 (legacy `service/clawdline` job, port 52256): `healthy`; health v2.12.1 at API 39,
    `GET /` 200, BUILD stamp `db9007cf…`, listener migrated to `service/clawdline-daemon`.
  - Both were removed with `setup --uninstall --purge`; their jobs were unloaded, plists gone and
    ports closed.

Not performed on signed binaries: candidate health failure, bootstrap failure, supervisor
interruption and resume, existing Session IDs and Cloud reconnect inside the isolated installs,
and a second update *from* v2.12.1 (no later release exists). Those remain covered only by the
portable failure-injection suite and the fixture drill above. A fresh installation was exercised
by each case's `setup`, not separately.

## This machine

`clawdline update --apply --version v2.12.1` replaced the running `v2.12.0-dev.47e9718` and
reported `healthy`. `/v1/health` reports v2.12.1 at API level 39; installed `BUILD.json` names
the released source; `GET /` returns 200; the listener is `service/clawdline-daemon`. All 11
Session IDs recorded immediately before the switch were present afterward. The new App is staged
and replaces the installed one when it quits.

## Hosted console

Not redeployed. Production `BUILD.json` names `47e971898`, the released commit's parent; the
release commit changes nothing under `web/`.

## Installation references and website

README download links and the fixed-version example name v2.12.1 and the source above.
`install.sh` is unchanged since v2.12.0, so the reviewed pin stays
`773e04fc70bdb88d864dc2d4e125946e9f0e29a5`: the pinned raw bytes, the official redirect's bytes
and this release's `install.sh` match with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
clawdline.com's download links live in the private website source and were not changed here.
