package app

import (
	"context"
	"testing"
	"time"
)

// A schedule moved to another machine is made there afresh (docs/schedules.md,
// "Moving a schedule"): the console disables the source, creates the copy on
// the target and deletes the source. The copy must not run the occurrence the
// source was responsible for, even when that occurrence is still inside the
// catch-up window — otherwise a move made at 09:00:30 runs the 09:00 task a
// second time on the target. It holds because a created schedule is first seen
// the instant it is made (`CreateScheduleFile(…, now, …)`) and carries
// `created_at` now, and an occurrence before either is not this machine's.
func TestAScheduleCreatedByAMoveDoesNotRunThePastOccurrence(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	// 09:00:30, six hours of catch-up: an occurrence the source had not run
	// yet would still be due here.
	made := f.book.Create(ctx, map[string]any{
		"title": "moved", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true,
		"catch_up_hours": 6, "close_tab": "on_success", "notify_on_failure": true,
		"timeout_minutes": 30,
	}, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	if p := f.book.Beat(ctx); p.Considered != 1 || p.Due != 0 || p.Fired != 0 {
		t.Fatalf("the beat after the move: %+v, want the schedule considered and nothing due", p)
	}
	// The next day's occurrence is the target's own.
	f.set(f.at().Add(24 * time.Hour))
	if p := f.book.Beat(ctx); p.Due != 1 {
		t.Fatalf("the next day's beat: %+v, want its occurrence due", p)
	}
}
