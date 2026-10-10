package orchestrator

import (
	"testing"
	"time"
)

// **A committed write tells the Cloud line; a pass that wrote nothing does
// not.** The Cloud publisher carries this machine's task list on its machine
// descriptor and used to read `GET /v1/orchestrator/tasks` on every
// five-second pass to find out whether the list had moved: 288 reads in 1,426
// seconds on the running daemon on 2026-10-10, on a machine where no task had
// changed. It waits for this signal instead, so the two halves of the signal's
// contract are what this asserts: every commit is told, and the beat's own
// unchanged call is not — telling there would put the five-second read back
// under another name.
func TestACommittedTaskWriteIsToldAndAnUnchangedChangeIsNot(t *testing.T) {
	b, ctx := newTestBroker(t)
	told := 0
	b.TaskListChanged = func() { told++ }

	id := "a1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	if told != 1 {
		t.Fatalf("a stored row was told %d times", told)
	}

	// A change that commits.
	if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error {
		r.Title = "the title a viewer reads"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if told != 2 {
		t.Errorf("a committed change was told %d times, want 2", told)
	}

	// The beat's call: the change it asked for is already true, so nothing is
	// committed and nothing is told.
	if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error { return errUnchanged }); err != nil {
		t.Fatal(err)
	}
	if told != 2 {
		t.Errorf("an unchanged pass was told; %d calls, want 2 — this is the five-second read coming back", told)
	}

	// A refused change writes nothing either.
	if _, err := b.mutate(ctx, "b2222222-2222-4222-8222-222222222222", "task.test",
		func(r *Record) error { return nil }); err == nil {
		t.Fatal("a change to a task that is not stored was accepted")
	}
	if told != 2 {
		t.Errorf("a refused change was told; %d calls, want 2", told)
	}

	// A daemon with no Cloud link tells nobody, and writes as it always did.
	b.TaskListChanged = nil
	if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error {
		r.Title = "written with nobody listening"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, _, err := b.Record(ctx, id); err != nil || got.Title != "written with nobody listening" {
		t.Fatalf("the write did not land without a listener: %v %v", got.Title, err)
	}
}
