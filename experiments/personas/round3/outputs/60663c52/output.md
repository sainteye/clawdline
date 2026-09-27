Confirmed: repo is restored to the original submitted state (`diff -q` shows no differences, scratch test file removed), and `go test ./...` still passes exactly as the developer reported. Now here's the verdict.

## Verdict: **Not proven — fails 5 of 6 acceptance criteria**

The code builds and the existing `go test ./...` suite is green, but that suite doesn't exercise most of the ticket's specific requirements. I wrote a targeted check for each unverified claim, confirmed each one fails against the submitted code, then patched the underlying bug in a scratch copy to confirm the same check passes (control) — so these are real defects, not test mistakes.

| AC | Requirement (quoted) | Check | Result | Control (bug fixed) |
|---|---|---|---|---|
| 2 | "changes only the fields present in the body... fields not in the body keep their values" | PATCH `{"done":true}` on a task with a due date/tags | **FAIL** — due reset to `0001-01-01`, tags wiped to `[]` | PASS |
| 2 | "Tags follow the same rules as in create" (trim/lowercase) | PATCH `{"tags":["Home"," errands "]}` | **FAIL** — stored as `["Home"," errands "]` verbatim | PASS |
| 2 | "at most 5 tags... Invalid input returns 400" | PATCH with 6 tags | **FAIL** — returns 200 | PASS |
| 2 | "An invalid body returns 400" | PATCH with malformed JSON | **FAIL** — returns 200 (decode error is silently discarded in `handlers.go:145`) | PASS |
| 4 | "DELETE /tasks/{id} returns 204; an unknown id returns 404" | DELETE unknown id | **FAIL** — returns 204 (`Store.Delete` never checks existence, `store.go:141-146`) | PASS |
| 3 | "Tasks with no due date are never overdue" | Task with no due date, `?overdue=true` | **FAIL** — included as overdue | PASS |
| 3 | "due date is before today" | Task due exactly today, `?overdue=true` | **FAIL** — included as overdue (uses "not after" instead of "strictly before") | PASS |
| 5 | "every task keeps its... creation time" | Migrate v1 task, check `created_at` | **FAIL** — always zero; `o.Created` is never copied (`migrate.go:42`) | PASS |
| 5 | "v1 labels become tags under the same rules as in create" | Migrate `"labels":"Health, Personal"` | **FAIL** — stored as `["Health"," Personal"]`, not trimmed/lowercased | PASS |
| 1 | Create tag/due validation | — | Not re-checked (already covered by existing `TestCreateAndGet`/`TestCreateValidation`) | passed pre-existing |
| 6 | `go test ./...` passes | `go test ./...` | **PASS** — ok (0.269s / 0.153s) | n/a (true but insufficient coverage) |

**Not measured:** production run against the real `data/tasks.json` v1 file (would need a live process + manual HTTP calls); I only exercised the migration logic via `Migrate()`/`OpenStore()` directly.

**What I tested against:** the working tree as delivered (no git history — this directory isn't a git repo), Go 1.27.1 darwin/arm64, via `go build ./...` and `go test ./...`.

The `PATCH` and `Delete`/overdue/migration bugs all trace back to real gaps in the existing test suite (no test patches a subset of fields on a task that already has a due date/tags, no test deletes an unknown id, no test checks a due-today or due-less task for overdue, no test checks `created_at` or multi-word/spaced labels after migration) — which is why `go test ./...` reported green despite the ticket not actually being satisfied.

This isn't ready to merge. Want me to fix these issues in the actual source (not just the scratch copy)?
