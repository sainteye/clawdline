package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func deleteDocument(w *WorkSystemV2, v *WorkV2View, session, docID string) error {
	got, err := w.DeleteDocument(context.Background(), v.Item.ID, docID, session, v.Item.Version, nil)
	if err == nil {
		v.Item = got.Item
	}
	return err
}

func refusedAs(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.HasPrefix(err.Error(), code+":") {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

// An item that filled its 32 places with superseded plans takes documents
// again once an older plan is deleted, while what the gates read stays.
func TestASupersededPlanCanBeDeletedToFreeAFullItem(t *testing.T) {
	w := newWorkV2Test(t)
	v := ownedDocumentItem(t, w, work.KindFeature)
	var plans []work.DocumentV2
	for i := 0; i < store.WorkV2DocumentLimit-1; i++ {
		d, err := writeDocument(t, w, &v, work.DocumentPlan, fmt.Sprintf("Plan round %d", i), "plan")
		if err != nil {
			t.Fatalf("plan %d: %v", i, err)
		}
		plans = append(plans, d)
	}
	boundary, err := writeDocument(t, w, &v, "other", work.ReviewBoundaryTitle,
		`{"new_risk_boundary":false,"reason":"same scope"}`)
	if err != nil {
		t.Fatal(err)
	}
	report, err := writeDocument(t, w, &v, work.DocumentCompletionReport, "Completion report", "done")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeDocument(t, w, &v, "spec", "One too many", "x"); err == nil || !strings.Contains(err.Error(), "doc-delete") {
		t.Fatalf("a full item's refusal does not name the deletion: %v", err)
	}

	latest := plans[len(plans)-1]
	refusedAs(t, deleteDocument(w, &v, "session-a", latest.ID), "document_kept")
	refusedAs(t, deleteDocument(w, &v, "session-a", boundary.ID), "document_kept")
	refusedAs(t, deleteDocument(w, &v, "session-a", report.ID), "document_kept")
	refusedAs(t, deleteDocument(w, &v, "session-a", "no-such-document"), "document_not_found")
	if err := deleteDocument(w, &v, "session-b", plans[0].ID); err == nil {
		t.Fatal("a Session that does not own the item deleted its document")
	}

	if err := deleteDocument(w, &v, "session-a", plans[0].ID); err != nil {
		t.Fatalf("deleting a superseded plan: %v", err)
	}
	if _, err := writeDocument(t, w, &v, "spec", "Now it fits", "x"); err != nil {
		t.Fatalf("the freed place does not take a document: %v", err)
	}
	full, _ := w.Item(context.Background(), v.Item.ID)
	for _, d := range full.Documents {
		if d.ID == plans[0].ID {
			t.Fatal("the deleted plan is still listed")
		}
	}
	if len(full.Documents) != store.WorkV2DocumentLimit+1 {
		t.Fatalf("documents = %d", len(full.Documents))
	}
	events, _ := w.Store.WorkV2Events(context.Background(), v.Item.ID, 0, 200)
	deleted := false
	for _, e := range events {
		if e.Kind == "document.deleted" && strings.Contains(e.Payload, plans[0].ID) {
			deleted = true
		}
	}
	if !deleted {
		t.Fatal("no document.deleted event")
	}

	// A document of another item is not this item's to delete.
	other := ownedDocumentItem(t, w, work.KindIssue)
	refusedAs(t, deleteDocument(w, &other, "session-a", plans[1].ID), "document_not_found")
}
