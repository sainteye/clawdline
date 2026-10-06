package cloud

import (
	"context"
	"fmt"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

// The daemon half of the browser's terminal timeline: publication and relay
// settlement of a receipt, in fixed words only.
func TestTerminalReceiptStagesAreContentFreeAndBounded(t *testing.T) {
	l, _, c, _, _ := terminalLifecycleFixture(t)
	var lines []string
	l.opts.Log = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: testRequestID, Connection: c.id, Operation: "capture", TerminalID: string(c.terminalID),
		Status: "ok", Result: map[string]any{"lines": []string{"secret screen"}}}); err != nil {
		t.Fatal(err)
	}
	var seq uint64
	for s := range c.receiptOps {
		seq = s
	}
	l.terminalReceiptSettled(terminalReceiptChannel("machine", c), seq, adaptercloud.SettleDelivered)
	want := []string{
		"cloud terminal stage n=1 stage=receipt_published op=capture outcome=ok",
		"cloud terminal stage n=2 stage=receipt_settled op=capture outcome=delivered",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stages:\n%s", strings.Join(lines, "\n"))
	}
	for _, secret := range []string{testRequestID, c.id, c.viewer, c.keyID, string(c.terminalID), "secret screen", "machine/"} {
		if strings.Contains(strings.Join(lines, "\n"), secret) {
			t.Fatalf("stage lines carry %q", secret)
		}
	}
	if len(c.receiptOps) != 0 {
		t.Fatal("a settled receipt kept its operation")
	}

}

// A connection that types for a while and then breaks must still log the
// break: routine stages thin out after the first rows, failures do not stop.
func TestTerminalStagesKeepLoggingFailuresAfterRoutineRows(t *testing.T) {
	l, _, c, _, _ := terminalLifecycleFixture(t)
	stage := func(name, op, outcome string) string {
		l.terminalMu.Lock()
		defer l.terminalMu.Unlock()
		return l.terminalStageLocked(c, name, op, outcome)
	}
	var lines []string
	for i := 0; i < 1000; i++ {
		if line := stage("receipt_published", "input", "ok"); line != "" {
			lines = append(lines, line)
		}
		if line := stage("receipt_settled", "input", "delivered"); line != "" {
			lines = append(lines, line)
		}
	}
	// 2000 routine stages: the first 128 in full, then every 64th.
	if want := CloudTerminalObservationRowsLimit + (2000-CloudTerminalObservationRowsLimit)/CloudTerminalStageRoutineEvery; len(lines) != want {
		t.Fatalf("routine stages logged %d lines, want %d", len(lines), want)
	}
	if last := lines[len(lines)-1]; last != "cloud terminal stage n=1984 stage=receipt_settled op=input outcome=delivered" {
		t.Fatalf("the last sampled routine line is %q", last)
	}
	for _, outcome := range []string{"refused", "unknown", "rate_limited", "viewer_offline"} {
		if got := stage("receipt_published", "input", outcome); !strings.HasSuffix(got, "op=input outcome="+outcome) {
			t.Fatalf("a %s stage after 2000 routine rows logged %q", outcome, got)
		}
	}
	if got := stage("receipt_published", "input", "ok"); got != "" {
		t.Fatalf("routine stage between samples logged %q", got)
	}
	// A storm of failures is bounded too: the first 128 in full, then every 16th.
	failures := 4
	for i := 0; i < 1000; i++ {
		if stage("receipt_settled", "input", "peer_error") != "" {
			failures++
		}
	}
	if want := CloudTerminalObservationRowsLimit + (1004-CloudTerminalObservationRowsLimit)/CloudTerminalStageNotableEvery; failures != want {
		t.Fatalf("failure stages logged %d lines, want %d", failures, want)
	}
}

func TestTerminalStageWordsRejectFreeText(t *testing.T) {
	for in, want := range map[string]string{"capture": "capture", "": "-", "rm -rf /": "unrecognized",
		strings.Repeat("a", 49): "unrecognized", "viewer_offline": "viewer_offline"} {
		if got := terminalStageWord(in); got != want {
			t.Fatalf("terminalStageWord(%q) = %q, want %q", in, got, want)
		}
	}
}
