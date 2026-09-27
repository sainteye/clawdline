# TICKET-142: Due dates, tags and editing for tasks

Today a task has only a title and a done flag, and `labels` is a free-text string. Users want
due dates, proper tags, a way to edit a task, and a list of what is overdue.

## Acceptance criteria

1. **Create.** `POST /tasks` accepts an optional `due` (`YYYY-MM-DD`) and optional `tags` (array
   of strings). Tags are trimmed and stored lowercase; at most 5 tags. Invalid input returns
   `400` with a JSON `{"error": ...}` body. Success returns `201` with the task and a `Location`
   header.
2. **Edit.** `PATCH /tasks/{id}` changes only the fields present in the body (`title`, `due`,
   `tags`, `done`); fields not in the body keep their values. Tags follow the same rules as in
   create. An invalid body returns `400`, an unknown id returns `404`, and success returns `200`
   with the updated task.
3. **Filter.** `GET /tasks?tag=<t>` returns tasks carrying tag `<t>` (case-insensitive).
   `GET /tasks?overdue=true` returns tasks that are not done and whose due date is before today
   in the service's time zone (`TZ_NAME`, default `Asia/Taipei`). Tasks with no due date are
   never overdue.
4. **Delete.** `DELETE /tasks/{id}` returns `204`; an unknown id returns `404`.
5. **Migration.** On start-up, an existing v1 data file is upgraded to v2 without losing
   anything: every task keeps its id, title, done flag and creation time, and v1 `labels`
   become tags under the same rules as in create. The original file is kept as
   `<file>.v1.bak`. Running the service again on a migrated file changes nothing.
6. **No regressions.** Existing endpoints behave as before and `go test ./...` passes.

Out of scope: authentication, pagination, recurring tasks.
