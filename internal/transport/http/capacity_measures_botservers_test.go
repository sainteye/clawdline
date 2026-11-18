package http

import "testing"

func TestBotServerCapacityCountsTheRealRegistryAndKeepsUnobservedBuffersUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	if f.s.projectAssistants.host == nil {
		t.Fatal("fixture did not install the real Host")
	}
	measures := f.s.projectAssistantCapacityMeasures()
	for _, name := range []string{"bots.server_definitions", "bots.server_processes"} {
		if got := measures[name](); !got.Known || got.Used != 0 || got.Note == "" {
			t.Errorf("fresh real registry count for %s: %+v", name, got)
		}
	}
	for _, name := range []string{"bots.server_log_bytes", "bots.server_caller_receipts"} {
		if got := measures[name](); got.Known || got.Note == "" {
			t.Errorf("installed Host occupancy was invented for %s: %+v", name, got)
		}
	}
	if got := measures["bots.server_spec_bytes"](); !got.Known || got.Note == "" {
		t.Errorf("effect-free input validator reading=%+v", got)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("measuring capacity contacted a provider")
	}
}
