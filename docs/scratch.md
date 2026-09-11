# Scratch has an owner

Clawdline reclaims what the broker itself creates: a task's `/tmp/.clawdline/<id>/work/` and an
isolated checkout's `.build`. Anything an agent creates on its own had no owner and no deadline, and
this repository's own recipes created exactly that. The two snapshot recipes `AGENTS.md` printed each
began `snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)` and removed neither directory. A root has no
`work/`, so every run it made left a whole repository snapshot and a `test_tmp` holding the
`clawdline-tests` binary behind.

Measured on this Mac on 2026-09-11 at 12:44 UTC: **106 `tmp.*` directories holding 1,871 MB** in this
user's temporary directory, 13 of them containing `clawdline-tests`, and 646 directories owned by this
user directly under `/private/tmp`. The root that dispatched this work had measured the same evening
that 539 of 634 such directories, 20.2 GB of 24.4 GB, carried Clawdline development names — landing
snapshots, merged trees, deploy copies — and that a deploy session had copied a gcloud configuration
(`application_default_credentials.json`, `credentials.db`, `access_tokens.db`) into a mode-755
directory under `/private/tmp`: 17,810 world-readable files that nothing ever removed. It was deleted
by hand. No rule said where a credential copy may live or when it must go.

This page is the one written home of the rule that fixes that. `tools/scratch.sh` implements it, and
the broker's sweep reclaims the same root against the same text; every other document links here
rather than restating it.

## The scratch contract, version 1

- **Owned root.** `${CLAWDLINE_SCRATCH_ROOT:-/tmp/clawdline-scratch}`, created with mode `0700`. The
  root is an absolute path, whether it is the default, `CLAWDLINE_SCRATCH_ROOT` or a `--root`
  argument. A root that is not absolute, is a symlink, is not a directory, or is not owned by the
  current uid is refused with a typed error before anything is created — never resolved against a
  working directory, never replaced, and never worked around by choosing another place. The tool
  and the broker's sweep refuse the same roots, because a relative root one of them accepted would
  hold entries the other never lists.
- **Entry.** A direct child directory of the root named `<purpose>.<random>`, mode `0700`; `<purpose>`
  matches `[a-z0-9][a-z0-9-]{0,39}`.
- **Marker.** `<entry>/.clawdline-scratch.json`, written atomically (temporary file + rename) before
  any payload:
  `{"clawdline_scratch":1,"created_at":<epoch seconds>,"purpose":"<slug>","owner":{"pid":<int>,"process_start":<epoch seconds>,"command":"<basename>"} | null,"keep_until":<epoch seconds> | null}`
- **Owner.** For a snapshot run, the tool process that removes the entry when it exits. For a
  directory a session keeps across tool calls, the nearest ancestor process that is the assistant
  (`claude` or `codex`). When no owner can be proved, `owner` is `null` and a time to live of 1–24
  hours is mandatory, recorded as `keep_until`.
- **Releasable.** The marker parses at version 1; and the owner is `null`, or its pid is not running,
  or the running pid's start time differs from `process_start` at whole-second resolution (±1 s);
  and `keep_until` is `null` or in the past. The broker then applies its own grace period.
- **Unknown.** A missing, unreadable or other-version marker makes the entry `unknown`. Unknown is
  never deleted automatically; it is reported.
- **Children.** A child task does not use the owned root. It passes `--root
  /tmp/.clawdline/<task-id>/work`, where the existing `work/` reclaim applies and no marker is needed.
- **Single written home.** This page. Every other document links here instead of restating it.

## Who uses what

| Who | For what | Command |
|---|---|---|
| a child | "does what I wrote work?" | `tools/scratch.sh snapshot-run --subject worktree --root /tmp/.clawdline/<task-id>/work -- ./test.sh` |
| a root | "will HEAD still build after this commit?" | `tools/scratch.sh snapshot-run --subject index -- ./test.sh` |
| a root | a directory kept across tool calls: a landing snapshot, a merged tree, a deploy copy | `dir=$(tools/scratch.sh new landing)` … `tools/scratch.sh remove "$dir"` |
| anyone | a copy of a credential, token or other secret | `dir=$(tools/scratch.sh new credentials)` … `tools/scratch.sh remove "$dir"` **before the turn ends** |

Three rules come with the table, and they are what the tool exists to make easy to keep:

- **A root creates scratch only through the tool.** A hand-made `mktemp -d` or `/tmp/<name>`
  directory has no marker, so nothing can ever tell whose it is or whether it may go.
- **A credential or secret copy lives only in an owned `0700` entry, and is removed before the turn
  that made it ends.** Not in a `mktemp` directory, not under a hand-chosen `/tmp` name, and not left
  for the sweep: the sweep is the backstop for a session that died, not the plan for one that
  finished.
- **Scratch a line of work created outside the checkout is part of its post-delivery inventory.**
  `docs/landing.md` [lists it with everything else](landing.md#post-delivery-worktree-reconciliation).

## The tool

```text
tools/scratch.sh snapshot-run --subject worktree|index [--root DIR] [--keep [--ttl-hours N]] -- COMMAND…
tools/scratch.sh new PURPOSE [--root DIR] [--ttl-hours N]
tools/scratch.sh remove PATH [--root DIR]
```

It runs on `/bin/bash` 3.2 with the stock macOS userland. Every `ps` and `date` whose output it parses
runs with `LC_ALL=C` and `TZ=UTC`: this Mac runs zh_TW, where date formats change their field counts.
`--help` prints the whole interface.

### `snapshot-run`

Makes an entry named `snapshot-<subject>.<random>`, writes its marker, copies the subject into
`<entry>/tree`, and runs `COMMAND` at the top of that copy with `TMPDIR=<entry>/tmp` — so the test
binary `./test.sh` writes to `${TMPDIR}/clawdline-tests` is private to the run and goes with it.
Standard input reaches the command.

- **The entry is removed however the run ends:** the command succeeding, the command failing, a
  refusal while copying, or `INT`, `TERM` or `HUP`. A signal is forwarded to the command as `TERM` and
  the tool waits for the command to exit before removing anything; a second signal while waiting ends
  the command with `KILL`. The tool then ends by the signal it received, so a calling script sees an
  interrupted child. (A job a non-interactive shell starts with `&` ignores `INT`, measured on bash 3.2
  even after `trap - INT`, which is why the forwarded signal is `TERM`.)
- **The command's exit status comes back unchanged**, including `75` from `./test.sh`, which means
  the machine was busy and not that the suite was red.
- **`SIGKILL` cannot be trapped.** An entry whose tool was killed outright keeps a marker naming a
  process that no longer exists, which is exactly what makes it releasable to the broker's sweep.
- **`--subject worktree`** is `HEAD` from `git archive`, then `git diff --binary --full-index
  --no-ext-diff HEAD` applied with `git apply --allow-empty --whitespace=nowarn`, then the untracked
  files from `git ls-files --others --exclude-standard -z` — the recipe `AGENTS.md` printed, step for
  step. One thing differs, because it was measured: **`git diff HEAD` writes back the index it reads**
  when a tracked file's stat information is stale (same bytes, new mtime), with and without
  `GIT_OPTIONAL_LOCKS=0`, on git 2.38.1. So the tool copies the index into the entry and points
  `GIT_INDEX_FILE` at the copy; the shared index is never written and no object is created in the
  shared `.git`.
- **`--subject index`** is `git archive "$(git write-tree)"`. It is for a root only: `write-tree`
  writes a tree object into the shared `.git`, and "will HEAD still build after this commit?" is a
  question only the session that is staging may ask.
- Both subjects then run `git init -q && git add -A` inside the copy, with every inherited `GIT_*`
  repository variable unset, so that `tools/check-version-strings.py` finds files to scan instead of
  failing closed with `version_scan_no_files`.
- **`--keep`** keeps the entry after the command exits and prints its path on stderr. A kept entry is
  a directory a session keeps across tool calls, so its marker is rewritten the way `new` writes one:
  the nearest `claude` or `codex` ancestor as owner, or `null`, and `keep_until` `--ttl-hours` from
  now (default 4). Remove it with `remove` when you are done with it.

### `new`

Makes an entry for a directory a session keeps across tool calls, writes its marker, and prints its
path on stdout. The owner is the nearest ancestor started as `claude` or `codex` — by the name it was
started as, because Claude Code's `claude` is a link to a binary named after its version and the
kernel's short name for it reads `2.1.268`. When no such ancestor exists — or the process table
cannot be read, so none can be proved — `--ttl-hours` (1–24) is required and `owner` is `null`; when
one does, `--ttl-hours` is optional and adds a `keep_until`.

### `remove`

Removes one entry, and refuses — leaving the path untouched — anything that is:

- not a directory directly under the root (`scratch_not_under_root`), judged by physical path;
- a symbolic link, or not named `<purpose>.<random>` (`scratch_not_an_entry`) — a link named like
  an entry is refused, and what it points at is never touched;
- without a marker (`scratch_marker_missing`) or with a marker that is not version 1
  (`scratch_marker_unknown`), because unknown is never removed;
- owned by a process that is still running and is not an ancestor of the caller
  (`scratch_owner_live`), because that entry is another session's live snapshot or credential copy.

### Refusals and exit status

| Exit | Typed code on stderr | Meaning |
|---|---|---|
| the command's | — | `snapshot-run` passes the command's status through |
| 64 | `scratch_usage`, `scratch_ttl_required` | bad arguments; no owner can be proved and no `--ttl-hours` was given |
| 70 | `scratch_not_in_git`, `scratch_snapshot_failed` | nothing to snapshot; the copy could not be made (the command never ran, the entry is gone) |
| 73 | `scratch_root_not_absolute`, `scratch_root_symlink`, `scratch_root_not_directory`, `scratch_root_not_owned`, `scratch_root_uncreatable` | the root is refused — before anything is created |
| 74 | `scratch_cleanup_failed` | an entry could not be removed; the message names it and the command's own status |
| 77 | `scratch_not_under_root`, `scratch_not_an_entry`, `scratch_marker_missing`, `scratch_marker_unknown`, `scratch_owner_live` | `remove` refused the path |

Because the command's status passes through, these numbers can collide with it. The typed code is
the authority.

### How the tool reads the contract, where the contract leaves a choice

- **The only `rm`** is in one function, every expansion reaching it is `${var:?}`, and before it runs
  the tool re-checks that the path is a direct child of the physical root, is not a link, and that the
  root itself is still that same real directory — because `rm` resolves every path component before
  the last.
- **The marker is one line in exactly the order above**, and `command` is the basename restricted to
  `[A-Za-z0-9._+-]`, so the tool needs no JSON escaping and recognises its own markers with a single
  pattern. Anything else is `unknown` to `remove`.
- **An entry under `--root` gets a marker too.** The contract says a child's entry needs none; writing
  one costs nothing and lets `remove --root` apply the same refusals.
- **A snapshot run whose own start time cannot be read** (a sandbox that hides the process table)
  cannot prove its owner, so it records `owner: null` with `keep_until` six hours out, which covers a
  queued suite. It still removes its entry itself when it exits.
- **An existing root is accepted at whatever mode it has**, as long as it is a real directory owned by
  this uid. Every entry inside it is `0700` regardless.

## Where it is held

[`Tests/scratch-tool.mjs`](../Tests/scratch-tool.mjs), run by `./test.sh`, drives the tool against a
temporary root and throwaway repositories: success, a failing command's exact status (`75` and `3`), a
copy that cannot be made, `INT`, `TERM` and `HUP` each leave nothing and leave no command running;
`--keep` leaves one `0700` entry with a valid marker; a symlinked root, a root that is a file and a
root owned by someone else are refused, and so is a relative root, from `CLAWDLINE_SCRATCH_ROOT` or
from `--root`, on each of `snapshot-run`, `new` and `remove`, with nothing created; `remove` refuses a
path outside the root, an entry with no marker or another version's, a link named like an entry, and
a live foreign owner. It also holds the worktree subject to not writing the shared index — beside a
control proving a plain `git diff HEAD` does write it in the same repository.

Whose an entry is depends on whether the process table can be read, and that is a fact about the
runner rather than the tool: a Codex sandbox answers `ps` with "operation not permitted". So the
suite puts a stand-in `ps` first on `PATH` and drives both of the tool's legal answers on every
runner — a process table it controls, where the marker must name the exact owner, and one that
refuses every question, where it must be `owner: null` with its mandatory `keep_until`. Each stand-in
logs what it was asked, and the suite requires that it was asked. This machine's own table is used
too when it can be read, and the run says so when it cannot. Its red proof is
[`Tests/guard-red-proofs/scratch-tool.sh`](../Tests/guard-red-proofs/scratch-tool.sh): with the
cleanup trap deleted, the suite goes red (see [guard red proofs](guard-red-proofs.md)).
