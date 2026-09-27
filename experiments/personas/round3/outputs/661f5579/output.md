I've gathered enough evidence. Note: the sandbox blocks `kill`/`pkill` for processes outside the current tool call, so three orphaned `taskapi` background processes remain from this session (PIDs 50576, 69847, 70299, listening on ports 8091/8193/8294). You may want to terminate them manually (e.g. `kill 50576 69847 70299` from a normal terminal).

## Verdict: **FAIL** — needs work, not ready to merge

Multiple acceptance criteria are violated despite `go test ./...` passing — the test suite doesn't cover the broken paths. All evidence below is from building, running, and curling the actual service.

## Criterion-by-criterion

| # | Criterion | Result | Evidence | Notes |
|---|---|---|---|---|
| 1 | Create (`POST /tasks`) | **PASS** | `curl -i POST /tasks {"title":"Buy milk","due":"2026-10-01","tags":["Home"," Errands "]}` → `201`, `Location: /tasks/t0001`, tags normalized to `["home","errands"]`. Invalid cases (empty title, bad date, 6 tags, malformed JSON, blank tag) all → `400` with `{"error":...}`. | Fully correct. |
| 2 | Edit (`PATCH /tasks/{id}`) | **FAIL** | See defects #1–#4 below. | Partial update, 404, JSON validation, and tag rules are all broken. |
| 3 | Filter (`tag=`, `overdue=true`) | **FAIL** (partial) | Tag filter: `GET /tasks?tag=WORK` correctly matches a task tagged `work` (case-insensitive) — PASS. Overdue: see defect #5 — FAIL. | |
| 4 | Delete (`DELETE /tasks/{id}`) | **FAIL** | Happy path → `204`, then `GET` → `404` (PASS). But `DELETE /tasks/t9999` (unknown id) → `204`, not `404`. See defect #6. | |
| 5 | Migration | **FAIL** | Migrated `data/tasks.json` (v1→v2): tags not trimmed/lowercased, and `created_at` reset to `0001-01-01T00:00:00Z` for every task, losing the original creation time. Re-running against the migrated file is a no-op (byte-identical) — that part passes. See defects #7–#8. | |
| 6 | No regressions | **PASS** (suite) / **misleading** | `go test ./...` → `ok example.com/taskapi 0.261s`, all 9 tests pass. `go build ./...` and `go vet ./...` also clean. | The suite passes only because it never exercises PATCH-unknown-id, PATCH-omits-field, DELETE-unknown-id, no-due-date overdue, or migration field/tag correctness — all of which are broken. |

## Defects

**1. PATCH silently wipes `due` and `tags` when they're omitted from the body** (violates AC2's core "changes only fields present in the body" rule)
- Repro: `POST /tasks {"title":"Task2","due":"2026-10-05","tags":["work","urgent"]}` → then `PATCH /tasks/t0002 {"done":true}`
- Expected: due/tags unchanged (`"due":"2026-10-05"`, `"tags":["work","urgent"]`)
- Actual: `{"id":"t0002","title":"Task2","done":true,"due":"","tags":null,...}`
- Cause: `handlers.go` `patchTask` unconditionally does `t.Due = due` (from `ParseDate(req.Due)`, which returns zero value for `""`) and `t.Tags = req.Tags` (nil when absent), with no "was this field present in the body" check — unlike the `Title` field, which correctly uses `if req.Title != ""`.

**2. PATCH on an unknown id returns 500, not 404**
- Repro: `curl -i -X PATCH /tasks/t9999 -d '{"done":true}'`
- Expected: `404 {"error":"task not found"}`
- Actual: `500 {"error":"could not save"}`
- Cause: `store.go` `Update()` does `fmt.Errorf("update %s: %v", id, ErrNotFound)` — using `%v` instead of `%w`, so `errors.Is(err, ErrNotFound)` in `patchTask` never matches, falling through to the generic 500 branch.

**3. PATCH ignores malformed JSON instead of returning 400**
- Repro: `curl -i -X PATCH /tasks/t0001 -d 'not json'`
- Expected: `400`
- Actual: `200`, and it wipes due/tags (per defect #1) because the decode error is discarded: `json.NewDecoder(r.Body).Decode(&req)` in `patchTask` (unlike `createTask`, which checks this error).

**4. PATCH doesn't validate or normalize tags** ("Tags follow the same rules as in create" — not implemented for PATCH)
- Repro: `curl -X PATCH /tasks/t0001 -d '{"tags":["a","b","c","d","e","f"]}'` → accepted with `200` and 6 raw tags stored (should be `400`, max 5).
- Repro: `curl -X PATCH /tasks/t0002 -d '{"tags":["  Work  ","URGENT"]}'` → stored verbatim as `["  Work  ","URGENT"]` instead of `["work","urgent"]`.
- Cause: `normalizeTags` is called in `createTask` but never called in `patchTask`.

**5. Tasks with no due date are treated as overdue** (directly contradicts AC3: "Tasks with no due date are never overdue")
- Repro: create a task with no `due`, then `GET /tasks?overdue=true`
- Expected: task excluded
- Actual: included — output includes `{"id":"t0001",...,"due":"",...}` and `{"id":"t0004","title":"NoDue","due":"",...}` in the overdue list.
- Cause: `listTasks` skip condition is `t.Done || t.Due.After(today.Time)`; a zero `Date` is never "after" today, so it's never filtered out. Needs an explicit `t.Due.IsZero()` check.

**6. DELETE on an unknown id returns 204, not 404**
- Repro: `curl -i -X DELETE /tasks/t9999`
- Expected: `404`
- Actual: `204 No Content`
- Cause: `store.Delete` calls `delete(s.tasks, id)` (a no-op on Go maps for missing keys) without checking existence first, then always saves and returns `nil`.

**7. Migration doesn't normalize tags from v1 `labels`** (violates "same rules as in create": trim + lowercase)
- Repro: migrated the real `data/tasks.json` (labels `"Health, personal"`, `"home, Errands"`, etc.)
- Expected: tags `["health","personal"]`, `["home","errands"]`
- Actual: tags `["Health"," personal"]`, `["home"," Errands"]` — untrimmed, uncased, and the `" personal"`/`" Errands"` entries have a leading space baked in.
- Cause: `migrate.go` does `strings.Split(o.Labels, ",")` with no call to `normalizeTags`.

**8. Migration loses each task's original creation time** (violates AC5: "every task keeps its id, title, done flag and creation time")
- Repro: same migration; v1 file has e.g. `"created":"2025-10-03T01:12:44Z"`
- Expected: migrated task's `created_at` = `2025-10-03T01:12:44Z`
- Actual: every migrated task has `"created_at":"0001-01-01T00:00:00Z"`.
- Cause: `migrate.go` builds `&Task{ID: o.ID, Title: o.Title, Done: o.Done}` and never copies `o.Created` into `CreatedAt`.

## Summary
AC1 (Create) and the case-insensitive part of AC3 (tag filter) work correctly. Everything else the developer claimed as "done" has real bugs: PATCH's partial-update semantics, 404 handling, and JSON/tag validation are all broken; overdue filtering incorrectly flags no-due-date tasks; and migration silently drops creation timestamps and skips tag normalization. `go test ./...` passing is not meaningful evidence here since the existing tests never touch these paths — recommend the developer add tests for each defect above before resubmitting.
