package app

import (
	"context"
	"testing"
	"time"
)

// C5 end to end through the book: a trigger-only schedule is made by the
// form, listed as trigger-only with no next fire, never fired or reported
// missed by the clock across a week, run by hand while enabled, and refused
// by hand once disabled.
func TestATriggerOnlyScheduleIsRunOnlyByHand(t *testing.T) {
	f, audit := permissionFixture(t)
	ctx := context.Background()
	device := ScheduleAuthority{}
	notices := 0
	f.book.Notify = func(context.Context, string, string, string) { notices++ }

	if r := f.book.Create(ctx, formBody(map[string]any{"trigger_only": true}), device); r.Code != "bad_request" {
		t.Fatalf("trigger_only beside a time was not refused: %+v", r)
	}
	body := formBody(map[string]any{"trigger_only": true})
	delete(body, "at")
	delete(body, "days")
	id := madeID(t, f.book.Create(ctx, body, device))

	rows, err := f.book.List(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %v", rows, err)
	}
	if rows[0]["trigger_only"] != true || rows[0]["next_fire"] != nil {
		t.Fatalf("list row: %v", rows[0])
	}
	record, _, _ := f.book.Detail(ctx, id)
	if when, _ := record["when"].(map[string]any); len(when) != 1 || when["trigger_only"] != true {
		t.Fatalf("detail when: %v", record["when"])
	}

	start := f.at()
	for now := start; now.Before(start.Add(7 * 24 * time.Hour)); now = now.Add(23 * time.Minute) {
		f.set(now)
		if p := f.book.Beat(ctx); p.Considered != 1 || p.Due != 0 || p.Fired != 0 {
			t.Fatalf("the clock fired a trigger-only schedule at %s: %+v", now, p)
		}
	}
	for _, line := range *audit {
		if line["event"] == "orchestrator.schedule.skipped" {
			t.Fatalf("the clock recorded a trigger-only schedule: %v", line)
		}
	}
	if notices != 0 || len(f.runs(t, id)) != 0 {
		t.Fatalf("after a week: %d notices, %d runs", notices, len(f.runs(t, id)))
	}

	if r := f.book.Run(ctx, id); r.Code != "" {
		t.Fatalf("a manual run of an enabled trigger-only schedule: %s %s", r.Code, r.Message)
	}
	if got := len(f.runs(t, id)); got != 1 {
		t.Fatalf("runs after a manual run: %d", got)
	}

	disabled := formBody(map[string]any{"trigger_only": true, "enabled": false})
	delete(disabled, "at")
	delete(disabled, "days")
	if r := f.book.Update(ctx, id, disabled, device); r.Code != "" {
		t.Fatalf("disable: %s %s", r.Code, r.Message)
	}
	if r := f.book.Run(ctx, id); r.Code != "schedule_disabled" {
		t.Fatalf("a manual run of a disabled trigger-only schedule: %+v", r)
	}

	// A save gives it a time back, and takes it away again.
	if r := f.book.Update(ctx, id, formBody(nil), device); r.Code != "" {
		t.Fatalf("back to a time: %s %s", r.Code, r.Message)
	}
	if rows, _ := f.book.List(ctx); rows[0]["trigger_only"] != nil || rows[0]["next_fire"] == nil {
		t.Fatalf("after a time was given back: %v", rows[0])
	}
}
