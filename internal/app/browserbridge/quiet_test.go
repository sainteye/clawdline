package browserbridge

import (
	"encoding/json"
	"testing"
	"time"
)

// The test Chrome may be relaunched only when nothing uses it.
func TestQuietWaitsForControlRecentActionsAndForgetsAfterRestart(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 5} })
	now := time.Now()
	if !b.Quiet(now, time.Minute) {
		t.Fatal("a channel that never acted is not quiet")
	}
	if _, err := b.Do(ctx, "s1", "open", json.RawMessage(`{"url":"https://example.com"}`)); err != nil {
		t.Fatal(err)
	}
	if b.Quiet(time.Now(), time.Minute) {
		t.Fatal("quiet right after an action")
	}
	if !b.Quiet(time.Now().Add(2*time.Minute), time.Minute) {
		t.Fatal("not quiet after the quiet period")
	}
	if err := b.SetPersonControl(5, true); err != nil {
		t.Fatal(err)
	}
	if b.Quiet(time.Now().Add(2*time.Minute), time.Minute) {
		t.Fatal("quiet while the person controls a tab")
	}
	b.ForgetAll()
	if st := b.Status(); len(st.Tabs) != 0 || len(st.PersonTabs) != 0 {
		t.Fatalf("tabs survived the restart: %+v", st)
	}
}
