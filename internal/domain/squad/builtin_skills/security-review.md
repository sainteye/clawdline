# Security review checklist

Use this skill when a change touches authentication, authorization, user input, file uploads, secrets, payments, sensitive data or a new external endpoint, to know where to look; findings still need a complete exploit path.

Check each area the change touches, in the project's own language and framework:

1. **Secrets.** None in source, fixtures, logs or history; they come from the environment or the project's secret store, and missing ones fail loudly at start. Ignore files cover local secret files.
2. **Input.** Every outside input is validated against an allow-list schema where it enters: type, length, range. Uploads are bounded by size, type and extension. Error messages do not reveal internals.
3. **Injection.** Queries and commands take parameters, never concatenated strings; the same for shell, path and template construction.
4. **Authentication and authorization.** Authorization is checked on the server before every sensitive operation, for the specific resource, not only at login. Session tokens are not readable by page scripts; cookies are `HttpOnly`, `Secure` and have a deliberate `SameSite`. Data stores enforce per-owner access where the platform supports it.
5. **Cross-site scripting.** User-provided HTML is sanitized with the project's existing sanitizer or not rendered as HTML; a content security policy starts strict, and `unsafe-inline` or `unsafe-eval` is debt with a removal plan.
6. **Cross-site request forgery.** State-changing requests carry a token or rely on a cookie policy that blocks cross-site use.
7. **Abuse.** Expensive or authentication endpoints are rate-limited per client and per account.
8. **Sensitive data.** Passwords, tokens, card data and personal data are never logged; users see generic errors, details stay in server logs.
9. **Signed transactions.** Where the system signs or relays value transfers, signatures, recipients, amounts and balances are verified before signing.
10. **Dependencies.** Lock files are committed. Use the audit command the project already runs and report what it says; do not install, upgrade or auto-fix dependencies as part of a review.

Tests for each control exercise the failure: unauthenticated, wrong role, invalid input, over the limit. Report a finding only with its exploit path; a checklist gap without one is a hardening suggestion, labelled as such.

Adapted from affaan-m/ECC's `security-review` skill at commit b38be6a7f6350964d59328b52f0624e1d1d552d1 (MIT; copyright 2026 Affaan Mustafa). See the pinned source and bundled license in the skill catalog.
