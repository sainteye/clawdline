package http

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestPinnedSessionAdmissionRequiresFreshProcessAndMatchingMachine(t *testing.T) {
	if swiftstore.ProcessStart(os.Getpid()).IsZero() {
		t.Skip("this platform has no kernel process-start reader")
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, PID: os.Getpid(), State: session.StateIdle}}
	s := paneServer(t, p)
	s.executionMachineID = "mac_a"
	ctx := context.Background()
	inv := s.freshReading(ctx)
	items := inv.Assistants()
	if len(items) != 1 {
		t.Fatalf("assistant reading: %+v", inv)
	}
	got := s.observeExecutions(ctx, inv, items, []swiftstore.Live{liveOf(items[0])})
	gen := got["%19"]
	if gen == "" {
		t.Fatalf("no generation from current process: %+v", got)
	}
	if err := s.AdmitExecutionTarget(ctx, "mac_a", "%19", gen); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitExecutionTarget(ctx, "mac_b", "%19", gen); err == nil || err.Error() != "execution_machine_mismatch" {
		t.Fatalf("other machine admitted: %v", err)
	}
	p.s.PID = 0 // terminal row remains, but the process pin is unreadable
	if err := s.AdmitExecutionTarget(ctx, "mac_a", "%19", gen); err == nil || err.Error() != "execution_source_unknown" {
		t.Fatalf("unverified source admitted: %v", err)
	}
	req := httptest.NewRequest("GET", "/v1/sessions/%2519/info", nil)
	req.Header.Set("X-Clawdline-Target-Machine", "mac_a")
	req.Header.Set("X-Clawdline-Execution-Generation", gen)
	rec := httptest.NewRecorder()
	if s.admitPinnedSessionRequest(rec, req) || rec.Code != 503 {
		t.Fatalf("pinned read did not fail closed: %d %s", rec.Code, rec.Body.String())
	}
}
