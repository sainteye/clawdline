package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// ownedDocumentItem is an item of kind owned by session-a, ready for
// documents.
func ownedDocumentItem(t *testing.T, w *WorkSystemV2, kind work.Kind) WorkV2View {
	t.Helper()
	v := createWorkV2Test(t, w, kind)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owned
}

func writeDocument(t *testing.T, w *WorkSystemV2, v *WorkV2View, role, title, body string) (work.DocumentV2, error) {
	t.Helper()
	got, err := w.AddDocument(context.Background(), v.Item.ID, AddDocumentV2{ExpectedVersion: v.Item.Version,
		SessionID: "session-a", Role: role, Title: title, Body: body}, nil)
	if err != nil {
		return work.DocumentV2{}, err
	}
	v.Item = got.Item
	if len(got.Documents) != 1 {
		t.Fatalf("answer carries %d documents", len(got.Documents))
	}
	return got.Documents[0], nil
}

func fillDocuments(t *testing.T, w *WorkSystemV2, v *WorkV2View) {
	t.Helper()
	for i := 0; i < store.WorkV2DocumentLimit; i++ {
		if _, err := writeDocument(t, w, v, "spec", fmt.Sprintf("Spec %d", i), "body"); err != nil {
			t.Fatalf("document %d: %v", i, err)
		}
	}
}

func TestFullItemStillAcceptsACompletionReport(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindIssue)
	fillDocuments(t, w, &v)
	report, err := writeDocument(t, w, &v, "completion_report", "Completion report", "The cause was found.")
	if err != nil {
		t.Fatalf("a full item refused its completion report: %v", err)
	}
	full, _ := w.Item(context.Background(), v.Item.ID)
	if len(full.Documents) != store.WorkV2DocumentLimit+1 || report.Version != 1 {
		t.Fatalf("documents = %d, report = %+v", len(full.Documents), report)
	}
}

func TestSameRoleAndTitleRevisesInPlace(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindIssue)
	first, err := writeDocument(t, w, &v, "design", "Design", "first")
	if err != nil {
		t.Fatal(err)
	}
	revised, err := writeDocument(t, w, &v, "design", "Design", "second")
	if err != nil {
		t.Fatal(err)
	}
	if revised.ID != first.ID || revised.Version != 2 || revised.Body != "second" {
		t.Fatalf("revision = %+v, first = %+v", revised, first)
	}
	full, _ := w.Item(context.Background(), v.Item.ID)
	if len(full.Documents) != 1 || full.Documents[0].Version != 2 || full.Documents[0].Body != "second" {
		t.Fatalf("documents = %+v", full.Documents)
	}
	events, _ := w.Store.WorkV2Events(context.Background(), v.Item.ID, 0, 100)
	if last := events[len(events)-1]; last.Kind != "document.revised" || !strings.Contains(last.Payload, first.ID) ||
		!strings.Contains(last.Payload, `"version":2`) {
		t.Fatalf("last event = %+v", last)
	}

	// The same text again, with the version the first write saw, is a retry:
	// the document and the item stay as they are.
	stale := v.Item.Version - 1
	again, err := w.AddDocument(context.Background(), v.Item.ID, AddDocumentV2{ExpectedVersion: stale,
		SessionID: "session-a", Role: "design", Title: "Design", Body: "second"}, nil)
	if err != nil || len(again.Documents) != 1 || again.Documents[0].Version != 2 || again.Item.Version != v.Item.Version {
		t.Fatalf("identical resend: %+v %v", again, err)
	}
	after, _ := w.Item(context.Background(), v.Item.ID)
	if after.Item.Version != v.Item.Version || after.Documents[0].Version != 2 {
		t.Fatalf("identical resend wrote: item v%d doc v%d", after.Item.Version, after.Documents[0].Version)
	}
}

func TestRevisionFitsAFullItem(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindIssue)
	fillDocuments(t, w, &v)
	revised, err := writeDocument(t, w, &v, "spec", "Spec 3", "revised")
	if err != nil || revised.Version != 2 {
		t.Fatalf("revision on a full item: %+v %v", revised, err)
	}
}

func TestPlanIsAddedAndCompletionReportIsRevised(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindFeature)
	first, err := writeDocument(t, w, &v, work.DocumentPlan, "Plan", "first plan")
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeDocument(t, w, &v, work.DocumentPlan, "Plan", "second plan")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Version != 1 {
		t.Fatalf("a plan was revised in place: %+v", second)
	}
	report, err := writeDocument(t, w, &v, "completion_report", "Report", "first report")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := writeDocument(t, w, &v, "completion_report", "Final report", "second report")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != report.ID || renamed.Version != 2 || renamed.Title != "Final report" {
		t.Fatalf("second completion report = %+v, first = %+v", renamed, report)
	}
	full, _ := w.Item(context.Background(), v.Item.ID)
	if len(full.Documents) != 3 {
		t.Fatalf("documents = %+v", full.Documents)
	}
}

func TestOneDocumentPastTheLimitIsRefusedWithACommand(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindIssue)
	fillDocuments(t, w, &v)
	_, err := writeDocument(t, w, &v, "test", "Test plan", "body")
	var refused *WorkError
	if !errors.As(err, &refused) || refused.Code != "documents_full" || refused.Status != http.StatusInsufficientStorage {
		t.Fatalf("33rd document: %v", err)
	}
	want := "`clawdline item doc " + v.Item.ID + " --role test --title \"Test plan\" --body-file <file>`"
	if !strings.Contains(refused.Message, want) || !strings.Contains(refused.Message, "nothing was written") {
		t.Fatalf("message = %q", refused.Message)
	}
	full, _ := w.Item(context.Background(), v.Item.ID)
	if len(full.Documents) != store.WorkV2DocumentLimit {
		t.Fatalf("documents = %d", len(full.Documents))
	}
}
