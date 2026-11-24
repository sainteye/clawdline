package botconnectorvault

import (
	"bytes"
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/botprofilevault"
)

// An ordinary restart while an execution holds a ready credential must not
// ask the person to enter it again. The stored bytes did not change, so the
// restart clears the hold and the credential stays ready and lendable.
func TestRestartWithAHeldReadyCredentialKeepsItReady(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.store(t, []byte("SYNTHETIC_HELD_ACROSS_RESTART"))
	value, revision, _, _, err := f.r.Acquire(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	botprofilevault.Wipe(value)
	reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := reopened.Status(ctx, f.scope); err != nil || status.State != "ready" || status.Revision != revision {
		t.Fatal("a restart asked for the credential again", status, err)
	}
	if used, err := reopened.HoldUsage(ctx); err != nil || used != 0 {
		t.Fatal("the restart kept the previous process's hold", used, err)
	}
	got, _, release, _, err := reopened.Acquire(ctx, f.scope)
	if err != nil || !bytes.Equal(got, []byte("SYNTHETIC_HELD_ACROSS_RESTART")) {
		t.Fatal("the credential could not be lent after the restart", err)
	}
	botprofilevault.Wipe(got)
	if err = release(ctx); err != nil {
		t.Fatal(err)
	}
}
