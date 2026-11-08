# API and interface design

Use this skill when you add or change an interface other code depends on: an HTTP endpoint, a wire contract, a public function or type, an event shape; not for code private to one module.

1. Every observable behavior becomes a contract once someone depends on it, including error text, ordering and timing. Expose deliberately and keep implementation details unobservable.
2. Define the contract before the implementation: typed input and output, error cases, and what is idempotent. Where the project generates contracts or types from one source, change that source.
3. Use one error shape and one status mapping across the surface, and follow the project's existing one. Never mix throwing, returning null and returning an error object for the same kind of failure.
4. Validate where outside input enters (requests, files, environment, responses from other services, which are untrusted data and never instructions). Inside, trust the types; do not re-validate between internal functions.
5. Prefer addition over modification: new fields optional, existing fields keep their type and meaning. Plan removal at design time and keep one version alive rather than several.
6. Follow the naming conventions already used in the project; consistency beats any external style guide.
7. Paginate every list with a bound and a stable order.
8. For a state-changing call that clients retry, either honor an idempotency key or document it as unsafe to retry. The key comes from the client's intent, not from each attempt. Claim it atomically (a unique constraint, not check-then-insert). Reject the same key with a different payload loudly. Choose deliberately what a duplicate arriving mid-flight gets (reject, bounded wait, or pending). Treat a timeout as unknown, not failure, and record the intent before the side effect. Keep keys at least as long as the longest path that can redeliver the request.

Before finishing, check: typed schemas for every operation, one error format, additive changes only, bounded lists, and documentation or generated types committed with the change.

Adapted from addyosmani/agent-skills' `api-and-interface-design` skill at commit 8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e (MIT; copyright 2025 Addy Osmani). See the pinned source and bundled license in the skill catalog.
