package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestAPlanConvertsOnceIntoExecutableWork(t *testing.T) {
	w := newWorkV2Test(t)
	plan := createWorkV2Test(t, w, work.KindPlan)

	converted, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
		ExpectedVersion: plan.Item.Version, Kind: work.KindEpic, Actor: "device:phone",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Item.Kind != work.KindEpic || converted.Item.Area() != "unassigned" ||
		converted.Item.Title != plan.Item.Title || converted.Item.Description != plan.Item.Description {
		t.Fatalf("converted plan = %+v", converted.Item)
	}
	if converted.Item.Version != plan.Item.Version+1 {
		t.Fatalf("version = %d, want %d", converted.Item.Version, plan.Item.Version+1)
	}

	full, err := w.Item(context.Background(), plan.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "item.converted" && event.Actor == "device:phone" &&
			strings.Contains(event.Payload, `"from":"plan"`) && strings.Contains(event.Payload, `"to":"epic"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("conversion event = %+v", full.Events)
	}

	if _, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
		ExpectedVersion: converted.Item.Version, Kind: work.KindIssue, Actor: "device:phone",
	}, nil); err == nil || !strings.Contains(err.Error(), "conversion_kind_invalid") {
		t.Fatalf("second conversion = %v", err)
	}
}

func TestAPlanOnlyConvertsIntoAnExecutableKind(t *testing.T) {
	w := newWorkV2Test(t)
	plan := createWorkV2Test(t, w, work.KindPlan)
	for _, kind := range []work.Kind{work.KindPlan, work.KindRefactor, work.Kind("unknown")} {
		if _, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
			ExpectedVersion: plan.Item.Version, Kind: kind, Actor: "local",
		}, nil); err == nil || !strings.Contains(err.Error(), "conversion_kind_not_executable") {
			t.Fatalf("convert to %q = %v", kind, err)
		}
	}
}

func TestAnEpicOrFeatureCanBecomeAPlanBeforeImplementation(t *testing.T) {
	for _, kind := range []work.Kind{work.KindEpic, work.KindFeature} {
		t.Run(string(kind), func(t *testing.T) {
			w := newWorkV2Test(t)
			created := createWorkV2Test(t, w, kind)
			owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
				ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
				TerminalID: "terminal-a", Actor: "local",
			}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			converted, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
				ExpectedVersion: owned.Item.Version, Kind: work.KindPlan, Actor: "device:phone",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if converted.Item.Kind != work.KindPlan || converted.Item.Phase != work.PhaseCreated ||
				converted.Item.OwnerSession != "" || converted.Item.Area() != "planning" ||
				converted.Item.Title != created.Item.Title || converted.Item.Description != created.Item.Description {
				t.Fatalf("converted item = %+v", converted.Item)
			}
			full, err := w.Item(context.Background(), created.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(full.Assignments) != 1 || full.Assignments[0].State != "released" {
				t.Fatalf("assignment after conversion = %+v", full.Assignments)
			}
			assigned, _, err := w.List(context.Background(), "", "session-a", "open", "")
			if err != nil || len(assigned) != 0 {
				t.Fatalf("old Session still owns the item: %+v %v", assigned, err)
			}
			if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
				ExpectedVersion: converted.Item.Version, Kind: kind, Actor: "device:phone",
			}, nil); err != nil {
				t.Fatalf("convert back to %s: %v", kind, err)
			}
		})
	}
}

func TestConversionRefusesAnItemThatStartedImplementation(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
		TerminalID: "terminal-a", Actor: "local",
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
		ExpectedVersion: started.Item.Version, Kind: work.KindPlan, Actor: "local",
	}, nil); err == nil || !strings.Contains(err.Error(), "item_not_convertible") {
		t.Fatalf("started item conversion = %v", err)
	}
}

func TestConversionWaitsForAnInFlightReassignment(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
		TerminalID: "terminal-a", Actor: "local",
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: owned.Item.Version, Mode: "new_session", Assistant: "codex", Actor: "local",
	}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
		ExpectedVersion: pending.Item.Version, Kind: work.KindPlan, Actor: "local",
	}, nil); err == nil || !strings.Contains(err.Error(), "assignment_pending") {
		t.Fatalf("conversion during reassignment = %v", err)
	}
}
