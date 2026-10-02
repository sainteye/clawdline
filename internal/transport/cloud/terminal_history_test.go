package cloud

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func TestCloudHistoryKeepsBoundedNewestWholeLines(t *testing.T) {
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = fmt.Sprintf("%04d %s", i, strings.Repeat("x", 90))
	}
	req := terminalRequest{RequestID: "synthetic-request", Connection: "synthetic-connection", TerminalID: "synthetic-terminal", Operation: "history"}
	result, err := boundedCloudHistory(lines, req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.OmittedLines <= 0 || result.OmittedLines+len(result.Lines) != len(lines) {
		t.Fatalf("history suffix metadata = %+v", result)
	}
	if len(result.Lines) == 0 || result.Lines[len(result.Lines)-1] != lines[len(lines)-1] {
		t.Fatal("newest complete line was lost")
	}
	if result.Lines[0] != lines[result.OmittedLines] {
		t.Fatal("history result is not the newest chronological suffix")
	}
	encoded, err := json.Marshal(terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
		Status: "ok", Result: result})
	if err != nil || len(encoded) > CloudTerminalHistoryReceiptBytesLimit {
		t.Fatalf("receipt bytes = %d, err = %v", len(encoded), err)
	}
}

func TestCloudHistoryRefusesOversizedLine(t *testing.T) {
	_, err := boundedCloudHistory([]string{strings.Repeat("x", 4097)}, terminalRequest{Operation: "history"})
	if err == nil {
		t.Fatal("oversized line was accepted")
	}
}

func TestCloudHistoryRefusesOversizedCaptureFromAHost(t *testing.T) {
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = strings.Repeat("x", 3000)
	}
	_, err := boundedCloudHistory(lines, terminalRequest{Operation: "history"})
	if code, ok := terminal.CodeOf(err); !ok || code != terminal.CodeHistoryTooLarge {
		t.Fatalf("capture refusal = %v", err)
	}
}

func TestCloudHistoryMaximumReceiptFitsReservedPublishSlot(t *testing.T) {
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	channel := "termr/" + strings.Repeat("m", 128) + "/" + strings.Repeat("v", 128) + "/" + strings.Repeat("c", 22)
	envelope, err := domaincloud.Seal(make([]byte, CloudTerminalHistoryReceiptBytesLimit), domaincloud.SealParams{
		Ch: channel, Seq: 9007199254740991, Ts: 9007199254740991,
		Class: domaincloud.ClassCtl, KeyID: strings.Repeat("k", 64), Sender: strings.Repeat("m", 128),
		Key: key, Signer: signer,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := envelope.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	frame := adaptercloud.PublishFrame(encoded)
	if len(frame) > 12<<10 {
		t.Fatalf("maximum history publish frame = %d bytes", len(frame))
	}
}
