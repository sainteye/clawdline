//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestLifecyclePreservesPendingTurnAfterFinalLookingTextAndCancel(t *testing.T) {
	var mu sync.Mutex
	var events []WireEvent
	text := make(chan struct{})
	c, _ := fakePermission(t, "lifecycle-held", func(context.Context, json.RawMessage) error {
		mu.Lock()
		defer mu.Unlock()
		if len(events) == 0 || events[len(events)-1].Stage != "received" || !strings.Contains(string(events[len(events)-1].Frame), "synthetic finished") {
			t.Error("incoming raw frame was not saved before interpretation")
		}
		close(text)
		return nil
	}, nil, func(e WireEvent) error {
		mu.Lock()
		defer mu.Unlock()
		e.Frame = append([]byte(nil), e.Frame...)
		events = append(events, e)
		return nil
	})
	ready(t, c)
	if got := c.Lifecycle(); got.Phase != "idle" || !got.TraceEnabled {
		t.Fatal(got)
	}
	done := make(chan error, 1)
	accepted := make(chan struct{})
	go func() {
		_, err := c.PromptAccepted(context.Background(), "synthetic request", func() { close(accepted) })
		done <- err
	}()
	<-accepted
	<-text
	before := c.Lifecycle()
	if before.Phase != "prompt_pending" || before.RequestID == "" || before.CompletedAt != 0 || before.StopReason != "" {
		t.Fatal(before)
	}
	if err := c.Interrupt(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	after := c.Lifecycle()
	if after.Phase != "cancel_requested" || after.RequestID != before.RequestID || after.CancelRequestedAt == 0 || after.CompletedAt != 0 {
		t.Fatal(after)
	}
	c.Cancel()
	if err := <-done; err == nil {
		t.Fatal("missing prompt response became success")
	}
	if got := c.Lifecycle(); got.Phase != "closed" || got.Failure == "" {
		t.Fatal(got)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatal("wire ordering", e)
		}
	}
}

func TestLifecycleObservesUpdatesWithoutInventingCompletion(t *testing.T) {
	kinds := map[string]bool{}
	var mu sync.Mutex
	c, _ := fakePermission(t, "lifecycle-updates", func(_ context.Context, raw json.RawMessage) error {
		var m struct {
			Params struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		mu.Lock()
		kinds[m.Params.Update.Kind] = true
		mu.Unlock()
		return nil
	}, nil)
	ready(t, c)
	result, err := c.Prompt(context.Background(), "synthetic request")
	if err != nil || result.StopReason != "end_turn" {
		t.Fatal(result, err)
	}
	got := c.Lifecycle()
	if got.Phase != "idle" || got.StopReason != "end_turn" || got.CompletedAt == 0 || got.RequestID != "" || got.Unrecognized != 1 {
		t.Fatal(got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, kind := range []string{"user_message_chunk", "agent_message_chunk", "agent_thought_chunk", "tool_call", "tool_call_update", "plan", "usage_update", "available_commands_update", "current_mode_update", "config_option_update", "session_info_update", "notice", "compaction_update", "compaction_summary_chunk", "future_update"} {
		if !kinds[kind] {
			t.Error("not delivered", kind)
		}
	}
}

func TestTraceFailureDoesNotFailCompletedProtocol(t *testing.T) {
	c, _ := fakePermission(t, "normal", nil, nil, func(WireEvent) error { return errors.New("synthetic disk failure") })
	ready(t, c)
	if _, err := c.Prompt(context.Background(), "synthetic request"); err != nil {
		t.Fatal(err)
	}
	if got := c.Lifecycle(); got.Phase != "idle" || got.TraceError != "trace_write_failed" || !got.TraceEnabled {
		t.Fatal(got)
	}
}

func TestLifecycleRetainsRPCCancellationWithoutInferringReadiness(t *testing.T) {
	c, _ := fake(t, "prompt-rpc-error", nil)
	ready(t, c)
	_, err := c.Prompt(context.Background(), "synthetic request")
	requireError(t, err, "rpc_error", Acknowledged)
	got := c.Lifecycle()
	if got.Phase != "request_failed" || got.RPCError == nil || *got.RPCError != -32800 || got.CompletedAt == 0 || got.RequestID != "" || got.StopReason != "" {
		t.Fatal(got)
	}
}

func TestMalformedRawFrameIsObservedBeforeDecodeFailure(t *testing.T) {
	var raw []byte
	c, _ := fakePermission(t, "malformed", nil, nil, func(e WireEvent) error {
		if e.Stage == "received" {
			raw = append([]byte(nil), e.Frame...)
		}
		return nil
	})
	_, err := c.Initialize(context.Background())
	requireError(t, err, "invalid_frame", Uncertain)
	if string(raw) != "{bad" || c.Lifecycle().Failure != "invalid_frame" {
		t.Fatal("missing pre-decoder evidence", string(raw), c.Lifecycle())
	}
}

func TestLifecycleSummaryWithholdsUnknownStopReasonAndKeepsClosed(t *testing.T) {
	c := &Client{}
	c.requestStarted("1", "session/prompt", false)
	c.requestSettled("1", json.RawMessage(`{"stopReason":"synthetic private diagnostic"}`), nil)
	if got := c.Lifecycle(); got.Phase != "idle" || got.StopReason != "unrecognized" {
		t.Fatal(got)
	}
	c.lifeMu.Lock()
	c.lifecycle.Phase = "closed"
	c.lifeMu.Unlock()
	c.requestStarted("2", "session/prompt", false)
	c.requestSettled("1", json.RawMessage(`{"stopReason":"end_turn"}`), nil)
	if got := c.Lifecycle(); got.Phase != "closed" {
		t.Fatal("late completion reopened a closed worker", got)
	}
}

func TestResponseIdentityDiagnosticsNeedNoRawTrace(t *testing.T) {
	for _, tc := range []struct{ id, kind, relation string }{
		{`4`, "number", "matching"}, {`3`, "number", "previous"}, {`5`, "number", "future"},
		{`"4"`, "string", "different"}, {`"private-account-id"`, "string", "different"},
		{`null`, "null", "different"}, {``, "missing", "different"}, {`{}`, "invalid", "different"},
	} {
		c := &Client{}
		c.requestStarted("4", "session/prompt", false)
		frame := `{"jsonrpc":"2.0","result":{}}`
		if tc.id != "" {
			frame = `{"jsonrpc":"2.0","id":` + tc.id + `,"result":{}}`
		}
		c.observe("in", "received", []byte(frame), 0)
		raw, _ := json.Marshal(c.Lifecycle())
		var got map[string]any
		_ = json.Unmarshal(raw, &got)
		if got["response_id_kind"] != tc.kind || got["response_id_relation"] != tc.relation {
			t.Fatalf("%s: %s", tc.id, raw)
		}
		if strings.Contains(string(raw), "private-account-id") || c.Lifecycle().TraceEnabled || c.Lifecycle().CompletedAt != 0 {
			t.Fatal("diagnostics leaked contents or completed a turn", string(raw))
		}
	}
}
