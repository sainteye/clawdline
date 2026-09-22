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

func TestPersonCommandsVersionReferenceImages(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	added, err := w.AddImage(context.Background(), v.Item.ID, AddImageV2{ExpectedVersion: v.Item.Version,
		Title: "state.png", Data: []byte("png"), Width: 3, Height: 2, Actor: "local"}, nil)
	if err != nil || added.Item.Version != v.Item.Version+1 || len(added.Images) != 1 {
		t.Fatalf("add: %+v %v", added, err)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil || len(full.Images) != 1 || full.Images[0].Title != "state.png" {
		t.Fatalf("read: %+v %v", full, err)
	}
	if _, err := w.DeleteImage(context.Background(), v.Item.ID, full.Images[0].ID, v.Item.Version, "local", nil); err == nil {
		t.Fatal("a stale version deleted a reference image")
	}
	deleted, err := w.DeleteImage(context.Background(), v.Item.ID, full.Images[0].ID, added.Item.Version, "local", nil)
	if err != nil || deleted.Item.Version != added.Item.Version+1 {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	full, _ = w.Item(context.Background(), v.Item.ID)
	if len(full.Images) != 0 {
		t.Fatalf("deleted image remains: %+v", full.Images)
	}
}

func TestPersonAddsImagesOnlyBeforeDirectTodoDelivery(t *testing.T) {
	w := newWorkV2Test(t)
	todo, err := w.CreateDirectTodo(context.Background(), NewDirectTodoV2{
		SessionID: "session-a", Text: "Use this screenshot", Actor: "local",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	added, image, err := w.AddDirectTodoImage(context.Background(), todo.ID, AddDirectTodoImageV2{
		ExpectedVersion: todo.Version, SessionID: todo.SessionID, Title: "screen.png",
		Data: []byte("png"), Width: 3, Height: 2, Actor: "local",
	}, nil)
	if err != nil || added.Version != todo.Version+1 || image.TodoID != todo.ID {
		t.Fatalf("add: %+v %+v %v", added, image, err)
	}
	images, err := w.DirectTodoImages(context.Background(), todo.ID)
	if err != nil || len(images) != 1 || images[0].Title != "screen.png" {
		t.Fatalf("read: %+v %v", images, err)
	}
	if _, err := w.MarkDirectTodoSent(context.Background(), todo.ID, todo.SessionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.AddDirectTodoImage(context.Background(), todo.ID, AddDirectTodoImageV2{
		ExpectedVersion: added.Version + 1, SessionID: todo.SessionID, Data: []byte("png"), Width: 1, Height: 1,
	}, nil); err == nil {
		t.Fatal("a delivered to-do accepted another image")
	}
}
