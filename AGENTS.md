# Working in this repository

This checkout is shared by several live agent sessions, including Claude Code and Codex.
Treat every change already present in the opening `git status` as another session's unfinished work.
Do not stage, revert, rewrite, or commit those changes.
Do not infer ownership from a filename, a recent timestamp, or a new commit; inspect the content and scope.

## Shared-tree discipline

- Record `git status --short` before editing so you know the pre-existing state.
- Edit only paths assigned to your task.
- Use `git add -- <file>...` with every path named explicitly.
- Never use `git add -A`, `git add .`, or another broad staging command.
- Before any root-owned commit, run `git diff --cached --stat` and remove anything outside that
  commit's scope.
- Read the staged diff itself; a clean stat does not prove that a file contains only your work.
- A child or worker session does not commit. It hands its changes back to the root session for
  review and commit.

The working tree is an editing buffer shared with other sessions, not a reproducible build input.
For an important commit, test the exact staged tree from an archive instead:

```sh
snapshot_dir=$(mktemp -d)
git archive "$(git write-tree)" | tar -x -C "$snapshot_dir"
test_tmp=$(mktemp -d)
(cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
```

Use a new, private `TMPDIR` for every `./test.sh` run.
The script writes its test binary to the fixed path `${TMPDIR}/clawdline-tests`; shared `TMPDIR`
values race and overwrite it.

A new test must be seen red before the change that makes it green. A test born green proves
nothing: reviews here have repeatedly found suites that stayed green after the guarded logic was
replaced with a stub — or deleted outright. Break the thing once, watch the test catch it, then fix it.
Do not build from the live working tree, because it may contain another session's partial edits or
untracked files.

## Dispatching substantial work with Clawdline

Use Clawdline for work that can be split into self-contained tasks and joined later.
Create `/tmp/.clawdline/<task-id>/task.json`, then register it with `POST /v1/orchestrator/tasks`.
Put the complete instructions in `task.json`; the POST body carries only `task_id` and the task secret.

Declare every path a task may write in `claims`, relative to `project_dir`:

```json
"claims": ["Sources/Feature.swift", "Tests/FeatureTests.swift"]
```

Claims reserve equal, ancestor, and descendant paths across separately identified root trees.
A conflict is rejected before a child starts with `409 workspace_busy`, including the blocking
task, root context, conflicting absolute paths, and retry advice.
Claims are a dispatch gate, not filesystem enforcement; instructions must still restrict the child
to its declared scope.

Use `serialize` for machine-global operations rather than files:

```json
"serialize": ["build"]
```

Tasks sharing a serialization token queue in creation order and acquire all requested tokens together.
Use it for operations such as builds whose fixed outputs can collide even when source claims do not.

Branch on the orchestrator's typed error `code`:

- `over_capacity`: wait for `retry_after`, reduce the batch, or send it in stages.
- `depth_exceeded`: stop dispatching; this session is at the tree's depth limit.
- `workspace_busy`: do not start; wait, coordinate with the named root, or narrow the claims honestly.
- `bad_task`: correct the invalid `task.json` field and resend the same task id.

The complete task schema, claim and serialization semantics, lifecycle, credentials, and result
protocol are in [`docs/orchestrator.md`](docs/orchestrator.md).
HTTP request and response shapes are in [`docs/api.md`](docs/api.md#post-v1orchestratortasks).

## When Clawdline dispatched you

If the first message names you as a Clawdline child, read the task's `CHILD.md` before doing anything else.
`CHILD.md` is authoritative for that task and overrides this file where they differ.
Stay inside the named project paths and your own task directory; do not inspect other `/tmp/.clawdline` tasks.
Write the requested artifacts and write `result.json` last, exactly as the briefing specifies.

## Never

- Never run `git commit`, `git reset`, `git checkout`, or `git stash` from a child or worker
  session; integration belongs to the root.
- Never run `./build.sh`; it replaces and restarts the user's running app and can interrupt other
  dispatched sessions.
- Never alter, stage, discard, or claim another session's pre-existing uncommitted work.
