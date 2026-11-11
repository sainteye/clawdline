//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AGY 1.5 restores its default model and effort when a conversation is loaded
// into a new process. The person's acknowledged choice must survive that.
func TestChosenModelAndEffortSurviveAReload(t *testing.T) {
	for _, effortID := range []string{"thinking_level", "thought_level"} {
		t.Run(effortID, func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_EFFORT_ID", effortID)
			h, row, opts := persistentFixture(t)
			if err := h.Send(context.Background(), row, "/model synthetic-pro"); err != nil {
				t.Fatal(err)
			}
			if err := h.Send(context.Background(), row, "/effort high"); err != nil {
				t.Fatal(err)
			}
			stopHost(t, h)
			if err := os.Remove(filepath.Join(opts.AccountDir, "config-writes")); err != nil {
				t.Fatal(err)
			}
			h = reopenHost(t, opts)
			// Before any process runs, the info sheet still has the offer to
			// switch from; the switch itself reconnects first.
			idle, _ := h.Facts(row)
			if len(idle.Models) != 2 || idle.Model != "synthetic-pro" || idle.Effort != "high" || len(idle.Efforts) == 0 || idle.Protocol.Phase != "not_connected" {
				t.Fatalf("disconnected Session lost its offer: %+v", idle)
			}
			row, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
			facts, _ := h.Facts(row)
			if facts.Model != "synthetic-pro" || facts.Effort != "high" {
				t.Fatalf("reload kept the runtime default: model=%q effort=%q", facts.Model, facts.Effort)
			}
			writes := readFile(t, filepath.Join(opts.AccountDir, "config-writes"))
			if !strings.Contains(writes, "model=synthetic-pro\n") || !strings.Contains(writes, effortID+"=high\n") {
				t.Fatalf("choice not applied to the new process: %q", writes)
			}
		})
	}
}

func TestAChoiceNoLongerOfferedLeavesTheRuntimeValue(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	c.chosenModel = "retired-model"
	err := c.saveState()
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
	if err != nil {
		t.Fatal("a retired choice blocked the reload:", err)
	}
	if facts, _ := h.Facts(row); facts.Model != "synthetic-flash" {
		t.Fatalf("model=%q", facts.Model)
	}
	if strings.Contains(readFile(t, filepath.Join(opts.AccountDir, "config-writes")), "retired-model") {
		t.Fatal("wrote a model the runtime does not offer")
	}
}
