Not ready to merge yet. `go test ./...` passes, but that's only because the existing test suite doesn't exercise several acceptance-criteria paths. I found 6 real bugs, confirmed each with a failing repro test (now removed, no files left behind):

**PATCH (`handlers.go:143-170`)**
1. **Wipes fields not present in the body.** `t.Due = due` and `t.Tags = req.Tags` run unconditionally, so `PATCH {"title":"y"}` erases the task's existing due date and tags. Violates "fields not present in the body keep their values."
2. **Tags aren't normalized.** `t.Tags = req.Tags` skips `normalizeTags`, so PATCH lets tags in with whitespace/mixed case and bypasses the max-5 check — inconsistent with create and breaks the case-insensitive tag filter.
3. **Malformed JSON body doesn't 400.** The decode error is discarded (`json.NewDecoder(r.Body).Decode(&req)`), so `PATCH` with invalid JSON returns 200 instead of 400.

**Filter (`handlers.go:75-90`)**
4. **No-due-date tasks show as overdue.** The exclusion check `t.Due.After(today.Time)` treats the zero-value `Date{}` (year 1) as "not after today," so tasks with no due date incorrectly appear when `overdue=true`. Spec says they should never be overdue.
5. **Due-today tasks show as overdue.** Same check doesn't exclude `Due == today`; spec requires "before today."

**Delete (`handlers.go:172-178`)**
6. **Unknown id returns 204, not 404.** `Store.Delete` silently no-ops on a missing key and never surfaces `ErrNotFound`, so the handler always returns 204.

**Migration (`migrate.go:41-51`)**
7. **`CreatedAt` is dropped.** The new `Task{}` never copies `o.Created`, so migrated tasks lose their creation time, contradicting "every task keeps its ... creation time."
8. **v1 labels aren't normalized.** `strings.Split(o.Labels, ",")` skips trim/lowercase/max-5, so migrated tags don't follow "the same rules as in create."

None of these are covered by the current test suite, which is why `go test ./...` passes despite the bugs. I'd send this back rather than merge — the PATCH and migration issues in particular can silently destroy user data (due dates, tags, creation timestamps).
