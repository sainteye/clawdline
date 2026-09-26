package app

import (
	"context"
	"fmt"
	"strings"
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

// moveBody is the flat create body a move sends, with the source's hidden
// template fields in `template`.
func moveBody(assistant string, template any) map[string]any {
	return map[string]any{
		"title": "moved", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": assistant, "instructions": "do it", "enabled": true,
		"template": template,
	}
}

func storedTask(t *testing.T, f *w4Fixture, made ScheduleReply) map[string]any {
	t.Helper()
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	summary, _ := made.Body["schedule"].(map[string]any)
	id, _ := summary["id"].(string)
	record, ok, err := f.book.Detail(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("the made schedule could not be read back: %v %v", ok, err)
	}
	task, _ := record["task"].(map[string]any)
	return task
}

// A schedule carrying settings the form has no control for arrives on the
// target with them: the create takes them in `template` and the file says
// exactly what the source's file said.
func TestACreateCarriesTheTemplateFieldsAMoveBrings(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	task := storedTask(t, f, f.book.Create(ctx, moveBody("codex", map[string]any{
		"claims": []any{"web"}, "serialize": []any{"nightly"}, "deliverables": []any{"docs/report.md"},
		"isolation": "worktree", "plan": "step one", "reasoning_effort": "high",
	}), ScheduleAuthority{}))
	for key, want := range map[string]string{
		"claims": "[web]", "serialize": "[nightly]", "deliverables": "[docs/report.md]",
		"isolation": "worktree", "plan": "step one", "reasoning_effort": "high",
	} {
		if got := fmt.Sprint(task[key]); got != want {
			t.Errorf("task.%s = %s, want %s", key, got, want)
		}
	}
	// reasoning_effort belongs to codex; a copy made for another assistant
	// drops it, as a save that changes the assistant does.
	other := storedTask(t, f, f.book.Create(ctx, moveBody("claude", map[string]any{
		"deliverables": []any{"docs/report.md"}, "reasoning_effort": "high",
	}), ScheduleAuthority{}))
	if _, kept := other["reasoning_effort"]; kept || fmt.Sprint(other["deliverables"]) != "[docs/report.md]" {
		t.Fatalf("a claude copy's task: %v", other)
	}
}

// What a template may not say is refused by name, and nothing is written.
func TestACreateTemplateRefusesWhatTheFormCouldNotGrant(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	cases := []struct {
		name     string
		template any
		code     string
		says     string
	}{
		{"permission", map[string]any{"permission_mode": "full", "claims": []any{"web"}},
			"template_permission_mode", "permission_mode"},
		{"a form field", map[string]any{"model": "x", "project_dir": "/home/bob/elsewhere"},
			"bad_request", "unknown template field: model, project_dir"},
		{"not an object", []any{"claims"}, "bad_request", "template must be an object"},
		// The parser stays the authority over each value.
		{"a value the parser refuses", map[string]any{"deliverables": "docs/report.md"},
			"bad_request", "task.deliverables must be an array"},
	}
	for _, c := range cases {
		r := f.book.Create(ctx, moveBody("claude", c.template), ScheduleAuthority{})
		if r.Code != c.code || !strings.Contains(r.Message, c.says) {
			t.Errorf("%s: answered %q %q, want %q naming %q", c.name, r.Code, r.Message, c.code, c.says)
		}
	}
	list, err := f.book.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("a refused create left %d schedules (%v)", len(list), err)
	}
}

// A save carries the stored fields itself; `template` is a create's word only.
func TestASaveRefusesATemplate(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	made := f.book.Create(ctx, moveBody("claude", map[string]any{"claims": []any{"web"}}), ScheduleAuthority{})
	summary, _ := made.Body["schedule"].(map[string]any)
	id, _ := summary["id"].(string)
	r := f.book.Update(ctx, id, moveBody("claude", map[string]any{"claims": []any{"api"}}), ScheduleAuthority{})
	if r.Code != "bad_request" || !strings.Contains(r.Message, "template is taken when a schedule is made") {
		t.Fatalf("a save with a template answered %q %q", r.Code, r.Message)
	}
}
