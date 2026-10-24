# Antigravity ACP v1 codec

This preparatory component implements initialization and native permission-offer
decoding under `internal/adapters/antigravityacp`. It prepares reply bytes; it has
no process, transport, account, model, HTTP, broker, Cloud or UI effect. It does
not enable a production provider capability or compatibility row. The full
[integration scope](antigravity-cli.md) and existing
[native launch and policy ownership](antigravity-native-policy.md) remain required.

## Contract

`DecodeInitialize(message, expectedID)` accepts exactly one complete JSON-RPC
2.0 response. It checks the expected ID, result/error exclusivity and protocol
version 1. IDs are JSON string values or integer number tokens; strings and
numbers are distinct and integers never pass through a floating-point value.
Fractional and exponent tokens are refused; numeric token spelling is compared
conservatively (including negative zero). A valid RPC error exposes its numeric
code through `CodecError`, never its provider message or data.

The returned initialization contains only declarations. Optional boolean
pointers distinguish missing/null from explicit false. Missing object
capabilities remain absent; null is unsupported, while a non-null object
advertises the named capability. `_meta` remains raw JSON and carries no current
authority. Returned values own their bytes and may be changed by their caller;
the codec retains no initialization state. Auth method choices do not identify
the active account and do not select or authenticate any method. An API-key-only
reply remains API-key-only; it never becomes personal authentication.

`DecodePermission(message, connectionGeneration, expectedSession)` requires a
nonempty caller-owned connection generation and actual session identity. Only
an identified `session/request_permission` request is accepted. The request ID,
tool call ID, opaque tool call and exact option IDs are retained. Empty or
duplicate option IDs, unsupported option kinds, malformed envelopes, duplicate
JSON keys at any depth, lossy Unicode identities and trailing JSON are refused.
Case variants of known struct fields are refused rather than allowing Go's
case-insensitive field matching to override protocol declarations. Unknown
fields do not grant capabilities or permissions. Localized display names,
tool kinds, model text, generic updates and elicitation questions are data,
never approval requests or grants.

An offer owns immutable private fields. `Fence` returns connection generation,
session and a SHA-256 revision over the complete original request bytes.
Whitespace or opaque metadata changes conservatively produce a different
revision. Ownership is compared independently of that digest. Getters copy
slices and raw JSON; repeated response preparation returns fresh bytes.
Concurrent reads and mutations of those returned copies cannot alter the offer.

`PrepareSelected(current, optionID)` requires an identical fence and an actually
offered ID. It returns the v1 selected outcome for that ID, without synthesizing
IDs or selecting by kind/name. `PrepareCancelled(current)` uses the same fence
and returns the cancelled outcome without an option ID. Neither method chooses
a default or stores a broad grant. Repeating either method only prepares bytes
again: this is not a one-shot send, durable answer receipt or execution proof.

Errors have stable, path-free `CodecError.Code` values: `invalid_message`,
`invalid_id`, `unexpected_id`, `rpc_error`, `unsupported_protocol`,
`invalid_initialize`, `invalid_owner`, `invalid_permission`, `wrong_session`,
`unsupported_option_kind`, `stale_offer` and `unknown_option`.

## Evidence and unknowns

The fixture preserves the public protocol result fields from Root's sanitized
Google Antigravity ACP 1.4.1 initialize observation. Its JSON-RPC envelope and ID
are synthetic: the sanitized record retained the result, not the original
envelope. Agent info declares `antigravity-acp`, `Google Antigravity`, `1.4.1`;
load, rich prompts, HTTP/SSE MCP, list/resume and logout are declarations.
The four advertised choices include `oauth-personal`; none proves sign-in or
that the standalone TUI's credentials are reusable. Vendor `_meta` flags for
tier and system-prompt append are opaque declarations, not billing or role proof.

Root's single initialize used zero model inputs and completed in approximately
1.243 seconds. A separate initialize plus `session/new` attempt received no
session response before a 15-second owner deadline and reported missing ACP
settings. That timeout proves neither successful session creation nor an auth
failure. This codec child ran byte-fixture tests only: no native binary,
account file, keychain, authenticate, session or prompt call was used.

Root subsequently ran one bounded native diagnostic with a new isolated ACP
account directory. Initialize answered and `authenticate(oauth-personal)`
printed a Google browser-confirmation link; the owner stopped on that event
at 1.529 seconds. No session/new or prompt was sent. All owned groups were
absent and resources disposed, with no cleanup error or automatic retry.
This proves the browser step for that fresh profile, not sign-in completion,
reuse of standalone credentials, free/Credits state, or Session creation.
The stopped link cannot be reused; a later interactive flow needs its own
live owner. The earlier direct session/new timeout remains distinct evidence.

This is a separate runtime from standalone Antigravity TUI. The standalone
1.4.5 single-handler control captured `force_ask`, then native `write_file`
changed the marker without a native approval key, followed by PostToolUse and
Stop. The earlier two-handler observation lacked captured helper stdout;
it has weaker evidence and must not be conflated. Missing/disabled-hook cases
were not reached. Native ACP permission messages provide an explicit approval
protocol; they do not prove every native tool is universally gated by it.

Keep the current free personal account and no-paid-API boundary. Never
automatically choose API-key, enterprise, ADC, paid credit or another provider.
Initialization is not authentication; authentication is not session creation;
session creation is not permission-enforcement acceptance.

## Next integration seam

Root must give the future owned transport a production frame/IO bound and
deadline, feed already bounded complete messages into this codec, and allocate
one connection generation per actual connection lifetime. No growing registry,
cache, queue, worker, retry, frame reader or numerical policy limit is added
here; no missing transport bound is claimed to exist.

The existing launch/receipt owner must persist the exact pending request and
revision, reserve its durable answer receipt, enforce its current product
ceiling, and recheck the real live connection/session/offer owner immediately
before writing once. A copied fence alone cannot establish current authority.
Owner loss, restart and uncertain/lost writes must reconcile through that same
durable owner without replaying grants or opening a fallback send lane. This
codec deliberately accepts no arbitrary effective-policy callback.

Complete runtime/auth discrimination and the native tool-family, cached-grant,
failure, resume and restart matrix before activating compatibility rows. Retain
accepted standalone discovery/history/resume, exact identity, immutable roles
and explicit skill receipts, local and encrypted Cloud flows, and independent
desktop/mobile bilingual UX and brand acceptance. This codec is not completion
of the full Antigravity integration. There is no storage migration or new wire
endpoint; existing readers and native policies are unchanged.

## Protocol sources and verification

Checked against the official [v1 initialization contract](https://agentclientprotocol.com/protocol/v1/initialization),
[tool-call permission contract](https://agentclientprotocol.com/protocol/v1/tool-calls#requesting-permission)
and [v1 schema](https://agentclientprotocol.com/protocol/v1/schema).
Transport and session ownership follow the separate
[transport](https://agentclientprotocol.com/protocol/v1/transports) and
[session setup](https://agentclientprotocol.com/protocol/v1/session-setup) contracts.
The public [registry entry](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json)
identifies the separate runtime; this child did not download or execute it.

Focused behavior tests cover the observed declaration, unknown versus false,
API-only auth, wrong protocol/ID, RPC errors, large integers/string IDs, exact
offered selections, cancellation, changed ownership/revision, malformed offers,
localized/misleading labels and alias isolation under concurrent reads.
A deliberate near-miss test first failed when Go's struct decoder accepted a
case-distinct capability override; the corrected decoder refuses it. The
private task artifact retains that failing output, without production data.
