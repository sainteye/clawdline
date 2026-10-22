# Durable Antigravity launch context

`internal/adapters/antigravitylaunch` is a private, local filesystem adapter. It
does not start a provider, discover accounts, read vendor preferences, grant
permission, or send initial input. Root owns those integrations and their
independent native and Cloud acceptance. This is one dependency of the complete
Antigravity feature.

## Existing boundaries

`projects.PrepareAntigravityContext`, `AntigravityContext.Document`, and
`RestoreAntigravityContext` define the original prompt/memory/language bytes and
their hashes. `projects.AntigravityContextResponse` composes a native
PreInvocation injection with `antigravity.ObserverResponse`. A prepared response
is not evidence that the provider loaded it.

`squadfiles.PublishFor` publishes the original prompt and referenced role skills
in one private immutable launch directory. The new adapter reads only that
trusted root; it never consults the mutable role catalog. The existing
`store.SquadSnapshotForConversation` and `PrepareSquadResume` remain responsible
for Squad identity and capability. They are distinct from this adapter's native
conversation-to-context evidence.

`app.Starter.startAntigravity` requires a `PreparedAntigravity` containing the
admitted request, private environment and a callback attached to one durable
intent. For resume, `RestoreAntigravity` verifies native historical context
before Squad publishes a new capability. The injected prompt retains its
original skill paths; the fresh Squad launch files serve the new capability and
terminal receipt, not replacement instruction text. Current role, memory and
language callbacks are not read on this resume path.

Admission checks that preparation retained the assistant, workspace, resume ID,
role, model, effort, memory, language and original prompt. A resumed context must
also equal the restored document. Missing preparation or terminal attachment
refuses before opening. Every supported terminal plan, including the Squad and
iTerm viewer paths, attaches the actual returned terminal once. If attachment
fails after opening, `Started.BindingPending` retains the opened outcome; it is
not a retryable opening failure and native authority remains unavailable.

The app behavior is exercised with synthetic terminals and the real private
context/Squad stores, including original skill paths and unknown history. This
does not establish real vendor launch readiness. The HTTP start/resume foundation
guards remain in place: before lifting them, the composition root must supply
validated installation and measured permission policy, expose a pending binding
outcome, complete initial-input admission and verify native context-loaded proof.

## API and ownership

```go
Open(Config{StateDir: providerStateDir, SquadLaunchRoot: squadLaunchRoot}) (*Store, error)
(*Store).Close() error
(*Store).Prepare(context.Context, projects.LaunchRequest) (Prepared, error)
(*Store).PrepareLaunch(context.Context, string, projects.LaunchRequest) (Prepared, error)
(*Store).AttachTerminal(context.Context, string, string, string) error
(*Store).PinProcess(context.Context, string, ports.AntigravityProcess) error
(*Store).LookupProcess(context.Context, ports.AntigravityProcess) (Record, error)
(*Store).Commitment(context.Context, Record) (antigravity.LaunchCommitment, antigravity.HookLaunch, error)
(*Store).VerifyLaunchCapability(context.Context, string, string) (Record, error)
(*Store).Deactivate(context.Context, string) error
(*Store).RecordBinding(context.Context, string, ports.AntigravityBinding, BindingPolicy) error
(*Store).Restore(context.Context, string, string) (projects.AntigravityContext, error)
(*Store).WorkspaceBindings(context.Context) (map[string]string, error)
(*Store).PreInvocationResponse(context.Context, ports.AntigravityBinding) ([]byte, error)
(*Store).ExpectedInjection(context.Context, string) (ExpectedInjection, error)
(*Store).Receipt(context.Context, string) (ResponseReceipt, bool, error)
(*Store).Stats(context.Context) (Stats, error)
```

`StateDir` is the already canonical private `configRoot/antigravity` directory;
its parent must exist. `SquadLaunchRoot` is the exact canonical trusted
`configRoot/squad/launches` directory. It is opened lazily, so roleless launches
work without any Squad files. Supported storage platforms are Darwin and Linux;
other platforms return `antigravity_context_platform_unsupported` before writes.

Root should allocate a lowercase UUID once per launch intent and call
`PrepareLaunch(ctx, launchID, request)`. `Prepare` is a convenience for a distinct
intent, not a retry key. Identical keyed retries return the same prepared bytes
and capability; conflicting workspace, context, resume ID or normalized launch
request digest is refused. No API here retries terminal opening.

`Prepared.Record` and `Prepared.Request` are private daemon values, not HTTP
response objects. `Prepared.PrivateEnv` contains one shell assignment:
`CLAWDLINE_ANTIGRAVITY_CAPABILITY_FILE='<absolute capability path>'` with correct
shell escaping. The file is exactly 64 lowercase hex bytes with no newline,
generated from 32 crypto-random bytes, created exclusively, owned, regular and
0600. Its SHA256 covers those exact 64 bytes. The raw token is returned only
through the private `HookLaunch` from `Commitment`; never serialize it publicly,
log it, or put it in argv.

After opening a terminal, Root calls `AttachTerminal(ctx, id, backend,
terminalID)`. Backends are `tmux`, `iterm`, or `iterm-tmux`. Next, Root supplies a
fresh independent kernel observation to `PinProcess`: PID, parent PID, full
RFC3339Nano UTC birth, executable, controlling TTY, exact terminal ID, exec
generation, and Linux boot ID. The terminal must match its durable receipt.
Conflicting active terminal/PID associations and replacement generations are
refused. `Deactivate` withdraws this instance's live authority when Root proves
a process is gone; it does not delete historical context.

`Open` starts no goroutines and never activates recovered process records.
`LookupProcess` and `Commitment` work only after explicit fresh `PinProcess` in
this instance, with every process field equal. They do not independently perform
kernel reads; Root must revalidate before every privileged action and call the
bridge's live verifier. This adapter is not a live permission guard.

Root then registers `Commitment` with `Bridge.CommitLaunch`. Trusted hook
admission can obtain the private `HookLaunch`; hook JSON cannot choose the
process or make a pin authoritative. `Bridge.Observe` allocates and verifies the
native binding. Root persists that exact binding through `RecordBinding` before
calling `PreInvocationResponse`.

## Resume, switch and original evidence

A requested `--conversation` UUID is provisional and creates no history.
`RecordBinding` requires `Verified`, a lowercase native UUID, a nonzero service
sequence, observation time, transcript path, and the complete pinned process.
Its first positive binding may differ from the requested UUID. Repeated exact
sequence/bytes are idempotent; conflicting bytes or lower sequences are refused.

Subsequent native UUID changes on one process require
`AllowConversationSwitch`. `KeepConversation` refuses such changes. Each newly
observed UUID gets a new historical mapping; old mappings and their original
context remain intact. A later resume of the same UUID may only match the
original workspace/context, and must not coexist with another active launch
bound to that UUID. Root retires stale live authority before resuming elsewhere.

`Restore` and `WorkspaceBindings` may use persisted positive native evidence
after restart. This is historical authority only. An unknown conversation is
`antigravity_context_unavailable`, an explicit legacy/unavailable boundary;
there is no fallback to today's role, memory or language. A conversation cannot
be restored into a different canonical workspace.

Resume loads the original context document and verifies its digest, all
component hashes, and the original prompt's private confined path and exact
bytes. Current memory/language selections are replaced with those original
values in `Prepared.Request`. If Root already published another Squad resume
prompt, the adapter compares its bytes with the original. Different bytes are
refused; identical bytes retain the original path. In particular, a changed
embedded skill pathname is a byte difference, not permission to silently swap
the role. Root should restore first and pass the original path through admission,
while preserving its existing original Squad snapshot and private capability.
Missing or changed original files are unavailable, even though the stored
context still contains the old text; no mutable catalog read repairs them.

## Prepared response and native proof

`PreInvocationResponse` requires the exact previously recorded binding, including
full process, UUID, event, sequence and binding digest. It composes the original
context with `ObserverResponse` and saves a `PREPARED` receipt before returning.
There is at most one receipt per launch: a sequence high-water mark, not an
execution queue or unbounded event table. An identical latest retry returns the
same bytes; older/conflicting observations cannot recreate an earlier response.
The bridge already rejects repeated observations before this layer.

Cancellation and write failure return a typed error and no response. An earlier
receipt remains `PREPARED`; no callback path creates delivered/loaded status.
If a rename may have occurred but sync/cancellation makes the transaction
uncertain, this Store refuses further work until reopened. Retrying the same
key after reopening resolves the durable state without inferring rollback.

`ExpectedInjection` exposes the original text and SHA256 for Root's separate
native transcript proof reader. A sanitized measured native envelope has
`source: SYSTEM_SDK`, `type: SYSTEM_MESSAGE`, `status: DONE`, and a content wrapper
identifying a PreInvocation-injected system message. Root must match the exact
injected text inside that native envelope, scoped to the exact conversation,
process/execution and transcript generation/offset after preparation. Reject
model-authored echoes, user text, other system sources, errors, stale receipts
and another conversation's matching text. That reader and any loaded role badge
are outside this adapter's scope.

## Durability, limits and failures

One mutex serializes adapter state; a nonblocking lifetime OS lock rejects a
second writer. Directory handles and that lock belong to `Store.Close`. No
goroutines or external calls exist. Local synchronous filesystem operations
cannot be interrupted mid-syscall; cancellation is checked at transaction and
publication boundaries.

All private directories are owned 0700; all data/lock files are owned regular
0600, with single links. Each path component is opened with no-follow semantics.
Reads are bounded, compare opened/named inode identity, and reject changed
parents. Index writes use a fixed private staging file, fsync, rename, and
parent fsync. Capability files use exclusive creation. The current index digest
and lock identity are checked before operations, so externally replaced state
is refused rather than overwritten. Errors contain only stable path-free codes;
`errors.Is(err, context.Canceled)` remains available.

Admission first reserves a durable pending index slot, then writes that launch's
two bounded files, then marks the record prepared. Interrupted slots stay
non-authoritative and consume capacity. A same-ID retry may recover only its
known private files; retries create no fresh temporary names or unindexed launch
directories. No unrelated files are cleared and no directory walk or automatic
GC occurs. Full disk or directory replacement refuses publication. Historical
contexts are never silently evicted. An abandoned pending slot requires a future
explicit reviewed cleanup policy; this change does not implement one.

| Resource | Ceiling | Full behavior |
| --- | ---: | --- |
| Durable launch slots, including pending | 512 | Refuse new launch |
| Positive historical native UUID mappings | 512 | Refuse new mapping |
| Latest prepared receipts | 512 | Refuse new receipt |
| Durable metadata index | 4 MiB | Refuse transaction |
| Each immutable context document | 256 KiB | Refuse preparation |

These reuse the bridge observation and Squad request budgets, but have separate
registered owners because their allocations are independent. Existing memory
budget is 9 KiB. On disk there are at most 512 reserved launch directories, each
with a context and 64-byte capability, one index, a fixed replacement staging
file and a lock. Filesystem allocation overhead is not an exact byte prediction.

`Stats` returns actual admitted/pending/active/history/receipt counts, index
bytes, verified context-document bytes and largest document bytes, or an error. It never reports a
read failure as zero. HTTP registers all five measures as unknown until Root
attaches the real store owner. Root must add the owner in server composition
and map these fields, using `LargestContextBytes` for the per-document size rather than the sum,
before claiming runtime capacity measurement.

No schema migration of old providers or Squad SQLite occurs. The new index has
version 1; an unknown version is refused. Removing this adapter leaves its
private historical files intact; process, terminal, initial-send and public
release acceptance remain with Root.


## Native runtime composition

The daemon now opens the context owner before its native hook runtime and joins
hook workers before closing that owner on normal exit, signals or a serve error.
`Server.ManagedAntigravityOptions` authenticates the private capability before
pinning any process, verifies the kernel ancestor and terminal, and compares the
kernel workspace with the immutable launch workspace. It rechecks that workspace
again when preparing context. A guessed capability cannot mutate process pins.

Only a positive native PreInvocation binding is retained as workspace history;
`PreInvocationResponse` then composes the saved context and writes PREPARED.
Unmanaged observations receive the observer response without context. This
composition neither installs a new role badge nor claims that a provider loaded
the prepared message. App-level new/resume preparation, terminal attachment,
initial input, native load proof and final Cloud/UI acceptance remain required.

The private store's `VerifyLaunchCapability` authenticates a prepared record
without changing its process or binding state. It verifies the immutable context
and capability files, honors cancellation and returns no capability bytes.


## First-input authority

[The first-input contract](antigravity-first-input.md) separates an opened
terminal from a positive native UUID. `ReserveFirstInput` stores at most one
private text digest/request/process reservation per launch before returning its
creator a volatile `FirstInputAttempt`. Identical requests return the retained
receipt with a nil attempt. Neither fresh pinning nor reopening restores that
execution grant. `BeforePaste` and `BeforeEnter` persist their respective
uncertain phases before terminal IO and refuse repeats. There is no release,
reset, expiry or automatic retry.

The index is version 2. Legacy version-1 indexes with no input authority open
without migration writes; the next atomic state write upgrades them. Old
version-1 readers refuse version 2. New readers refuse unknown fields and invalid
input phases instead of silently discarding future safety metadata.

`Actions.SendAntigravityFirst` uses this private port under the existing terminal
lane, without manufacturing a conversation UUID. It requires fresh native
process/foreground/workspace/context/policy checks from its composition port and
a positively empty composer. The tmux adapter checks again between buffer load
and paste; revocation removes only its private buffer. Before Enter, the app
persists the attempt phase and rereads the exact draft again. Lost reads stop
paste confirmation immediately. A possible paste or Enter never authorizes
replay. HTTP and frontend foundation guards remain: the live composition port,
free-use/permission policy and native loaded-context fence must be connected and
verified before this private path becomes an available product capability.
