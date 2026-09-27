Not ready to merge. `go test ./...` passes, but that's because the test suite doesn't exercise several acceptance criteria — I confirmed 5 real bugs against `docs/TICKET-142.md` with reproductions:

1. **PATCH wipes `due` and `tags` when they're not in the request body** (`handlers.go:155-156`). `t.Due = due` and `t.Tags = req.Tags` run unconditionally, so `PATCH {"done":true}` silently clears an existing due date and all tags. Violates AC2 ("fields not in the body keep their values").
2. **PATCH doesn't normalize tags at all** — no trim/lowercase/max-5 check, so `PATCH {"tags":["  Work  "]}` stores `"  Work  "` verbatim. Violates AC2 ("tags follow the same rules as in create").
3. **`DELETE` on an unknown id returns 204, not 404** (`store.go:141-146`) — Go's `delete()` on a missing map key is a no-op and never errors, so `deleteTask` always reports success. Violates AC4.
4. **Overdue filter is off-by-one and includes tasks with no due date** (`handlers.go:84`) — it excludes only `Due.After(today)`, so a task due *today* is (wrongly) included, and a task with no due date (zero value, year 1) is also always included since it's never "after" today. Violates AC3 twice over.
5. **Migration drops `CreatedAt` and doesn't normalize v1 labels** (`migrate.go:42-45`) — `&Task{ID: o.ID, Title: o.Title, Done: o.Done}` never sets `CreatedAt: o.Created`, so creation time is lost for every migrated task. Labels are split on `,` but never trimmed/lowercased, so `" Home , Garden "` becomes tags `" Home "` and `" Garden "` instead of `home`/`garden`. Violates AC5 on both "keeps... creation time" and "same rules as in create".

The existing tests happen to use inputs that don't trigger these paths (e.g. the migration test uses an already-lowercase, whitespace-free label; the overdue test never has a same-day or due-less task; PATCH tests never omit `due`/`tags` on a task that already has them).

Want me to fix these five issues and add regression tests for each before this goes back for merge?
