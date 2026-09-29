package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestHumanDocumentLocatorUsesRouteSessionAndMachine(t *testing.T) {
	const machine = "mac_example"
	const route = "pane-17"
	good := "https://app.clawdline.com/#document=1&machine=" + machine + "&session=" + route + "&scope=project&path=brief.md"
	if err := ValidateHumanDocumentURL(good, machine, route); err != nil {
		t.Fatalf("valid locator: %v", err)
	}
	for _, raw := range []string{
		"https://app.clawdline.com/#document=1&machine=mac_other&session=pane-17&scope=project&path=brief.md",
		"https://app.clawdline.com/#document=1&machine=mac_example&session=conversation-17&scope=project&path=brief.md",
		good + "&path=other.md",
		"https://app.clawdline.com/#document=1%26machine=mac_example&session=pane-17&scope=project&path=brief.md",
		"https://app.clawdline.com/#document=1&machine=mac_example&session=pane-17&scope=project&path=../secret.md",
		"https://other.example/#document=1&machine=mac_example&session=pane-17&scope=project&path=brief.md",
	} {
		if ValidateHumanDocumentURL(raw, machine, route) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestHumanInterventionsPruneOnlyResolvedHistoryAndPreserveCount(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	service := NewHumanInterventions(st)
	now := time.Unix(1700000000, 0)
	service.Now = func() time.Time { now = now.Add(time.Second); return now }
	c := CreateHumanIntervention{SourceConversation: "root", SourceLabel: "Manager", TargetConversation: "target", TargetSession: "pane-17",
		Kind: "read", Title: "Read report", Summary: "A release needs review.", Action: "Read the report.", Reason: "Only the person can review it."}
	ctx := context.Background()
	for i := 0; i < store.HumanInterventionsTotalLimit; i++ {
		c.TargetConversation = fmt.Sprintf("target-%d", i)
		row, _, err := service.Create(ctx, c, store.ReceiptKey{}, nil)
		if err != nil {
			t.Fatal(i, err)
		}
		if i == 0 {
			if _, _, err := service.Update(ctx, row.ID, c.TargetConversation, "resolve", "", 1, store.ReceiptKey{}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	c.TargetConversation = "fresh"
	if _, _, err := service.Create(ctx, c, store.ReceiptKey{}, nil); err != nil {
		t.Fatalf("prune resolved: %v", err)
	}
	_, pruned, err := service.List(ctx, "fresh")
	if err != nil || pruned != 1 {
		t.Fatalf("retention: %d %v", pruned, err)
	}
	if _, _, err := service.Create(ctx, c, store.ReceiptKey{}, nil); err == nil {
		t.Fatal("full of open rows accepted another")
	} else {
		var typed *WorkError
		if !errors.As(err, &typed) || typed.Status != http.StatusInsufficientStorage || typed.Code != "interventions_full" {
			t.Fatalf("full: %v", err)
		}
	}
}

func TestHumanInterventionReadAndResolutionSurviveStoreReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHumanInterventions(st)
	c := CreateHumanIntervention{SourceConversation: "source", SourceLabel: "Manager", TargetConversation: "target", TargetSession: "pane-17",
		Kind: "answer", Title: "Choose a date", Summary: "A date is needed.", Action: "Choose the date.", Reason: "Only the person knows it."}
	row, _, err := h.Create(context.Background(), c, store.ReceiptKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	read, _, err := h.Update(context.Background(), row.ID, "target", "read", "", row.Version, store.ReceiptKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h = NewHumanInterventions(st)
	rows, _, err := h.List(context.Background(), "target")
	if err != nil || len(rows) != 1 || rows[0].ReadAt == nil || rows[0].ResolvedAt != nil || rows[0].Version != read.Version {
		t.Fatalf("reopened read: %+v, %v", rows, err)
	}
	resolved, _, err := h.Update(context.Background(), row.ID, "target", "resolve", "", read.Version, store.ReceiptKey{}, nil)
	if err != nil || resolved.ResolvedAt == nil {
		t.Fatalf("resolve: %+v, %v", resolved, err)
	}
}
