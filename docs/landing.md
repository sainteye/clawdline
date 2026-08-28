# Closing a delivery: landing, and what it means to be finished

Read this when you are the root session that dispatched code-producing work and it has come back,
or when you are about to commit somebody else's delivery. A child never lands, so a child does not
need this file.

It was part of `AGENTS.md` until every session in this repository was paying to read it at the
start of every conversation, including the ones that would never land anything.

## Root-owned landing closure

- A child task reaching `success`, and a reviewer saying `SAFE TO LAND`, mean **delivered** and
  **reviewed**. Neither means the user's code change is complete. The root session that dispatched
  the graph owns integration until the intended target branch contains the reviewed change.
- Plan code-producing graphs through the root-owned landing step: name the delivery branch, target
  branch, landing owner, independent review, and post-integration verification. The last child may
  be a reviewer; the last step of the work is still the root's landing closure.
- When claimed child work comes back, use its task secret with
  `POST /v1/orchestrator/tasks/:id/landing` to mark the obligation `pending`; a named root that later
  accepts a handoff may use the machine-level orchestrator token instead. This makes the obligation
  visible in `GET /v1/orchestrator/landings` but does not block anyone.
- Before reporting completion, the root must integrate without absorbing another session's dirty
  files, test the exact integrated tree with a private `TMPDIR`, and record the resulting target
  commit. Then mark that same landing record `landed` with the commit. `SAFE TO LAND` is a pending
  state, not a completion phrase.
- **HEAD must compile standing alone, and a commit is the only thing that can break that.** It
  happened twice on 2026-08-26, from two different sessions: a whole-file `git add` carried three
  lines whose type was defined in a file that stayed uncommitted, and a protocol requirement landed
  in `Strings.swift` while its fourteen values stayed in the worktree. Both trees were green at the
  moment of committing. **A green tree says nothing about HEAD while anything is uncommitted** — the
  tree is the union of everybody's work and HEAD is only your slice, so a suite run in the tree is
  answering a question nobody asked.
  A partial commit is therefore not finished until its own slice has compiled on its own. Verify the
  staged tree the way [`AGENTS.md`](../AGENTS.md) describes, and where a change spans files ask what else defines what
  you are taking: a declaration without its values, a call without its function, a case without its
  enum — each of them passes in the tree and fails in HEAD.
  Recovering another session's half-landed commit is legitimate root work: restore the missing half,
  or lift the orphaned lines back into the worktree where their owner can still see them. Say in the
  message that it is not your line's work and why HEAD could not wait for its owner.
- If overlapping uncommitted work makes integration unsafe, do not merge and do not close the task.
  Keep the landing obligation pending while coordinating with the owning session. If this root must
  stop, use a Clawdline handoff that names the delivery branch/base/head, target branch, verdict and
  test evidence, overlapping paths and owner if known, and the one next landing action. Never leave
  integration to an unnamed future session. The original root remains owner until Clawdline's
  handoff receipt confirms that the first line reached the named receiving root.
- File-release coordination goes through Clawdline, never an assistant provider's native message
  mechanism. Address the terminal-neutral session `id`, which an agent reads from
  `GET /v1/orchestrator/sessions` with the local orchestrator credential — `GET /v1/sessions` lists
  the same ids and is the paired-device route, so it answers that credential with `401
  unauthorized`. The durable wait routes deliver request and release messages so Claude and Codex
  participate equally.
- Register a wait with Clawdline's durable coordination-wait route, naming the repository, exact
  paths, owner and waiter Clawdline session ids, reason, and release condition. Clawdline persists
  and deduplicates the relationship, delivers the request, and exposes it on both Session records.
  The owner explicitly releases it through Clawdline after committing or otherwise releasing the
  paths; Clawdline fans the release notice out to every waiter and records partial delivery so a
  retry does not notify successful recipients twice. A notice wakes the waiter; it never replaces
  the waiter's own HEAD/status/diff verification. Never infer release from a clean worktree sample.
- A peer wait is the Session's `coordination.state = waiting_on_session` overlay, not its terminal
  `state`. The latter remains `idle`, `working`, or `waiting`; `waiting` still means the assistant
  needs an answer from the person and is the only form that earns the loud UI and push alert. Native
  and web session rows quietly show the owner and release condition, so a person knows the
  idle-looking session is parked and should stay open. When that UI is unavailable, the fallback
  user-visible message ends with `⏳ [Clawdline waiting] <owner> — <condition>; please keep this
  session open.`
- **Documents split by audience, and the split decides where they live.** `docs/` is what the
  community gets: English, written from the outside, tracked here, linked from both READMEs.
  `artifacts/` is a door into the private `clawdline-cloud` repository — internal working
  documents are read and written there, in whatever language suits, and they are not part of this
  repository at all. Anything worth showing somebody who installed this belongs in `docs/`, in
  English, rewritten for a reader who does not work here; moving an internal page across is a
  rewrite, not a copy.
- The living protocol page is `docs/clawdline-protocol.html`. Any change to Clawdline task,
  handoff, landing, claims, file-wait or cross-session communication semantics must update that
  standalone HTML in the same line of work and re-check it against the authoritative docs. A
  protocol change is not closed while that page still teaches the previous behavior.
- **Nothing in the suite may depend on a path that is not in this repository.** `Tests/main.swift`
  read the protocol Artifact through the `artifacts/` symlink with `try!`, so `./test.sh` could
  only pass on a machine that also had the private repository checked out beside this one — and
  every snapshot built the way [`AGENTS.md`](../AGENTS.md) describes died on it, because `git archive` carries what
  is tracked and that path is ignored. A clone must be able to run the suite green. When a test
  needs a document, that document is in `docs/`.
- Check `GET /v1/orchestrator/assistants` before dispatching, and read a `409
  assistant_exhausted`'s `alternatives` before retrying the same assistant. This closure still
  applies when a child dies mid-task because its assistant ran out of quota: whatever it had not
  committed is root's to recover or discard, exactly as with any other child that never reported.
