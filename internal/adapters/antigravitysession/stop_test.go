//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestCloseCancelsInFlightRestoreAndPublicQuotaWithoutResurrectingWorker(t *testing.T) {
	t.Run("restore", func(t *testing.T) {
		h, row, opts := persistentFixture(t)
		stopHost(t, h)
		h = reopenHost(t, opts)
		hold := filepath.Join(opts.AccountDir, "hold-restore")
		held := filepath.Join(opts.AccountDir, "restore-held")
		release := filepath.Join(opts.AccountDir, "release-restore")
		if err := os.WriteFile(hold, nil, 0600); err != nil {
			t.Fatal(err)
		}
		resumeErr := make(chan error, 1)
		go func() {
			_, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
			resumeErr <- err
		}()
		waitFor(t, func() bool {
			_, err := os.Stat(held)
			return err == nil
		})
		closeDone := make(chan error, 1)
		go func() {
			closeDone <- h.Close(context.Background(), row)
		}()
		select {
		case err := <-closeDone:
			if err != nil {
				t.Fatal("close during restore failed", err)
			}
		case <-time.After(2 * time.Second):
			_ = os.WriteFile(release, nil, 0600)
			t.Fatal("close did not cancel in-flight restore")
		}
		select {
		case err := <-resumeErr:
			if err == nil {
				t.Fatal("cancelled restore still succeeded")
			}
		case <-time.After(time.Second):
			_ = os.WriteFile(release, nil, 0600)
			<-resumeErr
			t.Fatal("close returned without cancelling in-flight restore")
		}
		inv, err := h.Inventory(context.Background())
		if err != nil || len(inv.Sessions) != 0 {
			t.Fatal("closed conversation reappeared after restore", inv, err)
		}
		if processes, _ := h.Stats(); processes != 0 {
			t.Fatal("cancelled restore kept staging process", processes)
		}
		_ = os.Remove(hold)
		_ = os.Remove(held)
		_ = os.Remove(release)
		resumed, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
		if err != nil || resumed.ID != row.ID || resumed.PID <= 0 {
			t.Fatal("explicit later resume failed after close", resumed, err)
		}
	})

	t.Run("public-quota", func(t *testing.T) {
		t.Setenv("CLW_SESSION_PEER", "yes")
		t.Setenv("CLW_RESTORE_PEER", "yes")
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("CLW_NATIVE_TEST_EXE", exe)
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
			t.Fatal(err)
		}
		cli := filepath.Join(dir, "cli")
		body := "#!/bin/sh\nif [ \"$1\" = acp ]; then exec \"$CLW_NATIVE_TEST_EXE\" '-test.run=^TestOwnedPeer$'; fi\nif [ -f \"$GEMINI_HOME/hold-quota\" ]; then\n  printf 'held\\n' > \"$GEMINI_HOME/quota-held\"\n  while [ ! -f \"$GEMINI_HOME/release-quota\" ]; do sleep 0.01; done\nfi\nprintf 'Gemini Models\\tWeekly Limit Remaining\\t64%%\\t2031-01-02T03:04:05Z\\n'\n"
		if err := os.WriteFile(cli, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		opts := Options{Executable: cli, Args: []string{"acp", "--app-data-dir", "antigravity-acp"}, AccountDir: dir, StateDir: filepath.Join(dir, "state")}
		h := reopenHost(t, opts)
		row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
		if err != nil {
			t.Fatal(err)
		}
		stopHost(t, h)
		h = reopenHost(t, opts)
		if err := h.Send(context.Background(), row, "/usage"); err != nil {
			t.Fatal("reopened conversation refused public quota read without live worker", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hold-quota"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		quotaErr := make(chan error, 1)
		go func() {
			quotaErr <- h.RefreshQuota(context.Background(), row)
		}()
		waitFor(t, func() bool {
			_, err := os.Stat(filepath.Join(dir, "quota-held"))
			return err == nil
		})
		closeDone := make(chan error, 1)
		go func() {
			closeDone <- h.Close(context.Background(), row)
		}()
		select {
		case err := <-closeDone:
			if err != nil {
				t.Fatal("close during public quota read failed", err)
			}
		case <-time.After(2 * time.Second):
			_ = os.WriteFile(filepath.Join(dir, "release-quota"), nil, 0600)
			t.Fatal("close did not cancel in-flight public quota read")
		}
		select {
		case err := <-quotaErr:
			if err == nil {
				t.Fatal("cancelled quota read succeeded")
			}
		case <-time.After(time.Second):
			_ = os.WriteFile(filepath.Join(dir, "release-quota"), nil, 0600)
			<-quotaErr
			t.Fatal("close returned without cancelling in-flight quota read")
		}
		inv, err := h.Inventory(context.Background())
		if err != nil || len(inv.Sessions) != 0 {
			t.Fatal("closed conversation remained visible after quota read", inv, err)
		}
	})
}
