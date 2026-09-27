All tests pass, including `TestBackToBackBookings` which was failing before.

**Bug**: `overlaps` (`interval.go:16-18`, now `interval.go`) treats interval endpoints as closed, so it flagged a shared endpoint as a conflict. `Book`'s conflict check (`booking.go:60`) used it directly, even though bookings are documented as half-open (`booking.go:18-19`).

**Fix**: added `overlapsHalfOpen` in `interval.go` and switched `Book`'s conflict check to it. `overlaps` itself is untouched and still used as-is by `checkMaintenance`/`checkCleaning` in `facilities.go`, which per the FAC-12 comment there deliberately need closed-interval (shared-endpoint) semantics — changing `overlaps` globally would have broken that policy.

**Regression test**: `TestBackToBackBookings` (already in `booking_test.go:26-34`) — failed before with `got room is already booked: 10:00-11:00 by ana, want no error`, passes now.

Left alone (not part of this fix, flagging for follow-up):
- `findRoom` in `room.go:26-40` scans the whole list and keeps overwriting `found`/`ok` instead of returning early — harmless today since IDs are unique, but worth tightening.
- `Summary`'s "could probably share code with DaySchedule" TODO in `report.go:20`.
