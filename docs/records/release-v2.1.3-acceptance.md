# Release v2.1.3 acceptance

Published on 2026-10-22 from `fcc9059142485514d54683adb769710abb96e1ab`.

- [Exact-source CI](https://github.com/sainteye/clawdline/actions/runs/39741490782) passed.
- [Release workflow](https://github.com/sainteye/clawdline/actions/runs/39742478343) passed after the release environment was approved through Chrome.
- The publication gate verified the signed manifest, every archive size and digest, SBOMs, build provenance, Developer ID team, hardened runtime, timestamps, notarization, DMG payload and privacy scans.
- The public manifest was byte-identical to the accepted draft.
- DMG SHA-256: `ec2cf1930c2add8de8f4b9a40cc4841d7b0741d2c2ebe8ac7f9bbbe12059379b`.

## Installation evidence

The actual release DMG passed `tools/release/dmg-install-smoke.sh` in release mode: read-only mounting, copying into a disposable Applications directory, Apple signature and ticket checks, native-shell startup, shell-owned daemon identity, console HTTP 200, matching version and commit, and cleanup.

The installed signed updater then changed a release-based dev installation to v2.1.3. It reported `healthy`; the running daemon and console identified v2.1.3 at the released commit. The native App completed its staged replacement and identified version 2.1.3 with shell revision `fdbfdf2d9b62ecfa0c8053f6e7eda52c3e9cd8fe901f32d15013c057c19a3036`. Its signature and stapled ticket validated. All five Sessions in the immediate before-update snapshot remained present after the update.

## Explicit boundaries

A separate macOS account or VM was unavailable. Browser download, Finder drag installation and first launch in that environment were not performed. The release was explicitly approved with this exception; this does not change the ordinary release requirement. Isolated HOME is not evidence of fresh-account or TCC behavior.

The native pairing-code field accepted a paste and rejected an invalid test code without creating a pairing. Successful real GitHub OAuth, a valid native handover, and physical iPhone/Android camera pairing were not established by that check. Responsive browser fixtures and source reviews do not replace those device checks.

The installation examples now select the public v2.1.3 manifest and pin the installer at the same source commit. The public installer bytes were compared with that Git object; its code is unchanged from the previously pinned verifier revision.
