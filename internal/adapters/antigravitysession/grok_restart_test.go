//go:build darwin || linux

package antigravitysession

import (
	"context"
	"testing"
)

// A daemon restart (a dev install on 2026-11-11) ended a running Grok turn as
// grok_process_cancelled, with its running tools left without a status. The
// turn now says the restart stopped it, and its tools read as cancelled.
func TestARestartDuringAGrokTurnNamesTheRestartAndSettlesItsTools(t *testing.T) {
	t.Setenv("CLW_SILENT_TOOL_PEER", "yes")
	h, row, opts := grokFixture(t)
	if err := h.Send(context.Background(), row, "edit two synthetic pictures"); err != nil {
		t.Fatal(err)
	}
	c, _ := h.lookup(row)
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.activeTools) == 1 })
	stopHost(t, h)
	h = reopenHost(t, opts)
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	var tool, failure string
	for _, e := range page.Entries {
		if e.Role == "tool" && e.Activity != nil {
			tool = e.Activity.Status
		}
		if e.Role == "error" {
			failure = e.Text
		}
	}
	if failure != "grok_turn_interrupted_by_restart" || tool != "cancelled" {
		t.Fatalf("after a restart: error=%q tool=%q", failure, tool)
	}
}
