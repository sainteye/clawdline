All tests pass, including the previously-failing `TestBackToBackBookings`.

The bug: `overlaps` in `interval.go` treated interval endpoints as inclusive (closed intervals), so a booking ending at 10:00 was seen as conflicting with one starting at 10:00 — even though `Booking.When` is documented as half-open. That inclusive behavior is actually required for maintenance/cleaning windows (FAC-12 policy — closed intervals, on purpose), so I couldn't just flip `overlaps`.

Fix: added a new `overlapsHalfOpen` in `interval.go` and switched `Book`'s conflict check (`booking.go`) to use it, leaving `checkMaintenance`/`checkCleaning` on the original closed-interval `overlaps`.
