# Security policy

If you find a possible vulnerability, use [GitHub private vulnerability reporting](https://github.com/sainteye/clawdline/security/advisories/new) to contact the maintainers. Sign in to GitHub to open the report form. This route starts a private advisory discussion; do not open a public issue with exploit details.

## What to include

- The affected release version or source commit, operating system, and installer revision if relevant.
- A short description of the expected behavior, observed behavior, and security impact.
- A minimal reproduction using synthetic data, with the prerequisites and steps needed to reproduce it.
- Whether Cloud, browser pairing, remote commands, or a nondefault network configuration is involved.

Remove credentials, tokens, pairing links, private keys, personal conversations, and identifying project paths from screenshots and logs. A private report still does not need your real secrets. Test only systems and accounts you control or have permission to assess.

Continue reproduction details and fix coordination in the private advisory. If you cannot open the form, check [GitHub's private reporting instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately). If necessary, open a public issue asking only for help reaching the private reporting channel; omit vulnerability details and sensitive attachments.

## Supported versions and evidence

Security fixes target the current public stable release and the current reviewed installer. Older releases, beta builds, and development builds have no separate security backport commitment. See the [support policy](docs/security/support-policy.md) for the exact checked release and installer revisions and their limits.

A supported version is a target for fixes, not a claim that every part of it has been audited. This policy does not promise a response deadline, a fix deadline, or a bounty.

Before installing or pairing a browser, read the [security overview](https://clawdline.com/security/) for data and permission boundaries. The [security evidence index](docs/security/README.md) links the implementation and release evidence you can inspect.
