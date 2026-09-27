I have enough evidence now. Here is the QA report for TICKET-142, attempt 1.

## Verdict: **FAIL — needs work, not ready to merge**

`go build` and `go test ./...` succeed, and Create (AC1) works correctly, but four of the six acceptance criteria fail when exercised against the running service. The existing test suite has gaps that let these bugs through.

## Criteria table

| # | Criterion | Result | Evidence | Notes |
|---|---|---|---|---|
| 1 | Create | **PASS** | `curl -X POST /tasks -d '{"title":"Buy milk","due":"2026-10-01","tags":["Home"," Errands "]}'` → `201`, `Location: /tasks/t0009`, tags normalized to `["home","errands"]`. Invalid title/date/6-tags/malformed-JSON/blank-tag all → `400` with `{"error":...}` | Matches spec exactly |
| 2 | Edit (PATCH) | **FAIL** | `PATCH /tasks/t0010 -d '{"title":"New title"}'` on a task with `due:"2026-11-01", tags:["work","urgent"]` → `200` but `due` became `""` and `tags` became `null` | Partial update, tag rules, invalid-body, and unknown-id behaviors are all broken (see defects 1–4) |
| 3 | Filter | **PARTIAL FAIL** | Tag filter case-insensitivity works (`tag=WORK` matches `work`). But `GET /tasks?overdue=true` returned 7 tasks with **no due date** alongside the genuinely overdue one | Violates "tasks with no due date are never overdue" (defect 6) |
| 4 | Delete | **FAIL** | Happy path: `DELETE /tasks/t0013` → `204`, then `GET` → `404` (correct). But `DELETE /tasks/t9999` (never existed) → `204`, and deleting the already-deleted `t0013` again → `204` | Should be `404` for unknown id (defect 5) |
| 5 | Migration | **FAIL** | Migrated `data/tasks.json` (v1→v2): all 6 tasks present with correct id/title/done, `.v1.bak` written correctly, second run is a no-op (verified via log + md5 unchanged). But every migrated task has `created_at: "0001-01-01T00:00:00Z"` (creation time lost) and tags are unnormalized, e.g. `"Health, personal"` → `["Health"," personal"]` instead of `["health","personal"]` | Violates "keeps ... creation time" and "same rules as in create" (defects 7–8) |
| 6 | No regressions | **PASS** (as far as testable) | `go test ./...` → `ok example.com/taskapi 0.246s`, all 9 tests pass; `GET /healthz`, `GET /tasks`, `GET /tasks/{id}` all behave correctly | Not a git repo, no prior version to diff against; passing tests don't cover the gaps below |

## Defects

**1. PATCH wipes `due`/`tags` when they're absent from the body (critical).**
Repro: `POST /tasks -d '{"title":"Original title","due":"2026-11-01","tags":["work","urgent"]}'` → then `PATCH /tasks/{id} -d '{"title":"New title"}'`.
Expected: `due` and `tags` unchanged. Actual: `due:""`, `tags:null`.
Cause: `handlers.go` `patchTask` unconditionally does `t.Due = due; t.Tags = req.Tags` regardless of whether those keys were present in the JSON body.

**2. PATCH doesn't validate/normalize tags (critical).**
Repro: `PATCH /tasks/{id} -d '{"tags":["Home"," Errands "]}'` → tags stored as-is, not lowercased/trimmed. `PATCH ... -d '{"tags":["a","b","c","d","e","f"]}'` (6 tags) → `200`, should be `400`.
Cause: `patchTask` assigns `req.Tags` directly instead of calling `normalizeTags`.

**3. PATCH ignores JSON decode errors (critical).**
Repro: `PATCH /tasks/{id} -d 'not json'` → `200` (and wipes fields), should be `400`.
Cause: `json.NewDecoder(r.Body).Decode(&req)` return value is discarded.

**4. PATCH on unknown id returns 500, not 404 (critical).**
Repro: `PATCH /tasks/t9999 -d '{"title":"x"}'` → `500 {"error":"could not save"}`.
Cause: `store.go` `Update` does `fmt.Errorf("update %s: %v", id, ErrNotFound)` (`%v` not `%w`), so `errors.Is(err, ErrNotFound)` in `patchTask` never matches, falling through to the generic 500 branch.

**5. DELETE on unknown/already-deleted id returns 204, not 404 (critical).**
Repro: `DELETE /tasks/t9999` → `204`. Delete the same id twice → `204` both times.
Cause: `store.go` `Delete` calls `delete(s.tasks, id)` and saves unconditionally, never checking whether the id existed.

**6. Overdue filter includes tasks with no due date (critical).**
Repro: `GET /tasks?overdue=true` on data containing not-done tasks with empty `due` → they appear in the result.
Cause: `listTasks` skip condition is `t.Done || t.Due.After(today)`; a zero `Date` (no due date) is not "after" today, so it's never skipped.

**7. Migration loses creation timestamps (major).**
Repro: migrate the shipped `data/tasks.json` → every task's `created_at` becomes `0001-01-01T00:00:00Z` instead of the original v1 `created` value.
Cause: `migrate.go` builds `&Task{ID: o.ID, Title: o.Title, Done: o.Done}`, omitting `CreatedAt: o.Created`.

**8. Migration doesn't normalize tags (major).**
Repro: v1 label `"Health, personal"` → migrated tags `["Health"," personal"]` instead of `["health","personal"]`.
Consequence: `GET /tasks?tag=errands` fails to return the migrated task whose tag is `" Errands"`, even though "Errands" is visibly one of its labels — a directly observable, user-facing symptom of this bug.
Cause: `migrate.go` does `t.Tags = strings.Split(o.Labels, ",")` without passing through `normalizeTags`.

**Why `go test ./...` didn't catch these:** `TestPatchMarksDone` only patches `{"done":true}` on a task with empty due/tags (wipe is invisible). `TestOverdue` never creates a no-due-date task. There are no tests for PATCH/DELETE on an unknown id. `TestMigrateV1` uses an already-clean, single-word, lowercase label and never checks `CreatedAt`.

## Note on environment
Two background `go run .` instances I started for testing (ports 8091, 8093, both localhost-only) could not be terminated from this session due to a sandbox restriction on signaling processes from a different tool invocation. They're harmless but you may want to kill them manually (`lsof -i :8091`, `lsof -i :8093`).

This needs another pass from the developer before it can merge — recommend focusing on `patchTask` (defects 1–4), `Store.Delete` (5), the overdue check (6), and `Migrate` (7–8).
