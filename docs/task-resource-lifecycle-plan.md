# Task resources and safe reclamation

Status: the managed service and command path are implemented. The policy is
24 hours for all terminal scratch and 7 days for non-artifact raw logs. Existing
unregistered data remains protected by its original ownership boundary.

The daemon creates callback scratch/cache before launch and provides a separate
retained artifact directory. Explicit task/Root allocation, source-backed output
sealing, read-only inventory, reviewed plan/apply, pins, durable recovery and
usage/admission share `internal/app/taskresources`. Legacy task `work/`, registered
gate caches and callback logs are included in accounting without path adoption;
nested gate-cache bytes are counted once. Legacy scratch removal also refreshes
saved-result, protected-output, tree and open-reader proofs. Ownership-root failure keeps
reads and ordinary waits available while fencing new heavy work. The machine CLI/API is at
API level 79. The existing background reclaimer visits one advancing registered
page per sweep; pressure admission examines all pages. Retention is the earliest
eligibility time, not a promise of deletion at that exact hour.

Native Root/child environments and externally selected caches are not silently
migrated. Use command-scoped callbacks for new managed build/test caches; explicit
allocations are available for task/Root scratch. Root completion requires both a
safe exact-executor closure and a saved delivery from the same conversation;
missing evidence retains data. Protected files and old assignment directories
are never targets of this service. The future Bot has a common service contract,
but no Bot adapter, credential or schedule is created by this delivery.

## Outcome and first delivery

Completed work should release rebuildable scratch without losing a result,
attachment, unpublished change or the evidence needed to resume it. Every retained
resource should explain its owner, protection and next action. A future inspection
Bot should be able to inspect those facts and request the same guarded cleanup
that a person or scheduled daemon sweep requests.

Registration, artifact protection, read-only inventory and guarded plan/apply
share source-backed facts and failure-injection coverage. Do not create a Bot, schedule one, broaden its native
tool permission or start paid model work as part of this delivery.

## Evidence and existing boundaries

The current broker registers verification-gate Go caches at the exact task
`work/cache` path. Ordinary Roots and Agents inherit Go's environment; an explicit
cache outside that path has no broker ownership receipt. The Linux build wrapper
has its own registered budget and remains a separate owner.

Task reclamation removes only declared `work/` scratch after a terminal task and
positive owner-absence evidence. Briefs, results and `artifacts/` remain. Existing
worktree reclamation verifies Git ownership and either proves landing or preserves
changes; it must remain the sole implementation of that Git procedure.

Root closure receipts record the exact executor's safe close independently of
`briefed`. The legacy Root cleanup projection cannot authorize removal; registered
resources have independent plans. An old assignment folder may contain repositories, binaries and test
state as well as its brief. Its name and age do not classify its contents.

These facts are established by `go_cache.go`, `reclaim.go`, `root_completion.go`
and `handoffs.go` in `internal/app/orchestrator`, with the durable cache rows in
`internal/adapters/store/go_caches.go`.

## Data ownership

Use one durable resource register for new managed data. Reuse existing task,
Root, worktree, artifact and reclaim records through references; do not create
competing completion or Git-landing truth.

| Field | Authority and meaning |
| --- | --- |
| Resource ID and generation | Daemon-created identity; path reuse never reuses an old removal grant. |
| Owner reference | Exact task, Root assignment or Session execution, with Project and machine identity. |
| Storage class | Rebuildable cache, disposable scratch, execution log, protected artifact or retained evidence. |
| Location and filesystem identity | Daemon-allocated root and relative path, resolved location, device and directory identity. |
| Creation and lifecycle | Durable allocation, publication, completion and cleanup facts with versions. |
| Producers and readers | Registered command uses and releases; an expired heartbeat alone proves no exit. |
| Protection | Pins, outstanding delivery references, preservation records and a sealed output manifest. |
| Policy reference | Versioned retention and capacity settings, distinct from an observed eligibility decision. |
| Last cleanup receipt | Intent, outcome, reason, execution identity and measurement; retained after removal. |

Allocation is authenticated and server-controlled. A caller requests a purpose
and its own scope; it does not submit an arbitrary path or make a directory
rebuildable by assertion. The daemon creates private directories under its managed
root and records allocation before launching a producer. Recover incomplete
allocation after a crash without claiming that an unknown directory is owned.

Admit only supported storage classes and verified owner references. Reject a
different owner reusing a resource ID or location. A cleanup Bot cannot register
somebody else's directory, downgrade an artifact to scratch or transfer ownership.

## Results and scratch are separate

Managed callback launch contexts expose separate scratch, cache and protected artifact roots.
Root briefs and handoff packages remain retained evidence. A command may place
temporary code copies, test environments and caches in scratch. Deliveries publish
their required files into the artifact root before reporting output preservation. Root and child allocations are explicit;
only callback environments are automatically set in this delivery. Command
caches are isolated by execution and can repeat dependency compilation. The
aggregate budget can refuse later builds within the retention window. Shared
reuse requires tracked consumers; a path alone cannot prove a shared lifetime.
Producer briefs direct temporary binaries and raw traces to scratch and retain
concise verification evidence unless the task explicitly requires larger outputs.
Existing artifacts are never reclassified by this guidance.

Publish files atomically, then seal a manifest containing stable output IDs,
relative paths, sizes and digests. The manifest records the owning execution and
publication generation. A digest is integrity evidence, not an independent claim
that the task is accepted. Missing files, changed bytes, unsupported links or an
unfinished publication block reclamation that depends on those files.

An existing result, attachment, source archive, preservation patch, brief or
Cloud document reference never expires merely because its task is old. Artifact
relocation needs a verified replacement and stable locator resolution before the
old copy can become disposable. No relocation is needed in the first delivery.

For old data, retain existing ownership boundaries. Never infer scratch from a
folder name, extension, old modification time, a Bot's opinion or a scan of the
system temporary directory. Offer unknown resources for review with their missing
evidence; do not auto-adopt them into the register.

## Eligibility and removal

A dry run reports independent facts rather than a single unexplained boolean:
owner completion, process use, path ownership, protection, preservation, retention
and policy pressure. Return a typed kept or unknown reason and the next owner
for each failed requirement.

Removal needs all applicable positive proofs:

1. The authenticated caller is allowed to request cleanup in this scope.
2. The resource was registered by its owning producer and still has the recorded
   generation and filesystem identity under the managed root.
3. The task or exact Root executor has completed through its authoritative
   receipt. `briefed`, turn delivery, a missing heartbeat and a forced close
   are insufficient substitutes.
4. Producers and readers have released their uses and their command executions
   have actually exited. A process whose CWD is elsewhere may still use a cache;
   use process identity, launch generations, leases and open-file evidence as
   applicable. Missing or incomplete process evidence keeps the resource.
5. Required results and attachments are durably published and verified. There
   is no pin, unresolved preservation obligation or protected reference inside
   the proposed removal boundary.
6. The selected policy's retention has elapsed, or it explicitly permits this
   class to be reclaimed under pressure. Capacity pressure never overrides
   ownership, live use, artifact protection or preservation.
7. Git worktrees pass the existing Git procedure with a fresh checkout and
   landing or preservation check. Do not implement a second `rm -rf` path.

Record an intent durably before unlinking. Revalidate after acquiring the
resource's mutation lock and immediately before removal. Use rooted filesystem
operations and verify parent and directory identity so a symlink or replacement
cannot redirect a previously checked path. Do not follow nested mounts or links
into external content. Links and filesystem features the implementation cannot
safely classify produce a kept decision.

Crash recovery reconciles the exact intent and generation. A missing path with
no matching removal intent is unknown. A new directory at a removed location is
not an old deletion completing successfully. A failed deletion stays retryable
and records the actual failure. Retrying an execution ID returns its receipt;
it cannot silently expand the subjects in the request.

## Inspection and the future Bot

Keep inspection separate from mutation. The app service exposes bounded,
versioned operations that both CLI and future Bot tools can adapt:

| Operation | Contract |
| --- | --- |
| Inventory | Cursor-paged resource IDs, classes, owner references, measurements and typed protection reasons within the caller's allowed Project/machine scope. |
| Inspect outcome | Read task results, artifact manifest and existing verification/landing evidence; write a separately attributed advisory observation. |
| Plan cleanup | Exact resource IDs and generations, policy version, snapshot digest, reasons and proposed bytes; no deletion or protection changes. |
| Apply plan | Explicit reviewed plan ID/digest and idempotent execution ID; refresh every proof and return per-resource receipts. |
| Read receipts | Report removed, kept, unknown and failed subjects with their evidence times and next actions. |

A Bot's outcome review is advisory. It does not mark a person's acceptance,
create a task-completion receipt, prove that a process exited, or substitute for
Git landing or artifact integrity evidence. A negative review can request that
evidence be retained. Unpinning or relaxing protection needs the existing owner
or the person through a distinct authorized operation.

Grant inspection and cleanup-request capabilities separately. Bind a future Bot
to explicit Projects and machines; do not give it the general orchestrator
credential or a path-based delete command. Apply is a daemon operation, not a
shell command assembled by the Bot. An approved recurring cleanup capability
may submit plans under the approved policy; it still cannot bypass fresh checks.

This is an application authority boundary, not an operating-system sandbox.
A Bot separately granted unrestricted native shell access could write as the
same OS user. A dedicated inspection Bot should use the restricted tool surface;
this plan does not change existing native tool permissions.

## Policy decision

The selected settings below are approved for implementation, not a statement
that the running daemon has changed. Each value is configurable per machine.
Register every new bound in `internal/domain/capacity` and demonstrate its guard
rejecting an unregistered bound before the final green check.

| Policy | Selected setting |
| --- | --- |
| Successful-task disposable scratch | 24 hours after proved completion |
| Failed/cancelled-task disposable scratch | 24 hours after proved completion |
| Non-artifact execution logs | 7 days after completion |
| Idle managed cache, each / aggregate | 4 GiB / 16 GiB |
| Other managed scratch, aggregate | 16 GiB |
| Minimum available filesystem space | 2 GiB |
| Results, attachments and protected evidence | No automatic deletion |

The person also requested size attribution before further cleanup: identify
compiled variants, test data, source snapshots, raw logs and protected outputs
separately. Cleanup eligibility still requires the independent proofs below.
The separately authorized one-time cleanup uses current safety and retention
checks and keeps results and attachments.

Define retention from an authoritative completion/release receipt, not mtime.
The table does not retroactively shorten an existing producer's ownership or
override the repository rule for disposing of Session-owned command caches.
Keep the existing Linux wrapper's ownership and between-command budget intact.

Budgets count non-overlapping registered physical roots once. A cache nested in
scratch is a category breakdown of the parent, not additional disk usage. State
whether a measurement is logical or allocated; unavailable measurements are
unknown, not zero. Protected evidence counts towards storage visibility but
never becomes disposable merely to meet a budget.

These budgets are reclamation and new-work admission thresholds, not hard
in-flight filesystem quotas. Check before producer admission and after release;
reclaim eligible data first. If pressure remains, refuse a new relevant build or
scratch allocation with a typed reason and recovery action. Do not kill an
active job, delete its open files or block reading results, preserving work,
closing a Session or performing cleanup. If a true in-flight ceiling is later
required, specify OS quota support separately rather than claiming a preflight
check guarantees it.

Bound the generation side too: ordinary execution logs rotate within a registered
byte allowance, state their truncation and retain completion metadata. A file
explicitly published as an artifact follows artifact protection. Do not silently
truncate an artifact or a protocol receipt to satisfy a log budget.

## Delivery units and acceptance

| Unit | Inputs and output | Acceptance evidence |
| --- | --- | --- |
| Ownership and output register | Verified launch identity creates exact managed roots and immutable typed rows; artifacts publish independently. | Real-store restart, duplicate registration, foreign owner/path, incomplete publication, corrupt manifest and legacy-unknown tests. |
| Inspection and immutable plans | Current facts yield bounded inventory and exact dry-run plans. | Read-only requests leave files and store unchanged; pagination advances; scope leakage, stale versions, pins and unknown process sources are rejected. |
| Guarded cleanup and recovery | Approved policy and plan produce per-resource intents and receipts. | Failure injection for active readers, exit/heartbeat mismatch, path replacement, symlink/mount escapes, changed/pinned outputs, concurrent reuse, duplicate apply and crashes before/after unlink. |
| Capacity and log generation | Registered budgets drive idle reclamation, log rotation and new-work admission. | Red/green bound guard, overlapping-root accounting, unavailable measurements, pressure with only protected data, write failure and preserved child-command exit status. |
| Producer and Bot-facing adapters | Task, Root and command producers register/release; CLI exposes inspect/plan/apply; future Bot calls the same service. | End-to-end CLI through its real entry point; real HTTP authorization and decoder checks; result/attachment digest parity before/after cleanup; no Bot or schedule created. |
| Operational acceptance | Isolated test data exercises a sweep and its recovery; authorized deployment separately verifies the running version. | Fresh before/after inventory, exact selected IDs, receipts, protected-file hashes and same-filesystem measurements. Existing Sessions and unknown paths remain intact. |

Implementation must keep each unit independently reviewable and update
`disk-lifecycle.md` as behavior actually ships. Compilation alone is not cleanup
acceptance. Do not test new destructive behavior against the person's retained
data; use synthetic owned directories and existing safe one-time cleanup only.

## Verification refinements

Native verification gates use their durable gate submission/result receipts and
uploaded evidence as preservation proof; an ordinary result.json has no gate
authority. Ordinary task results bind both accepted fields and raw-file integrity.
A scoped inspector does not receive private cross-Project recovery target lists.
Unknown protection fields, missing/null protection and unknown lifecycle states
are retained rather than treated as an unpinned disposable resource.

Compile admission runs outside the store transaction only when a request could
receive a new grant. The transaction is retried after preflight with fresh lease
proofs. Holder renewals and queue polls remain available without repeated disk
walks; storage pressure prevents the new grant when it reaches the front.
Linux use inspection includes executable and mapped-file paths as well as CWD
and open descriptors; unavailable readings remain unknown.
