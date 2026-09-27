I wouldn't merge this yet. `go test ./...` passes, but that's only because the existing tests don't exercise the paths where the acceptance criteria actually break. I wrote and ran targeted tests against a scratch copy of the code (not committed) to confirm each issue below — all reproduced.

**PATCH (`handlers.go:143-170`) is broken in three ways:**
1. It unconditionally overwrites `due` and `tags` on every PATCH, even when those fields are absent from the body — because `Due`/`Tags` in `patchReq` aren't pointers, there's no way to tell "omitted" from "empty". Result: `PATCH {"done":true}` on a task that had a due date and tags silently wipes both to empty. Directly violates "changes only the fields present in the body."
2. The JSON decode error is discarded (`json.NewDecoder(r.Body).Decode(&req)` — no `err` check), so a malformed body (`not json`) returns `200` instead of the required `400`.
3. `normalizeTags` (trim/lowercase/max-5) is never called in PATCH — tags sent via PATCH go through unnormalized (`["Work"," errands "]` stays as-is instead of becoming `work,errands`).

**PATCH on an unknown id returns 500, not 404** (`store.go:132`): `Store.Update` builds the not-found error with `fmt.Errorf("... %v", ErrNotFound)` — `%v` instead of `%w` — so it doesn't wrap `ErrNotFound`, and `errors.Is` in `handlers.go:161` never matches.

**DELETE on an unknown id returns 204, not 404** (`store.go:141-146` / `handlers.go:172-178`): deleting a nonexistent key from a Go map is a no-op, not an error, so `Store.Delete` always succeeds and the handler always returns `204`.

**Overdue filter is wrong on two edges** (`handlers.go:84`): the skip condition `t.Done || t.Due.After(today)` means a task with **no due date** (zero-value `Date`, which is always "before" today) is treated as overdue, and a task due **exactly today** is also treated as overdue. The spec explicitly requires "tasks with no due date are never overdue" and overdue means due *before* today, not on-or-before.

**Migration drops `created_at`** (`migrate.go:42`): `&Task{ID: o.ID, Title: o.Title, Done: o.Done}` never sets `CreatedAt: o.Created`, so every migrated task loses its original creation timestamp — contradicts "every task keeps its ... creation time."

**Migration doesn't normalize tags** (`migrate.go:44`): it does a raw `strings.Split(o.Labels, ",")` without calling `normalizeTags`, so labels like `"Health, personal"` become tags `["Health", " personal"]` instead of `["health", "personal"]` — contradicts "become tags under the same rules as in create." This will visibly corrupt the real `data/tasks.json` on first migration (e.g. `"home, Errands"`, `"personal,finance"`).

None of this shows up in `go test ./...` because the existing tests only patch tasks that never had due/tags set, never PATCH/DELETE an unknown id, never check overdue with a no-due-date or due-today task, and never assert on `CreatedAt` or tag casing after migration.

This isn't ready to merge — the PATCH and DELETE bugs are core CRUD correctness issues, and the migration bugs will corrupt real data on first run. I'd send it back rather than fix it silently, since the developer should know their test coverage missed all of this — happy to fix it myself if you'd like.
