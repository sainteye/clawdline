# Release v3.1.0 acceptance

Published on 2026-11-21 at 16:09:11 UTC from
`a61aded4e86158ef1347fea627291c5b16d4d845`. The release adds only its notes to
the requested Dev source `bf889fd44cd366b374ca18a3c4637cc669298ce9`. Exact-source
CI passed all five jobs. The earlier Dev CI's Linux-only test timeout did not recur.
[CI 44918269873](https://github.com/sainteye/clawdline/actions/runs/44918269873)

## Approval and publication

After both carried Linux tmux builds passed, the operator used Chrome to select the
`release` environment under **Review deployments** and press **Approve and deploy**.
GitHub recorded approval for that environment. The workflow passed and created a draft
at the expected commit with 16 assets.
[Release workflow 44919276762](https://github.com/sainteye/clawdline/actions/runs/44919276762)

The draft and publication gates both passed. They checked the manifest signature,
archives, SBOMs, provenance, macOS code signatures, notarization, Gatekeeper, DMG
delivery evidence, and privacy scans. The public latest manifest and signature matched
the accepted draft byte for byte. The v3.1.0 tag points to the signed commit.
[Public release](https://github.com/sainteye/clawdline/releases/tag/v3.1.0)

- Manifest SHA-256 `cd17f15ed568ad4d6d09f450be56299afc3c1ffa1d6df98b5345150cebd957e0`.
- DMG SHA-256 `dad446ad672bd19146e7c54bc44a9430fc6ed34a1dc7da255a54de7ce4ecc731`.
- Signed App archive SHA-256 `ae357501dbc40e5b8812e3d98a161dcee1cf979c04c6d0b28c9016b61cf781b6`.

The release-mode smoke test used the public DMG. It mounted the image read-only,
copied the App into a disposable Applications directory, and verified its version,
console, signatures, and cleanup. Its result recorded `accepted: true` and
`cleanup: true`.

## Update to the public release

The installed Dev build advanced to `v3.0.0-dev.3e69ef2` while the release was in
progress. After publication, `clawdline update --apply --version v3.1.0` switched
that installation to the approved stable version and reported `healthy`. The local
daemon reported API 109 and v3.1.0. The console returned HTTP 200 and its authenticated
BUILD stamp matched the tagged commit. All six Session IDs captured before the switch
were still present. The staged App replaced itself and relaunched. All three installed
App executables matched the same files in the signed public archive.

Updater, installer, and service-registration sources were unchanged from the previous release.
Acceptance covered the actual Dev-to-stable update and isolated public DMG.

## Hosted console and website

The hosted console serves `1dc3130907b080a15ba24f56a4e6d6bbd982849e`, a descendant
of the released commit. The served CloudGate declaration, both stylesheets, and all
nine advertised language catalogs passed the production checks.

The website source is `53dd357d3b268deae283cf8bd77f1de5e8dd996c`.
Pages deployment `8993bae4` serves this commit. The immediate homepage probe returned
HTTP 500 after upload. A separate test of the same deployment passed all 83 public
checks, including the production receipt, expected redirects, and branded 404s.
Both homepage languages and both install guide languages link to the v3.1.0 DMG.
The pinned official installer and the release source match with SHA-256
`65fd5f2d4aa57e4ece0799089ab5f6c7caf59c40da16e9a13b3b31df0e46b78f`.
The homepage, console, API, and relay health probes passed.

Fresh-account macOS permissions and physical-phone acceptance were outside this check.
