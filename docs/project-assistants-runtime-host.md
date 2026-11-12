# Lazy Bot runtime host

`internal/adapters/projectassistanthost` composes the existing Service, SQLite
Store, Engine, ACP provider port and work Broker. It is default off. This module
does not mount HTTP or MCP, change settings, start a daemon, install a provider,
run a model, deploy Cloud or establish native acceptance.

## Composition API

```go
host, err := projectassistanthost.New(ctx, projectassistanthost.Options{
    Database: machineStore.ProjectAssistantDatabase(),
    MachineID: machineID,
    Projects: canonicalProjects,
    Broker: existingBroker,
    Clock: clock,
    ID: collisionResistantID,
    Limits: registeredRuntimeLimits,
    Authority: currentHumanAuthority,
    Source: currentBackgroundSourcePolicy,
    DispatchPolicy: currentWorkerPolicy,
    Provider: configuredProviderFactory,
    NativeWork: registerOwnedNativeEntry,
})
```

All names on the right are supplied by the composition owner. No identity,
directory, permission ceiling, runtime limit, worker timeout or expiry comes
from an HTTP body or model argument. Runtime limits have no defaults here.
Worker `TimeoutMinutes` is a separate work lifetime from the native actor's
`PromptTimeout`; a missing/invalid dispatch policy refuses dispatch.

| Method | Result and responsibility |
| --- | --- |
| `New(ctx, Options)` | `(*Host, error)`; inspect existing schema/data without installing tables or constructing an Engine/provider. |
| `Status(ctx)` | `(pa.FeatureState, error)`; source opt-in plus real retained Bots for this machine. An empty schema is not data. |
| `Service()` | `(*projectassistant.Service, error)`; metadata/history facade, or typed disabled when no feature schema exists. |
| `Enable(ctx)` | Install feature tables explicitly, prepare the shared Store/Service and connect the existing Broker. No provider factory or capability probe. |
| `Disable(ctx)` | Reject writes, seal Engine admission, cancel owned work and wait for cleanup under `CleanupTimeout`. Retain data and unknown owners. |
| `Close(ctx)` | As Disable, permanently close this Host, without closing the shared database or ordinary Broker. |
| `Usage()` | Existing counts-only `RuntimeUsage`; unresolved cleanup is not a known zero. |
| `Recover(ctx, key)` | Explicit enabled queue recovery. Mark interrupted turns unknown; execute only proven unsent queued turns. Unknown barriers do not initialize a provider. |
| `RecoverWork(ctx, key, taskID, expires)` | Human-authorized, pure ReadWork reconciliation without creating a provider/Engine; retained disabled history is readable. Expiry is trusted composition input. |

`Authority(ctx, key, write)` returns the current authenticated human Authority,
including principal, reader, authorization revision and source policy. Host
intersects its opt-in with this authority rather than replacing a revoked grant.
`Source(ctx, key, write)` is a separate callback for current background native
access; a read must return the complete status including the write ceiling. It
must not depend on a captured HTTP token or a previous human principal. Runtime
workers descend from actor-owned background lifetimes. Human request contexts
authorize human operations only.

`DispatchPolicy(ctx, key)` returns `DispatchPolicy{Allowed, AllowedClaims,
Assistant, PermissionMode, TimeoutMinutes}`. Missing/error policy denies work
while keeping chat/history available. AllowedClaims are exact relative files;
the existing Broker still checks claims, paths, worktrees, concurrency, quota,
worker launch and landing. Each authority operation re-reads the real Bot,
Conversation, Binding, active Turn, Source and canonical Project. A changed
canonical identity cannot reuse an old execution binding.

The composition owner calls New before starting feature work and exclusively owns
the Broker's two optional assistant fields. A foreign non-nil authority or sink
is refused, never overwritten. Once installed, this Host reuses its authority
and `bridge.Sync` across disable/enable; it does not replace them during a
running callback. Do not externally rebind these fields while this Host lives.
They remain installed for durable result projection and disabled history reads.
Close does not stop ordinary Sessions, schedules, workers or the Broker.
The composition root must finish a persisted opt-out on startup by calling
Disable, including when there is retained history and no live Engine, before
offering a later Enable. New itself remains a read-only inspection; it cannot
infer whether a previous process completed its durable opt-out.

## Database and lazy execution

`store.Store.ProjectAssistantDatabase()` lends its existing writer `*sql.DB` to
the internal Host. There is no second database/pool and no global schema edit.
Feature Close never closes that database. Installation is an explicit Enable
operation. Unknown feature schema versions are rejected without changing data or
the host `PRAGMA user_version`.

One `projectassistantstore.Store` instance serves Repository, Memory and
EventFeed across disable/enable. Consequently `WithCurrent`, Commit, Save and
Forget keep their shared per-Bot lock. Creating a second Store for the same
database would defeat this memory exclusion rule and is not supported.

Service uses a lazy Runtime facade. Create/list/get/history/export, reader
metadata, Save and Forget do not construct an Engine or call Provider
Capabilities/Open. The first Submit constructs the Engine and invokes the
provider factory on its capability admission path. A missing provider returns
typed `adapter_not_found`; metadata remains usable. A configured ACP provider
must still supply actual read-only and memory-isolation evidence. This Host
does not turn a fake or absent evidence into native readiness.

## Native work entry lifetime

```go
NativeWork func(context.Context, projectassistant.OpenRequest, NativeWork) (func(), error)

func (NativeWork) SendMessage(ctx context.Context, id, text string) error
func (NativeWork) Dispatch(ctx context.Context, id, brief string, claims []string) (pa.WorkLink, error)
func (NativeWork) Status(ctx context.Context, taskID string) (pa.WorkLink, error)
```

Only the Host's actual Provider.Open wrapper invokes the registration callback.
Before registration it reads one Export, checks the exact Open binding against
its real current active running/waiting Turn, and captures that fixed TurnID.
Open must have a real context deadline; there is no new credential TTL. Neither
an HTTP/model Fence nor an arbitrary signing API is exposed. The callback gets
private callable functions, with no actor or credential in its arguments,
serialization, prompt, task record or log.

Each method rechecks source, Project, feature/binding, captured turn and original
Open lifetime. A caller's Background context cannot extend that lifetime.
The actual operation context obeys both caller cancellation and the original
Open cancellation/deadline. Changing the active turn within the same binding
does not retarget an old closure. Status on this native entry retains its turn
lifetime; disabled historical reads use a separate pure RecoverWork actor.

Host recomputes dispatch command hashes. IDs are scoped idempotency identities:
same ID/text publishes once; changed text conflicts. Dispatch retains the
existing original unknown receipt and never retries an uncertain intent.
Task reads still pass the Broker's actual Bot/Project/conversation ownership.

Registration returns a non-nil process-local unregister function. It must be
synchronous and nonblocking: no IO, work launch or detached background task.
Host first atomically invalidates callable functions, then invokes unregister
through `sync.Once`. It handles unregister-plus-error, Open failure, original
context cancellation, Session.Close and Disable. Registration and provider
operations must honor their contexts. Native transport/MCP request concurrency
remains with its existing bounded owner; this seam introduces no registry,
polling loop, model retry or fallback provider.

The integrating root still owns real MCP registration/authentication and its
transport cleanup. These private function tests do not establish that an actual
native provider has been connected to MCP.

## Shutdown and unknown resources

`Engine.Close` seals new acquisition before canceling actor lifetimes. In-flight
admission remains owned until it returns and rechecks authorization. Workers
cancel accepted queue entries, preserve started turns as unknown and finish
their pending permission offers. Slot waiters never launch after cancellation.

A process slot covers setup/Open, prompt work and verified provider Close.
The worker remains the sole close owner during execution. Late Open retains its
actor/slot until the returned session is actually cleaned; a timeout does not
make that owner disappear. A failed Close retains its session, slot and unknown
counts. An explicit repeated shutdown can retry that same cleanup handle, with
at most one cleanup owner per existing bounded actor. No name-based process
kill or containment implementation is added here.

Shutdown waits on state changes under the existing CleanupTimeout, including
when provider IO holds an actor mutex. It returns typed
`execution_effect_unknown` at the bound. If Open reports an unknown effect
without a cleanup handle, the owner/slot stays unresolved; no automatic restart
can prove that missing resource clean. The composition owner must retain this
Host and report/escalate that uncertainty, rather than drop it or construct a
new Host. Host refuses reenable while any prior owner/process is unresolved.

Stopping actors alone is insufficient: a restart or a failed queue read may
leave durable accepted work without a live actor. Host also scans real Store
pages for its machine under the same single total CleanupTimeout. The narrow
`projectassistant.QuiesceHistory(ctx, Repository, Clock, ID, BotKey)` helper
cancels queued/accepted turns, marks running/waiting turns and their pending
offers unknown, and preserves existing unknown turns and original receipts.
Engine stop and Host history cleanup share that helper. It constructs neither
an Engine nor a provider and allocates no permanent actor for a saved Bot.

Any page/SQL/CAS error or deadline keeps Host's source cleanup pending and
Usage.Known false, even if there is no Engine and the process count is zero.
Enable must successfully repeat the durable cleanup before it can reopen
admission. Completed Bot mutations are idempotent across partial-scan retries;
there is no per-page deadline reset, retained cursor registry or background loop.

No new fixed operational bound is introduced. Actors, slots and cleanup retries
are bounded by the existing host-injected MaxBots/MaxProcesses/CleanupTimeout.
The existing per-project disable latch remains fail closed; source-wide Host
disable uses Close and creates a new Engine only after proven cleanup.

## Work projection ownership

Engine owns the original dispatch unknown receipt. Bridge owns preparation,
accepted/result/landing/Board projections and their receipt namespaces.
After Dispatch returns, Engine verifies an existing persisted link with matching
fence, turn and task ownership and returns that current link. It no longer writes
`runtime-work-*` or overwrites a more advanced state with a stale accepted link.
This resolves the overlap noted in `project-assistants-broker.md`; original
unknown receipts remain unchanged while verified broker facts advance.

## Verification and remaining acceptance

Real temporary host SQLite, feature Installer/Store and Broker tests use their
own temporary Git repositories/worktrees and fake provider/worker launchers.
They cover default-off absence of tables/subscriptions/provider, empty-schema
status, metadata zero-model behavior, first-send lazy execution, reader identity,
forget isolation/new bindings, permission/queue/slot shutdown, unknown barriers,
late Open, retained cleanup and continued host database/schedule operations.
The existing host Session execution generation remains unchanged and admissible
after disable; this is a durable identity fixture, not a live terminal UI test.
Durable cleanup tests reopen a Host with more than one page of saved queues,
inject a second-Bot SQLite trigger failure, and hold the same shared writer
through the total cleanup deadline. Failure remains fenced until explicit
retry, without initializing execution or duplicating completed cleanup events.
Private entry tests cover same-ID deduplication/conflict, current revocation,
same-binding turn replacement, new generation, original context cancellation,
setup failure, expiry, cross-Bot task ownership and disabled work recovery.

The real Store/Broker test verifies one initial WorkUpdated, one visible result,
later verified landing/Board updates, preserved original unknown receipt and one
worker launch. Restoring just the old duplicate projection makes that same test
fail with two initial WorkUpdated events. Full module tests and race runs are
recorded in the task artifacts; they are not a full commit/integration receipt.

Server/settings/Project registry wiring, real MCP/native enforcement, Cloud,
UI, production removal, integration gate, landing, dev installation and Release
acceptance remain with the integrating root. No paid API/model call was made.

## Removal

Remove `internal/adapters/projectassistanthost/{host.go,host_test.go,provider.go,
provider_test.go}`, `internal/app/projectassistant/{lifecycle.go,lifecycle_test.go}`,
`internal/adapters/store/project_assistant_database.go` and this document. Revert
only this Host delivery's lifecycle/persisted-link changes in
`internal/app/projectassistant/{runtime.go,runtime_test.go}`. No worker change
was needed in `project_assistant_database_test.go`.

At the composition root remove Host construction, persisted-setting enable/
disable hooks, Service/Status forwarding, usage measurements, native registration
and historical RecoverWork wiring, plus this Host's two Broker assistant fields
after all owners are proven clean. Root separately owns its transparency/catalog
entries and HTTP/MCP/UI additions. Preserve the shared database, original
Sessions/schedules/Broker and retained Bot data; disable/removal never implies
data deletion.
