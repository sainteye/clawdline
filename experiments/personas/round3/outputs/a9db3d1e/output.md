Not ready to merge — `go test ./...` passes, but that's because the existing test suite doesn't exercise the edge cases the ticket actually calls out. I wrote targeted probe tests (against the real source, then deleted them) and found **7 confirmed bugs**, all reproducible:

**Overdue filter (AC 3)**
1. Tasks with no due date show up as overdue — `handlers.go:84` compares `t.Due.After(today)`, and a zero-value `Due` is never "after" today, so it slips through. Ticket explicitly says "tasks with no due date are never overdue."
2. A task due *today* is also reported as overdue — the comparison should be `Due.Before(today)`, not "not after."

**PATCH (AC 2)**
3. Omitting `due` from a PATCH body wipes the task's existing due date (`handlers.go:155` sets `t.Due = due` unconditionally instead of only when present).
4. Omitting `tags` wipes existing tags likewise (`t.Tags = req.Tags` unconditionally).
5. Tags sent via PATCH are never trimmed/lowercased/limited — `normalizeTags` isn't called in `patchTask`, so `" Home "` stays as-is and 6+ tags are silently accepted instead of returning 400.

**DELETE (AC 4)**
6. Deleting an unknown id returns `204` instead of `404` — `Store.Delete` calls Go's `delete()` on the map, which is a no-op for a missing key and never surfaces `ErrNotFound`.

**Migration (AC 5)**
7. Migrated tasks lose `CreatedAt` — `migrate.go:42` builds the new `Task` without copying `o.Created`. Also, v1 labels aren't trimmed/lowercased during migration (`strings.Split(o.Labels, ",")` with no normalization), so `"Health, personal"` becomes tags `"Health"` and `" personal"` instead of `"health"`/`"personal"` as required ("same rules as in create").

None of these are caught by `handlers_test.go`/`migrate_test.go` because those tests happen to only cover the passing paths (e.g., `TestOverdue` never has a task with no due date or one due exactly today; `TestPatchMarksDone` never checks that `due`/`tags` survive; `TestMigrateV1` never checks `CreatedAt` and its only non-empty label is already lowercase/trimmed).

Recommend fixing before merge:
- `listTasks`: change overdue check to something like `!t.Due.IsZero() && t.Due.Time.Before(today.Time)`.
- `patchTask`: only assign `t.Due`/`t.Tags` when the corresponding field is present in the JSON body (e.g. using a raw map or `*string`/pointer-based req), and run `normalizeTags` on patch tags too.
- `Store.Delete`: check existence first and return `ErrNotFound` if absent.
- `migrate.go`: copy `o.Created` into `t.CreatedAt`, and run labels through the same trim/lowercase (and ideally reuse `normalizeTags`) as create.

Want me to fix these directly?
