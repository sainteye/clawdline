# Project worktrees: lifecycle evidence and confirmed cleanup

A Project accumulates checkouts: the main one, a linked worktree per isolated task, and whatever
somebody registered by hand. Once work lands, what matters about those checkouts is whether they
still hold anything unfinished — and a clean `git status` cannot answer that, because a clean
checkout is not evidence of landing and a local landing is not evidence of publication.

This page describes the one owner that answers it, `ProjectWorktreeLifecycleService`
(`Sources/ProjectWorktreeLifecycle.swift`), and its authenticated codec, `ProjectWorktreeHTTP`
(`Sources/ProjectWorktreeHTTP.swift`). The policy it implements is the post-delivery cleanup rule in
[`AGENTS.md`](../AGENTS.md#post-delivery-cleanup-is-part-of-delivery) and
[`landing.md`](landing.md#post-delivery-worktree-reconciliation). The exact routes are in
[`api.md`](api.md#project-worktree-lifecycle).

## One owner, and what it refuses to be

The service is the only Git and filesystem probe, classifier, preservation step, preview and cleanup
executor for this feature. The Board and the Web UI consume its read model; they never run Git for
it and never delete anything. The existing task-ending disposal (`OrchestratorDraft.disposeWorktree`)
is a different authority — it removes a checkout that provably holds nothing when its own task ends —
and this service does not call it.

- **Reads never probe.** `GET` serves the bounded cache or an explicit `not_observed` snapshot. Only an
  explicit refresh, a preview or an apply runs git, always with optional locks off, and nothing here
  ever runs `git fetch`.
- **Unknown and error never mean clean.** A count that could not be read is `null`, never `0`. A row
  whose status, owner or liveness could not be established carries `unknown_incomplete_evidence`, and
  that class alone makes a row ineligible.
- **No caller-selected target.** A preview names opaque worktree ids from a fresh observation. Paths,
  branches and refs are re-derived from Git's own registration and pinned; there is no request field
  that can name a directory, a command, or a ref to reset.

## The snapshot

`schemaVersion` is `1`. Each row carries `worktreeId`, `path`, `branch`, `base`, `head`, `target`,
`owner`, `active`, `status`, `classifications`, `localObservation`, `canonicalTargetObservation` and
`cleanup`, and nothing else.

- `owner` is `{taskId, sessionId, terminalId, title, evidence}`. `sessionId` is the child's
  conversation UUID and `terminalId` is the separate, nullable terminal address (`%…`). The service
  holds no machine identity: the Cloud client attaches the authenticated route machine to the answer,
  and the local transport its own, so a row is located by *(machine, owner.sessionId)*, never by title.
- `active` is `true`, `false`, or `null` when the Session inventory was incomplete.
- `status` is `{complete, staged, modified, untracked}`; the counts are `null` whenever `complete` is
  false.
- `localObservation` says when this checkout was read and whether that reading is `current`, `stale`,
  `unknown` or `failed`. `canonicalTargetObservation` says the same about the comparison subject: the
  remote-tracking target (`refs/remotes/origin/<target>`, dated by the last recorded fetch) when a
  remote exists, otherwise the local target branch. The two ages are separate on purpose.

A successful refresh is coalesced for five seconds. If a later refresh fails, the typed top-level
error is returned while the last good rows remain visible as stale. Local observations are current
for five minutes; canonical remote observations are current for six hours.

## Seven classes, and a row may be several

| class | evidence |
|---|---|
| `active_in_use` | a non-terminal task owns the checkout, or a live Session's working directory is inside it |
| `landed_identical_residue` | every changed path — committed on the branch or dirty in the checkout — is byte-identical in the local target, or the branch is contained by it |
| `genuinely_unlanded` | at least one changed path differs from the target |
| `mixed_conflicted` | landed and unlanded paths share the checkout, a conflict or operation is in progress, or the owner record contradicts the registration |
| `task_owned_temporary` | a finished task's checkout that holds nothing beyond its recorded base |
| `prunable_stale_metadata` | Git still registers a checkout whose directory is gone, and no live owner uses it |
| `unknown_incomplete_evidence` | liveness, ownership, status or comparison could not be established, or the checkout is not one Clawdline created |

Classes accumulate: a checkout with a staged file identical to the target and an untracked file the
target lacks is `landed_identical_residue`, `genuinely_unlanded` and `mixed_conflicted` at once.
Dirty bytes decide a path they touch. Staged bytes that differ from both `HEAD` and the working file
make that path unknown, because a working-tree patch could not carry them.

## Cleanup is preview-first and fail-closed

`cleanup.eligible` is true only for rows whose classes are a subset of `landed_identical_residue` and
`task_owned_temporary`, or exactly `prunable_stale_metadata` for a finished task, with no blocker. A
blocker is a typed `{code, message}` and `cleanup.nextOwner` names who can move it: `main_worktree`,
`worktree_in_use`, `live_inventory_incomplete`, `task_registry_unavailable`, `foreign_worktree`,
`owner_unknown`, `owner_identity_conflict`, `worktree_locked`, `mixed_or_conflicted`, `unlanded_work`,
`evidence_incomplete`, `unsupported_dirty_entry`, `ignored_entries_present`, `landing_target_mismatch`,
`canonical_target_stale`, `landing_not_published`,
or an observation failure's own code.

**Preview** (orchestrator token only) re-observes the Project and returns a plan with an explicit
`expiresAt` (five minutes) and a `pinDigest` over six pin families: repository identity, each
worktree's path/branch/HEAD/lock/prunable state, each owner record and landing, each liveness answer,
each status and path-content digest, and the comparison target. Ignored entries and non-regular
dirty entries are typed blockers because their bytes are not pinned. Every action names its recovery
`method`, `artifact`, `base` and `digest`. The actions are:

- `preserve_patch` — offered for unlanded bytes even where cleanup is refused, because it deletes
  nothing. It never grants deletion authority by itself.
- `remove_checkout` — `git worktree remove` of the pinned registered path; `--force` only after that
  row's dirty residue was preserved and verified.
- `delete_branch` — `git update-ref -d refs/heads/clawdline/task/<id> <pinned commit>`, only when the
  branch is contained by the target; a branch that moved is left alone.
- `prune_metadata` — `git worktree remove <pinned missing path>`, once per exact registered path.
  Repository-wide prune is never used because it can remove corrupt admin records absent from the
  observable worktree list.

**Apply** (orchestrator token only) requires `confirm: true`, an `idempotency_key`, the `preview_id`
and its `pin_digest`. It re-observes every pinned row and refuses the whole plan before any effect on
`repository_changed`, `worktree_changed`, `owner_changed`, `live_changed`, `comparison_changed` or
`status_changed`, and on `preview_expired`, `preview_not_found` or `pin_digest_mismatch`. It runs
every preservation first; if one fails it stops with `preservation_failed` and nothing destructive
runs. Replaying that key returns the same failure status and receipt. Immediately before each
removal it re-reads the complete pinned row.

**Preservation proves itself.** The checkout's staged, modified and untracked (non-ignored) bytes are
staged into a private index file, written as a tree, and exported as a binary patch against `HEAD`.
The patch is then applied to that same base in a second private index; the resulting tree must equal
the snapshot tree, and the snapshot's path contents must equal the digest the preview showed. The
patch and a manifest with its SHA-256 are kept under
`~/Library/Application Support/Clawdline/worktree-lifecycle/preserved/<preview>/<worktree>/`. Neither
index is the checkout's own; no ref, index or working file of the checkout is written.

**Idempotency is durable.** The ledger (`cleanup-ledger.json`, `0600`) records `started` before the
first effect and the receipt when it finishes. The same key and preview replay the stored success or
failure with `replayed: true` and do nothing; the same key with another preview is `idempotency_key_reused`;
a key whose apply started and never finished is `apply_outcome_unknown` and needs a fresh preview; a
ledger that exists but cannot be read is `cleanup_ledger_unavailable`, and its bytes are left in place.

## Who may do what

| surface | read | refresh | preview / apply |
|---|---|---|---|
| orchestrator token (a local process) | yes | yes | yes |
| paired device with `read` | yes | yes | `403 machine_token_required` |
| verified Cloud viewer | `project-worktree-lifecycle` | `project-worktree-lifecycle-refresh` | not in the vocabulary; `403` if reached |

A read capability never implies a cleanup capability, and a Cloud request is never treated as the
machine credential whatever it carries. The Web transports expose exactly two methods,
`projectWorktreeLifecycle(project)` and `projectWorktreeLifecycleRefresh(project)`, and no cleanup
method.

## Bounds

At most 200 rows (`truncated` says when more exist), 500 changed paths compared per row, 1,000 branch
paths, 5,000 status entries, a two-minute observation budget, one observation at a time, four
queued-plus-running lifecycle requests (`429 worktree_lifecycle_busy` beyond that), 16 live previews,
a 2 MiB response and a 16 KiB request body. Past a bound a row becomes unknown rather than partially
compared.

## What this does not see

A delivery squashed or cherry-picked into the target is compared by content, so it reads as landed
residue when its bytes match and as unlanded when later work touched the same paths. Publication is
judged from the last recorded fetch; this owner never fetches, so a remote that moved since then is
not seen until someone fetches and refreshes. A patch whose text is not valid UTF-8 cannot be
verified here and fails preservation rather than being written unverified.
