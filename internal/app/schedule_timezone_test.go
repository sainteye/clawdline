package app

import (
	"context"
	"testing"
	"time"
)

func TestScheduleUsesItsNamedTimeZoneInsteadOfTheMachineZone(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.set(time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC))
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	made := f.book.Create(ctx, map[string]any{
		"title": "Taipei morning", "at": "09:00", "days": "daily", "time_zone": "Asia/Taipei",
		"place_id": "p1", "assistant": "claude", "instructions": "do it", "enabled": true,
	}, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	summary := made.Body["schedule"].(map[string]any)
	want := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC).Unix()
	if got := summary["next_fire"]; got != want {
		t.Fatalf("next_fire = %v, want %d (09:00 Asia/Taipei)", got, want)
	}

	f.set(time.Date(2026, 9, 18, 1, 0, 30, 0, time.UTC))
	if pulse := f.book.Beat(ctx); pulse.Due != 1 || pulse.Fired != 1 {
		t.Fatalf("09:00 Asia/Taipei beat = %+v, want one firing", pulse)
	}
}

func TestScheduleTimeZoneIsValidatedAndReturned(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true}
	body["time_zone"] = "not/a-zone"
	if reply := f.book.Create(ctx, body, ScheduleAuthority{}); reply.Code != "bad_request" {
		t.Fatalf("invalid time zone answered %q %q", reply.Code, reply.Message)
	}
	body["time_zone"] = "Asia/Taipei"
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	record, ok, err := f.book.Detail(ctx, id)
	if err != nil || !ok || record["time_zone"] != "Asia/Taipei" {
		t.Fatalf("detail = %v, %v, %v; want named time zone", record, ok, err)
	}
}

func TestChangingOnlyScheduleTimeZoneIsARetime(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true}
	body["time_zone"] = "UTC"
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	f.set(f.at().Add(time.Minute))
	body["time_zone"] = "Asia/Taipei"
	if reply := f.book.Update(ctx, id, body, ScheduleAuthority{}); reply.Code != "" {
		t.Fatalf("update refused: %s %s", reply.Code, reply.Message)
	}
	h, ok, err := f.book.named(ctx, id)
	if err != nil || !ok || h.s.WhenChangedAt.IsZero() {
		t.Fatalf("time-zone-only update did not stamp a retime: %+v, %v, %v", h.s, ok, err)
	}
}

func TestOlderScheduleClientDoesNotEraseAStoredTimeZone(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true, "time_zone": "Asia/Taipei"}
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	delete(body, "time_zone") // the body sent by a console from before this field existed
	body["title"] = "renamed"
	if reply := f.book.Update(ctx, id, body, ScheduleAuthority{}); reply.Code != "" {
		t.Fatalf("legacy update refused: %s %s", reply.Code, reply.Message)
	}
	record, ok, err := f.book.Detail(ctx, id)
	if err != nil || !ok || record["time_zone"] != "Asia/Taipei" {
		t.Fatalf("legacy update erased the zone: %v, %v, %v", record, ok, err)
	}
}
