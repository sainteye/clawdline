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

// Tabs the extension no longer lists are forgotten, takeovers included, so
// a restarted Chrome's old takeover no longer blocks quiet.
func TestForgetClosedTabsDropsWhatTheExtensionNoLongerLists(t *testing.T) {
	b, ctx := connected(t)
	next := 40
	fakeHost(ctx, b, func(c Command) Result {
		switch c.Op {
		case "open":
			next++
			return Result{ID: c.ID, OK: true, Tab: next}
		case "tabs":
			return Result{ID: c.ID, OK: true, Value: json.RawMessage(`{"tabs":[{"tab":42}]}`)}
		}
		return Result{ID: c.ID, OK: true, Tab: c.Tab}
	})
	for _, s := range []string{"s1", "s2"} {
		if _, err := b.Do(ctx, s, "open", json.RawMessage(`{"url":"https://example.com"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetPersonControl(41, true); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(5 * time.Minute)
	if b.Quiet(later, time.Minute) {
		t.Fatal("quiet with a takeover remembered")
	}
	if err := b.ForgetClosedTabs(ctx); err != nil {
		t.Fatal(err)
	}
	st := b.Status()
	if len(st.Tabs) != 1 || st.Tabs[0].Tab != 42 {
		t.Fatalf("tabs after pruning: %+v, want only 42", st.Tabs)
	}
	if !b.Quiet(later, time.Minute) {
		t.Fatal("a takeover of a closed tab still blocks quiet")
	}
}
