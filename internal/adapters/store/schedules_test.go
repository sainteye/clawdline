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
