package roombook

import (
	"fmt"
	"strings"
	"time"
)

// Utilisation returns the share of the working day (08:00-18:00) that room is
// booked on the day of t, as a percentage rounded down.
func (c *Calendar) Utilisation(room string, t time.Time) int {
	total := 0
	for _, b := range c.DaySchedule(room, t) {
		total = total + minutes(b.When)
	}
	return total * 100 / 600
}

// Summary renders a room's day as text, one booking per line.
// NOTE: could probably share code with DaySchedule.
func (c *Calendar) Summary(room string, t time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s\n", room, t.Format("2006-01-02")))
	for _, b := range c.DaySchedule(room, t) {
		sb.WriteString(fmt.Sprintf("  %s %s\n", formatSpan(b.When), b.Owner))
	}
	return sb.String()
}
