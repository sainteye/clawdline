package store

import (
	"context"
	"testing"
	"time"
)

// A sighting half a second into a schedule's own minute must not be stored as
// that minute: the occurrence would then not be before its sighting, and the
// row would fire on sight.
func TestSightingIsRoundedUp(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	nine := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	if err := st.CreateScheduleFile(ctx, "a", []byte("{}"), nine.Add(400*time.Millisecond), time.Time{}); err != nil {
		t.Fatal(err)
	}
	f, ok, err := st.ScheduleFileByID(ctx, "a")
	if err != nil || !ok {
		t.Fatalf("read back: %v %v", ok, err)
	}
	if !nine.Before(f.FirstSeen) {
		t.Errorf("first_seen %v is not after the 09:00 occurrence it was sighted inside", f.FirstSeen)
	}
}

// One occurrence is claimed once: a second claimant — another pass, a manual
// run, a second daemon on the same state — is told it lost.
func TestOccurrenceIsClaimedOnce(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	fire := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	if err := st.CreateScheduleFile(ctx, "a", []byte("{}"), fire.Add(-time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	first, err := st.ClaimScheduleFire(ctx, "a", fire)
	if err != nil || !first {
		t.Fatalf("first claim: %v %v", first, err)
	}
	if again, _ := st.ClaimScheduleFire(ctx, "a", fire); again {
		t.Error("the same occurrence was claimed twice")
	}
	if err := st.RestoreScheduleFire(ctx, "a", fire, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if back, _ := st.ClaimScheduleFire(ctx, "a", fire); !back {
		t.Error("an occurrence handed back could not be claimed again")
	}
}

// A schedule moved to another machine is deleted here, and its webhook moves
// with it (docs/schedules.md, "Moving a schedule"). The binding must go with
// the schedule: a row naming a deleted schedule would refuse the same hook
// when it is moved back and bound to the new copy (`binding_conflict`). A
// daemon older than this left such rows behind, so a bind replaces a binding
// whose schedule no longer exists instead of refusing on it.
func TestAWebhookBindingLeavesWithItsSchedule(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	at := time.Date(2026, 9, 26, 9, 0, 0, 0, time.Local)
	hook := "swh_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, id := range []string{"first", "second", "third"} {
		if err := st.CreateScheduleFile(ctx, id, []byte("{}"), at, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.BindScheduleWebhook(ctx, hook, "first", "", at); err != nil {
		t.Fatal(err)
	}
	if gone, err := st.DeleteScheduleFile(ctx, "first"); err != nil || !gone {
		t.Fatalf("delete: %v %v", gone, err)
	}
	if _, bound, err := st.ScheduleForWebhook(ctx, hook); err != nil || bound {
		t.Fatalf("the deleted schedule's hook is still bound here: %v %v", bound, err)
	}
	if err := st.BindScheduleWebhook(ctx, hook, "second", "", at); err != nil {
		t.Fatalf("the hook could not be bound again after its schedule was deleted: %v", err)
	}

	// A binding an older daemon left behind: its schedule row is gone, the
	// binding is not.
	if _, err := st.db.ExecContext(ctx, `DELETE FROM schedule_files WHERE id = 'second'`); err != nil {
		t.Fatal(err)
	}
	if err := st.BindScheduleWebhook(ctx, hook, "third", "", at); err != nil {
		t.Fatalf("a binding to a deleted schedule refused the hook: %v", err)
	}
	if scheduleID, _, _ := st.ScheduleForWebhook(ctx, hook); scheduleID != "third" {
		t.Fatalf("hook is bound to %q, want third", scheduleID)
	}
	// A hook bound to a schedule that still exists is still refused.
	if err := st.CreateScheduleFile(ctx, "fourth", []byte("{}"), at, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := st.BindScheduleWebhook(ctx, hook, "fourth", "", at); err != ErrBindingConflict {
		t.Fatalf("a hook bound to a live schedule was taken: %v", err)
	}
}
