//go:build darwin || linux

package antigravitysession

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func TestStopWorkerJoinsItsProcessAndPreservesOtherConversation(t *testing.T) {
	h, a, opts := persistentFixture(t)
	b, err := h.Open(context.Background(), ports.OpenRequest{Cwd: a.CWD})
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := h.lookup(a)
	ca.mu.Lock()
	client := ca.client
	ca.mu.Unlock()
	wrong := a
	wrong.ConversationID = b.ConversationID
	if err := h.Stop(context.Background(), wrong); err == nil || client.Err() != nil {
		t.Fatal("wrong native identity stopped a worker", err)
	}
	if err := h.Send(context.Background(), a, "one synthetic input"); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if client.Err() == nil {
		t.Fatal("owned process was not stopped")
	}
	if err := h.Stop(context.Background(), a); err != nil {
		t.Fatal("repeat stop did not confirm the same stopped worker", err)
	}
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].ID != b.ID {
		t.Fatal("another conversation was changed", inv, err)
	}
	page, err := h.Transcript(a, 20)
	if err != nil || len(page.Entries) == 0 || page.Entries[0].Text != "one synthetic input" {
		t.Fatal("stopping discarded history", page, err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	page, err = h.Transcript(a, 20)
	if err != nil || len(page.Entries) == 0 || page.Entries[0].Text != "one synthetic input" {
		t.Fatal("restart discarded stopped history", page, err)
	}
}
