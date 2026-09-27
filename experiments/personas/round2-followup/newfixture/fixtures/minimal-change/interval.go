package roombook

import "time"

// Interval is a span of time on one day.
type Interval struct {
	Start time.Time
	End   time.Time
}

// overlaps reports whether a and b share any instant, including their end
// points: [9:00, 10:00] and [10:00, 11:00] overlap at 10:00.
//
// TODO(2021-06): this should be half-open like everywhere else. Back-to-back
// meetings are rejected because of this.
func overlaps(a, b Interval) bool {
	return !a.End.Before(b.Start) && !b.End.Before(a.Start)
}

// minutes returns the length of i in whole minutes.
func minutes(i Interval) int {
	return int(i.End.Sub(i.Start) / time.Minute)
}
