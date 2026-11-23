# `/v1/health` said `capacity_exhausted` on 2026-11-23

The `memory.entries` row of the capacity register (an Evidence row, limit 1152 live entries:
1024 active plus 128 pending) reached its limit in the fullest Project, so the open health route
turned `ok:false` with `capacity_exhausted`. At that point `memory add` was refused, because both
the active set and the pending overflow were full.

## Resolution

A person approved an archive list of 224 candidate entries. Every move below is an archive, not a
deletion: the original text stays readable through `memory show` and `memory restore` brings it
back. No `user` or `feedback` entry was touched, nothing was forgotten, and no content was
rewritten other than one appended line on each Dream candidate.

Counts in the affected Project (`clawdline memory dream --json`):

| When | Active | Pending | Archived | Live | `/v1/health` |
|---|---|---|---|---|---|
| Before (10:36 UTC) | 1024 | 128 | 0 | 1152 | `capacity_exhausted` |
| After the review archives (10:40 UTC) | 983 | 121 | 48 | 1104 | `capacity_exhausted` |
| Projected after the pending Dream pass | — | — | 146 | 1006 | expected green |

Steps, in the order run (high confidence first, then medium):

1. A read-only snapshot and a copy of the Project's memory folder were taken outside the repository.
2. 48 entries routed `review` were archived with `POST /v1/projects/{place}/memory/{name}/review`
   (`action: archive`), each fenced by the current revisions of the entry and its replacement.
   48 succeeded, 0 were stale, 0 failed.
3. 98 entries routed `dream` had `Superseded-by: <replacement>` appended as their last line with
   `memory update` (description, type and group carried back unchanged). 98 succeeded, 0 skipped,
   0 failed. The Dream plan immediately listed all 98 under `archive` with reason
   `archived_superseded`.
4. 78 entries routed `needs_decision` were not handled. The existing review route requires a
   replacement of the same type, which these entries do not have (73 obsolete without a
   successor, 5 reference entries whose closest successor is a project entry).

Why health stayed red after step 2: the register leaves `full` only once the ratio falls a
hysteresis of 0.05 below 1.0, that is at or below 1094 of 1152 live entries. 1104 is still `full`.
The review archives alone already made `memory add` accept writes again (it refuses only when
active is at 1024 and pending at 128 together; active was 983).

The Dream candidates are archived by the daemon's own maintenance tick, not by a route: it runs
every five minutes, only after the Project has had no memory write for five minutes and no
Session working in it, and only when live entries are at or above 820. There is no route to start
it by hand. While Sessions are working in the Project the pass waits; once it runs, live falls to
1006 (ratio 0.873) and the row leaves `full`.

Restoring one archived entry: `clawdline memory restore <name> --project <dir>`. The route has no
dry run, so the check here was read-only: a randomly chosen archived entry was still readable
through `memory show`, and its body matched the pre-archive copy. A restored entry is excluded
from later automatic archiving and lands in pending storage when the active set is full.

Remaining:

- 78 `needs_decision` entries await a person's choice of route.
- After the Dream pass, live is 1006. Bringing live to 80% of the active set (819) needs 187
  more archives; bringing the `memory.entries` row to 80% of its 1152 limit (921) needs 85 more.
