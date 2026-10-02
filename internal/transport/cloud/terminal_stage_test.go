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

	lines = nil
	for i := 0; i < CloudTerminalObservationRowsLimit+10; i++ {
		l.terminalMu.Lock()
		line := l.terminalStageLocked(c, "receipt_published", "input", "ok")
		l.terminalMu.Unlock()
		if line != "" {
			lines = append(lines, line)
		}
	}
	if c.stageLines != CloudTerminalObservationRowsLimit || len(lines) != CloudTerminalObservationRowsLimit-2 {
		t.Fatalf("stage lines are not bounded: %d logged, %d counted", len(lines), c.stageLines)
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
