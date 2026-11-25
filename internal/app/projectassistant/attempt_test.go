package projectassistant

import (
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A turn refused while its attempt is being started under the actor lock
// leaves the lock released: the next message is admitted and runs. Submit
// takes the same lock, so a start that kept it would block the second
// submission instead of failing it.
func TestARefusedAttemptStartReleasesTheActorForTheNextTurn(t *testing.T) {
	f := newRuntimeFixture(t)
	var calls atomic.Int64
	// The first call is Submit's admission check, the second is the attempt
	// start; only that one is refused.
	f.provider.onCapabilities = func() {
		if calls.Add(1) == 2 {
			f.provider.capsErr = runtimeFailure(pa.MemoryIsolationUnverified, "native_memory")
		} else {
			f.provider.capsErr = nil
		}
	}
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, first.ResourceID, pa.TurnFailed)
	if f.provider.count() != 0 {
		t.Fatal("a refused start opened a native session")
	}

	admitted := make(chan error, 1)
	var second pa.Receipt
	go func() {
		var err error
		second, err = f.submit(t, "two", "two")
		admitted <- err
	}()
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the next message was not admitted: the refused start kept the actor lock")
	}
	if q := receivePrompt(t, f.provider); q.Text != "two" {
		t.Fatalf("the next turn sent %q", q.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, second.ResourceID, pa.TurnSucceeded)
}
