package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func newWorkV2Test(t *testing.T) *WorkSystemV2 {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := NewWorkSystemV2(st)
	w.Now = func() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }
	return w
}

func createWorkV2Test(t *testing.T, w *WorkSystemV2, kind work.Kind) WorkV2View {
	t.Helper()
	v, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p", Kind: kind,
		Title: "Owned work", Description: "A complete description", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWorkV2KeepsHumanAndAgentAuthoritiesSeparate(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	title := "Agent refined title"
	if _, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Title: &title, Actor: "session-b", OwnerSession: "session-b"}, nil); err == nil {
		t.Fatal("another Session edited the item")
	}
	edited, err := w.Edit(context.Background(), v.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Title: &title, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if err != nil || edited.Item.Title != title {
		t.Fatalf("owner edit: %+v %v", edited, err)
	}
	if _, err := w.Cancel(context.Background(), v.Item.ID, edited.Item.Version, "local", "no longer wanted", nil); err != nil {
		t.Fatal(err)
	}
}

func TestPlanningNeverBecomesExecutableAndProposalNeedsOwnedEvidence(t *testing.T) {
	w := newWorkV2Test(t)
	planning := createWorkV2Test(t, w, work.KindEpic)
	if _, err := w.Assign(context.Background(), planning.Item.ID, AssignWorkV2{ExpectedVersion: 1,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil); err == nil {
		t.Fatal("planning item was assignable")
	}
	executable := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), executable.Item.ID, AssignWorkV2{ExpectedVersion: 1,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.Propose(context.Background(), work.ProposalV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Follow-up", Description: "A newly discovered concern", Reason: "Found while implementing",
		SessionID: "session-a", SourceWorkID: assigned.Item.ID})
	if err != nil || p.State != "pending" {
		t.Fatalf("proposal: %+v %v", p, err)
	}
	_, err = w.Propose(context.Background(), work.ProposalV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Foreign", Description: "Not this owner's evidence", Reason: "No authority",
		SessionID: "session-b", SourceWorkID: assigned.Item.ID})
	var refusal *WorkError
	if !errors.As(err, &refusal) || refusal.Code != "proposal_source_invalid" {
		t.Fatalf("foreign proposal: %v", err)
	}
}
