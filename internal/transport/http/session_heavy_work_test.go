package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

func TestOnlyTheRunningHeavyCallbackOwnsTheSessionMarker(t *testing.T) {
	const taskID = "c0000000-0000-4000-8000-000000000001"
	const sessionID = "d0000000-0000-4000-8000-000000000001"
	holder := orchestrator.LeaseHolder{CallbackTaskID: taskID, Session: sessionID, Phase: "running", Liveness: "alive", Reason: "go test"}
	record := orchestrator.Record{ID: taskID, State: orchestrator.StateBriefed,
		Callback: &orchestrator.Callback{Intent: orchestrator.CallbackIntentHeavy}, Root: &orchestrator.RootRef{SessionID: sessionID}}
	assert := func(name string, h orchestrator.LeaseHolder, r orchestrator.Record, want bool) {
		t.Helper()
		id, work := heavyCallbackOwner(&h, r)
		if (work != nil) != want {
			t.Fatalf("%s: id %q, work %+v, want marker %t", name, id, work, want)
		}
		if want && (id != sessionID || work.TaskID != taskID || work.Reason != "go test") {
			t.Fatalf("%s: id %q, work %+v", name, id, work)
		}
	}
	assert("running", holder, record, true)
	h := holder
	h.Phase = "waiting"
	assert("slot held while waiting for memory", h, record, false)
	h = holder
	h.Liveness = "unknown"
	assert("unproven holder", h, record, false)
	h = holder
	h.CallbackTaskID = ""
	assert("direct heavy command", h, record, false)
	r := record
	r.Callback = &orchestrator.Callback{Intent: orchestrator.CallbackIntentWait}
	assert("wait callback", holder, r, false)
	r = record
	r.Callback = &orchestrator.Callback{}
	assert("legacy callback", holder, r, false)
	r = record
	r.Root = &orchestrator.RootRef{SessionID: "someone-else"}
	assert("other session", holder, r, false)
	r = record
	r.State = orchestrator.StateSuccess
	assert("completed callback", holder, r, false)
}
