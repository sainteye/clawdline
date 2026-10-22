# Antigravity native policy and launch ownership

Status: private storage, native execution-plan observation, shared preparation
and live policy checks are implemented. Publishing the actual project-consent
and effective-mode owners, HTTP/broker entrypoints and native behavior activation
remain pending. This supplements decision A07 in
[the integration design](antigravity-cli.md#decisions-and-alternatives), without
replacing the existing work, receipt, claims or final acceptance policies.
No compatibility row is enabled by this storage change.

Local Project consent now has a [SQLite storage contract](antigravity-project-consent.md):
preparing, activation and revocation are durable CAS transitions with atomic
request receipts. A private coordinator now composes filesystem installation, recovery,
revocation and per-worktree checks. A lifecycle-owned private server manager
now resolves current catalog membership and bounded Git worktree registration.
Authenticated consent UI and the live policy publisher remain to be connected;
an active historical command answer is never current authority.

## Decision and alternatives

Use one server-owned native policy for HTTP Starter, broker children, handoff
and root launches. Reuse their durable opening receipts, immutable Squad
snapshots, terminal lanes and the private Antigravity launch store. A browser
boolean, generic mode name or historical loaded receipt cannot authorize a
native operation. A new policy service or mutable registry would duplicate
those owners, so neither is introduced.

`launchplan.PreparedAntigravity` is the shared private result, with a
source-compatible `app.PreparedAntigravity` alias. The proposed placement in
`ports` failed compilation: `ports -> projects -> transcript -> process ->
ports`. The separate value-only package avoids that cycle and lets app and
orchestrator share the result without importing each other. It owns no effect.
`ports.AntigravityLaunchIntent` carries the owner's once-allocated launch ID and
admitted product ceiling. The owner must persist the ID before an opening
effect and reuse it on recovery. Public request schemas do not accept it.
`AbortBeforeOpen` is reserved for a positively proved no-open result; attachment
failure or uncertain opening must retain the original terminal/receipt.
These callbacks are payload seams, not newly enabled launch behavior.

## Implemented private durability

`PrepareLaunchWithPolicy` binds the normalized launch request and `NativePolicy`
in the same bounded index transaction that reserves the launch slot. Policy
contains a version, the product ceiling, exact executable digest, compatibility
revision, native mode, requested model/effort, hook/helper digests and the digest
of the explicit project-consent revision. These are private daemon values, with no account,
credential, transcript or capability bytes. Policy mode must equal the prepared
request's native mode; that equality does not prove the mode's semantics. Policy
version 2 additionally binds model and effort. Version-1 policies remain
historical and cannot reactivate authority; their original context stays readable.

Identical keyed retries reuse context and capability. Changing any policy or
request field conflicts; a policy-free launch cannot be silently upgraded.
The store copies policy values on transaction cloning and public Go returns so
callers cannot mutate its authority through an aliased pointer. Commitment
compares immutable values, including policy, rather than pointer identity.

The index is now version 4. Known policy-free v1/v2 states and v3 states migrate
in memory; Open never rewrites their bytes. Prior proof and consumed slots
survive. Legacy versions claiming policy/revocation metadata are rejected.
Unknown index versions/fields are rejected. A structurally readable future
policy version can retain original context/history, but cannot prepare,
authenticate, pin or issue a managed commitment; it can still be revoked.
`PolicyForLaunch` also refuses absent legacy policy. Legacy preparation remains
available for existing private fixtures, with all public production guards
retained; it is not the production policy entry point.

`RevokeLaunch` atomically persists revocation and removes volatile process
activation. Capability verification, prepared-record access, process pinning,
commitments, managed responses and existing first-input grants cannot reactivate
a successfully revoked launch, including after reopen. The same ID cannot be
prepared again. Original history, loaded receipts and consumed input status
remain readable. A new resume must use a fresh launch ID and current policy,
while restoring the exact original immutable context.

`Deactivate` retains its narrower meaning: clear this store instance's current
process activation. It does not revoke a launch. Revoke failure invalidates
volatile activation and makes this store instance require reopening. A failure
before publication is not durable revocation; after reopening, the owner must
reconcile/retry that same revocation and the current consent/policy checks must
still refuse execution. Lost acknowledgement after publication retains the
revoked record. No revocation operation claims to terminate the vendor process
or prevent manual native input.

Policy uses existing `MaxLaunches` and `MaxIndexBytes`; no new map, queue,
background worker, eviction rule or resource bound is introduced. Oversized
policy preparation refuses before creating launch files or authority.

## Implemented native observation and tool-response seam

`Bridge.ImageFingerprint` returns the already pinned image digest only while
its current kernel file identity/change stamp still matches. It does not hash
the entire binary again, probe an account or run a provider. `ExecutionPlan`
also requires that exact image and the expected complete process identity.
Both methods refuse bridge contention immediately with the existing typed busy
result. Plan reading shares the registered process-read deadline.

The native execution-plan reader brackets two argv reads with three complete
process/workspace/terminal observations. It accepts only the exact executable,
one explicit `--mode`, and optional unique `--model`, `--effort` and exact UUID
`--conversation` pairs. Unknown flags, inline prompts, positional arguments,
duplicate/conflicting values, invalid UTF-8 and incomplete buffers refuse the
entire result. The private result contains only those fields, native identity,
workspace and an argv digest. A process or workspace/generation change refuses;
there is no retry, polling worker, command invocation or generic argv return.
The observations do not make mutable argv atomic or establish effective mode.

Linux reads its bounded kernel cmdline pseudo-file. Darwin's
[KERN_PROCARGS2 ABI](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_sysctl.c)
can include an environment tail. The reader bounds the queried size before
allocation, decodes only the reported argc arguments, excludes the tail from
all returned fields/digests, and clears its owned buffer on return. It never
decodes environment keys or values. A changed/short read refuses without retry.
Both platforms reuse the existing native process-file byte budget; this does
not add a new resource bound. Runtime execution plans normalize TTY spelling in
the same way as the existing process port. Native runtime evidence in this
stage is from an owned synthetic process on the current platform, not a vendor
CLI or foreign-platform runtime result.

`PreToolUseResponse` now travels from runtime options to the hook service.
After authenticated persisted Observe and the existing completion callback,
only PreToolUse selects this response. Failure, cancellation, oversized or
non-object data produces a hook error and no observer response. Other events
keep their original response path. The shared managed callback now supplies this seam as described below; it
does not publish a production behavior row, label unmanaged observations as
managed, or assert that native `force_ask` is effective. That behavior remains
a native matrix item.

## Implemented server composition

`prepareProductionAntigravity` takes the HTTP receipt or broker owner's durable
intent and returns the shared private launch result. It selects the exact
pinned image and server-selected behavior, binds the current project plan and
consent revision, prepares immutable context, and checks real `projects.Admit`
before returning. It allocates no ID, installs no project hooks, opens no
terminal and sends no input. Identical intents reuse private state. An
admission failure can revoke only a positively unopened launch.
`RevokeUnopenedLaunch` atomically refuses a recorded terminal/process; absence
of a receipt alone is still not proof that an opener did nothing. A caller
must retain pending/unknown opening outcomes for their original owner.

The live Check reruns private context/capability, image, behavior tuple,
project consent/helper/hook revision, exact native process/terminal/workspace,
mode/model/effort/resume arguments and separately supplied effective-mode
verification. It checks owner pointers and persisted policy again after those
reads, so revocation during an inspector or effective-state callback does not
leave an input grant. No new process, provider request or retry is performed.

Policy-bearing managed ingress checks before pinning; context preparation and
proof reads recheck current policy. Managed PreToolUse selects the dedicated
response only after the same policy checks. A missing dependency or behavior
row refuses; all real rows remain inactive. The private dependency snapshot has
no browser setter or production publisher in this stage: the actual narrow
project-consent owner and proven effective-mode implementation are still
required. Tests inject explicit synthetic adapter contracts, not real consent
or native permission evidence. No existing hooks file is treated as opt-in.

`ManagedRecordForProcess` distinguishes positively unowned native processes
from durable managed provenance, including inactive, revoked and old-policy
records. It reads only the existing bounded launch index and refuses ambiguous
ownership or unreadable state. Deactivation, revocation and restart cannot
convert a known managed process into an unmanaged fallback. Context and tool
responses use this distinction before live lookup.

Server-created bound actions now consult the same live policy around their
existing capture/paste/navigation/Enter effects. A managed process must retain
its active private owner and exact positively recorded conversation. Unknown
ownership refuses, while positively unowned sessions retain their existing
native interaction path. Policy failures use the existing bilingual binding
refusal copy with the typed cause retained. Synthetic action tests show a
retained draft and no Enter after policy withdrawal, and no generic Send
fallback. Broker/root/HTTP opening and initial-input ownership still need their
remaining composition; adding these checks does not enable public starts.

## Remaining production contract

1. Resolve a compiled compatibility tuple for the exact pinned image, platform
   and requested `ask`, `edits` or `full` ceiling. Initially enable no rows.
   Native mode spelling and argv are intent, not effective enforcement proof.
2. Require current explicit project hook opt-in and exact intact helper/hook
   configuration. Compose the existing execution-plan reader under the same kernel
   generation if required by the tuple; establish effective-mode authority
   separately and do not expose raw argv or environment.
3. Connect the implemented shared preparation and current policy Check to the
   HTTP and broker owning receipts. Preserve checks before process pinning,
   managed context/tool responses and each capture/load/paste/Enter effect. The
   managed PreToolUse seam has no observer fallback on failure. The initial candidate is
   measured `force_ask`, not the observer's grant-reusing `ask` response.
4. Persist the native launch reference before each owner's one open. Retain
   binding-pending and unknown outcomes without generic Type, Send or reopen
   fallback. Broker claims, worktree, actor/defaults and one-shot first input
   keep their existing owners.
5. Implement a separate exact-resume bootstrap if the provider requires input
   before its first identity event. Requested UUID/history/argv stay provisional
   until matching native ingress; do not relax the UUID-free new-launch path.
6. Activate only native-proven behavior tuples. Cover each admitted side-effect
   family, cached grants, helper/service failure, configuration/mode mutation,
   exact resume and restart. Unsupported ceilings remain explicitly owed.

Free-only Starter/Credits-off controls belong to this project's finite private
verification batch, not a global restriction on product users. No paid API,
Credits or fallback is authorized by this design or its storage tests.

## Acceptance boundary

Storage tests cover changed-policy conflicts, alias isolation, partial
publication, durable/failed/uncertain revocation, active first-input grant
invalidation, restart, old proof retention, future policy refusal and existing
byte-budget rejection. Execution-plan tests additionally read an owned kernel
process with synthetic argv/environment, reject identity/argument mutations and
changed images, and exercise the runtime tool response through private tmux and
Unix peer/helper verification. These are synthetic local tests, not native permission
or account acceptance. Actual product new/resume, each ceiling, immutable roles
and explicit skill receipts, native iTerm/platform behavior, local/encrypted
Cloud delivery and independent code plus desktop/mobile bilingual theme and
brand review remain required by the original full integration scope.
