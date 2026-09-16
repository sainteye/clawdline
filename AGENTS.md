# Working in this repository

This checkout is shared by several live agent sessions, including Claude Code and Codex. Treat
every opening `git status --short` row as another Session's unfinished work until scope and content
prove otherwise. Read [`CONTEXT.md`](CONTEXT.md) before changing task, graph, review, landing,
handoff, or verification semantics. The ownership and evaluation index for every heading in this
file is [`docs/agent-instruction-coverage.json`](docs/agent-instruction-coverage.json).

Applicable instructions compose in this order: global, repository, nearest-domain, then task
brief; the last applicable rule wins a conflict. An intentionally empty nearest-domain file adds
no rules. If an instruction bundle must be truncated, preserve the nearest-domain instructions
whole. A dispatched child's `CHILD.md` is its task brief and overrides broader files where they
differ.

## Shared-tree discipline

- Record `git status --short` before editing and edit only assigned paths.
- Stage only named files: `git add -- <file>...`; never `git add -A`, `git add .`, or a wildcard.
- Before a root commit, read `git diff --cached --stat` as “is every path mine?”, unstage any
  stranger with `git reset -- <path>`, then read the staged diff itself.
- After hunk staging, use plain `git commit -m <message>` with no pathspec. A commit with no
  pathspec can sweep in somebody else's staged file; a commit with a pathspec can sweep in their
  unstaged hunks. The staged stat and staged diff defend the two opposite traps.
- Install the executable guard with `sh tools/install-git-hooks.sh`. Its scope, fail-open broker
  check, fail-closed sequencer check, and escape hatch are in
  [`docs/shared-tree-guard.md`](docs/shared-tree-guard.md).
- A child or worker does not commit. A root commits completed work only when it can isolate its
  exact change from all pre-existing or unrelated work.

### Root-owned landing closure

`success` means delivered and `SAFE TO LAND` means reviewed; neither means integrated. The root
that dispatched the graph owns integration, exact-tree acceptance, the target commit, and the
landing receipt until they are durable. Read [`docs/landing.md`](docs/landing.md) when any delivery
comes back.

### Post-delivery cleanup is part of delivery

After landing, refresh the target, receipt, status and worktree inventory. Classify every residue
and task-created scratch path as `landed_identical`, `unlanded`, `mixed_conflict`,
`task_temporary`, `prunable_metadata`, or `unknown`. Preserve unlanded and mixed work before
cleanup; remove only proven landed-identical, task-temporary, or prunable rows. Roots create
scratch only through `tools/scratch.sh`; secret copies use owned `0700` scratch and are removed in
the same turn. A blocked cleanup names its next owner. Every handoff says **Fixed but not yet
released (awaiting review)** or `Nothing`. The fail-closed procedure is in
[`docs/landing.md`](docs/landing.md#post-delivery-worktree-reconciliation).

#### Closing a root is an act with victims; look before you do it

Closing a root cancels all of its queued, spawning and briefed descendants, deepest first. Before
closing, read the root's live task inventory as [`docs/orchestrator.md`](docs/orchestrator.md)
specifies. Let each task finish or say what will be cancelled and why. Every orphan gets a named
root or person; an unknown owner is a user decision, not a silent `pending` row. This rule retains
the lesson from the 2026-08-27 four-task close cascade without requiring every Session to reread
the incident transcript.

### Root completion receipt

Only after the root's whole turn is delivered—integration, required verification, and commit
included—send one session-scoped completion receipt through the route in
[`docs/api.md`](docs/api.md#post-v1orchestratorsessionsidcomplete). It means delivered, awaiting
approval; it does not claim review or broker-verified landing. Do not send it for partial,
diagnosis-only, blocked, clarifying, or child work.

### A peer handoff is a status, not only a message

When another Session must move the work, persist `waiting_session` with a one-line note, exact
`moved_by`, and `person_needed:false`. A fully delivered root uses the stronger completion receipt.
A user decision is neither: put it in `owed` and follow the notification rule. The route contract
is in [`docs/api.md`](docs/api.md#post-v1orchestratorsessionsidstate).

### A status audit has a technical handoff and a human ending

The technical handoff gives Clawdfather paths, commits, trees, receipts, broker rows and
closeability evidence. The final Session message is a separate plain-language summary for the
person, with these headings:

- **✅ What was completed**
- **🧭 What remains**
- **🙋 What you need to decide**
- **📌 Project Board**
- **🚀 Release status**
- **⚙️ Process feedback**
- **🔒 Can this Session close?**

Write `Nothing` where appropriate; distinguish written, committed, installed, Cloud-published and
Cloud-checked. Name a line by its **Session title**. Keep an internal Session **ID in the technical appendix**,
because it is not a human navigation link. A stale Board row is evidence to report, not
product truth. The shipped operator guide owns the complete ending contract—including an owner for
every remaining item, estimated lost time plus one process improvement, and Clawdfather synthesis
rather than raw-handoff pasting—under
[`Close a whole batch, when you are the coordinator`](Resources/skill-guides/clawdline.md#close-a-whole-batch-when-you-are-the-coordinator).

### Notify before waiting for the user

When the next blocker requires a user decision, device return, credential, permission, or external
confirmation, persist the wait and send one attention request before waiting. Roots use
`POST /v1/orchestrator/notify`; children use their task secret with
`POST /v1/orchestrator/tasks/:id/notify`. Name the project/task, one action, and why it is needed;
never send a secret. Do not notify for routine progress or duplicate an alert while the user is
already answering here. A failed, disabled, unsubscribed, or limited notification is reported once
and never routed around. See [`docs/dispatching.md`](docs/dispatching.md#attention-requests-are-part-of-the-work).

## Decisions that are the user's go to the user, as options

A decision only the user can make goes through one explicit options prompt at a time. Put the
recommended choice first and mark it; every option says what happens, its cost, and who acts next.
Keep technical next steps and user decisions as two separately labelled lists. When work resumes,
ask each line for both lists; never bury the user's decision in a status paragraph.

## Verifying your work

**What deserves a test, how much of the suite to run, and the order a delivery goes through are
decided by [`docs/testing-policy.md`](docs/testing-policy.md).** Where anything below or in the
documents it links asks for more verification than that page, that page wins.

The order, in one line: **one review that runs nothing, one correction pass, then the single test
run at the commit or the release build, and at most one correction after it.** The review never
comes back a second time.

Verification follows the question, not the role:

- A child proves its complete working overlay with the narrowest meaningful static/focused check;
  for a repository snapshot use `tools/scratch.sh snapshot-run --subject worktree --root
  /tmp/.clawdline/<task-id>/work -- <command>`. Do not touch the shared index.
- A root proves the exact staged candidate with `tools/scratch.sh snapshot-run --subject index --
  <command>`, then verifies committed HEAD standing alone when required. Only roots use
  `git write-tree`.
- Use a fresh private `TMPDIR`. `./test.sh` owns the machine-wide compile lock; exit 75 means busy,
  not a red suite. Never compile around the queue or kill a holder.
- Accumulate related edits and perform the cheapest proof that answers the delivery question. A new
  regression test must be able to fail on the old code — see *A test that cannot fail proves
  nothing* — but that is read or run once, not recorded as a receipt. A docs/static slice does not
  buy a Swift compile merely for ceremony.
- One release candidate gets one exact full suite, owned by the landing root. A second full requires
  typed `inconclusive_environment` evidence.
- A reviewer runs nothing and is owed no receipt: it reads the change and answers whether the design
  is right, before any test run exists. The implementer answers the whole sealed finding set in one
  correction pass and does not send it back. The integrator tests only changed seams or dependencies
  and never repeats an unchanged green because ownership moved.
- Assert completion receipts and check counts, not only process exit. If exit 133 recurs, capture
  bytes, lines, `grep -a -c '✓'`, the last complete tick line, fatal count, and tree identity once;
  do not retry it into green.

The exact receipt tuple, risk-triggered review, correction wave, no-repeat policy, overlay digest
and runner direction live in [`docs/verification-workflow.md`](docs/verification-workflow.md).
Exact integrated-tree acceptance and the release candidate's one full suite live in
[`docs/landing.md`](docs/landing.md#root-owned-landing-closure). Machine capacity and lock evidence live in
[`docs/machine-resource-scheduling.md`](docs/machine-resource-scheduling.md).

### A live task is not by itself a build/restart blocker

Build only an exact clean candidate, but do not require zero live tasks. Restart admission follows
recoverability: `spawning` and queued work without a recoverable sealed secret block replacement;
recoverable queued and briefed tasks do not. The operational sequence is compile beside the live
app, replace only after a safe maintenance receipt, then verify the new listener and reconcile.
See [`docs/api.md`](docs/api.md#post-get-delete-v1orchestratormaintenancerestart).

### A confirmation is worth what it names

A confirmation proves only its explicit subject, predicate, time, method and observer. “Looked
fine” is not evidence for a different tree, device, route, or moment. Keep the incident lesson:
passing a proxy or screenshot never silently promotes adjacent assumptions to facts.

### To compare two surfaces, hold the observed thing still

Compare the same bytes, revision, environment and time window. If one axis moved, report two
observations rather than claiming a difference between surfaces. Historical counterexamples are
rationale, not current architecture truth.

### A rendering collapses distinctions, and then we reason about the rendering

Treat screenshots, terminal wrapping, summaries and lossy logs as projections. When identity or
count matters, inspect the source bytes or typed record and keep units attached to every number.

### A defect on the phone is read from a file, never from a paste

For phone/Cloud document defects, inspect the canonical stored artifact and its scoped Cloud
identity. A pasted excerpt is a new artifact and cannot prove what the phone rendered.

### A test that cannot fail proves nothing

A regression test must fail on the code it is about, and an assertion that stays green while the
behavior it protects is removed is not protecting anything. Read it against the old code, or run it
there once, and say which you did. That is the whole obligation: there are no stored red proofs, no
mutation receipt per failure class, and no check over the checks — those cost more than they caught
and were removed on 2026-09-15. What deserves a test at all is
[`docs/testing-policy.md`](docs/testing-policy.md).

### Before moving a file, list what names it — that costs nothing, and testing costs four minutes

Before extraction, use `rg` to enumerate the old path, moved symbols, generated manifests and
guards. Calibrate against a known positive and prepare negatives. Report the exact referrer count;
do not spend repeated compiles discovering one pinned spelling at a time.

### A sample taken along one path measures that path

Sample across time, thread and path. When that is impossible, state which path was measured; never
generalize a fast-path sample into absence everywhere.

### A single exit constrains its readers, not what reaches it

Audit the whole journey, including writers and accumulation, not only the final reader. State the
invariant—for example, coverage marks accumulate—instead of blessing one current exit mechanism.

### Measure a constant here; do not recall it

Measure machine constants in the environment where the claim is used and record unit plus method.
Do not borrow remembered values from another architecture. The page-size/buffering incident and
current resource measurements are archived in
[`docs/machine-resource-scheduling.md`](docs/machine-resource-scheduling.md).

## Dispatching substantial work with Clawdline

When work splits into coherent self-contained units that can be joined later, use Clawdline's
brokered task protocol. The root owns the graph, synthesis, review, integration and landing. Read
[`docs/dispatching.md`](docs/dispatching.md) before any dispatch; it owns sizing, inventory,
claims, serialization, retry and monitoring detail. The shipped operator procedure is
[`Resources/skill-guides/clawdline.md`](Resources/skill-guides/clawdline.md#6-report-wait-then-close-the-root-obligation),
and broker finalization is specified by
[`docs/api.md`](docs/api.md#post-v1orchestratortasksidcomplete).

### A major Feature has a visible independent owner

<!-- clawdline-dispatch-role-contract:v1 -->

- **Owned child.** `POST /v1/orchestrator/tasks` creates a bounded child only when Clawdfather
  retains synthesis, integration, and landing.
- **Handoff.** `POST /v1/orchestrator/handoffs` is continuation or transfer of an existing work
  line; the receiver must walk the sender's complete REFERENCES, answer VERIFICATION, and continue
  from OPEN THREADS.
- **Detached automation.** `POST /v1/orchestrator/detached-tasks` is the only public route that
  accepts `root.session_id: null` with `root.poll_only: true`; ordinary
  `POST /v1/orchestrator/tasks` refuses poll-only. It is only unattended automation, never a Root
  or Major Feature owner.
- **Root Assignment / Feature Launch.** `POST /v1/orchestrator/root-assignments` opens an
  ordinary independent Root and briefs only objective, scope, constraints, relevant references,
  and acceptance. Its durable machine-auth record and UI classification carry no child, handoff,
  detached, timeout, secret, result, parent, or landing lineage.

<!-- /clawdline-dispatch-role-contract:v1 -->

Use Root Assignment only for a genuinely independent Feature. Keep bounded work under Clawdfather;
use handoff only to continue an existing line with complete state. Provider-native subagents are
short disposable helpers, never invisible substitutes for a refused Clawdline task.

### Cross-session assistant communication uses the message route

Send assistant-to-assistant reports through `POST /v1/orchestrator/messages`; a typed refusal is
surfaced, never replaced with an ordinary `POST /v1/sessions/:id/send` user turn. A message cannot
assign work, transfer ownership or bypass claims. See [`docs/messages.md`](docs/messages.md).

### Prove a localhost failure before calling Clawdline offline

A restricted sandbox may block loopback while Clawdline is healthy. Check the configured port and
retry the same minimal read-only `/v1/health` with required loopback permission before declaring an
outage, relaunching, or changing dispatch shape. Distinguish `observer_unreachable` from service
failure. The filesystem fallback is in
[`docs/orchestrator.md`](docs/orchestrator.md#the-filesystem-is-the-protocol).

### Name a session to a person, address it by id

Human prose names the Session title, then id and project on first mention. Machine routes use the
exact terminal-neutral id. The wire already pairs `source.label` with `source.id`; the id is
reissued whenever the app restarts while the label survives. See
[`docs/messages.md`](docs/messages.md#clawdline-messages).

### Repeated communication stalls require a capacity and protocol audit

For recurring latency, loading, pending sends or dropped events, trace queue/concurrency bounds,
backpressure, synchronous dependencies, retries, idempotency, receipts, SSE resume, stale
snapshots and failure isolation. Distinguish accepted, executed, delivered, observed and
acknowledged. Require typed errors, counters and representative failure injection; do not close a
cross-layer incident by widening only a timeout.

### "Dispatch" means a Clawdline task, not a provider-native subagent

“Clawdline Agent”, “dispatch”, “open a new tab”, “independent task”, and “派 Agent／派下去” require
`POST /v1/orchestrator/tasks`: a broker task id and ordinary assistant Session in its own tab.
Provider-native children do not satisfy that request. A root may use them only without dispatch
language or when explicitly requested. A Clawdline child is already the bottom of the broker tree
and may use only its built-in subagents for internal help.

### Everything else about dispatching is in `docs/dispatching.md`

Before dispatching, read [`docs/dispatching.md`](docs/dispatching.md). Normal progress is
event-driven. Watchdogs inspect queued/spawning once after 90 seconds, healthy briefed/working
after 15 quiet minutes, and a known compile/test after expected duration plus three minutes. A
schema-valid `result.json.tmp` belongs to the broker stable-result finalizer, which requires two
stable observations at least 30 seconds apart; it does not belong to model polling. Unchanged state
does not earn a timed user update. English and zh-TW operator procedures ship in
`Resources/skill-guides/` and must remain semantically paired.

## When Clawdline dispatched you

If the first message names you as a Clawdline child, read the task's `CHILD.md` before anything
else. It is authoritative and overrides this file where they differ. Stay inside named project
paths and your own task directory; do not inspect sibling `/tmp/.clawdline` tasks. Write requested
artifacts, then authenticated `result.json` last. A child never lands and never opens another
Clawdline task. The generated briefing and completion-file contract are in
[`docs/orchestrator.md`](docs/orchestrator.md#childmd--written-by-the-app-read-by-the-child).

## Never

- Never run `git commit`, `git reset`, `git checkout`, or `git stash` from a child or worker. The
  sole exception is a Claude child whose isolated briefing explicitly makes committed milestones
  its delivery; a Codex child leaves worktree bytes uncommitted.
- Never run `./build.sh` from a child. A root may build/install only when requested and from an
  exact isolated candidate whose wildcard inputs cannot absorb foreign work.
- Never deliver a document by `file://`, localhost, LAN, a named tunnel, or `machine=this-mac` when
  the user expects Cloud. Use the canonical `https://app.clawdline.com` document identity with
  verified machine, Session, scope and relative path; credentials never enter the URL. The route
  scope is specified in [`docs/api.md`](docs/api.md#get-v1sessionsiddocumentsprojectpath-·-documents-tasktaskidpath).
- **An unqualified Clawdline deploy includes the hosted console.** Install the exact accepted
  commit as the native app. Build the matching web bundle with
  `tools/build-web-app.py --out dist/app-console`, upload it to the Cloudflare Pages project
  `clawdline-app`, verify the deployment-specific `*.pages.dev` URL, and verify that
  `https://app.clawdline.com/BUILD.json` carries the expected build stamp. It excludes unrelated
  marketing, API and relay deployments. The build procedure is in
  [`docs/cloud.md`](docs/cloud.md#building-and-deploying-the-hosted-console).
- **One named operational finalizer owns the side effects.** When a workflow explicitly names a
  downstream root as finalizer, the landing root stops after the commit and exact-tree verification
  receipt. It sends that exact commit and receipt to the named owner and must not build, restart, or deploy
  the same change itself. The ordinary landing root acts only when no downstream finalizer
  was named. Transport acceptance proves only the handoff was sent; obtain explicit acknowledgement
  or re-read the named owner and see it acknowledge before treating the handoff as live.
- Never alter, stage, discard, or claim another Session's pre-existing work.
