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
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// answerAt waits for spool row seq and opens it as a read answer under the
// account content key, which is what every Session answer is sealed with.
func (f *directFixture) answerAt(t *testing.T, seq uint64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if row, ok := f.spool.Row(seq); ok {
			env, err := domaincloud.DecodeEnvelope(row.Sealed)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := env.Open(f.master, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
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

// openMaster opens one carrier message as the page does: a Session answer is
// sealed under the account content key, not under a terminal connection's.
func (f *directFixture) openMaster(t *testing.T, m directMessage) (domaincloud.Envelope, map[string]any) {
	t.Helper()
	if m.T != "env" {
		t.Fatalf("got %q, want an envelope", m.T)
	}
	env, err := domaincloud.DecodeEnvelope(m.E)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.Open(f.master, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatal(err)
	}
	return env, got
}

// carrierID is a viewer-minted 22-character id, as the browser makes them.
func carrierID(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// openCarrierAnswer is the browser's half: the answer SDP opened with the key
// this offer minted for it.
func openCarrierAnswer(t *testing.T, key domaincloud.ContentKey, connection, sealed string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	clear, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(carrierAnswerAAD+connection))
	if err != nil {
		t.Fatal(err)
	}
	return string(clear)
}

// carrier opens a carrier for the fixture's viewer and returns the browser end
// and the key the offer minted, as a page with no terminal at all would.
func (f *directFixture) carrier(t *testing.T) (*testBrowser, domaincloud.ContentKey, string) {
	t.Helper()
	b, offer := newTestBrowser(t)
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connection := carrierID(t)
	body, _ := json.Marshal(map[string]string{"sdp": offer})
	f.l.handleCarrierOffer(context.Background(), f.svc, f.p, terminalRequest{V: 1, Type: "terminal_request",
		RequestID: testRequestID, Connection: connection, Operation: carrierOfferOperation,
		KeyID: "rk-" + carrierID(t), Key: base64.StdEncoding.EncodeToString(key.Bytes()), Body: body})
	answer := f.answerAt(t, 0)
	if answer["read"] != "read:"+testRequestID || answer["status"] != float64(200) ||
		answer["session_id"] != cloudops.MachineReplySession || answer["machine_id"] != "machine" {
		t.Fatalf("the carrier answer is not a read answer: %v", answer)
	}
	got, _ := answer["body"].(map[string]any)
	if got["carrier"] != connection || got["direct_receipts"] != true {
		t.Fatalf("carrier answer body: %v", got)
	}
	sealed, _ := got["sdp_sealed"].(string)
	if sealed == "" {
		t.Fatal("the answer SDP was not sealed")
	}
	sdp := openCarrierAnswer(t, key, connection, sealed)
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the carrier's data channel did not open")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !f.l.directPeerOpen("viewer") {
		if time.Now().After(deadline) {
			t.Fatal("the machine did not see the carrier open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return b, key, connection
}

// A page with no terminal opens the channel by itself, its answer travels as an
// ordinary read answer, and the channel is the viewer's own: a terminal that
// borrows it and then retires does not take it away.
func TestACarrierOpensWithNoTerminalAndOutlivesOne(t *testing.T) {
	f := newDirectFixture(t)
	_, _, connection := f.carrier(t)
	if _, ok := f.spool.Row(1); ok {
		t.Fatal("the carrier answer was published twice")
	}

	// A terminal upgrade borrows it: the machine asks only that the viewer have
	// an open peer, so there is no second negotiation and no second peer.
	if !f.l.directPeerOpen("viewer") {
		t.Fatal("the carrier is not open")
	}
	f.l.moveDirectOwner("viewer", f.c.id)
	f.l.directMu.Lock()
	peer := f.l.directPeers["viewer"]
	f.l.directMu.Unlock()
	peer.mu.Lock()
	owner, carrier := peer.owner, peer.carrier
	peer.mu.Unlock()
	if owner != "" || !carrier {
		t.Fatalf("a borrowed carrier took an owner: owner=%q carrier=%v", owner, carrier)
	}
	f.l.directOwnerRetired(f.c)
	if !f.l.directPeerOpen("viewer") {
		t.Fatal("a retiring terminal closed the viewer's carrier")
	}
	if connection == f.c.id {
		t.Fatal("the carrier named a terminal connection")
	}
}

// A Session answer for a request that arrived on the carrier goes to the
// carrier, in the carrier's own envelope sequence, and never to the relay.
func TestASessionAnswerOnTheCarrierNeverEntersTheSpool(t *testing.T) {
	f := newDirectFixture(t)
	b, _, _ := f.carrier(t)
	f.l.relay.Direct = f.l.sendCarrierEnvelope

	out := func(channel string) Outbound {
		return Outbound{Channel: channel, Class: string(domaincloud.ClassStream), Direct: true,
			Payload: []byte(`{"read":"transcript","status":200}`),
			Reply:   Reply{Sender: "viewer", Name: "transcript", Status: 200}}
	}
	for want := uint64(1); want <= 2; want++ {
		if err := f.l.relay.Publish(context.Background(), out("t/machine/session")); err != nil {
			t.Fatal(err)
		}
		env, body := f.openMaster(t, b.next(t))
		if env.Ch != "t/machine/session" || env.Seq != want || body["read"] != "transcript" {
			t.Fatalf("answer %d: ch=%s seq=%d %v", want, env.Ch, env.Seq, body)
		}
	}
	if _, ok := f.spool.Row(1); ok {
		t.Fatal("an answer that went to the carrier was also spooled")
	}

	// A retained value is the relay's. A status row or an orchestrator snapshot
	// moved here would leave a reconnecting page realigning from a row the
	// machine never published there.
	if err := f.l.relay.Publish(context.Background(), out("s/machine/session")); !errors.Is(err, ErrNoDirectCarrier) {
		t.Fatalf("a status row on the carrier: %v", err)
	}
	// Nothing says which carrier an answer with no sender came on.
	anonymous := out("t/machine/session")
	anonymous.Reply.Sender = ""
	if err := f.l.relay.Publish(context.Background(), anonymous); !errors.Is(err, ErrNoDirectCarrier) {
		t.Fatalf("an answer with no viewer: %v", err)
	}
	if _, ok := f.spool.Row(1); ok {
		t.Fatal("a refused direct answer fell back onto the relay")
	}
}

// The channel closes when this machine has thrown the device out, and stays up
// when the machine cannot tell right now.
func TestACarrierClosesOnlyWhenItsDeviceIsDenied(t *testing.T) {
	f := newDirectFixture(t)
	f.carrier(t)
	file := nextconfig.Open(t.TempDir())
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	// Cloud on, commands on, the device's key authenticated, and no roster to
	// ask: unverified, not denied.
	f.l.file = file
	f.l.settings = adaptercloud.Settings{Enabled: true, TerminalDirect: true}
	f.l.terminalMu.Lock()
	f.l.terminalAuthenticated = map[string]ed25519.PublicKey{"viewer": f.viewer.PublicKey()}
	f.l.terminalMu.Unlock()
	if verdict, _ := f.l.terminalAuthority("viewer", true); verdict != TerminalUnverified {
		t.Fatalf("the fixture does not produce an unverified verdict: %v", verdict)
	}
	f.l.sweepDirectCarriers()
	if !f.l.directPeerOpen("viewer") {
		t.Fatal("a carrier was closed over a verdict the machine could not reach")
	}

	if _, err := file.Set(map[string]any{adaptercloud.KeyCommands: false}); err != nil {
		t.Fatal(err)
	}
	if verdict, _ := f.l.terminalAuthority("viewer", true); verdict != TerminalDenied {
		t.Fatalf("commands off is not a denial: %v", verdict)
	}
	f.l.sweepDirectCarriers()
	deadline := time.Now().Add(2 * time.Second)
	for f.l.directPeerOpen("viewer") {
		if time.Now().After(deadline) {
			t.Fatal("a denied device kept its carrier")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A page whose carrier died without this machine hearing it opens another.
//
// The machine keeps one carrier per viewer and gives it no idle bound, so a
// peer it still believes in is a peer nothing retires. Before this, every
// offer that page made afterwards was refused `busy` and it read over the
// relay for as long as it lived (measured 2026-10-11: 63 reads, 922-1031 ms,
// against 610 ms on the carrier it could not reopen).
func TestAPageWhoseCarrierDiedUnheardOpensAnother(t *testing.T) {
	f := newDirectFixture(t)
	first, _, firstConnection := f.carrier(t)
	f.l.directMu.Lock()
	stale := f.l.directPeers["viewer"]
	f.l.directMu.Unlock()

	// The page's side is gone, and nothing told this machine: no data channel
	// close, no connection state change. `first` is deliberately left open so
	// the machine's own view of the carrier stays exactly as it was.
	b, offer := newTestBrowser(t)
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connection := carrierID(t)
	body, _ := json.Marshal(map[string]string{"sdp": offer})
	second := strings.Join([]string{"87654321", "4321", "4321", "8321", "cba987654321"}, "-")
	f.l.handleCarrierOffer(context.Background(), f.svc, f.p, terminalRequest{V: 1, Type: "terminal_request",
		RequestID: second, Connection: connection, Operation: carrierOfferOperation,
		KeyID: "rk-" + carrierID(t), Key: base64.StdEncoding.EncodeToString(key.Bytes()), Body: body})
	answer := f.answerAt(t, 1)
	if answer["read"] != "read:"+second || answer["status"] != float64(200) {
		t.Fatalf("the second carrier offer was not answered: %v", answer)
	}
	got, _ := answer["body"].(map[string]any)
	if got["carrier"] != connection {
		t.Fatalf("the answer named %v, want the new carrier %q", got["carrier"], connection)
	}
	if connection == firstConnection {
		t.Fatal("the new carrier reused the stale carrier's id")
	}
	sdp := openCarrierAnswer(t, key, connection, got["sdp_sealed"].(string))
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement carrier's data channel did not open")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !f.l.directPeerOpen("viewer") {
		if time.Now().After(deadline) {
			t.Fatal("the machine did not see the replacement carrier open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.l.directMu.Lock()
	now := f.l.directPeers["viewer"]
	peers := len(f.l.directPeers)
	f.l.directMu.Unlock()
	if now == stale {
		t.Fatal("the stale carrier is still the viewer's peer")
	}
	if peers != 1 {
		t.Fatalf("the viewer holds %d peers, want one carrier", peers)
	}
	stale.mu.Lock()
	closed := stale.closed
	stale.mu.Unlock()
	if !closed {
		t.Fatal("the superseded carrier was left open")
	}
	_ = first
}

// A Session row this machine publishes takes every open carrier as well as the
// relay, as the very envelope the relay will deliver: the browser orders a row
// against this machine's other rows by that sequence, so the carrier is a
// second road for the envelope and not a second numbering.
func TestAPublishedSessionRowTakesTheCarrierToo(t *testing.T) {
	f := newDirectFixture(t)
	b, _, _ := f.carrier(t)
	// What `wire` installs on the real link; this fixture builds one by hand.
	f.l.relay.MirrorStatus = f.l.mirrorStatusRow

	row := func(channel string) Outbound {
		return Outbound{Channel: channel, Class: string(domaincloud.ClassStream),
			Payload: []byte(`{"session":{"id":"session"},"at":1}`)}
	}
	seq, err := f.l.relay.PublishTracked(context.Background(), row("s/machine/session"))
	if err != nil {
		t.Fatal(err)
	}
	env, body := f.openMaster(t, b.next(t))
	if env.Ch != "s/machine/session" || env.Seq != seq || body["at"] != float64(1) {
		t.Fatalf("the row on the carrier: ch=%s seq=%d (published %d) %v", env.Ch, env.Seq, seq, body)
	}
	// The relay still has its own copy: a page with no carrier, and every other
	// device of the account, reads the row exactly as before.
	if _, ok := f.spool.Row(seq); !ok {
		t.Fatal("a mirrored row was not spooled for the relay")
	}

	// The content-free status feed costs no subscription and reaches every
	// device of the account without one, so mirroring it would only duplicate
	// it. The `s/` inventory drives the older whole-list recovery, which the
	// hosted list does not use. Both stay on the relay.
	for _, channel := range []string{"ss/machine/session", "s/machine/" + InventorySessionID, "orch/machine"} {
		if _, err := f.l.relay.PublishTracked(context.Background(), row(channel)); err != nil {
			t.Fatalf("%s: %v", channel, err)
		}
	}
	select {
	case raw := <-b.msgs:
		t.Fatalf("a channel that stays on the relay reached the carrier: %s", raw)
	case <-time.After(500 * time.Millisecond):
	}
}
