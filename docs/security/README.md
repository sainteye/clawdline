# Security evidence

Use this index to check Clawdline's data, permission, and installation boundaries before you install it or pair a browser. To report a possible vulnerability, follow the [security policy](../../SECURITY.md) and use the [private reporting form](https://github.com/sainteye/clawdline/security/advisories/new).

## Start here

| Your question | Evidence |
| --- | --- |
| What data does Clawdline read, and what can Cloud see? | [Security overview and data-flow diagram](https://clawdline.com/security/) |
| Which outbound flows have reproducible synthetic checks? | [Content and outbound data inventory](data-flows.md), [reproduction steps and exact test limits](egress-evidence.md) |
| Which versions receive security fixes, and what was checked? | [Support policy and revision boundaries](support-policy.md) |
| What do pairing and remote access allow? | [Remote access guide](../user/remote-access.md), [local authorization implementation](../../internal/domain/auth/authority.go), and [HTTP authentication gate](../../internal/transport/http/auth.go) |
| Where does Cloud encryption happen? | [Go daemon and browser architecture](../cloud.md), [envelope implementation](../../internal/domain/cloud), and [browser implementation](../../web/console/src/cloud) |
| What does revocation check? | [Local revocation boundary tests](../../internal/domain/auth/revocation_boundary_test.go) and [Cloud content authority tests](../../internal/transport/cloud/content_authority_test.go) |
| What authenticates an installation or update? | [Installation and first-install trust](../user/install.md), [release manifest and signing process](../releasing.md), [installer source](../../install.sh), and [update verification](../../internal/adapters/release/release.go) |
| What do independent agents report about comparable tools? | [English comparison and full responses](https://clawdline.com/ai-review/), [Traditional Chinese comparison and reading guides](https://clawdline.com/zh-TW/ai-review/) |
| What supply-chain evidence must a new release provide? | [Release evidence, verification commands, and pending acceptance](release-evidence.md) |
| How are source changes checked? | [Check scope and release verification boundaries](../checks.md) |

Source links describe the revision you are reading. A test file shows the cases it checks; its presence alone does not prove a passing run or a production deployment. The public repository describes the machine and browser implementation. It does not independently establish the private Cloud service's deployed configuration or operations.

For an installed release, use its release tag and signed manifest to identify the program's source commit. Check the installer revision separately. The [support policy](support-policy.md) records that distinction for the checked stable release.
