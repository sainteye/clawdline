package cloud

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

type directFixture struct {
	l       *Link
	spool   *adaptercloud.Spool
	signer  domaincloud.DeviceKey
	viewer  domaincloud.DeviceKey
	master  domaincloud.ContentKey
	key     domaincloud.ContentKey
	c       *terminalConnection
	svc     *terminals.Service
	p       terminals.Principal
	inbound chan []byte
	retired chan string
}

func newDirectFixture(t *testing.T) *directFixture {
	t.Helper()
	f := &directFixture{inbound: make(chan []byte, 8), retired: make(chan string, 8),
		p: terminals.Principal{Device: "viewer", Cloud: true}}
	var err error
	if f.spool, err = adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now); err != nil {
		t.Fatal(err)
	}
	if f.signer, err = domaincloud.NewDeviceKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	if f.viewer, err = domaincloud.NewDeviceKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	if f.master, err = domaincloud.NewContentKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	if f.key, err = domaincloud.NewContentKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	tr, err := adaptercloud.New(adaptercloud.Options{RelayURL: "wss://relay.invalid", Token: &adaptercloud.TokenSource{},
		Identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "credential"},
		Signer:   f.signer, Replay: domaincloud.NewReplayWindow(64), ContentKey: f.master,
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
			return f.viewer.PublicKey(), sender == "viewer"
		},
		Inbound: func(_ domaincloud.Envelope, plain []byte, _ ed25519.PublicKey) { f.inbound <- plain },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.c = &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: f.key,
		expires: time.Now().Add(time.Minute), receipts: map[string][]byte{}, terminalID: terminal.NewID()}
	f.l = &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: time.Now},
		settings: adaptercloud.Settings{TerminalDirect: true}, transport: tr, directLoopback: true, directNoSTUN: true,
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): f.c}}
	f.l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: f.spool, MachineID: "machine", Signer: f.signer,
		Secret: f.master, KeyID: adaptercloud.MasterKeyID}
	f.l.terminalMetadata = func(_ context.Context, action string, c *terminalConnection) error {
		if action == "retire" {
			f.retired <- c.id
		}
		return nil
	}
	f.svc = terminals.New(nil, func(terminals.Principal) error { return nil })
	return f
}

// sealDirectOffer is the browser's half of the offer's sealing
// (docs/cloud-terminal-wire.md, Direct carrier).
func sealDirectOffer(t *testing.T, key domaincloud.ContentKey, connection, sdp string) json.RawMessage {
	t.Helper()
	block, err := aes.NewCipher(key.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(sdp), []byte(directOfferAAD+connection))
	body, _ := json.Marshal(map[string]string{"sealed": base64.StdEncoding.EncodeToString(sealed)})
	return body
}

// receiptAt waits for spool row seq and opens it as a terminal receipt.
func (f *directFixture) receiptAt(t *testing.T, seq uint64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		row, ok := f.spool.Row(seq)
		if ok {
			env, err := domaincloud.DecodeEnvelope(row.Sealed)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := env.Open(f.key, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(plain, &got); err != nil {
				t.Fatal(err)
			}
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("no relay row %d", seq)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// testBrowser is the browser's end: an offering peer on loopback that has
// opened the terminal data channel.
type testBrowser struct {
	pc     *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	opened chan struct{}
	msgs   chan []byte
}

func newTestBrowser(t *testing.T) (*testBrowser, string) {
	t.Helper()
	se := webrtc.SettingEngine{}
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	dc, err := pc.CreateDataChannel(directLabel, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &testBrowser{pc: pc, dc: dc, opened: make(chan struct{}), msgs: make(chan []byte, 256)}
	dc.OnOpen(func() { close(b.opened) })
	dc.OnMessage(func(m webrtc.DataChannelMessage) { b.msgs <- m.Data })
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	return b, pc.LocalDescription().SDP
}

// next reads one whole message from the machine, reassembling chunks.
func (b *testBrowser) next(t *testing.T) directMessage {
	t.Helper()
	var whole strings.Builder
	for {
		select {
		case raw := <-b.msgs:
			var m directMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if m.T != "chunk" {
				if whole.Len() != 0 {
					t.Fatal("a message arrived between another message's chunks")
				}
				return m
			}
			whole.WriteString(m.D)
			if m.I == m.N-1 {
				var inner directMessage
				if err := json.Unmarshal([]byte(whole.String()), &inner); err != nil {
					t.Fatal(err)
				}
				return inner
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no message from the machine")
		}
	}
}

func (f *directFixture) open(t *testing.T, m directMessage) (domaincloud.Envelope, map[string]any) {
	t.Helper()
	if m.T != "env" {
		t.Fatalf("got %q, want an envelope", m.T)
	}
	env, err := domaincloud.DecodeEnvelope(m.E)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.Open(f.key, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatal(err)
	}
	return env, got
}

func (f *directFixture) viewerEnvelope(t *testing.T, seq uint64, payload string) []byte {
	t.Helper()
	env, err := domaincloud.Seal([]byte(payload), domaincloud.SealParams{Ch: "termi/machine/viewer", Seq: seq,
		Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID,
		Sender: "viewer", Key: f.master, Signer: f.viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(directMessage{T: "env", E: raw})
	return data
}

func (f *directFixture) connected(t *testing.T) *testBrowser {
	t.Helper()
	b, offer := newTestBrowser(t)
	f.l.handleDirectOffer(context.Background(), f.svc, f.p, f.c, terminalRequest{RequestID: testRequestID,
		Operation: "direct_offer", Connection: testConnection, Body: sealDirectOffer(t, f.key, testConnection, offer)})
	receipt := f.receiptAt(t, 0)
	result, _ := receipt["result"].(map[string]any)
	answer, _ := result["sdp"].(string)
	if receipt["status"] != "ok" || answer == "" {
		t.Fatalf("offer receipt: %v", receipt)
	}
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the data channel did not open")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !f.l.directPeerOpen("viewer") {
		if time.Now().After(deadline) {
			t.Fatal("the machine did not see the channel open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return b
}

func TestDirectCarrierCarriesFramesAndInputOffTheRelay(t *testing.T) {
	f := newDirectFixture(t)
	b := f.connected(t)
	f.l.terminalMu.Lock()
	f.c.carrier = carrierDirect
	f.l.terminalMu.Unlock()

	ctx := context.Background()
	if err := f.l.sendTerminalFrame(ctx, f.svc, f.p, f.c, f.c.terminalID, terminal.Frame{Rev: "a", At: time.Now(), Lines: []string{"$ "}}); err != nil {
		t.Fatal(err)
	}
	if err := f.l.sendTerminalFrame(ctx, f.svc, f.p, f.c, f.c.terminalID, terminal.Frame{Rev: "b", At: time.Now(), Lines: []string{"$ l"}}); !errors.Is(err, terminals.ErrFrameDeferred) {
		t.Fatalf("a second frame went out before the viewer's ack: %v", err)
	}
	env, frame := f.open(t, b.next(t))
	if env.Ch != "term/machine/viewer/"+testConnection || env.Seq != 1 || frame["frame_seq"] != float64(1) {
		t.Fatalf("first direct frame: ch=%s seq=%d %v", env.Ch, env.Seq, frame)
	}
	if _, ok := f.spool.Row(1); ok {
		t.Fatal("a direct frame was also put on the relay")
	}
	ack, _ := json.Marshal(directMessage{T: "ack", Connection: testConnection, FrameSeq: 1})
	if err := b.dc.SendText(string(ack)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.l.terminalMu.Lock()
		pending := f.c.framePending
		f.l.terminalMu.Unlock()
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the viewer's ack did not settle the frame")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A screen larger than one chunk arrives whole.
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = fmt.Sprintf("%04d %s", i, strings.Repeat("x", 120))
	}
	if err := f.l.sendTerminalFrame(ctx, f.svc, f.p, f.c, f.c.terminalID, terminal.Frame{Rev: "c", At: time.Now(), Lines: lines}); err != nil {
		t.Fatal(err)
	}
	env, frame = f.open(t, b.next(t))
	if env.Seq != 2 || frame["frame_seq"] != float64(2) {
		t.Fatalf("chunked frame: seq=%d %v", env.Seq, frame)
	}

	// Input rides the channel into the same ladder as the relay's, in one
	// message or in chunks.
	small := f.viewerEnvelope(t, 1, `{"n":1}`)
	if err := b.dc.SendText(string(small)); err != nil {
		t.Fatal(err)
	}
	big := f.viewerEnvelope(t, 2, `{"pad":"`+strings.Repeat("y", 40000)+`"}`)
	n := (len(big) + directChunkUnits - 1) / directChunkUnits
	for i := 0; i < n; i++ {
		piece, _ := json.Marshal(directMessage{T: "chunk", ID: 1, I: i, N: n, D: string(big[i*directChunkUnits : min((i+1)*directChunkUnits, len(big))])})
		if err := b.dc.SendText(string(piece)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{`{"n":1}`, `{"pad":`} {
		select {
		case got := <-f.inbound:
			if !strings.HasPrefix(string(got), want) {
				t.Fatalf("inbound %q, want %s…", got[:min(len(got), 20)], want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("input %s did not arrive", want)
		}
	}
	if f.l.TerminalCapacity("cloud.terminal_direct_peers").Used != 1 {
		t.Fatal("the open peer is not counted")
	}

	// A viewer that asked for them gets its typing's receipts on the channel,
	// so typing costs the relay nothing; a read's receipt still takes the relay.
	f.l.terminalMu.Lock()
	f.c.directReceipts = true
	f.l.terminalMu.Unlock()
	if err := f.l.sendTerminalReceipt(ctx, f.c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: "input-1",
		Connection: testConnection, Operation: "input", TerminalID: string(f.c.terminalID), Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	env, receipt := f.open(t, b.next(t))
	if env.Ch != "termr/machine/viewer/"+testConnection || env.Class != domaincloud.ClassCtl || receipt["request_id"] != "input-1" {
		t.Fatalf("direct receipt: ch=%s class=%s %v", env.Ch, env.Class, receipt)
	}
	if _, ok := f.spool.Row(1); ok {
		t.Fatal("a direct receipt was also put on the relay")
	}
	if err := f.l.sendTerminalReceipt(ctx, f.c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: "read-1",
		Connection: testConnection, Operation: "read", TerminalID: string(f.c.terminalID), Status: "refused", Error: "terminal_invalid"}); err != nil {
		t.Fatal(err)
	}
	if got := f.receiptAt(t, 1); got["request_id"] != "read-1" {
		t.Fatalf("a read receipt did not take the relay: %v", got)
	}
}

// The relay refuses a publish with rate_limited while the account's terminal
// budget is spent, which says nothing about the connection. A receipt is
// published again and a probe asked again, within bounds, instead of retiring
// the connection; that retirement was what dropped a terminal mid-typing.
func TestARelayBusyRefusalDelaysAReceiptOrProbeInsteadOfRetiring(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	gone := func(f *directFixture) bool { return f.l.getTerminalConnection("viewer", testConnection) == nil }

	f := newDirectFixture(t)
	var timers []func()
	var delays []time.Duration
	f.l.terminalAfter = func(d time.Duration, fn func()) { delays = append(delays, d); timers = append(timers, fn) }
	channel := terminalReceiptChannel("machine", f.c)
	if err := f.l.sendTerminalReceipt(context.Background(), f.c, terminalReceipt{V: 1, Type: "terminal_receipt",
		RequestID: "input-1", Connection: testConnection, Operation: "input", TerminalID: string(f.c.terminalID), Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= CloudTerminalReceiptBusyRetriesLimit; attempt++ {
		f.l.terminalReceiptSettled(channel, uint64(attempt-1), adaptercloud.SettleRateLimited)
		if gone(f) || len(timers) != attempt || delays[attempt-1] != CloudTerminalReceiptBusyRetrySecondsLimit*time.Second {
			t.Fatalf("attempt %d: gone=%v timers=%d", attempt, gone(f), len(timers))
		}
		timers[attempt-1]()
		if got := f.receiptAt(t, uint64(attempt)); got["request_id"] != "input-1" || got["operation"] != "input" {
			t.Fatalf("attempt %d sent %v", attempt, got)
		}
	}
	f.l.terminalReceiptSettled(channel, uint64(CloudTerminalReceiptBusyRetriesLimit), adaptercloud.SettleRateLimited)
	if !gone(f) || len(timers) != CloudTerminalReceiptBusyRetriesLimit {
		t.Fatal("a receipt the relay kept refusing kept the connection")
	}

	f = newDirectFixture(t)
	f.l.opts.Now = clock
	f.c.expires = clock().Add(time.Hour)
	f.c.carrier = carrierDirect
	f.c.probeAt = clock()
	channel = terminalReceiptChannel("machine", f.c)
	for i := 0; i < 3; i++ {
		advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
		f.l.sweepDirect(f.c)
		if gone(f) || !f.c.probePending {
			t.Fatalf("probe %d: gone=%v pending=%v", i, gone(f), f.c.probePending)
		}
		f.l.terminalReceiptSettled(channel, f.c.probeSeq, adaptercloud.SettleRateLimited)
		if gone(f) || f.c.probePending {
			t.Fatalf("a busy probe %d retired the connection or stayed pending", i)
		}
	}
	advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
	f.l.sweepDirect(f.c)
	if !gone(f) {
		t.Fatal("a relay that never delivered a probe kept the connection")
	}

	// A delivered probe clears the bound.
	f = newDirectFixture(t)
	f.l.opts.Now = clock
	f.c.expires = clock().Add(time.Hour)
	f.c.carrier = carrierDirect
	f.c.probeAt = clock()
	for i := 0; i < 6; i++ {
		advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
		f.l.sweepDirect(f.c)
		kind := adaptercloud.SettleRateLimited
		if i%2 == 1 {
			kind = adaptercloud.SettleDelivered
		}
		f.l.terminalReceiptSettled(terminalReceiptChannel("machine", f.c), f.c.probeSeq, kind)
	}
	if gone(f) {
		t.Fatal("a relay delivering every other probe lost the connection")
	}
}

func TestDirectOfferRefusals(t *testing.T) {
	t.Run("sealing", func(t *testing.T) {
		f := newDirectFixture(t)
		other, _ := domaincloud.NewContentKey(rand.Reader)
		for name, body := range map[string]json.RawMessage{
			"another key":        sealDirectOffer(t, other, testConnection, "v=0"),
			"another connection": sealDirectOffer(t, f.key, "BBBBBBBBBBBBBBBBBBBBBB", "v=0"),
			"too large":          sealDirectOffer(t, f.key, testConnection, strings.Repeat("a", CloudTerminalDirectSDPBytesLimit+1)),
			"unknown field":      json.RawMessage(`{"sealed":"AAAA","sdp":"v=0"}`),
		} {
			if _, err := openDirectOffer(f.c, body); codeOf(err) != terminal.CodeInvalid {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
	t.Run("admission", func(t *testing.T) {
		f := newDirectFixture(t)
		f.l.settings.TerminalDirect = false
		if err := f.l.directOfferAdmitted("viewer"); codeOf(err) != terminal.CodeDirectDisabled {
			t.Fatalf("switched off: %v", err)
		}
		f.l.settings.TerminalDirect = true
		if err := f.l.directOfferAdmitted("viewer"); err != nil {
			t.Fatal(err)
		}
		if err := f.l.directOfferAdmitted("viewer"); codeOf(err) != terminal.CodeBusy {
			t.Fatalf("second offer while one is held: %v", err)
		}
		for i := 1; i < CloudTerminalDirectPeersLimit; i++ {
			if err := f.l.directOfferAdmitted(fmt.Sprint("v", i)); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.l.directOfferAdmitted("one-too-many"); codeOf(err) != terminal.CodeBusy {
			t.Fatalf("peer bound: %v", err)
		}
		g := newDirectFixture(t)
		for i := 0; i < CloudTerminalDirectOffersPerMinuteLimit; i++ {
			if err := g.l.directOfferAdmitted("viewer"); err != nil {
				t.Fatal(err)
			}
			g.l.releaseDirectReservation("viewer")
		}
		if err := g.l.directOfferAdmitted("viewer"); codeOf(err) != terminal.CodeBusy {
			t.Fatalf("offer rate: %v", err)
		}
	})
	t.Run("candidates", func(t *testing.T) {
		f := newDirectFixture(t)
		f.l.directLoopback = false
		// Built rather than written, so that no address literal outside the
		// documentation ranges sits in the repository.
		linkLocal, multicast := net.IPv4(169, 254, 1, 1).String(), net.IPv4(224, 0, 0, 1).String()
		sdp := strings.Join([]string{
			"v=0",
			"a=candidate:1 1 udp 1 127.0.0.1 5000 typ host",
			"a=candidate:2 1 udp 1 " + linkLocal + " 5000 typ host",
			"a=candidate:3 1 tcp 1 192.0.2.7 5001 typ host",
			"a=candidate:4 1 udp 1 " + multicast + " 5000 typ host",
			"a=candidate:5 1 udp 1 192.0.2.7 5000 typ host",
			"a=candidate:6 1 udp 1 0f1e2d3c.local 5000 typ host",
		}, "\r\n")
		got, err := f.l.filterDirectOffer(sdp)
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"127.0.0.1", linkLocal, " tcp ", multicast} {
			if strings.Contains(got, gone) {
				t.Errorf("kept %s", gone)
			}
		}
		for _, kept := range []string{"192.0.2.7 5000", ".local"} {
			if !strings.Contains(got, kept) {
				t.Errorf("dropped %s", kept)
			}
		}
		many := []string{"v=0"}
		for i := 0; i <= CloudTerminalDirectCandidatesLimit; i++ {
			many = append(many, fmt.Sprintf("a=candidate:%d 1 udp 1 192.0.2.%d 5000 typ host", i, i))
		}
		if _, err := f.l.filterDirectOffer(strings.Join(many, "\r\n")); codeOf(err) != terminal.CodeInvalid {
			t.Fatalf("too many candidates: %v", err)
		}
	})
	t.Run("viewer", func(t *testing.T) {
		for name, setup := range map[string]func(*directFixture){
			"no terminal permission": func(f *directFixture) {
				f.svc = terminals.New(nil, func(terminals.Principal) error { return errors.New("read only") })
			},
			"revoked": func(f *directFixture) { f.c.denied = true },
		} {
			f := newDirectFixture(t)
			setup(f)
			f.l.handleDirectOffer(context.Background(), f.svc, f.p, f.c, terminalRequest{RequestID: testRequestID,
				Operation: "direct_offer", Connection: testConnection, Body: sealDirectOffer(t, f.key, testConnection, "v=0")})
			if got := f.receiptAt(t, 0); got["status"] != "refused" {
				t.Errorf("%s: %v", name, got)
			}
			if _, held := f.l.directPeers["viewer"]; held {
				t.Errorf("%s: a peer place was reserved", name)
			}
		}
	})
}

func TestDirectConnectionRetiresWhenItsAckOrTheRelayProbeIsLate(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	fresh := func() *directFixture {
		f := newDirectFixture(t)
		f.l.opts.Now = clock
		f.c.expires = clock().Add(time.Hour)
		f.c.carrier = carrierDirect
		f.c.probeAt = clock()
		return f
	}
	gone := func(f *directFixture) bool { return f.l.getTerminalConnection("viewer", testConnection) == nil }

	f := fresh()
	f.c.framePending, f.c.framePendingChannel, f.c.framePendingAt = true, carrierDirect, clock()
	advance(CloudTerminalDirectAckSecondsLimit*time.Second - time.Millisecond)
	f.c.probeAt = clock()
	f.l.sweepDirect(f.c)
	if gone(f) {
		t.Fatal("retired before the ack deadline")
	}
	advance(time.Millisecond)
	f.l.sweepDirect(f.c)
	if !gone(f) {
		t.Fatal("an unacknowledged direct frame kept the connection")
	}

	f = fresh()
	advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
	f.l.sweepDirect(f.c)
	probe := f.receiptAt(t, 0)
	if probe["type"] != "terminal_carrier_probe" || !f.c.probePending {
		t.Fatalf("no probe on the relay: %v", probe)
	}
	f.l.terminalReceiptSettled(terminalReceiptChannel("machine", f.c), f.c.probeSeq, adaptercloud.SettleDelivered)
	if f.c.probePending || gone(f) {
		t.Fatal("a delivered probe did not settle")
	}
	advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
	f.l.sweepDirect(f.c)
	advance(CloudTerminalDirectProbeUnsettledSecondsLimit * time.Second)
	f.l.sweepDirect(f.c)
	if !gone(f) {
		t.Fatal("a probe the relay never settled kept the connection")
	}

	for _, kind := range []adaptercloud.SettleKind{adaptercloud.SettlePeerError, adaptercloud.SettleViewerOffline} {
		f = fresh()
		advance(CloudTerminalDirectProbeSecondsLimit * time.Second)
		f.l.sweepDirect(f.c)
		f.l.terminalReceiptSettled(terminalReceiptChannel("machine", f.c), f.c.probeSeq, kind)
		if !gone(f) {
			t.Fatalf("a probe settled %s kept the connection", kind)
		}
	}
}

// A rekeyed direct connection does not own the peer until it activates. When
// its frame goes unacknowledged the channel is evidently not carrying it, so
// the peer closes too: the browser learns at once and falls back to the relay.
func TestALateAckClosesThePeerEvenBeforeActivation(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f := newDirectFixture(t)
	f.l.opts.Now = clock
	f.c.expires = clock().Add(time.Hour)
	f.c.carrier = carrierDirect
	f.c.probeAt = clock()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	peer := &directPeer{l: f.l, viewer: "viewer", owner: "AAAAAAAAAAAAAAAAAAAAAB", pc: pc, open: true}
	f.l.directPeers = map[string]*directPeer{"viewer": peer}
	f.c.framePending, f.c.framePendingChannel, f.c.framePendingAt = true, carrierDirect, clock()
	mu.Lock()
	now = now.Add(CloudTerminalDirectAckSecondsLimit * time.Second)
	mu.Unlock()
	f.c.probeAt = clock()
	f.l.sweepDirect(f.c)
	if f.l.getTerminalConnection("viewer", testConnection) != nil {
		t.Fatal("an unacknowledged direct frame kept the connection")
	}
	if !peer.closed || f.l.directPeers["viewer"] != nil {
		t.Fatal("the peer stayed open after its rekeyed connection was retired, so the browser never learns")
	}
}

func TestTerminalRosterReadIsDatedFromItsStart(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		mu.Lock()
		now = now.Add(1500 * time.Millisecond) // the read is slow
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer",
			PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{"send_prompt"}}}})
	}))
	defer server.Close()
	l := &Link{opts: LinkOptions{Now: clock}, roster: adaptercloud.NewRoster(server.URL, "credential", clock)}
	if !l.rosterFresh() {
		t.Fatal("first read failed")
	}
	// 1.5 s into the cache's life by the read's end; 2.1 s since it began.
	mu.Lock()
	now = now.Add(600 * time.Millisecond)
	mu.Unlock()
	l.rosterFresh()
	if reads.Load() != 2 {
		t.Fatalf("a read that began %v ago was still trusted", CloudTerminalRosterRefreshLimit*time.Second+100*time.Millisecond)
	}
}

func TestDirectPeerFollowsItsOwnerAndClosesWithIt(t *testing.T) {
	f := newDirectFixture(t)
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	second := &terminalConnection{viewer: "viewer", id: "BBBBBBBBBBBBBBBBBBBBBB", key: f.key,
		expires: time.Now().Add(time.Minute), carrier: carrierDirect}
	f.c.carrier = carrierDirect
	f.l.terminalConnections[terminalConnectionID("viewer", second.id)] = second
	peer := &directPeer{l: f.l, viewer: "viewer", owner: f.c.id, pc: pc, open: true}
	f.l.directPeers = map[string]*directPeer{"viewer": peer}

	f.l.closeTerminalConnection(f.c) // a direct→direct rotation retires the old one
	if peer.closed || peer.owner != second.id {
		t.Fatalf("the peer did not move to the live direct connection: closed=%v owner=%s", peer.closed, peer.owner)
	}
	f.l.closeTerminalConnection(second)
	if !peer.closed || f.l.directPeers["viewer"] != nil {
		t.Fatal("the peer outlived its last direct connection")
	}

	// Closing a peer retires its viewer's direct connections, so the browser falls back.
	g := newDirectFixture(t)
	pc2, _ := webrtc.NewPeerConnection(webrtc.Configuration{})
	g.c.carrier = carrierDirect
	peer2 := &directPeer{l: g.l, viewer: "viewer", owner: g.c.id, pc: pc2, open: true}
	g.l.directPeers = map[string]*directPeer{"viewer": peer2}
	g.l.closeDirectPeer(peer2, "test")
	if g.l.getTerminalConnection("viewer", testConnection) != nil {
		t.Fatal("a closed peer left its direct connection live")
	}
}

func TestDirectChunkViolationsCloseThePeer(t *testing.T) {
	text := func(m directMessage) webrtc.DataChannelMessage {
		data, _ := json.Marshal(m)
		return webrtc.DataChannelMessage{IsString: true, Data: data}
	}
	for name, msgs := range map[string][]webrtc.DataChannelMessage{
		"binary":           {{IsString: false, Data: []byte("{}")}},
		"unknown kind":     {text(directMessage{T: "hello"})},
		"starts mid-way":   {text(directMessage{T: "chunk", ID: 1, I: 1, N: 3, D: "a"})},
		"single chunk":     {text(directMessage{T: "chunk", ID: 1, I: 0, N: 1, D: "a"})},
		"out of order":     {text(directMessage{T: "chunk", ID: 1, I: 0, N: 3, D: "a"}), text(directMessage{T: "chunk", ID: 1, I: 2, N: 3, D: "a"})},
		"another id":       {text(directMessage{T: "chunk", ID: 1, I: 0, N: 3, D: "a"}), text(directMessage{T: "chunk", ID: 2, I: 1, N: 3, D: "a"})},
		"interleaved":      {text(directMessage{T: "chunk", ID: 1, I: 0, N: 3, D: "a"}), text(directMessage{T: "ack", Connection: testConnection, FrameSeq: 1})},
		"not an envelope":  {text(directMessage{T: "chunk", ID: 1, I: 0, N: 2, D: `{"t":"a`}), text(directMessage{T: "chunk", ID: 1, I: 1, N: 2, D: `ck"}`})},
		"oversized chunks": {text(directMessage{T: "chunk", ID: 1, I: 0, N: 2, D: strings.Repeat("a", directChunkUnits*3+1)})},
	} {
		f := newDirectFixture(t)
		pc, _ := webrtc.NewPeerConnection(webrtc.Configuration{})
		peer := &directPeer{l: f.l, viewer: "viewer", owner: f.c.id, pc: pc, open: true}
		f.l.directPeers = map[string]*directPeer{"viewer": peer}
		for _, m := range msgs {
			peer.receive(m)
		}
		if !peer.closed {
			t.Errorf("%s: the peer stayed open", name)
		}
	}

	f := newDirectFixture(t)
	pc, _ := webrtc.NewPeerConnection(webrtc.Configuration{})
	peer := &directPeer{l: f.l, viewer: "viewer", owner: f.c.id, pc: pc, open: true}
	f.l.directPeers = map[string]*directPeer{"viewer": peer}
	peer.receive(text(directMessage{T: "chunk", ID: 1, I: 0, N: 2, D: "a"}))
	time.Sleep(CloudTerminalDirectChunkSecondsLimit*time.Second + 200*time.Millisecond)
	peer.mu.Lock()
	closed := peer.closed
	peer.mu.Unlock()
	if !closed {
		t.Fatal("an unfinished chunked message held the channel open")
	}
}

func codeOf(err error) terminal.RefusalCode {
	code, _ := terminal.CodeOf(err)
	return code
}

// A rekey that asks for the direct carrier is refused with its own code when
// the machine has it switched off or the viewer has no open channel, and the
// browser stays on the relay.
func TestDirectRekeyRefusesWithoutAnOpenChannel(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer",
			PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{"send_prompt"}}}})
	}))
	defer server.Close()
	for _, tc := range []struct {
		name    string
		enabled bool
		want    terminal.RefusalCode
	}{
		{"switched off", false, terminal.CodeDirectDisabled},
		{"no channel", true, terminal.CodeDirectUnavailable},
	} {
		f := newDirectFixture(t)
		file := nextconfig.Open(t.TempDir())
		if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
			t.Fatal(err)
		}
		f.l.file = file
		f.l.settings = adaptercloud.Settings{Enabled: true, TerminalDirect: tc.enabled}
		f.l.pinned = adaptercloud.NewPinnedStore(t.TempDir())
		if err := f.l.pinned.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub)}); err != nil {
			t.Fatal(err)
		}
		f.l.roster = adaptercloud.NewRoster(server.URL, "credential", time.Now)
		next, err := domaincloud.NewContentKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"old_connection": testConnection, "carrier": "direct"})
		f.l.openTerminalConnection(context.Background(), f.svc, f.p, terminalRequest{RequestID: testRequestID,
			Operation: "rekey_connection", Connection: "BBBBBBBBBBBBBBBBBBBBBB", KeyID: "rk-AgICAgICAgICAgICAgICAg",
			Key: base64.StdEncoding.EncodeToString(next.Bytes()), Body: body})
		f.key = next
		got := f.receiptAt(t, 0)
		if got["status"] != "refused" || got["error"] != string(tc.want) {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}
