package cloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/domain/capacity"
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
	settleReceiptTestRows(t, l)
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

// settleReceiptTestRows writes and settles, as delivered, every row the
// spool has ready, through the same calls the transport makes for a relay ack
// (Spool.Settle, then OnSettled). Until 2026-10-06 this helper cleared the
// connection's receiptOps and receiptRetries itself, which hid any path that
// left them behind.
func settleReceiptTestRows(t *testing.T, l *Link) {
	t.Helper()
	for {
		row := l.relay.Spool.SendNext().Row
		if row == nil {
			return
		}
		if _, err := l.relay.Spool.Settle(row.Seq, row.Recipient, adaptercloud.SettleDelivered); err != nil {
			t.Fatal(err)
		}
		l.terminalReceiptSettled(row.Recipient, row.Seq, adaptercloud.SettleDelivered)
	}
}

func TestTerminalConnectionKeepsAnsweringPastItsFirstSixtyFourRequests(t *testing.T) {
	at := time.Now()
	l, c := receiptTestLink(t, func() time.Time { return at })
	// Held-down key repeat, about thirty keys a second, for fifty seconds.
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

// A key held down with the fastest macOS repeat sends about forty requests a
// second. With 512 receipts kept for 15 s the connection filled after about
// 12.8 s and refused every key after that as terminal_busy; the limit only
// bounds memory, so the oldest receipt now goes instead.
func TestAHeldKeyIsNeverRefusedForWantOfReceiptRoom(t *testing.T) {
	at := time.Now()
	l, c := receiptTestLink(t, func() time.Time { return at })
	for n := 1; n <= 40*30; n++ {
		at = at.Add(time.Second / 40)
		receipt := sendReceiptTestRequest(t, l, c, n)
		if receipt.Error == string(terminal.CodeBusy) || receipt.Status == "missing" {
			t.Fatalf("request %d at %.2fs: %+v; a held key was refused", n, float64(n)/40, receipt)
		}
	}
	l.terminalMu.Lock()
	kept, ops, retries := len(c.receiptOrder), len(c.receiptOps), len(c.receiptRetries)
	l.terminalMu.Unlock()
	if kept > CloudTerminalReceiptsLimit || ops != 0 || retries != 0 {
		t.Fatalf("kept %d receipts (limit %d), %d unsettled ops, %d retries", kept, CloudTerminalReceiptsLimit, ops, retries)
	}
	reading := l.TerminalCapacity(capacity.CloudTerminalReceipts)
	if reading.Used != CloudTerminalReceiptsLimit || reading.Counters.Evicted == 0 || reading.Counters.LastActionAt.IsZero() {
		t.Fatalf("capacity reading %+v does not show the evictions", reading)
	}
}

// A receipt whose request id was let go early is not replayed: the next
// request with that id is answered afresh, and the stage row says a receipt
// was evicted.
func TestAnEvictedReceiptIsLoggedAsAStage(t *testing.T) {
	at := time.Now()
	l, c := receiptTestLink(t, func() time.Time { return at })
	var lines []string
	l.opts.Log = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	for n := 1; n <= CloudTerminalReceiptsLimit+1; n++ {
		sendReceiptTestRequest(t, l, c, n)
	}
	for _, line := range lines {
		if strings.Contains(line, "stage=receipt_evicted") && strings.Contains(line, "outcome=evicted") {
			return
		}
	}
	t.Fatalf("no receipt_evicted stage in %d lines", len(lines))
}

// receiptBurnLink is receiptTestLink with a spool on the same fake clock, so a
// row's attempt window can pass.
func receiptBurnLink(t *testing.T) (*Link, *terminalConnection, *terminals.Service, *adaptercloud.Spool, func(time.Duration)) {
	t.Helper()
	at := time.Now()
	clock := func() time.Time { return at }
	l, spool, c, svc, _ := terminalLifecycleFixture(t)
	burnSpool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, clock)
	if err != nil {
		t.Fatal(err)
	}
	_ = spool
	l.relay.Spool = burnSpool
	l.opts.Now = clock
	c.expires = at.Add(time.Hour)
	return l, c, svc, burnSpool, func(d time.Duration) { at = at.Add(d) }
}

// burnAndSettle is what the transport's drain does each tick: burn the rows
// whose attempt window passed, then tell OnSettled about each one.
func burnAndSettle(l *Link, spool *adaptercloud.Spool) int {
	spool.BurnExpired()
	n := 0
	for _, row := range spool.TakeBurned() {
		l.terminalReceiptSettled(row.Recipient, row.Seq, adaptercloud.SettleBurned)
		n++
	}
	return n
}

// The relay stopped answering for longer than the spool's attempt window (30
// s) while the socket stayed up: the frame awaiting its ack was burned, and
// nothing ever told the connection, so framePending kept every later screen
// back for the rest of the connection's life.
func TestAFrameTheRelayNeverAnsweredDoesNotHoldBackTheNextScreen(t *testing.T) {
	l, c, svc, spool, advance := receiptBurnLink(t)
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, terminal.Frame{Rev: "a", At: time.Now(), Lines: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if row := spool.SendNext().Row; row == nil {
		t.Fatal("the frame was not written")
	}
	advance(adaptercloud.DefaultSpoolLimits().AttemptWindow + time.Second)
	if burnAndSettle(l, spool) != 1 {
		t.Fatal("the unanswered frame was not burned")
	}
	if l.getTerminalConnection(c.viewer, c.id) != c {
		t.Fatal("an unanswered frame closed the connection")
	}
	if c.framePending || c.frameCandidate != nil {
		t.Fatal("the burned frame is still pending")
	}
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, terminal.Frame{Rev: "b", At: time.Now(), Lines: []string{"b"}}); err != nil {
		t.Fatalf("the next screen after the relay came back: %v", err)
	}
	row := spool.SendNext().Row
	if row == nil || !c.framePending || c.framePendingSeq != row.Seq {
		t.Fatal("the next screen did not go out")
	}
	if _, err := spool.Settle(row.Seq, row.Recipient, adaptercloud.SettleDelivered); err != nil {
		t.Fatal(err)
	}
	l.terminalReceiptSettled(row.Recipient, row.Seq, adaptercloud.SettleDelivered)
	if c.framePending || c.frameBase == nil {
		t.Fatal("the delivered screen did not become the base")
	}
}

// Receipts the relay never answered are let go too: receiptOps and
// receiptRetries held them until the connection ended.
func TestAReceiptTheRelayNeverAnsweredIsLetGo(t *testing.T) {
	l, c, _, spool, advance := receiptBurnLink(t)
	for n := 1; n <= 3; n++ {
		id := fmt.Sprintf("12345678-1234-4123-8123-%012x", n)
		if err := l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: id,
			Connection: c.id, Operation: "input", TerminalID: string(c.terminalID), Status: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	for spool.SendNext().Row != nil {
	}
	advance(adaptercloud.DefaultSpoolLimits().AttemptWindow + time.Second)
	if n := burnAndSettle(l, spool); n != 3 {
		t.Fatalf("burned %d receipts, want 3", n)
	}
	l.terminalMu.Lock()
	ops, retries := len(c.receiptOps), len(c.receiptRetries)
	l.terminalMu.Unlock()
	if ops != 0 || retries != 0 {
		t.Fatalf("%d receipt ops and %d retries outlived their burned rows", ops, retries)
	}
	if l.getTerminalConnection(c.viewer, c.id) != c {
		t.Fatal("an unanswered input receipt closed the connection; the viewer reconciles a lost receipt itself")
	}
}
