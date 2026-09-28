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

	converted, err := w.ConvertPlan(context.Background(), plan.Item.ID, ConvertPlanV2{
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

	if _, err := w.ConvertPlan(context.Background(), plan.Item.ID, ConvertPlanV2{
		ExpectedVersion: converted.Item.Version, Kind: work.KindIssue, Actor: "device:phone",
	}, nil); err == nil || !strings.Contains(err.Error(), "not_plan") {
		t.Fatalf("second conversion = %v", err)
	}
}

func TestAPlanOnlyConvertsIntoAnExecutableKind(t *testing.T) {
	w := newWorkV2Test(t)
	plan := createWorkV2Test(t, w, work.KindPlan)
	for _, kind := range []work.Kind{work.KindPlan, work.KindRefactor, work.Kind("unknown")} {
		if _, err := w.ConvertPlan(context.Background(), plan.Item.ID, ConvertPlanV2{
			ExpectedVersion: plan.Item.Version, Kind: kind, Actor: "local",
		}, nil); err == nil || !strings.Contains(err.Error(), "conversion_kind_not_executable") {
			t.Fatalf("convert to %q = %v", kind, err)
		}
	}
}
