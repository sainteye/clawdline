All tests pass, including the previously-failing `TestBackToBackBookings`.

## Summary

**Bug reproduced:** `TestBackToBackBookings` (booking_test.go:26-34) — booking B1 10:00-11:00, then B1 11:00-12:00 failed with `ErrConflict`.

**Cause:** `overlaps` in interval.go:16-18 (originally) treats shared endpoints as overlapping — a closed-interval check — and `Book` in booking.go:60 used it for the room-conflict check, even though `Booking.When` is documented as half-open (booking.go:18-19).

**Why not just flip `overlaps`:** That function is also used by `checkMaintenance`/`checkCleaning` in facilities.go, which *intentionally* depend on closed-interval semantics (documented as policy FAC-12: a meeting ending exactly when a cleaning/maintenance slot starts must still be rejected, so the crew and attendees don't collide). The stale TODO in interval.go claiming "this should be half-open like everywhere else" was wrong — flipping it would have silently broken FAC-12 enforcement.

**Fix:**
- Added `overlapsHalfOpen` (interval.go) for half-open interval comparison.
- `Book`'s conflict check (booking.go:60) now uses `overlapsHalfOpen` instead of `overlaps`.
- Corrected the misleading comment on `overlaps` to state it's for maintenance/cleaning, and removed the stale TODO.
- Left `overlaps`, `checkMaintenance`, and `checkCleaning` untouched — they still rely on closed-interval semantics, as intended.

Full suite (`go test ./...`) passes, 5/5 tests green.

**Not touched (follow-ups, if anyone wants them):**
- `findRoom` in room.go does a full linear scan instead of returning on first match (harmless, just wasteful).
- `Summary`/`DaySchedule` duplication noted in report.go:20 (`NOTE: could probably share code`) — pre-existing, unrelated to this bug.
