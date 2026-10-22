# Antigravity native policy and launch ownership

Status: storage stage implemented; production composition and native behavior
activation remain pending. This supplements decision A07 in
[the integration design](antigravity-cli.md#decisions-and-alternatives), without
replacing the existing work, receipt, claims or final acceptance policies.
No compatibility row is enabled by this storage change.

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
revision, native mode, hook/helper digests and the digest of the explicit
project-consent revision. These are private daemon values, with no account,
credential, transcript or capability bytes. Policy mode must equal the prepared
request's native mode; that equality does not prove the mode's semantics.

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

## Remaining production contract

1. Resolve a compiled compatibility tuple for the exact pinned image, platform
   and requested `ask`, `edits` or `full` ceiling. Initially enable no rows.
   Native mode spelling and argv are intent, not effective enforcement proof.
2. Require current explicit project hook opt-in and exact intact helper/hook
   configuration. Observe execution-plan tokens under the same kernel
   generation if required by the tuple; do not expose raw argv or environment.
3. Share preparation and current policy Check across HTTP and broker. Check
   before process pinning, managed context/tool responses, and every
   capture/load/paste/Enter effect. Add a production-specific PreToolUse
   response with no observer fallback on failure. The initial candidate is
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
byte-budget rejection. These are synthetic local tests, not native permission
or account acceptance. Actual product new/resume, each ceiling, immutable roles
and explicit skill receipts, native iTerm/platform behavior, local/encrypted
Cloud delivery and independent code plus desktop/mobile bilingual theme and
brand review remain required by the original full integration scope.
