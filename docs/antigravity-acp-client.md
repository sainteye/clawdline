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
Other notifications are ignored. Requests are never automatically granted:
`session/request_permission` receives `outcome: {outcome: "cancelled"}` using
the original RPC ID, with no invented option ID. Other server requests receive
JSON-RPC -32601. This conservative client currently has no permission grant API;
the owner must add its separately reviewed offer/decision composition when needed.

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
permission cancellation, personal/free refusal, Credits changed between calls,
sanitized RPC errors, EOF/malformed/oversized/exclusive-result failures, stalled
read/write cancellation, deadline/busy/no-replay behavior, repeated concurrent
Close and actual descendant cleanup. No real provider, API key, model call or
account cache was used. Darwin is runtime-tested; Linux shares the Unix
implementation but has not received runtime acceptance here. Windows and other
platforms explicitly return unsupported.

Root retains HTTP/ordinary Session authorization, existing admin operationsAccess
entry visibility, Cloud/mobile display, event normalization, permission UI,
capacity registration, native provider acceptance, commit and integration.
There is no backend admin entitlement or pilot authorization framework here.


## Native owner acceptance and existing Session composition

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
