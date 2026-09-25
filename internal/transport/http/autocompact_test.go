package http

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// `claude_auto_compact_window` (orchestrator compact.go) through the daemon:
// the settings file's row, the broker's reading of it, and the usage answers
// that say which window a session was launched with.

// The settings file and a task.json take one range: the file's row is the
// broker's bounds, plus 0 for none.
func TestTheWindowSettingHasTheBrokersBounds(t *testing.T) {
	key, ok := nextconfig.SettableByName("claude_auto_compact_window")
	if !ok {
		t.Fatal("claude_auto_compact_window is not a key the settings route takes")
	}
	lo, hi := orchestrator.AutoCompactBounds()
	if key.Kind != "int" || key.Min != float64(lo) || key.Max != float64(hi) || !key.Zero {
		t.Fatalf("the settings row is %+v; the broker's bounds are %d…%d plus 0", key, lo, hi)
	}
}

// The round trip the console's settings page and `clawdline setting` make:
// written, answered, read by the broker at its next launch; 0 turns it off;
// a value outside the range is refused by name and leaves the file alone.
func TestTheWindowSettingRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}
	if got := brokerAutoCompactWindow(s); got != 0 {
		t.Fatalf("no file reads as %d, want 0 (none)", got)
	}
	rec, snap, refusal := settingsCall(t, s, http.MethodPost, "application/json", `{"claude_auto_compact_window":60000}`)
	if rec.Code != http.StatusOK || snap.ClaudeAutoCompactWindow == nil || *snap.ClaudeAutoCompactWindow != 60000 {
		t.Fatalf("set: %d %s %v", rec.Code, refusal.Error, snap.ClaudeAutoCompactWindow)
	}
	if got := brokerAutoCompactWindow(s); got != 60000 {
		t.Fatalf("the broker reads %d after the write", got)
	}
	for _, bad := range []string{"49999", "1000001", "-1", "60000.5", `"60000"`} {
		rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", `{"claude_auto_compact_window":`+bad+`}`)
		if rec.Code != http.StatusBadRequest || refusal.Error != "invalid_auto_compact_window" {
			t.Errorf("%s: %d %s", bad, rec.Code, refusal.Error)
		}
	}
	if got := brokerAutoCompactWindow(s); got != 60000 {
		t.Fatalf("a refused write changed the setting to %d", got)
	}
	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json", `{"claude_auto_compact_window":0}`)
	if rec.Code != http.StatusOK || snap.ClaudeAutoCompactWindow == nil || *snap.ClaudeAutoCompactWindow != 0 {
		t.Fatalf("off: %d %v", rec.Code, snap.ClaudeAutoCompactWindow)
	}
	if got := brokerAutoCompactWindow(s); got != 0 {
		t.Fatalf("off, the broker reads %d", got)
	}
}

// Each session's usage says the window Clawdline launched it with, from the
// record of its task or Root Assignment; a session Clawdline did not launch
// says null, not 0.
func TestUsageSaysWhichWindowASessionWasLaunchedWith(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	st := u.s.store
	u.s.broker = &orchestrator.Broker{Store: st, Dir: u.s.cfg.Dir}
	ctx := context.Background()
	record, _ := json.Marshal(orchestrator.Record{ID: "task-1", Assistant: "claude", AutoCompactWindow: ptr(int64(60000))})
	if err := st.SaveBrokerTask(ctx, store.BrokerRow{ID: "task-1", Project: "/p", Assistant: "claude", State: "running",
		CreatedAt: u.at, UpdatedAt: u.at, SecretHash: "h", Record: record}, nil); err != nil {
		t.Fatal(err)
	}
	assignment, _ := json.Marshal(map[string]any{"id": "ra-1", "assistant": "claude",
		"executor": map[string]any{"terminal_id": "%5", "backend": "tmux", "opened_at": 1, "auto_compact_window": 0}})
	if err := st.CreateOpened(ctx, store.TableRootAssignments, store.Opened{ID: "ra-1", State: "briefed",
		Record: assignment, CreatedAt: u.at, UpdatedAt: u.at}, nil); err != nil {
		t.Fatal(err)
	}
	u.row(store.UsageRow{Assistant: "claude", Conversation: "sess-root", RootAssignment: "ra-1", ReadAt: u.at, OpeningRead: true},
		map[string]float64{"impl": 1}, `{"calls":2,"peak_context":1000}`)

	window := func(path string) *int64 {
		t.Helper()
		code, body := u.get(path, nil)
		var s contract.UsageSession
		if code != http.StatusOK || json.Unmarshal(body, &s) != nil {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		return s.AutoCompactWindow
	}
	if w := window("/v1/usage/sessions/sess-read"); w == nil || *w != 60000 {
		t.Fatalf("a child launched at 60000 answers %v", w)
	}
	if w := window("/v1/usage/sessions/sess-root"); w == nil || *w != 0 {
		t.Fatalf("a Root Assignment launched with none answers %v, want 0", w)
	}
	if w := window("/v1/usage/sessions/sess-gone"); w != nil {
		t.Fatalf("a session Clawdline did not launch answers %d, want null", *w)
	}
	code, body := u.get("/v1/usage/tasks/task-1", nil)
	var task contract.UsageTask
	if code != http.StatusOK || json.Unmarshal(body, &task) != nil || len(task.Sessions) != 1 {
		t.Fatalf("task-1: %d %s", code, body)
	}
	if w := task.Sessions[0].AutoCompactWindow; w == nil || *w != 60000 {
		t.Fatalf("the task's session answers %v", w)
	}
}

func ptr[T any](v T) *T { return &v }

// The task row a root and the console read says which window the child was
// launched with and what its task.json asked for; a Codex task says null.
func TestTheTaskRowCarriesTheWindow(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{broker: &orchestrator.Broker{Store: st}, store: st}
	row := s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "claude",
		AutoCompactWindow: ptr(int64(0)), AutoCompactRequested: ptr(int64(0))})
	body, _ := json.Marshal(row)
	var got map[string]json.RawMessage
	_ = json.Unmarshal(body, &got)
	if string(got["auto_compact_window"]) != "0" || string(got["auto_compact_requested"]) != "0" {
		t.Fatalf("a task.json null: window %s requested %s", got["auto_compact_window"], got["auto_compact_requested"])
	}
	row = s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "codex"})
	body, _ = json.Marshal(row)
	got = nil
	_ = json.Unmarshal(body, &got)
	if string(got["auto_compact_window"]) != "null" {
		t.Fatalf("a Codex task's window is %s, want null", got["auto_compact_window"])
	}
}
