package cloudops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A hosted page's failure rows reach this machine's log, one line each, with
// nothing in them a page did not mean to say — and a batch resent because its
// receipt was lost is acknowledged without being written twice.
func TestAPageReportsItsReadFailuresIntoTheLog(t *testing.T) {
	var lines []string
	at := time.UnixMilli(1_760_000_000_000)
	b := Bridge{MachineID: "mac-01", ViewerEvents: &ViewerEventLog{
		Log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) },
		Now: func() time.Time { return at },
	}}
	b.Authority = func(_ context.Context, _ string, _ ed25519.PublicKey, requiresWriteGate bool) Authority {
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: !requiresWriteGate}
	}
	batch := map[string]any{"v": 1, "batch_id": "b1c2d3e4-batch", "created_at_ms": 1, "device": "web-1", "tab": "t",
		"web_build": "abc123", "completeness": map[string]any{"dropped_rate_limited": 2},
		"rows": []any{map[string]any{"n": 7, "at_ms": at.Add(-12 * time.Second).UnixMilli(), "event": "cloud.read.failed", "tab": "t",
			"data": map[string]any{"word": "transcript", "stage": "viewer_refused", "code": "execution_generation_changed",
				"cond": "marker_stale", "marker_age_s": 312, "text": "never written", "title": "never written either",
				"note": "two words\nand a line"}}}}
	send := func() Answer {
		cmd := request(t, ClassCtl, map[string]any{"type": "diagnostics.events", "session": MachineReplySession,
			"request": "req-events", "batch": batch})
		cmd.Sender = "web_viewer"
		return b.Handle(context.Background(), cmd)
	}
	first := send()
	if first.Status != 200 {
		t.Fatalf("answered %d %s: %s", first.Status, first.Code, first.Payload)
	}
	body, _ := answerOf(t, first)["body"].(map[string]any)
	if body["batch_id"] != "b1c2d3e4-batch" || body["rows"] != float64(1) || body["duplicate"] != false {
		t.Fatalf("receipt %v", body)
	}
	if len(lines) != 2 {
		t.Fatalf("wanted one row line and one summary line, got %q", lines)
	}
	want := "cloud: viewer reported: event=cloud.read.failed age_s=12 code=execution_generation_changed cond=marker_stale marker_age_s=312 note=two_words_and_a_line stage=viewer_refused word=transcript sender=web_viewer batch=b1c2d3e4 n=7"
	if lines[0] != want {
		t.Fatalf("row line\n got %s\nwant %s", lines[0], want)
	}
	if strings.Contains(strings.Join(lines, "\n"), "never written") {
		t.Fatalf("a field the page may not send was written: %q", lines)
	}
	if !strings.Contains(lines[1], "dropped_rate_limited=2") || !strings.Contains(lines[1], "web_build=abc123") {
		t.Fatalf("summary line %s", lines[1])
	}
	again := send()
	body, _ = answerOf(t, again)["body"].(map[string]any)
	if again.Status != 200 || body["duplicate"] != true || len(lines) != 2 {
		t.Fatalf("a resent batch was written again: %d %v %q", again.Status, body, lines)
	}
}

// The page drops a batch whose bytes are refused and keeps sending the rows
// behind it; it holds them, without retrying, on any other 4xx. Both are
// answers rather than silence, and a daemon without the log answers the word
// it always did.
func TestABatchThisMachineCannotReadIsRefusedByItsContent(t *testing.T) {
	b := Bridge{MachineID: "mac-01", ViewerEvents: &ViewerEventLog{}}
	ask := func(b Bridge, batch map[string]any) Answer {
		return b.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "diagnostics.events",
			"session": MachineReplySession, "request": "req", "batch": batch}))
	}
	if a := ask(b, map[string]any{"v": 2, "batch_id": "x", "rows": []any{}}); a.Code != "viewer_events_malformed" || a.Status != 400 {
		t.Fatalf("a batch of another version answered %d %s", a.Status, a.Code)
	}
	if a := ask(b, map[string]any{"v": 1, "batch_id": "x", "rows": []any{map[string]any{"n": 1}}}); a.Code != "viewer_events_malformed" {
		t.Fatalf("a row without an event answered %d %s", a.Status, a.Code)
	}
	errorOf, _ := answerOf(t, ask(b, map[string]any{"v": 1, "batch_id": "", "rows": []any{}}))["error"].(map[string]any)
	if errorOf["layer"] != "mac_route" {
		t.Fatalf("a content refusal must be in the route layer, which the page drops on: %v", errorOf)
	}
	if a := ask(Bridge{MachineID: "mac-01"}, map[string]any{"v": 1, "batch_id": "x", "rows": []any{}}); a.Code != "unknown_command" {
		t.Fatalf("a bridge with no log answered %d %s", a.Status, a.Code)
	}
}

// A row's line is cut at its bound, between characters, and says so.
func TestAReportedRowLineIsBounded(t *testing.T) {
	data := map[string][]byte{}
	for i := 0; i < 40; i++ {
		data[fmt.Sprintf("f%02d", i)] = []byte(`"` + strings.Repeat("界", 60) + `"`)
	}
	row := viewerEventRow{N: 1, Event: "cloud.read.failed", Data: map[string]json.RawMessage{}}
	for k, v := range data {
		row.Data[k] = v
	}
	line := viewerEventLine("s", "b", row, time.Now())
	if len(line) > ViewerEventsLineBytesLimit || !strings.HasSuffix(line, " …cut") || !utf8.ValidString(line) {
		t.Fatalf("line of %d bytes, suffix %q", len(line), line[len(line)-8:])
	}
}
