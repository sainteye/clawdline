package lane

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A full machine is a refusal before anything happens, a caller that gives
// up waiting is told so, and neither leaves a slot behind.
func TestAFullMachineRefusesAndAWaiterMayLeave(t *testing.T) {
	l := New(2)
	ctx := context.Background()
	first, err := l.Acquire(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	// The second is admitted and waits behind the first on the same key.
	waiting, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err = l.Acquire(waiting, "a")
	var busy Busy
	if !errors.As(err, &busy) || !busy.Waited {
		t.Fatalf("a waiter that gave up answered %v, want Busy{Waited}", err)
	}
	if st := l.Stats(); st.Admitted != 1 {
		t.Fatalf("a waiter that left is still admitted: %+v", st)
	}

	second, err := l.Acquire(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Acquire(ctx, "c")
	if !errors.As(err, &busy) || busy.Waited || busy.Limit != 2 {
		t.Fatalf("a full machine answered %v, want an immediate Busy", err)
	}
	if st := l.Stats(); st.Refused != 2 || st.Admitted != 2 || st.Keys != 2 {
		t.Fatalf("stats %+v", st)
	}
	first()
	first() // a second release is harmless
	second()
	if st := l.Stats(); st.Admitted != 0 || st.Keys != 0 {
		t.Fatalf("released lanes left %+v", st)
	}
	if _, err := l.Acquire(ctx, "c"); err != nil {
		t.Fatalf("a machine with room refused: %v", err)
	}
}
