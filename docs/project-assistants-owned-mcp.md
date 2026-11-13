# Owned Bot tools over MCP

A Bot's ACP session receives its captured fixed instructions and one private
loopback MCP endpoint. The endpoint calls the existing Host functions for
`send_message`, `dispatch_work` and `work_status`. It does not create another
actor registry, broker, receipt store or model provider. This follows the pinned
Codync host-owned chat-tool boundary; the Go host retains Clawdline's existing
Project, claims, worktree and durable-result authority.

## Execution and authority

`Host` puts its private `NativeTools` functions in the internal `OpenRequest`.
The field is excluded from JSON. Every function captures the actual execution
fence and active turn, checks the current source/feature/revocation state, and
uses the existing engine and broker bridge. The model supplies operation data,
never a Bot/Project identity, actor, human approval or machine credential.

The ACP provider creates an HTTP MCP capability only after native capability
admission. Its random bearer is independent of the daemon's credential. It is
restricted to a literal loopback address with an explicit port, one execution
fence and one listener. Metadata/capability reads do not start a listener or
native process. Production still has no configured, admitted native provider.

The transport follows the JSON-response profile of
[MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports).
It binds to `127.0.0.1`, checks the exact Host and bearer, rejects browser Origin
headers, and exposes no CORS access. GET has no SSE stream and returns 405.
Initialization and its initialized notification precede tools. Subsequent
requests carry the negotiated protocol revision. There is no resumable HTTP
session or server-initiated stream to restore after the execution ends.

The three [MCP tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)
have closed argument schemas. Mutating calls require an explicit durable
`idempotency_key`, independent of the JSON-RPC request ID. The host's existing
receipt conflict/replay rules remain authoritative; this transport does not
retry an unknown dispatch. Failed tool results expose a stable error code,
not native stderr, a bearer or arbitrary exception text. Duplicate JSON keys,
invalid Unicode, unsafe numbers and excessive nesting are rejected through the
existing Cloud JSON parser. This narrow profile accepts string/integer RPC IDs.

## Native session settings

Before creating/loading a session, the client checks ACP protocol 1, the exact
verified adapter package/version, and the needed system-prompt/HTTP-MCP
capabilities. Both new and loaded sessions receive the same captured settings.

For Claude ACP 0.106.0, the request uses the pinned Codync-style append preset
with dynamic sections excluded, an empty built-in tool list, empty setting
sources, strict MCP configuration and memory/dream/hooks disabled in request
settings. For Codex ACP 2.6.0, it uses the adapter's typed append-only text
`systemPrompt`. These are launch requests, not proof that native settings,
filesystem restrictions or memory isolation took effect. Independently verified
native admission remains required; no full-privilege fallback is added.

## Capacity and cleanup

The existing per-session `Limits.RPCPending` is one shared budget for outbound
ACP requests and accepted MCP connections. Headers and body reads therefore
cannot allocate an unbounded number of native handlers. Idle connections have
bounded header/read deadlines; keep-alive is disabled. A full budget refuses
new MCP connections with a typed `pa_capacity_full` HTTP 503 after a bounded
header read, without decoding a body or executing a tool. ACP calls use the same budget and occupancy reading.
A native message-capable configuration needs at least two slots so a pending
prompt can coexist with its tool request.

MCP bodies/responses use `Limits.FrameBytes`; HTTP headers use that server
setting plus Go's standard parser overhead. Existing write/RPC deadlines and
the existing bounded runtime process slots also bound listeners. No new
numeric resource policy is introduced.

Closing seals tool admission and cancels tool contexts before process cleanup.
A handler retains its shared slot until it exits, including when a connection
has been closed. A cancellation-ignoring callback produces unknown cleanup;
its handle remains attached to the engine until explicit cleanup succeeds.
Partial provider initialization likewise returns a cleanup handle when
resources cannot be proved stopped. It never converts an unknown close to an
invented successful resource release.

## Acceptance scope

The local behavior checks use an owned Go ACP peer and synthetic Project data.
They can exercise the actual HTTP MCP endpoint, Host, SQLite repository,
claims/worktree broker and durable result projection without a vendor/model
request. They do not establish Claude/Codex native sandbox or memory admission,
production Cloud, a physical phone, a dev installation or a release.

The separate authorized offline adapter initialization allowance remains
consumed at one attempt per provider. These tests do not invoke those installed
adapters and do not reset or expand that allowance.

For removal, remove `mcp.go`, `session_configuration.go` and their paired tests,
the Host MCP integration test, this page, and the paired wiring in
`capabilities.go`, `client.go`, Host `provider.go`, app `protocol.go` and the
capacity source/measurement/copy changes. The earlier physical-removal rehearsal
is evidence for its recorded revision only; adding this unit requires updating
any later complete removal inventory.

## Permission lifetime

Cancellation seals permission admission before writing native cancellation
frames. Answer and cancellation writes are serialized, offers retain their
captured turn, and late permission requests receive a cancelled outcome. A
native prompt response seals the same permission lifetime. A cancelled client
cannot be reused for another prompt or promote a late successful result;
the runtime opens a new execution-owned client for its next turn.
