//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedInterruptKeepsHistoryAndAcceptsNextInput(t *testing.T) {
	t.Setenv("CLW_INTERRUPT_PEER", "yes")
	h, row, dir := fixture(t)
	ctx := context.Background()
	if err := h.Send(ctx, row, "synthetic first"); err != nil {
		t.Fatal(err)
	}
	if err := h.InterruptProtocol(ctx, row, nil); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	raw, err := os.ReadFile(filepath.Join(dir, "cancelled"))
	if err != nil || string(raw) != row.ConversationID {
		t.Fatal(string(raw), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(ctx, row, "synthetic follow-up"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	inv, err := h.Inventory(ctx)
	if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].PID != row.PID || inv.Sessions[0].ConversationID != row.ConversationID {
		t.Fatal(inv, err)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, e := range page.Entries {
		if e.Role == "user" {
			found[e.Text] = true
		}
	}
	if !found["synthetic first"] || !found["synthetic follow-up"] {
		t.Fatal(found)
	}
}
