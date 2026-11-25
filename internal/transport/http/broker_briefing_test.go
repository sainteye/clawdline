package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// The task row says how the briefing's typing went and why a task that never
// started failed, as stable codes; a task with no briefing carries neither.
func TestTheTaskRowSummarizesTheBriefing(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{broker: &orchestrator.Broker{Store: st}, store: st}
	entered := time.Unix(1000, 0)
	last := time.Unix(1300, 0)
	row := s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "claude", State: orchestrator.StateSpawnFailed,
		SpawnError: "terminal: held after enter",
		Briefing:   &orchestrator.Briefing{ShownAt: entered, EnteredAt: entered, HeldSince: entered, Enters: 2, LastEnterAt: last, CoveredAt: last, ResumedAt: last}})
	body, _ := json.Marshal(row)
	var got struct {
		Briefing           map[string]any `json:"briefing"`
		SpawnFailureReason string         `json:"spawn_failure_reason"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"attempts": float64(3), "last_attempt_at": float64(1300), "blocked": "dialog", "deferred_by_restart": true}
	if len(got.Briefing) != len(want) {
		t.Fatalf("briefing = %v, want %v", got.Briefing, want)
	}
	for k, v := range want {
		if got.Briefing[k] != v {
			t.Fatalf("briefing = %v, want %v", got.Briefing, want)
		}
	}
	if got.SpawnFailureReason != "dialog_unanswered" {
		t.Fatalf("spawn_failure_reason = %q", got.SpawnFailureReason)
	}

	row = s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "claude", State: orchestrator.StateBriefed})
	body, _ = json.Marshal(row)
	var plain map[string]json.RawMessage
	_ = json.Unmarshal(body, &plain)
	if _, ok := plain["briefing"]; ok {
		t.Fatalf("a task with no briefing carries one: %s", body)
	}
	if _, ok := plain["spawn_failure_reason"]; ok {
		t.Fatalf("a running task carries a failure reason: %s", body)
	}
}
