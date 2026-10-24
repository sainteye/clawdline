# Personal Antigravity ACP process client

`internal/adapters/antigravityprocess` owns one explicitly selected executable,
one personal profile and one ACP session. It uses ordinary stdin/stdout pipes;
it neither discovers a global install nor copies credentials. This package does
not enable a public provider or change HTTP, Cloud, entitlement or UI policy.

## Concrete owner flow

```go
client, err := antigravityprocess.Start(lifetime, antigravityprocess.Options{
    Executable: executable, // absolute; owner supplies ACP executable/arguments
    Args: args,
    AccountDir: profile,    // absolute; supplied as GEMINI_HOME
    WorkingDir: workspace, // absolute; also session/new cwd
    MaxFrameBytes: maxFrameBytes,
    CallTimeout: callTimeout,
    OnUpdate: func(ctx context.Context, frame json.RawMessage) error {
        // Pass the complete bounded envelope to the owner's DecodeSessionUpdate.
        return consumeUpdate(ctx, frame)
    },
})
if err != nil { return err }
defer client.Close() // production owner must observe cleanup errors
if _, err = client.Initialize(ctx); err != nil { return err }
if _, err = client.AuthenticatePersonal(ctx); err != nil { return err }
if _, err = client.NewSession(ctx); err != nil { return err }
sessionID := client.SessionID()
result, err := client.Prompt(ctx, text)
// result.StopReason is explicit completion evidence only when err == nil.
// client.Cancel() is destructive local process cancellation, not an ACP ack.
```

Both resource settings are required positive owner inputs; there are no default
numeric production policies here. The composition must register its selected
bounds in the product capacity registry. MaxFrameBytes bounds incoming and
outgoing JSON frame bytes (excluding newline) and the local settings read.
CallTimeout applies to each RPC, owner callback context and server reply write.

Only one call may be active; concurrent calls return `busy` without writing.
There is no pending-call map, notification queue, replay cache, automatic retry,
model change, auth fallback or prompt replay. Successful setup calls return their
raw result for the owner's capability/model codecs. Different model config and
runtime model identifiers are retained without interpreting them as failure.

## Personal/free checks

Before authentication and session creation, and again immediately before each
prompt, the client reads only the explicitly supplied profile's
`antigravity-acp/settings.json`. The exact known settings require
`useG1Credits: false` and `auth.type: "oauth-personal"`. Missing, malformed,
duplicate security keys, null, or other values refuse before writing. The client
does not read account credential files. An initialization response or advertised
auth method cannot mark a client authenticated: a successful `authenticate`
response is required.

`session/new` must return a nonempty session ID and
`_meta.antigravity.tier.id: "free-tier"` with name
`"Antigravity Starter Quota"`. A missing/different tier closes the process and
never enables prompting. The session ID remains available even after a tier
refusal. Environment filtering removes inherited API-key, Google/ADC/Vertex,
enterprise, AI-credit and supported provider selectors; GEMINI_HOME, LC_ALL=C
and a disabled browser are supplied explicitly. Stderr goes to a null FD, so
this client does not retain or surface provider auth links. Interactive profile
preparation remains the owner's responsibility.

## Wire and failure contract

Every outgoing call has a monotonically increasing integer ID. Replies must
match its exact JSON numeric token; a string `"1"` cannot answer request `1`.
Result/error exclusivity, object results, protocol version and explicit prompt
stopReason are checked. Numeric RPC error codes survive in `Error.RPCCode`;
provider error messages and underlying OS errors do not. `Error.Code` is stable
and `Error.Delivery` distinguishes:

- `not_written`: local refusal; no request write was attempted.
- `acknowledged`: a matching valid RPC response arrived, including RPC rejection
  or a setup result rejected by the local free-tier/protocol gate.
- `uncertain`: a write was attempted but its final effect is unproven (including
  partial/stalled writes, EOF, malformed/oversized frames and cancellation).

Neither an update nor a successful pipe write proves prompt completion. Never
blindly resend an uncertain request. Typed error codes include `invalid_options`,
`unsupported_platform`, `pipe_failed`, `start_failed`, `busy`, `invalid_state`,
`not_ready`, `personal_settings_required`, `free_tier_required`,
`unsupported_protocol`, `invalid_session`, `invalid_stop_reason`, `unexpected_id`,
`invalid_response`, `invalid_frame`, `invalid_id`, `frame_too_large`, `rpc_error`,
`write_failed`, `read_failed`, `eof`, `cancelled`, `closed`, `update_rejected` and
`cleanup_failed` (plus `invalid_context` for a nil call context).

The reader forwards complete raw `session/update` notifications in wire order,
including late tool/status/usage notifications and updates after the prompt
response. It does not duplicate the normalized event decoder. The owner callback
is synchronous backpressure, must honor its context, and must not synchronously
reenter the client; queueing and durable delivery, if desired, belong to the
owner. A callback error terminates the process without exposing callback text.
Other notifications are ignored. Requests are never automatically granted.
`session/request_permission` is decoded by the existing immutable permission
codec against this connection and its exact native Session ID. `OnPermission`
publishes one `PermissionRequest` and must return promptly. The reader then
waits in wire order, within the existing call/process deadlines, with no queue
or retry. `Select` accepts only an exact offered option ID and performs the
normal operation check under the pipe write lock before writing one selected
outcome with the original RPC ID. Duplicate, unknown and expired selections
write nothing. `Cancel` writes a cancelled outcome without inventing an option.
No callback, an unsupported offer, or a callback refusal defaults to cancelled.
Close, lifetime cancellation and prompt timeout invalidate outstanding offers;
an uncertain reply write is never replayed. Other server requests receive
JSON-RPC -32601.

## Lifetime and platform boundary

Darwin and Linux implementations start a private process group. The leader is
never reaped until shutdown has signalled that group with SIGKILL. Shutdown then
closes both parent pipe FDs and waits for the leader. This pins group identity
even when the leader exits before a live descendant. No process-name matching
is used. Deliberate descendant escape into another process group is outside
this process-group ownership contract. Unsupported platforms refuse before any
process starts; this pilot adds no Windows process-tree implementation.

Lifetime cancellation, call timeout, Cancel and concurrent/repeated Close share
one shutdown. Close joins the reader and lifetime watcher. An active call joins
its own bounded worker before returning. The only channels are unbuffered
handoffs and lifecycle signals. Close must be called by the owning composition;
owner callbacks that ignore cancellation violate this API's contract.

On this restricted Darwin worker, killing a group containing only an exited,
unreaped leader returned EPERM in five independent short-process probes. The
client preserves this as `cleanup_failed` from Close, rather than asserting a
successful cleanup. Tests explicitly record this natural-EOF limitation. The
tests with a live descendant, including an already exited leader, verify actual
descendant removal. The integrating owner should verify the same lifecycle in
the native deployment environment and preserve any cleanup failure.

## Verification and remaining integration

Behavioral tests re-exec the Go test executable as a fake ACP peer and exchange
real pipe bytes. They exercise setup/prompt methods and IDs, raw update order,
permission selection/cancellation, personal/free refusal, Credits changed between calls,
sanitized RPC errors, EOF/malformed/oversized/exclusive-result failures, stalled
read/write cancellation, deadline/busy/no-replay behavior, repeated concurrent
Close and actual descendant cleanup. No real provider, API key, model call or
account cache was used. Darwin is runtime-tested; Linux shares the Unix
implementation but has not received runtime acceptance here. Windows and other
platforms explicitly return unsupported.

The owner composes permissions through the ordinary Session inventory/menu/key
routes. Existing HTTP/Cloud authorization, terminal lanes, operation receipts
and entry visibility remain the authority. There is no separate approval API,
permission ledger, backend admin entitlement or pilot authorization framework.
Synthetic tests cover native waiting inventory, allow/reject IDs, same-looking
later offers, duplicates, wrong Sessions, revocation before writing and Close.
Process tests also cover cancellation and timeout. These tests use real pipes
and synthetic settings; they make no model calls and establish no native mobile
or production Cloud deployment acceptance.


## Historical native owner acceptance

The following one-shot measurements predate interactive permission composition
and durable history. They establish only the specific behavior measured then.

Root ran this client against the pinned official ACP 1.4.1 executable with the
already authenticated isolated personal profile. The initialize/authenticate/
session-new/prompt flow returned the exact synthetic greeting and `end_turn` in
8.209 seconds. One model input was attempted; the Starter and explicit Credits-off
checks passed. The actual normalized decoder consumed one message chunk and one
context snapshot. Close returned nil, and both the owned leader and private
process group were absent afterward. There was no automatic retry or paid API
fallback. Internal provider request counts and invoice data were not measured.
This live-process success does not erase the separately documented natural-EOF
cleanup limitation.

`PromptAccepted` additionally signals once after the complete request reaches
the private pipe, while continuing to await the RPC completion. The owner can
return an existing Session write receipt at that point and keep observing the
same turn. Acceptance is neither completion nor proof that the provider read the
message. A local refusal never invokes this signal. Real-pipe tests cover a
stalled completion, cancellation and no replay, and a Credits change that refuses
before any prompt write. `PID()` exposes the originally started child for the
existing kernel-start/execution binding; its number alone is not liveness proof.

The mobile pilot will compose these operations through the existing Session
start/send/transcript and encrypted Cloud paths. The existing administrator UI
controls whether the Antigravity start option appears. The separate chat-endpoint
proposal was not enabled. Ordinary machine pairing, capabilities, execution pins
and write receipts remain the authority; there is no new admin entitlement API.

## Integrated native acceptance

The two-process composition was exercised against the native CLI as well as
pipe-based peers. Two empty conversations in one synthetic workspace were opened
concurrently, the Host and store were reopened, and both exact identities were
resumed concurrently. That test took 15.24 seconds, made 12 setup RPC calls and
sent no model prompt.

Two separate free-account inputs then exercised tool execution. An owned
synthetic path outside the workspace was rejected by the native pre-tool hook;
the test failed its expected file check after 18.62 seconds. Loading that exact
native conversation without another prompt established the rejection reason.
An independent positive case inside the workspace received one native
permission offer, selected its exact `allow_once` option through the Host, and
produced the expected file and completed tool projection in 13.57 seconds.
The projection retained both the native failed and completed tool rows.

Each model test had a 35-second Host lifetime and at most one input and one
selection; no user message was resent. Personal OAuth, AI Credits off and native
Starter-tier admission remained enforced. These are configuration and behavior
receipts, not billing measurements or a production-phone acceptance claim.

## Existing Session preview

`antigravitysession.Host` composes the measured process client and update decoder
behind the existing Session start, send and transcript routes. An explicitly
configured machine opens personal ACP previews within the registered active-process
limit in `antigravitysession/limits.go`. Each conversation owns its process,
native identity and permission offer. The native
`session/new` identity is carried as `protocol`, with the real owned PID and an
`owned:acp:` terminal identifier. Inventory does not overwrite this structured
reading with the standalone CLI's screen or on-disk transcript resolver.

A send returns when the complete prompt reaches the private pipe. The daemon
continues reading the same turn after that HTTP request ends. The normal Session
operation check still runs immediately before the write; an uncertain write is
never replayed. Tool updates merge only present fields, including completions
that arrive after visible assistant text. Context occupancy is not billing. The native session/new model and usage_update
context snapshot supply the existing Session info route without inventing prices
or offering an unverified terminal model picker.

The preview is disabled unless the machine's state directory contains
`antigravity-acp-pilot.json`, with absolute `executable` and `accountDir` paths and
optional `args`. These paths select an already authenticated personal profile;
configuration does not copy credentials or launch a process. Daemon shutdown
cancels and joins the owned processes and prompts. Entry visibility reuses the
Cloud console's existing administrator decision, with no new entitlement API.

With a configured state directory, history is persisted locally and read through
bounded pages. Restarted conversations retain their stable local identity;
resume/load verifies the exact native conversation and workspace before making
it writable. The in-memory fallback remains bounded and explicitly truncated.
Native model and thinking choices use acknowledged configuration responses and
subsequent updates in wire order. `/model` and `/effort` do not become prompts.

A live native permission is projected as the existing waiting Session menu,
preserving its title and all offered labels. Its numeric rows map only to the
original option IDs. Reject is the offered reject option, not a synthesized
cancel. An optional opaque `menu.revision` distinguishes each occurrence, even
when request ID, tool title and options are identical. Go and TypeScript include
that revision in the same fingerprint; absent revisions preserve all historical
provider fingerprints. Older pages cannot answer native offers whose revision
they omitted. The waiting card's hold key also uses that revision so a previous
answer cannot keep a later identical question closed. Owned previews advertise
menu capability, and current native offers carry the ordinary waiting reply
signal in Cloud status without claiming a terminal screen was read. The normal
`/key` route requires the exact current fingerprint
and Session identity; only rows 1–9 are actionable through its existing key
allowlist. Native requests with more options retain the additional visible,
unanswerable rows. Close may terminate a Session waiting on permission and
removes its menu; cancellation/timeout also remove the live projection. Pending
offers are not persisted or restored after restart.

Terminal attachment, turn-only cancellation and independent UI/UX and brand
acceptance remain outside this composition. This change does not broaden preview
visibility, model/role/memory behavior or the existing public-network policy.

An earlier Root-only one-shot acceptance on the Host returned the expected synthetic
reply and `end_turn` in 8.192 seconds with one model input, the native Starter tier
and AI Credits off checked by the client, and both owned leader and process group
absent after close. There were no automatic retries or paid API calls. This is
configuration/tier evidence, not an invoice measurement, and does not establish
Cloud or mobile deployment acceptance. Later retention/path projection changes
are covered separately by local tests, not by another native model call.


### Independent preview review corrections

Independent code and UI reviews found three executable counterexamples: authority
revoked while profile settings were being read, retained tool IDs escaping the
view budget, and oversized late text remaining readable. Regression tests retain
these controlled interleavings. The final operation check now runs under the pipe
write lock after settings I/O and serialization; a local refusal remains unsent
and leaves a healthy Session usable. The retained budget includes exact tool IDs,
and whole-entry eviction also removes an oversized single entry with truncation.
The capacity reading uses that same total; it is a payload budget, not a measurement
of Go allocator overhead.

The Board's role-based assignment route does not accept the ordinary text preview,
so it does not offer Antigravity. The administrator preview remains in ordinary
Session Start. Native turn failures keep their diagnostic code beneath readable
English and Taiwan Traditional Chinese guidance. Known tool status labels are
localized, and meaningful tool/unavailable text uses the existing dim text token.
These corrections do not complete the full integration acceptance scope above.
