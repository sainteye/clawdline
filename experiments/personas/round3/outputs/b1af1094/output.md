The real `data/tasks.json` was never touched — all testing used isolated copies in `$TMPDIR`. Note: three background test servers (ports 8099/8100/8101) are still running; the sandbox blocked my `kill` calls with "operation not permitted." They're harmless (127.0.0.1-only, isolated temp data files), but you may want to stop them manually from your own terminal (`kill 56588 57619 57793`, or by port).

## Verdict: **FAIL — needs work**

The developer's summary ("done... all tests pass... ready to merge") is not supported by behavior. `go test ./...` passing is real, but the existing suite has significant blind spots that let multiple criteria-violating bugs through. Manual endpoint testing found confirmed defects in 4 of 6 criteria.

## Criteria table

| # | Criterion | Result | Evidence (command → output) | Notes |
|---|---|---|---|---|
| 1 | Create | **PASS** | `POST /tasks -d '{"title":"Buy milk","due":"2026-10-05","tags":[" Home "," ERRANDS "]}'` → `201`, `Location: /tasks/t0001`, `tags:["home","errands"]`. Bad date → `400 {"error":"due must be YYYY-MM-DD"}`. 6 tags → `400 {"error":"at most 5 tags"}`. `not json` → `400 {"error":"invalid JSON body"}`. Empty title → `400 {"error":"title is required"}` | Works as specified |
| 2 | Edit (PATCH) | **FAIL** | See defects #1–#4 below | Partial update, validation, and 404 all broken |
| 3 | Filter — tag | **PASS** | `GET /tasks?tag=WORK` and `?tag=work` both return the same tagged task; `?tag=nonexistent` → `[]` | Case-insensitive as required |
| 3 | Filter — overdue | **FAIL** | See defect #5 below | Includes no-due-date and due-today tasks |
| 4 | Delete | **FAIL (partial)** | Happy path: `DELETE /tasks/t0002` → `204`, then `GET /tasks/t0002` → `404`. Unknown id: `DELETE /tasks/t9999` → `204` (should be `404`) | See defect #6 |
| 5 | Migration | **FAIL (partial)** | v1 file migrated; backup byte-identical to original (`diff` confirms); ids/titles/done preserved; second run is a no-op (`diff` before/after identical). But `created_at` becomes `0001-01-01T00:00:00Z` for every task, and tags keep original case/whitespace (`"Health"`, `" personal"`, `" Errands"`, `" planning"`) | See defects #7–#8 |
| 6 | No regressions / `go test ./...` | **PASS (surface only)** | `go test ./... -v` → 9/9 tests pass | Suite doesn't exercise any of the bugs below — green tests, not proof |

## Defects

**#1 — PATCH silently wipes `due` and `tags` when they're absent from the body**
- Repro: `POST /tasks -d '{"title":"Base task","due":"2026-12-25","tags":["work","urgent"]}'` (→ t0003), then `PATCH /tasks/t0003 -d '{"title":"Renamed task"}'`
- Expected: `due` stays `"2026-12-25"`, `tags` stays `["work","urgent"]` (spec: "fields not in the body keep their values")
- Actual: `{"due":"","tags":null,...}` — both destroyed
- Cause: `handlers.go` `patchTask` unconditionally does `t.Due = due; t.Tags = req.Tags` regardless of whether the JSON body contained those keys; plain-string/slice fields can't distinguish "absent" from "empty."

**#2 — PATCH on unknown id returns 500, not 404**
- Repro: `PATCH /tasks/t9999 -d '{"title":"x"}'`
- Expected: `404`
- Actual: `500 {"error":"could not save"}`
- Cause: `store.go` `Update` does `fmt.Errorf("update %s: %v", id, ErrNotFound)` — `%v` instead of `%w`, so `errors.Is(err, ErrNotFound)` in the handler returns false and falls into the 500 branch.

**#3 — PATCH with invalid JSON body returns 200, not 400**
- Repro: `PATCH /tasks/t0003 -d 'not json'`
- Expected: `400`
- Actual: `200` with the task unchanged (decode error is discarded: `json.NewDecoder(r.Body).Decode(&req)` return value ignored)

**#4 — PATCH doesn't validate/normalize tags at all**
- Repro: `PATCH /tasks/t0004 -d '{"tags":["a","b","c","d","e","f"]}'` (6 tags) → `200`, saved with 6 tags (spec: max 5, should be `400`). `PATCH /tasks/t0004 -d '{"tags":[" Foo ","BAR"]}'` → `200`, saved as `[" Foo ","BAR"]` verbatim (spec: trim + lowercase)
- Cause: `patchTask` assigns `req.Tags` directly, never calling `normalizeTags` (unlike `createTask`)

**#5 — Overdue filter includes tasks that shouldn't be overdue**
- Repro (server clock = 2026‑09‑27, Asia/Taipei): created tasks with due `2026-09-20` (past), `2026-09-27` (today), `2026-10-05` (future), and no due date. `GET /tasks?overdue=true`
- Expected: only the past-due task
- Actual: past-due task, **the no-due-date task**, and **the due-today task** all appear
- Cause: `listTasks` excludes with `t.Due.After(today.Time)`. A zero `Date{}` (no due date, year 0001) is never `After` today, so it's never excluded — violates "tasks with no due date are never overdue." And a due date equal to today is also never `After` today, so it's kept — should require strictly `Before` today.

**#6 — DELETE on unknown id returns 204, not 404**
- Repro: `DELETE /tasks/t9999`
- Expected: `404`
- Actual: `204 No Content`
- Cause: `Store.Delete` calls Go's `delete(map, key)`, which is a no-op for a missing key and never returns an error, so the handler can never observe "not found."

**#7 — Migration drops `CreatedAt`**
- Repro: migrate real `data/tasks.json` (v1, `"created":"2025-10-03T01:12:44Z"` for t0001), inspect resulting v2 file
- Expected: `created_at` preserved (spec: "every task keeps its ... creation time")
- Actual: every migrated task has `"created_at":"0001-01-01T00:00:00Z"`
- Cause: `migrate.go` builds `&Task{ID: o.ID, Title: o.Title, Done: o.Done}` and never copies `o.Created`

**#8 — Migration doesn't normalize tags from v1 `labels`**
- Repro: same migration; v1 `labels` were `"Health, personal"`, `"home, Errands"`, `"work, planning"`
- Expected: tags trimmed + lowercased, same rule as create (spec: "become tags under the same rules as in create")
- Actual: resulting tags are `["Health"," personal"]`, `["home"," Errands"]`, `["work"," planning"]` — untrimmed, uncased
- Cause: `migrate.go` does `t.Tags = strings.Split(o.Labels, ",")` with no call to `normalizeTags`

Idempotency itself (`Migrate` run twice → second run is a no-op, verified with `diff`) does hold, but it's a no-op on top of already-wrong data.

## What you tested against
- Commit/build: working tree as checked out (no git repo present), `go1.27.1 darwin/arm64`, `go build ./...` clean.
- Server run via `go run .` on `127.0.0.1:8099/8100/8101`, `TZ_NAME=Asia/Taipei`, isolated `DATA` files under `$TMPDIR` — never the real `data/tasks.json` (confirmed unchanged via `diff` at the end).
- Not measured: concurrency/race behavior on the store, and behavior when `TZ_NAME` is set to a non-default zone (only default `Asia/Taipei` was exercised).

**Recommendation:** send back to the developer. The PATCH handler needs a real fix (track which fields were present, e.g. via `json.RawMessage`/pointers, call `normalizeTags`, use `%w` for the not-found wrap, and check the JSON decode error), the overdue comparison needs to use `Due.Before(today)` while still special-casing zero dates, `Store.Delete` needs to check existence before deleting, and `Migrate` needs to copy `Created` into `CreatedAt` and run labels through `normalizeTags`.
