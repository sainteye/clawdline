//go:build darwin || linux

package antigravitysession

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func TestCloseBusyWorkerCancelsMetadataAndModelWithoutLosingHistory(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "model", true: "metadata"}[metadata], func(t *testing.T) {
			if !metadata {
				t.Setenv("CLW_INTERRUPT_PEER", "yes")
			}
			h, row, _ := persistentFixture(t)
			other, err := h.Open(context.Background(), ports.OpenRequest{Cwd: row.CWD})
			if err != nil {
				t.Fatal(err)
			}
			c, err := h.lookup(row)
			if err != nil {
				t.Fatal(err)
			}
			client := c.client
			if metadata {
				_, done, err := h.beginMetadata(c)
				if err != nil {
					t.Fatal(err)
				}
				defer done()
			} else if err := h.Send(context.Background(), row, "synthetic unfinished request"); err != nil {
				t.Fatal(err)
			}
			if err := h.Close(context.Background(), row); err != nil {
				t.Fatal("explicit close refused an owned busy worker", err)
			}
			if client.Err() == nil {
				t.Fatal("close left native process running")
			}
			inv, err := h.Inventory(context.Background())
			if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].ID != other.ID || inv.Sessions[0].PID != other.PID {
				t.Fatal("close changed another conversation", inv, err)
			}
			page, err := h.Transcript(row, 10)
			if err != nil {
				t.Fatal("closed history unreadable", err)
			}
			if !metadata && (len(page.Entries) == 0 || page.Entries[0].Text != "synthetic unfinished request") {
				t.Fatal("close discarded unfinished request", page)
			}
		})
	}
}

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

func TestCloseFailedIntentLeavesWorkerVisibleAndAllowsRetry(t *testing.T) {
	h, row, _ := persistentFixture(t)
	c, _ := h.lookup(row)
	client := c.client
	if _, err := h.store.db.Exec(`CREATE TRIGGER reject_close BEFORE UPDATE ON conversations BEGIN SELECT RAISE(FAIL,'synthetic close write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(context.Background(), row); err == nil {
		t.Fatal("failed close intent was accepted")
	}
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].PID != row.PID || client.Err() != nil {
		t.Fatal("failed intent hid or stopped its worker", inv, err)
	}
	if _, err := h.store.db.Exec(`DROP TRIGGER reject_close`); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(context.Background(), row); err != nil || client.Err() == nil {
		t.Fatal("close could not retry after persistence recovered", err)
	}
}
