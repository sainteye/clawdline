package cloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// On 2026-10-06 (22:09:07) a direct terminal connection answered 64 requests
// (a lease renewal, two commands typed and 16 vim `j`s) and refused every one
// after that as terminal_busy: the receipts it keeps for a re-sent request id
// were never let go, so a connection could serve 64 requests in its whole
// ten-minute life. A key repeat reaches that in about two seconds.
func receiptTestLink(t *testing.T, now func() time.Time) (*Link, *terminalConnection) {
	t.Helper()
	limits := adaptercloud.DefaultSpoolLimits()
	spool, err := adaptercloud.NewSpool(limits, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc := terminals.New(nil, func(terminals.Principal) error { return nil })
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: now().Add(time.Minute), receipts: map[string][]byte{}}
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: now,
		TerminalService: func() (*terminals.Service, error) { return svc, nil }},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer,
		Secret: master, KeyID: adaptercloud.MasterKeyID}
	return l, c
}

func sendReceiptTestRequest(t *testing.T, l *Link, c *terminalConnection, n int) terminalReceipt {
	t.Helper()
	id := fmt.Sprintf("12345678-1234-4123-8123-%012x", n)
	data, err := json.Marshal(terminalRequest{V: 1, Type: "terminal_request", RequestID: id,
		Connection: testConnection, Operation: "resize", TerminalID: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	l.handleTerminal(context.Background(), Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl),
		Sender: "viewer", Plaintext: data})
	l.terminalMu.Lock()
	c.receiptOps, c.receiptRetries = nil, nil // as if each receipt settled
	l.terminalMu.Unlock()
	raw := l.terminalReceipt(c, id)
	if raw == nil {
		return terminalReceipt{Status: "missing"}
	}
	var receipt terminalReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestTerminalConnectionKeepsAnsweringPastItsFirstSixtyFourRequests(t *testing.T) {
	at := time.Now()
	l, c := receiptTestLink(t, func() time.Time { return at })
	// Held-down key repeat, about thirty keys a second, for fifty seconds (the
	// test spool, which nothing settles, stops taking rows at the 1,801st).
	for n := 1; n <= 30*50; n++ {
		at = at.Add(time.Second / 30)
		receipt := sendReceiptTestRequest(t, l, c, n)
		if receipt.Error == string(terminal.CodeBusy) || receipt.Status == "missing" {
			t.Fatalf("request %d: %+v; the connection stopped answering", n, receipt)
		}
	}
}

func TestTerminalReceiptIsReplayedForARequestSentAgainWithinItsWindow(t *testing.T) {
	at := time.Now()
	l, c := receiptTestLink(t, func() time.Time { return at })
	first := sendReceiptTestRequest(t, l, c, 1)
	for n := 2; n <= 20; n++ {
		at = at.Add(100 * time.Millisecond)
		sendReceiptTestRequest(t, l, c, n)
	}
	if again := sendReceiptTestRequest(t, l, c, 1); again.Error != first.Error || again.Status != first.Status {
		t.Fatalf("re-sent request answered %+v, first answer %+v", again, first)
	}
}
