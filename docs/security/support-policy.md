# Security support and evidence scope

Use this policy to choose a security fix target and to distinguish that target from the code actually inspected. Report suspected vulnerabilities through the [private reporting channel](../../SECURITY.md), including findings in older versions.

## Targets for fixes

| Component | Security fix target |
| --- | --- |
| Published program | The current public stable release. Fixes may arrive in a later stable release; support does not mean an existing archive is replaced. |
| Installer | The current reviewed installer revision linked by the [installation guide](../user/install.md). The installer and program have separate source revisions. |
| Older stable releases | No separate backport commitment. Upgrade to the stable release containing a relevant fix when one is published. |
| Beta, development, and source builds | No separate maintained security release line. Identify the exact commit in a report. |

These are maintenance targets, not audit results, guarantees that a version has no vulnerabilities, or a record that a vulnerability has been fixed. A specific fix needs its own advisory or release evidence identifying affected and fixed versions.

## Checked public baseline — 2026-10-21

The [GitHub latest-release metadata](https://api.github.com/repos/sainteye/clawdline/releases/latest) named [v2.0.0](https://github.com/sainteye/clawdline/releases/tag/v2.0.0), published on 2026-10-20. Its [release manifest](https://github.com/sainteye/clawdline/releases/download/v2.0.0/manifest.json) names stable version `v2.0.0` and source commit `d2ed0c0633dec8dc7c4f0d2b8d78cf229a2795d4`. Reading this metadata identifies the baseline; it is not an independent signature verification or an audit of the release archive.

The README and installation guide instead pin [installer revision `74fc07e5bc8c8d092f216478f26d99b7ed1b77ee`](https://github.com/sainteye/clawdline/blob/74fc07e5bc8c8d092f216478f26d99b7ed1b77ee/install.sh). The public script at that revision matches the installer inspected for this policy. It verifies the manifest with trusted local OpenSSL 3 and pinned public keys, then checks the downloaded archive's signed size and SHA-256 before extraction and execution. You still trust the reviewed script and local tools. See the [installation guide](../user/install.md) for prerequisites and the [release process](../releasing.md#what-a-release-is) for the manifest trust boundary.

The [installer at the v2.0.0 program commit](https://github.com/sainteye/clawdline/blob/d2ed0c0633dec8dc7c4f0d2b8d78cf229a2795d4/install.sh) is different: it extracts and executes the downloaded program before that program performs its verification. The newer script's pre-execution verification does not apply to that older script, even when both install the same program version. Updating the script on `main` does not update copies already downloaded.

When this baseline changes, check the new public release metadata, signed manifest, installer revision, and relevant implementation again. Do not infer that a newer source commit has shipped merely because it appears on `main`.

## Limits of the evidence

The [security evidence index](README.md) provides implementation and test entry points. Each review or test result applies only to its named revision and scope. It does not establish that every historical release was audited, that every code path was tested, or that the current production Cloud deployment matches a local checkout.

The baseline above does not establish an SBOM, macOS notarization, a restricted read-only viewer mode, or completion of a key rotation. Check each feature's specific release and acceptance evidence before relying on it. Release signing, macOS code signing, and notarization are separate claims.
