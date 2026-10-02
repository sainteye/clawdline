package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// doneItem stores one finished Feature and the assignments that owned it: a
// session per stint, the later ones marked as a handoff's when via is set.
func (h *compareHarness) doneItem(id string, cycle int64, via string, owners ...string) {
	h.t.Helper()
	closed := h.now.Add(-time.Hour)
	item := work.ItemV2{ID: id, ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature, Title: "Work",
		Phase: work.PhaseDone, DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "person",
		CreatedAt: closed.Add(-10 * time.Hour), UpdatedAt: closed, ClosedAt: closed, Cycle: cycle, Version: 1}
	if err := h.st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		open := item
		open.ClosedAt = time.Time{}
		if err := tx.CreateItem(open, "person", "{}"); err != nil {
			return err
		}
		// A create never writes closed_at; the change to done does.
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if err := tx.PutItem(prev, item, "item.done", "person", "{}"); err != nil {
			return err
		}
		for i, s := range owners {
			actor := "person"
			if i > 0 && via != "" {
				actor = "handoff:" + via
			}
			from := closed.Add(-time.Duration(len(owners)-i) * 4 * time.Hour)
			if err := tx.CreateAssignment(work.AssignmentV2{ID: orchestrator.NewUUID(), WorkID: id,
				Mode: "existing_session", SessionID: s, Assistant: "claude", State: "released", HumanActor: actor,
				CreatedAt: from, UpdatedAt: from, ReleasedAt: closed}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
}

// root stores one owner Session's reading: its own cache reads and calls.
func (h *compareHarness) root(conversation string, cacheRead float64, calls int64) {
	h.t.Helper()
	r := store.UsageRow{Assistant: "claude", Conversation: conversation}
	r.Spent, _ = json.Marshal(map[string]map[string]float64{"impl": {"cache_read": cacheRead}})
	r.Measured, _ = json.Marshal(map[string]float64{"cache_read": cacheRead})
	r.State, _ = json.Marshal(map[string]any{"calls": calls})
	r.Path, r.ReadAt, r.OpeningRead = "/nowhere/"+conversation+".jsonl", h.now, true
	if err := h.st.SaveUsageRow(context.Background(), r); err != nil {
		h.t.Fatal(err)
	}
}

func (h *compareHarness) handoff(id, state string, milestone bool, carried ...string) {
	h.t.Helper()
	at := h.now.Add(-2 * 24 * time.Hour)
	body, _ := json.Marshal(orchestrator.Handoff{ID: id, State: state, Milestone: milestone, Carried: carried})
	if err := h.st.CreateOpened(context.Background(), store.TableHandoffs,
		store.Opened{ID: id, State: state, Record: body, CreatedAt: at, UpdatedAt: at}, nil); err != nil {
		h.t.Fatal(err)
	}
}

func handoffGroup(t *testing.T, c HandoffComparison, name string) HandoffGroup {
	t.Helper()
	for _, g := range c.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("no group %s", name)
	return HandoffGroup{}
}

// A long Root's cache reads are shared among the items it finished, a
// milestone handoff's item carries both its sender and its receiver, and an
// item with no reading or no owner is counted apart, never as zero.
func TestTheHandoffComparisonBillsEachItemItsOwnersShare(t *testing.T) {
	h := newCompareHarness(t)
	const long, sender, receiver, plainA, plainB, unread = "c0000000-0000-4000-8000-000000000001",
		"c0000000-0000-4000-8000-000000000002", "c0000000-0000-4000-8000-000000000003",
		"c0000000-0000-4000-8000-000000000004", "c0000000-0000-4000-8000-000000000005",
		"c0000000-0000-4000-8000-000000000006"
	const milestone, plain, failed = "c1000000-0000-4000-8000-000000000001", "c1000000-0000-4000-8000-000000000002",
		"c1000000-0000-4000-8000-000000000003"
	for i, id := range []string{"c2000000-0000-4000-8000-000000000001", "c2000000-0000-4000-8000-000000000002",
		"c2000000-0000-4000-8000-000000000003", "c2000000-0000-4000-8000-000000000004"} {
		cycle := int64(1)
		if i == 0 {
			cycle = 2
		}
		h.doneItem(id, cycle, "", long)
	}
	h.root(long, 4_000_000, 400)
	h.handoff(milestone, orchestrator.HandoffDelivered, true)
	h.handoff(plain, orchestrator.HandoffDelivered, false)
	h.handoff(failed, orchestrator.HandoffSpawnFailed, true)
	h.doneItem("c2000000-0000-4000-8000-000000000010", 1, milestone, sender, receiver)
	h.root(sender, 300_000, 30)
	h.root(receiver, 200_000, 20)
	h.doneItem("c2000000-0000-4000-8000-000000000020", 1, plain, plainA, plainB)
	h.root(plainA, 700_000, 70)
	h.root(plainB, 100_000, 10)
	h.doneItem("c2000000-0000-4000-8000-000000000030", 1, "", unread)
	h.doneItem("c2000000-0000-4000-8000-000000000040", 1, "")

	got, err := h.u.CompareHandoff(context.Background(), h.now.Add(-CompareDefaultSince))
	if err != nil {
		t.Fatal(err)
	}
	single := handoffGroup(t, got, HandoffGroupSingle)
	if single.Items != 4 || single.CacheReadItem == nil || *single.CacheReadItem != 1_000_000 || single.ReopenedCount != 1 {
		t.Fatalf("single_root: %+v (cache/item %v)", single, deref(single.CacheReadItem))
	}
	ms := handoffGroup(t, got, HandoffGroupMilestone)
	if ms.Items != 1 || *ms.CacheReadItem != 500_000 || *ms.CallsItem != 50 {
		t.Fatalf("milestone_handoff: %+v", ms)
	}
	if ms.ElapsedHours == nil || *ms.ElapsedHours != 8 {
		t.Fatalf("milestone elapsed = %v, want 8h from the first owner", deref(ms.ElapsedHours))
	}
	pl := handoffGroup(t, got, HandoffGroupPlain)
	if pl.Items != 1 || *pl.CacheReadItem != 800_000 {
		t.Fatalf("plain_handoff: %+v", pl)
	}
	if got.Excluded["unread"] != 1 || got.Excluded["unowned"] != 1 {
		t.Fatalf("excluded = %v", got.Excluded)
	}
	if got.MilestoneHandoffs != 2 || got.FailedHandoffs != 1 {
		t.Fatalf("handoffs: %d milestone, %d failed", got.MilestoneHandoffs, got.FailedHandoffs)
	}
	if got.Verdict != HandoffVerdictInsufficient || got.Saving != nil {
		t.Fatalf("verdict %s saving %v with 4 and 1 items", got.Verdict, deref(got.Saving))
	}
	if len(got.NotMeasured) == 0 {
		t.Fatal("the answer does not say what it cannot measure")
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func units(group string, n int, cacheRead float64, reopened int, hours float64) []HandoffUnit {
	var out []HandoffUnit
	for i := 0; i < n; i++ {
		out = append(out, HandoffUnit{ItemID: group + string(rune('a'+i)), Group: group, CacheRead: cacheRead,
			Calls: 10, Elapsed: time.Duration(hours * float64(time.Hour)), Reopened: i < reopened})
	}
	return out
}

// The verdict recommends handing over by default only with enough items in
// both groups, a saving of at least 30%, and no guardrail slipping; anything
// else keeps it a Session's own choice and says why.
func TestTheHandoffVerdictNeedsTwentyItemsThirtyPercentAndItsGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		units   []HandoffUnit
		failed  int
		verdict string
		saving  float64
		reason  string
	}{
		{"nineteen items", append(units(HandoffGroupSingle, 30, 100, 0, 10), units(HandoffGroupMilestone, 19, 10, 0, 10)...),
			0, HandoffVerdictInsufficient, -1, "fewer than 20"},
		{"thirty percent", append(units(HandoffGroupSingle, 20, 100, 0, 10), units(HandoffGroupMilestone, 20, 70, 0, 10)...),
			0, HandoffVerdictRecommend, 0.30, ""},
		{"twenty percent", append(units(HandoffGroupSingle, 20, 100, 0, 10), units(HandoffGroupMilestone, 20, 80, 0, 10)...),
			0, HandoffVerdictBelowTarget, 0.20, "under 30%"},
		{"more reopened", append(units(HandoffGroupSingle, 20, 100, 1, 10), units(HandoffGroupMilestone, 20, 50, 3, 10)...),
			0, HandoffVerdictGuardrail, 0.50, "reopened"},
		{"slower", append(units(HandoffGroupSingle, 20, 100, 0, 10), units(HandoffGroupMilestone, 20, 50, 0, 12)...),
			0, HandoffVerdictGuardrail, 0.50, "longer"},
		{"a handoff failed", append(units(HandoffGroupSingle, 20, 100, 0, 10), units(HandoffGroupMilestone, 20, 50, 0, 10)...),
			1, HandoffVerdictGuardrail, 0.50, "failed to open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := FoldHandoffComparison(tc.units)
			c.FailedHandoffs = tc.failed
			decideHandoff(&c)
			if c.Verdict != tc.verdict {
				t.Fatalf("verdict %s, want %s (%v)", c.Verdict, tc.verdict, c.Reasons)
			}
			switch {
			case tc.saving < 0 && c.Saving != nil:
				t.Fatalf("a saving %v with too few items", *c.Saving)
			case tc.saving >= 0 && (c.Saving == nil || *c.Saving < tc.saving-1e-9 || *c.Saving > tc.saving+1e-9):
				t.Fatalf("saving %v, want %v", deref(c.Saving), tc.saving)
			}
			if tc.reason != "" && !strings.Contains(strings.Join(c.Reasons, ";"), tc.reason) {
				t.Fatalf("reasons %v, want one about %q", c.Reasons, tc.reason)
			}
		})
	}
}
