//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// 2026-11-11 13:05: a turn ended with a deploy running as a background task
// and a `schedule` timer set. The timer woke Gemini every 20-30 s for six
// minutes; it checked the deploy and wrote short progress lines, and when the
// deploy finished it wrote its report. None of it crossed ACP, and the
// conversation showed nothing after "Waiting for the backend build".
func TestTimerWokenTurnsBeforeTheTaskFinishesAreShown(t *testing.T) {
	t.Setenv("CLW_NATIVE_WAKE_PEER", "yes")
	prior := backgroundPollInterval
	backgroundPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { backgroundPollInterval = prior })
	h, row, opts := persistentFixture(t)
	if err := h.Send(context.Background(), row, "deploy"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	c, _ := h.lookup(row)
	c.mu.Lock()
	native := c.row.ConversationID
	c.mu.Unlock()
	logPath := filepath.Join(opts.AccountDir, "antigravity-acp", "brain", native, ".system_generated", "logs", "transcript_full.jsonl")
	appendRows := func(rows string) {
		t.Helper()
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(rows); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	shows := func(what string) bool {
		p, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Entries {
			if e.Role == "assistant" && strings.Contains(e.Text, what) {
				return true
			}
		}
		return false
	}
	waitShows := func(what string) contract.TranscriptPage {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !shows(what) {
			if time.Now().After(deadline) {
				p, _ := h.Transcript(row, 100)
				t.Fatalf("%q never appeared: %+v", what, p.Entries)
			}
			time.Sleep(5 * time.Millisecond)
		}
		p, _ := h.Transcript(row, 100)
		return p
	}
	// A timer Gemini set fires before the deploy ends; Gemini checks and writes.
	appendRows(`{"step_index":4,"source":"MODEL","type":"GENERIC","status":"RUNNING","created_at":"2026-11-11T05:05:00Z","content":"Tool is running as a background task with task id: ` + native + `/task-4\nTask Description: Timer: 20s, Prompt: Check deploy\n"}` + "\n" +
		`{"step_index":5,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","created_at":"2026-11-11T05:05:20Z","content":"<SYSTEM_MESSAGE> sender=` + native + `/task-4 content=Check deploy </SYSTEM_MESSAGE>"}` + "\n" +
		`{"step_index":6,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-11T05:05:20Z","tool_calls":[{"name":"manage_task","args":{"Action":"status"}}]}` + "\n" +
		`{"step_index":7,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-11T05:05:23Z","content":"Task status: running"}` + "\n" +
		`{"step_index":8,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-11T05:05:26Z","content":"Build still in progress."}` + "\n")
	waitShows("Build still in progress.")
	// Then the deploy itself finishes and Gemini reports.
	appendRows(`{"step_index":9,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","created_at":"2026-11-11T05:10:21Z","content":"sender=` + native + `/task-2 content=Task id \"` + native + `/task-2\" finished with result:\nCommand exited with code 0.\n"}` + "\n" +
		`{"step_index":10,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-11T05:11:40Z","content":"Deploy finished."}` + "\n")
	p := waitShows("Deploy finished.")
	for _, e := range p.Entries {
		if e.Activity != nil && e.Activity.ToolCallID == "bg-1" && e.Activity.Status != "completed" {
			t.Fatalf("the deploy tool is %q", e.Activity.Status)
		}
	}
}

// A live 2026-11-11 run polled its task with manage_task after each timer and
// agy never logged a "finished" message: only the status result said DONE.
func TestATaskGeminiFindsDoneThroughItsStatusIsCompleted(t *testing.T) {
	t.Setenv("CLW_NATIVE_WAKE_PEER", "yes")
	prior := backgroundPollInterval
	backgroundPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { backgroundPollInterval = prior })
	h, row, opts := persistentFixture(t)
	if err := h.Send(context.Background(), row, "deploy"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	c, _ := h.lookup(row)
	c.mu.Lock()
	native := c.row.ConversationID
	c.mu.Unlock()
	logPath := filepath.Join(opts.AccountDir, "antigravity-acp", "brain", native, ".system_generated", "logs", "transcript_full.jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"step_index":4,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","created_at":"2026-11-11T05:30:43Z","content":"<SYSTEM_MESSAGE> sender=` + native + `/task-9 content=Check it </SYSTEM_MESSAGE>"}` + "\n" +
		`{"step_index":5,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-11T05:30:43Z","tool_calls":[{"name":"manage_task","args":{"Action":"status"}}]}` + "\n" +
		`{"step_index":6,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-11T05:30:48Z","content":"Task: ` + native + `/task-2\nStatus: DONE\nLog: file:///x"}` + "\n" +
		`{"step_index":7,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-11T05:31:10Z","content":"Done.\n\n[[clawdline:turn-end]]"}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		p, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		completed, said := false, false
		for _, e := range p.Entries {
			if e.Activity != nil && e.Activity.ToolCallID == "bg-1" && e.Activity.Status == "completed" {
				completed = true
			}
			if e.Role == "assistant" && strings.Contains(e.Text, TurnEndMarker) {
				t.Fatalf("the turn-end marker was shown: %q", e.Text)
			}
			if e.Role == "assistant" && e.Text == "Done." {
				said = true
			}
		}
		if completed && said {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed=%v said=%v: %+v", completed, said, p.Entries)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
