package voicework

import (
	"encoding/json"
	"fmt"
	"testing"
)

func batchFixture(n int) State {
	s := New("workspace")
	for i := 0; i < n; i++ {
		s.Tasks = append(s.Tasks, Task{ID: fmt.Sprint(i), Text: fmt.Sprintf("Read %d", i), Tool: "get_session_summary", Args: map[string]any{"session_id": fmt.Sprint(i)}, Version: 1, Status: "queued"})
	}
	return s
}
func finishBatchRead(t *testing.T, s *State, w *Task) {
	t.Helper()
	if w == nil {
		t.Fatal("missing claim")
	}
	if err := s.Finish(w.ID, w.Runner, w.ActionKey, "result", "", false, 1); err != nil {
		t.Fatal(err)
	}
}
func TestAutomaticContinuationStopsAfterThreeAndRetainsTheRest(t *testing.T) {
	s := batchFixture(5)
	for i := 0; i < 3; i++ {
		finishBatchRead(t, &s, s.Claim("runner", 1))
	}
	if next := s.Claim("another-device", 2); next != nil {
		t.Fatalf("fourth automatic action ran: %s", next.ID)
	}
	if len(s.Tasks) != 5 || s.Tasks[3].Status != "queued" || s.Tasks[4].Status != "queued" {
		t.Fatal("overflow was lost")
	}
	raw, _ := json.Marshal(s)
	var restored State
	_ = json.Unmarshal(raw, &restored)
	if restored.Claim("restarted", 2) != nil {
		t.Fatal("restart renewed allowance")
	}
	offer := restored.Continuation(3)
	if offer == nil || len(offer.Tasks) != 2 {
		t.Fatal("missing remaining pair")
	}
	if err := restored.Continue(*offer, 3, "", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		finishBatchRead(t, &restored, restored.Claim("runner", 3))
	}
	restored.Tasks = append(restored.Tasks, Task{ID: "later", Text: "later", Tool: "list_machines", Args: map[string]any{}, Version: 1, Status: "queued"})
	if restored.Claim("runner", 4) != nil {
		t.Fatal("new work stole an unused reviewed slot")
	}
	if restored.Continuation(3) == nil {
		t.Fatal("short batch cannot continue")
	}
}
func TestContinuationWaitsForRunningReadsAndBindsExactVersions(t *testing.T) {
	s := batchFixture(6)
	first := s.Claim("one", 1)
	second := s.Claim("two", 1)
	if s.Continuation(3) != nil {
		t.Fatal("offered before settled")
	}
	finishBatchRead(t, &s, first)
	third := s.Claim("one", 2)
	if s.Claim("another", 2) != nil {
		t.Fatal("exceeded parallel or batch budget")
	}
	finishBatchRead(t, &s, second)
	if s.Continuation(3) != nil {
		t.Fatal("third still running")
	}
	finishBatchRead(t, &s, third)
	offer := s.Continuation(3)
	s.Tasks[3].Version++
	if s.Continue(*offer, 3, "", "") != ErrConflict {
		t.Fatal("stale specification approved")
	}
	offer = s.Continuation(3)
	if s.Continue(*offer, 2, "", "") != ErrConflict {
		t.Fatal("changed configured limit approved")
	}
}
func TestSpokenContinuationNeedsStoredExplicitSameCallConsent(t *testing.T) {
	s := batchFixture(4)
	for i := 0; i < 3; i++ {
		finishBatchRead(t, &s, s.Claim("runner", 1))
	}
	offer := s.Continuation(3)
	_, _ = s.AddInput(Input{ID: "yes", Call: "call", Text: "可以", Deferred: true})
	if s.Continue(*offer, 3, "call", "yes") != ErrConflict {
		t.Fatal("bare yes accepted")
	}
	_, _ = s.AddInput(Input{ID: "explicit", Call: "call", Text: "可以，繼續。", Deferred: true})
	if s.Continue(*offer, 3, "other-call", "explicit") != ErrConflict {
		t.Fatal("other call accepted")
	}
	if err := s.Continue(*offer, 3, "call", "explicit"); err != nil {
		t.Fatal(err)
	}
	if !s.Inputs[1].Planned {
		t.Fatal("consent not consumed")
	}
	if s.Continue(*offer, 3, "call", "explicit") != ErrConflict {
		t.Fatal("replayed consent")
	}
}
func TestBatchNeverApprovesAnImportantWriteAndExplicitRetryIsSeparate(t *testing.T) {
	s := batchFixture(4)
	for i := 0; i < 3; i++ {
		finishBatchRead(t, &s, s.Claim("runner", 1))
	}
	s.Tasks = append(s.Tasks, Task{ID: "write", Text: "Send", Tool: "send_message_to_session", Args: map[string]any{}, Version: 1, Status: "queued", Priority: 10})
	if s.Claim("runner", 2) != nil || s.Tasks[4].Status != "waiting_confirmation" {
		t.Fatal("batch approved write")
	}
	s.Tasks[3].Status = "failed"
	if err := s.Edit("3", 1, "resume", 3); err != nil {
		t.Fatal(err)
	}
	if task := s.Claim("runner", 3); task == nil || task.ID != "3" {
		t.Fatal("explicit retry was blocked by automatic allowance")
	}
}
