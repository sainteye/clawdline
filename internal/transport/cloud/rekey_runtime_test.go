package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestRuntimeWireIsOptInAndPairingUsesDurableState(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "enabled"}[enabled], func(t *testing.T) {
			keys, err := cloudkeys.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			pins := adaptercloud.NewPinnedStore(keys.Dir())
			l := &Link{keys: keys, pinned: pins, pairing: &Pairing{Pinned: pins}, identityPairing: &IdentityPairing{}, identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "synthetic"}, settings: adaptercloud.Settings{RelayURL: "ws://127.0.0.1:9/v1/connect"}, opts: LinkOptions{ContentKeyRuntime: enabled, Now: time.Now}}
			if err := l.wire(); err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if l.contentEpoch != nil || l.relay.Keys != nil || l.pairing.RevokeContent != nil {
					t.Fatal("legacy path unexpectedly enabled")
				}
				if _, err := os.Stat(filepath.Join(keys.Dir(), "content-key-state-v1")); !os.IsNotExist(err) {
					t.Fatal("legacy path migrated epoch", err)
				}
				return
			}
			if l.contentEpoch == nil || l.relay.Keys != l.contentEpoch || l.pairing.ContentState == nil {
				t.Fatal("runtime fence not wired")
			}
			state, _ := l.contentEpoch.Current()
			next, err := keys.RotateContentKey("synthetic-external-recovery", state.Epoch, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.contentEpoch.Recover(keys.RecoverContentKey, l.retireContent); err != nil {
				t.Fatal(err)
			}
			_, key, err := l.pairingKeys()
			if err != nil || !bytes.Equal(key.Bytes(), next.Key.Bytes()) {
				t.Fatal("pairing read legacy key", err)
			}
			current, err := l.pairing.ContentState()
			if err != nil || current.KeyID != next.KeyID {
				t.Fatal("pairing labels retired epoch", err)
			}
		})
	}
}

func TestRuntimePairingHandoverCarriesRotatedKeyAndID(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	next, err := l.keys.RotateContentKey("synthetic-handover", old.Epoch, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := newPairingHarness(t)
	h.pairing.ContentState = func() (domaincloud.ContentKeyState, error) { return next, nil }
	if _, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment(), adaptercloud.ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	h.plane.mu.Lock()
	raw := bytes.Clone(h.plane.delivered)
	h.plane.mu.Unlock()
	wrapper, err := domaincloud.DecodePairingWrapper(raw)
	if err != nil {
		t.Fatal(err)
	}
	handover, err := domaincloud.OpenPairingHandover(wrapper, h.viewer.offer, h.viewer.ephemeral, h.plane.machineID, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if handover.KeyID != next.KeyID || handover.MasterSecret != base64.StdEncoding.EncodeToString(next.Key.Bytes()) {
		t.Fatal("pairing disclosed a stale or mislabeled key")
	}
}

func TestRuntimePairingFinalPinCannotRacePastRevocation(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	h := newPairingHarness(t)
	l.pinned = h.pinned
	h.pairing.Authority = &l.contentAuthority.mu
	h.pairing.Generation = &l.contentAuthority.generation
	h.pairing.ContentState = l.contentEpoch.Current
	entered := make(chan struct{})
	release := make(chan struct{})
	h.pairing.Keys = func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
		close(entered)
		<-release
		return h.key, h.secret, nil
	}
	paired := make(chan error, 1)
	go func() {
		_, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment(), adaptercloud.ScopeReadOnly)
		paired <- err
	}()
	<-entered
	type outcome struct {
		changed bool
		err     error
	}
	revoked := make(chan outcome, 1)
	go func() { changed, err := l.revokeContentViewer(h.viewer.deviceID); revoked <- outcome{changed, err} }()
	select {
	case got := <-revoked:
		close(release)
		<-paired
		t.Fatalf("revocation escaped the final-pin barrier: %+v", got)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-paired; err != nil {
		t.Fatal(err)
	}
	if got := <-revoked; got.err != nil || !got.changed {
		t.Fatalf("revocation after handover: %+v", got)
	}
	if refused, err := h.pinned.Refused(h.viewer.deviceID); err != nil || !refused {
		t.Fatal("final pin resurrected revoked viewer", err)
	}
}

func TestPairingFromRetiredGenerationCannotDeliver(t *testing.T) {
	var generation atomic.Uint64
	p := &Pairing{Generation: &generation}
	ctx := p.withGeneration(context.Background())
	generation.Add(1)
	p.Keys = func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
		t.Fatal("stale pairing reached key disclosure")
		return domaincloud.DeviceKey{}, domaincloud.ContentKey{}, nil
	}
	if _, err := p.deliver(ctx, domaincloud.PairingOffer{}); err != domaincloud.ErrRekey {
		t.Fatalf("stale generation: %v", err)
	}
}

func TestRuntimeAnswerDeliveryPrecedesRevocation(t *testing.T) {
	l, _, b := runtimeFixture(t)
	raw, _ := runtimeOffer(t, "B", b, time.Now().UnixMilli())
	entered := make(chan struct{})
	release := make(chan struct{})
	answerDone := make(chan error, 1)
	go func() { answerDone <- l.answerRekey(raw, func([]byte) error { close(entered); <-release; return nil }) }()
	<-entered
	revoked := make(chan error, 1)
	go func() { _, err := l.revokeContentViewer("B"); revoked <- err }()
	select {
	case err := <-revoked:
		t.Fatalf("revocation returned before key delivery left authority sequence: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-answerDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if err := l.answerRekey(raw, func([]byte) error { t.Fatal("revoked B received late answer"); return nil }); err == nil {
		t.Fatal("revoked offer accepted")
	}
}

func runtimeFixture(t *testing.T) (*Link, domaincloud.DeviceKey, domaincloud.DeviceKey) {
	t.Helper()
	dir := t.TempDir()
	keys, err := cloudkeys.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := domaincloud.LoadOrCreateDeviceKey(keys, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = machine
	state, err := keys.LoadOrCreateContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := domaincloud.NewKeyEpoch(state)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := domaincloud.NewDeviceKey(rand.Reader)
	b, _ := domaincloud.NewDeviceKey(rand.Reader)
	pins := adaptercloud.NewPinnedStore(keys.Dir())
	for id, key := range map[string]domaincloud.DeviceKey{"A": a, "B": b} {
		if err := pins.Pin("account", adaptercloud.PinnedDevice{DeviceID: id, PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey()), Scope: adaptercloud.ScopeReadOnly, PairedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{
			{ID: "A", PublicKey: base64.StdEncoding.EncodeToString(a.PublicKey()), Caps: []string{"read_transcript"}},
			{ID: "B", PublicKey: base64.StdEncoding.EncodeToString(b.PublicKey()), Caps: []string{"read_transcript"}},
		}})
	}))
	t.Cleanup(server.Close)
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	l := &Link{keys: keys, pinned: pins, contentEpoch: epoch, contentAuthority: &runtimeAuthority{}, opts: LinkOptions{Now: time.Now}, identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine"}, roster: adaptercloud.NewRoster(server.URL, "synthetic", time.Now), relay: &Relay{Spool: spool}}
	return l, a, b
}

func runtimeOffer(t *testing.T, id string, key domaincloud.DeviceKey, now int64) ([]byte, []byte) {
	t.Helper()
	private, err := domaincloud.NewX25519PrivateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := domaincloud.X25519PublicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	raw, err := domaincloud.SignRekeyOffer(domaincloud.RekeyOffer{Type: "content_key_offer", Version: 1, AccountID: "account", MachineID: "machine", ViewerDeviceID: id, OperationID: "synthetic-op", FromEpoch: 1, FromKeyID: "ms-1", ViewerEphemeralKey: base64.StdEncoding.EncodeToString(public), Nonce: base64.StdEncoding.EncodeToString(nonce), ExpiresAtMS: now + 60000}, key, now)
	if err != nil {
		t.Fatal(err)
	}
	return raw, private
}

func TestRuntimeRevokedCannotReadFutureAndReadonlyRecovers(t *testing.T) {
	l, a, b := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	changed, err := l.revokeContentViewer("A")
	if err != nil || !changed {
		t.Fatalf("revoke %v %v", changed, err)
	}
	next, err := l.contentEpoch.Current()
	if err != nil || next.Epoch != old.Epoch+1 {
		t.Fatalf("next %v %v", next, err)
	}
	changed, err = l.revokeContentViewer("A")
	if err != nil || changed {
		t.Fatalf("retry %v %v", changed, err)
	}
	retry, _ := l.contentEpoch.Current()
	if retry.KeyID != next.KeyID {
		t.Fatal("retry minted another key")
	}
	now := time.Now().UnixMilli()
	rejected, _ := runtimeOffer(t, "A", a, now)
	sent := false
	if err := l.answerRekey(rejected, func([]byte) error { sent = true; return nil }); err == nil || sent {
		t.Fatal("revoked A received a key")
	}
	offer, private := runtimeOffer(t, "B", b, now)
	var answer []byte
	if err := l.answerRekey(offer, func(raw []byte) error { answer = raw; return nil }); err != nil {
		t.Fatal(err)
	}
	machine, _, _ := l.keys.DeviceKey()
	recovered, err := domaincloud.OpenRekeyAnswer(answer, offer, domaincloud.RekeyAuthority{AccountID: "account", MachineID: "machine", ViewerDeviceID: "B", ActiveViewerKey: b.PublicKey()}, private, machine.PublicKey(), 1, "ms-1", time.Now().UnixMilli())
	if err != nil || !bytes.Equal(recovered.Key.Bytes(), next.Key.Bytes()) {
		t.Fatalf("readonly recovery %v", err)
	}
	// Exercise real AEAD, not just state fields: old A cannot open future data.
	envelope, err := domaincloud.Seal([]byte("future-canary"), domaincloud.SealParams{Ch: "s/machine/session", Seq: 1, Ts: uint64(now), Class: domaincloud.ClassStream, KeyID: next.KeyID, Sender: "machine", Key: next.Key, Signer: machine})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envelope.Open(old.Key, func(string) (ed25519.PublicKey, bool) { return machine.PublicKey(), true }); err == nil {
		t.Fatal("revoked old key opened future canary")
	}
	opened, err := envelope.Open(recovered.Key, func(string) (ed25519.PublicKey, bool) { return machine.PublicKey(), true })
	if err != nil || string(opened) != "future-canary" {
		t.Fatalf("B canary %q %v", opened, err)
	}
}

func TestRuntimeRestartResumesExactRevocation(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	before, _ := l.contentEpoch.Current()
	r, err := l.keys.BeginRevocation("A", before.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	// Crash after durable rotation, before queue retirement and completion.
	durable, err := l.keys.RotateContentKey(r.Operation, r.ExpectedEpoch, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := cloudkeys.Open(l.keys.Dir() + "/..")
	if err != nil {
		t.Fatal(err)
	}
	l.keys = reopened
	if err := l.resumeRevocation(r); err != nil {
		t.Fatal(err)
	}
	after, _ := l.contentEpoch.Current()
	if after.KeyID != durable.KeyID {
		t.Fatal("restart reminted key")
	}
	if refused, err := l.pinned.Refused("A"); err != nil || !refused {
		t.Fatal("persistent deny missing", err)
	}
	if _, pending, err := l.keys.PendingRevocation(); err != nil || pending {
		t.Fatal("journal incomplete", err)
	}
}

func TestRuntimeRejectsUnknownAndWrongSigningPin(t *testing.T) {
	l, a, _ := runtimeFixture(t)
	for _, id := range []string{"unknown", "B"} {
		raw, _ := runtimeOffer(t, id, a, time.Now().UnixMilli())
		if err := l.answerRekey(raw, func([]byte) error { t.Fatal("unverified pin received key"); return nil }); err == nil {
			t.Fatal("unverified offer accepted")
		}
	}
}

func TestRuntimeReadonlyReservationCannotObtainAKey(t *testing.T) {
	l, _, b := runtimeFixture(t)
	// A new readonly row is a pre-delivery ceiling, not a completed pairing.
	if err := l.pinned.ReserveReadOnly("account", adaptercloud.PinnedDevice{DeviceID: "pending", PublicKey: base64.StdEncoding.EncodeToString(b.PublicKey()), PairedAt: 1}); err != nil {
		t.Fatal(err)
	}
	raw, _ := runtimeOffer(t, "pending", b, time.Now().UnixMilli())
	if err := l.answerRekey(raw, func([]byte) error { t.Fatal("unfinished pairing got content key"); return nil }); err == nil {
		t.Fatal("reservation admitted as completed pin")
	}
}

func TestRuntimeRetiredDirectFrameIsRefusedAtSend(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	if _, err := l.revokeContentViewer("A"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(directMessage{T: "env", E: json.RawMessage(`{"key_id":"` + old.KeyID + `"}`)})
	peer := &directPeer{l: l, viewer: "B"}
	if err := peer.send(raw); err != domaincloud.ErrKeyEpochRegress {
		t.Fatalf("retired frame reached carrier: %v", err)
	}
}

// identityRuntimeHarness exercises the real adapter, signed transcript and AEAD;
// no roster key is used to manufacture the completed pin.
type identityRuntimeHarness struct {
	link           *Link
	pairing        *IdentityPairing
	viewer         domaincloud.DeviceKey
	beforeGrant    func()
	beforeActivate func()
	beforeConfirm  func()
	grants         atomic.Int64
	confirms       atomic.Int64
	lifetime       time.Duration
}

func newIdentityRuntimeHarness(t *testing.T) *identityRuntimeHarness {
	t.Helper()
	l, _, viewer := runtimeFixture(t)
	machine, err := domaincloud.LoadOrCreateDeviceKey(l.keys, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &identityRuntimeHarness{link: l, viewer: viewer, lifetime: time.Minute}
	var q domaincloud.IdentityPairingQR
	var offer domaincloud.IdentityOffer
	var offerWrapper, activate domaincloud.PairingWrapper
	private, _ := domaincloud.NewX25519Private(nil)
	public, _ := domaincloud.X25519PublicKey(private)
	var epoch int64
	var grantDigest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		reply := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/v1/pairing/identity/start":
			json.Unmarshal(body["content_key_epoch"], &epoch)
			q = domaincloud.IdentityPairingQR{PairingID: "pai_synthetic", AccountID: "account", MachineID: "machine", ExpiresAt: time.Now().Add(h.lifetime).UnixMilli(), MachineSigningKey: base64.StdEncoding.EncodeToString(machine.PublicKey()), MachineFingerprint: machine.Fingerprint(), ClaimNonce: base64.StdEncoding.EncodeToString(make([]byte, 32))}
			json.Unmarshal(body["machine_ephemeral_key"], &q.MachineEphemeralKey)
			json.Unmarshal(body["pairing_nonce"], &q.PairingNonce)
			offer = domaincloud.IdentityOffer{PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID, ViewerDeviceID: "identity-viewer", ViewerSigningKey: base64.StdEncoding.EncodeToString(viewer.PublicKey()), ViewerFingerprint: viewer.Fingerprint(), ViewerEphKey: base64.StdEncoding.EncodeToString(public)}
			offerWrapper, err = domaincloud.SealIdentityPhase(q, domaincloud.IdentityPhaseOffer, offer.ViewerDeviceID, private, q.MachineEphemeralKey, make([]byte, 12), offer.Canonical())
			if err != nil {
				t.Error(err)
			}
			reply(map[string]any{"qr": json.RawMessage(domaincloud.CanonicalBytes(q.Value())), "identity": map[string]int64{"identity_epoch": 4, "machine_key_epoch": 1}})
		case "/v1/pairing/identity/poll":
			var phase string
			json.Unmarshal(body["phase"], &phase)
			blob := offerWrapper
			if phase == domaincloud.IdentityPhaseActivate {
				if h.beforeActivate != nil {
					h.beforeActivate()
				}
				blob = activate
			}
			reply(map[string]any{"status": "ready", "blob": json.RawMessage(domaincloud.CanonicalBytes(blob.Value())), "phase_sha256": domaincloud.PhaseDigest(blob)})
		case "/v1/pairing/identity/phases/grant":
			if h.beforeGrant != nil {
				h.beforeGrant()
			}
			blob, e := domaincloud.DecodePairingWrapper(body["blob"])
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			clear, e := domaincloud.OpenIdentityPhase(q, blob, domaincloud.IdentityPhaseGrant, q.MachineID, q.MachineEphemeralKey, private)
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			grant, e := domaincloud.DecodeIdentityGrant(clear, q, domaincloud.PhaseDigest(offerWrapper), offer.ViewerDeviceID)
			if e != nil || grant.ContentKeyEpoch != epoch {
				t.Errorf("grant epoch mismatch: %v %+v", e, grant)
				w.WriteHeader(400)
				return
			}
			state, _ := l.contentEpoch.Current()
			if grant.MasterSecret != base64.StdEncoding.EncodeToString(state.Key.Bytes()) {
				t.Error("identity grant disclosed a stale key")
			}
			grantDigest = domaincloud.PhaseDigest(blob)
			kc, e := domaincloud.KeyConfirmation(state.Key.Bytes(), domaincloud.KeyConfirmationInput{AccountID: q.AccountID, PairingID: q.PairingID, MachineID: q.MachineID, ViewerDeviceID: offer.ViewerDeviceID, ViewerSigningKey: offer.ViewerSigningKey, ContentKeyEpoch: epoch, IdentityEpoch: 4, GrantSHA256: grantDigest})
			if e != nil {
				t.Error(e)
			}
			act := domaincloud.IdentityActivate{PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID, ViewerDeviceID: offer.ViewerDeviceID, ContentKeyEpoch: epoch, GrantSHA256: grantDigest, KeyConfirmation: base64.StdEncoding.EncodeToString(kc)}
			activate, e = domaincloud.SealIdentityPhase(q, domaincloud.IdentityPhaseActivate, offer.ViewerDeviceID, private, q.MachineEphemeralKey, make([]byte, 12), act.Canonical())
			if e != nil {
				t.Error(e)
			}
			h.grants.Add(1)
			reply(map[string]any{"phase_sha256": grantDigest, "content_key_epoch": epoch, "viewer_key_epoch": 2})
		case "/v1/pairing/identity/phases/confirm":
			if h.beforeConfirm != nil {
				h.beforeConfirm()
			}
			blob, e := domaincloud.DecodePairingWrapper(body["blob"])
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			clear, e := domaincloud.OpenIdentityPhase(q, blob, domaincloud.IdentityPhaseConfirm, q.MachineID, q.MachineEphemeralKey, private)
			if e != nil {
				t.Error(e)
			}
			e = domaincloud.DecodeIdentityConfirm(clear, domaincloud.IdentityConfirm{PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID, ViewerDeviceID: offer.ViewerDeviceID, ContentKeyEpoch: epoch, ActivateSHA256: domaincloud.PhaseDigest(activate)})
			if e != nil {
				t.Error(e)
			}
			statement := domaincloud.HandoverStatement{AccountID: q.AccountID, PairingID: q.PairingID, MachineID: q.MachineID, MachineKeyEpoch: 1, ViewerDeviceID: offer.ViewerDeviceID, ViewerSigningKey: offer.ViewerSigningKey, ViewerKeyEpoch: 2, IdentityEpoch: 4, ContentKeyEpoch: epoch, ActivateSHA256: domaincloud.PhaseDigest(activate), ConfirmSHA256: domaincloud.PhaseDigest(blob)}
			msg, _ := statement.Message()
			var signature string
			json.Unmarshal(body["handover_attestation"], &signature)
			sig, _ := base64.StdEncoding.DecodeString(signature)
			if !ed25519.Verify(machine.PublicKey(), msg, sig) {
				t.Error("identity attestation signature failed")
			}
			h.confirms.Add(1)
			reply(map[string]any{"content_key_grant": map[string]any{"status": "recorded", "viewer_device_id": offer.ViewerDeviceID, "content_key_epoch": epoch, "identity_epoch": 4}})
		default:
			t.Error("unexpected synthetic route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	h.pairing = &IdentityPairing{AccountID: "account", Client: &adaptercloud.IdentityClient{BaseURL: server.URL, Credential: func(context.Context) (string, error) { return "synthetic", nil }}, Keys: func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
		s, e := l.contentEpoch.Current()
		return machine, s.Key, e
	}, Epochs: adaptercloud.ContentEpochStore{Dir: l.keys.Dir()}, Authority: &l.contentAuthority.mu, Generation: &l.contentAuthority.generation, ContentState: l.contentEpoch.Current, Pinned: l.pinned}
	h.pairing.Run = func(ctx context.Context, m *adaptercloud.MachineIdentityPairing, account string) (adaptercloud.IdentityPairingResult, error) {
		result, err := m.Run(ctx, account)
		if err != nil {
			t.Logf("synthetic identity adapter: %v", err)
		}
		return result, err
	}

	return h
}

func (h *identityRuntimeHarness) begin(t *testing.T) {
	t.Helper()
	if _, err := h.pairing.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := h.pairing.State()
		if s.Phase == IdentityPairingConfirm {
			return
		}
		if s.Phase == IdentityPairingFailed {
			t.Fatalf("begin failed: %+v", s)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("identity pairing never requested decision")
}
func (h *identityRuntimeHarness) finish(t *testing.T) IdentityPairingState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := h.pairing.State()
		if s.Phase == IdentityPairingPaired || s.Phase == IdentityPairingFailed || s.Phase == IdentityPairingStopped {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("identity pairing never completed")
	return IdentityPairingState{}
}

func TestIdentityRuntimeCurrentEpochAndVerifiedPin(t *testing.T) {
	h := newIdentityRuntimeHarness(t)
	old, _ := h.link.contentEpoch.Current()
	next, err := h.link.keys.RotateContentKey("identity-test", old.Epoch, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.link.contentEpoch.Recover(h.link.keys.RecoverContentKey, h.link.retireContent); err != nil {
		t.Fatal(err)
	}
	oldID, _ := domaincloud.ContentKeyID(old.Key.Bytes())
	if err = h.pairing.Epochs.Save(adaptercloud.ContentEpoch{V: 1, AccountID: "account", ContentKeyEpoch: 1, KeyID: oldID}); err != nil {
		t.Fatal(err)
	}
	h.begin(t)
	h.pairing.Decide(h.pairing.State().PairingID, true)
	s := h.finish(t)
	if s.Phase != IdentityPairingPaired || s.ContentKeyEpoch != int64(next.Epoch) {
		t.Fatalf("handover: %+v", s)
	}
	pin, err := h.link.pinned.ActiveRekeyPin("account", "identity-viewer")
	if err != nil || !bytes.Equal(pin, h.viewer.PublicKey()) {
		t.Fatal("verified offer did not become a completed pin", err)
	}
	if scope, err := h.link.pinned.ViewerScope("identity-viewer"); err != nil || scope != adaptercloud.ScopeReadOnly {
		t.Fatal("identity pairing raised authority", scope, err)
	}
	restarted := adaptercloud.NewPinnedStore(h.link.keys.Dir())
	if _, err = restarted.ActiveRekeyPin("account", "identity-viewer"); err != nil {
		t.Fatal("pin not durable", err)
	}
	e, known, err := h.pairing.Epochs.Load("account", next.Key.Bytes())
	if err != nil || !known || e != int64(next.Epoch) {
		t.Fatal("legacy epoch bridge lost current epoch", e, known, err)
	}
	// A completed readonly identity pin can receive a targeted current-key answer.
	roster := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "identity-viewer", PublicKey: base64.StdEncoding.EncodeToString(h.viewer.PublicKey()), Caps: []string{"read_transcript"}}}})
	}))
	defer roster.Close()
	h.link.roster = adaptercloud.NewRoster(roster.URL, "synthetic", time.Now)
	raw, _ := runtimeOffer(t, "identity-viewer", h.viewer, time.Now().UnixMilli())
	var answer []byte
	if err := h.link.answerRekey(raw, func(b []byte) error { answer = bytes.Clone(b); return nil }); err != nil || len(answer) == 0 {
		t.Fatal("completed readonly identity pin cannot rekey", err)
	}
	if changed, err := h.link.revokeContentViewer("identity-viewer"); err != nil || !changed {
		t.Fatal("completed identity pin is not revocable", changed, err)
	}
}

func TestIdentityRuntimeRevocationInvalidatesLongRun(t *testing.T) {
	for _, stage := range []string{"decision", "activate"} {
		t.Run(stage, func(t *testing.T) {
			h := newIdentityRuntimeHarness(t)
			if stage == "activate" {
				h.beforeActivate = func() {
					if _, err := h.link.revokeContentViewer("identity-viewer"); err != nil {
						t.Error(err)
					}
				}
			}
			h.begin(t)
			if stage == "decision" {
				if _, err := h.link.revokeContentViewer("identity-viewer"); err != nil {
					t.Fatal(err)
				}
			}
			h.pairing.Decide(h.pairing.State().PairingID, true)
			if s := h.finish(t); s.Phase != IdentityPairingFailed {
				t.Fatalf("retired generation completed: %+v", s)
			}
			if h.confirms.Load() != 0 {
				t.Fatal("retired generation wrote confirm")
			}
			if stage == "decision" && h.grants.Load() != 0 {
				t.Fatal("retired generation disclosed secret")
			}
			if _, err := h.link.pinned.ActiveRekeyPin("account", "identity-viewer"); err == nil {
				t.Fatal("retired generation left completed pin")
			}
		})
	}
}

func TestIdentityRuntimeFinalPinSerializesRevocation(t *testing.T) {
	h := newIdentityRuntimeHarness(t)
	revoked := make(chan error, 1)
	h.beforeConfirm = func() {
		go func() {
			changed, err := h.link.revokeContentViewer("identity-viewer")
			if !changed && err == nil {
				err = fmt.Errorf("handover pin was not visible to revoke")
			}
			revoked <- err
		}()
		select {
		case err := <-revoked:
			t.Errorf("revocation escaped confirm/pin authority: %v", err)
			revoked <- err
		case <-time.After(25 * time.Millisecond):
		}
	}
	h.begin(t)
	h.pairing.Decide(h.pairing.State().PairingID, true)
	if s := h.finish(t); s.Phase != IdentityPairingPaired {
		t.Fatalf("handover failed: %+v", s)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if refused, err := h.link.pinned.Refused("identity-viewer"); err != nil || !refused {
		t.Fatal("late pin resurrected viewer", err)
	}
}

func TestIdentityRuntimeStorageFailureDoesNotReportPaired(t *testing.T) {
	h := newIdentityRuntimeHarness(t)
	h.beforeActivate = func() {
		path := h.link.pinned.Path()
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Error(err)
		}
	}
	h.begin(t)
	h.pairing.Decide(h.pairing.State().PairingID, true)
	if s := h.finish(t); s.Phase != IdentityPairingFailed {
		t.Fatalf("failed pin storage reported success: %+v", s)
	}
	if _, err := adaptercloud.NewPinnedStore(h.link.keys.Dir()).ActiveRekeyPin("account", "identity-viewer"); err == nil {
		t.Fatal("restart accepted an unpersisted pin")
	}
}

func TestIdentityRuntimeCancelAndReplacementCannotComplete(t *testing.T) {
	for _, action := range []string{"cancel", "replace", "state-change"} {
		t.Run(action, func(t *testing.T) {
			h := newIdentityRuntimeHarness(t)
			h.begin(t)
			switch action {
			case "cancel":
				h.pairing.Cancel()
				if s := h.finish(t); s.Phase != IdentityPairingStopped {
					t.Fatalf("cancel state: %+v", s)
				}
			case "replace":
				// Capture the first run's authorization seam, then start another generation.
				var old *adaptercloud.MachineIdentityPairing
				h.pairing.Run = func(ctx context.Context, m *adaptercloud.MachineIdentityPairing, account string) (adaptercloud.IdentityPairingResult, error) {
					old = m
					return m.Run(ctx, account)
				}
				h.pairing.Cancel()
				h.begin(t)
				h.pairing.Cancel()
				if old == nil {
					t.Fatal("replacement runner missing")
				}
				called := false
				if err := old.Authorize(context.Background(), func() error { called = true; return nil }); err == nil || called {
					t.Fatal("cancelled run still authorized")
				}
			case "state-change":
				current, _ := h.link.contentEpoch.Current()
				if _, err := h.link.keys.RotateContentKey("external-key-change", current.Epoch, rand.Reader); err != nil {
					t.Fatal(err)
				}
				if _, err := h.link.contentEpoch.Recover(h.link.keys.RecoverContentKey, h.link.retireContent); err != nil {
					t.Fatal(err)
				}
				h.pairing.Decide(h.pairing.State().PairingID, true)
				if s := h.finish(t); s.Phase != IdentityPairingFailed {
					t.Fatalf("changed key completed old transcript: %+v", s)
				}
			}
			if h.grants.Load() != 0 || h.confirms.Load() != 0 {
				t.Fatal("abandoned transcript disclosed content")
			}
		})
	}
}

func TestIdentityRuntimeSignedInvitationExpiresWithoutPin(t *testing.T) {
	h := newIdentityRuntimeHarness(t)
	h.lifetime = 250 * time.Millisecond
	h.begin(t)
	time.Sleep(300 * time.Millisecond)
	if s := h.finish(t); s.Phase != IdentityPairingFailed {
		t.Fatalf("expired invitation completed: %+v", s)
	}
	if h.grants.Load() != 0 || h.confirms.Load() != 0 {
		t.Fatal("expired invitation disclosed content")
	}
}

// A persistence error may precede rename or follow a readable durable intent.
// Both outcomes must stop all old-key publication until a retry recovers.
func TestRuntimeJournalCreationFailureStopsPublication(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(fmt.Sprintf("durable=%v", durable), func(t *testing.T) {
			l, a, b := runtimeFixture(t)
			old, _ := l.contentEpoch.Current()
			_, err := l.revokeContentViewerWithJournal("A", func(viewer string, epoch uint64) (cloudkeys.RevocationIntent, error) {
				var intent cloudkeys.RevocationIntent
				if durable {
					var err error
					intent, err = l.keys.BeginRevocation(viewer, epoch)
					if err != nil {
						t.Fatal(err)
					}
				}
				return intent, errors.New("injected uncertain journal persistence")
			})
			if err == nil {
				t.Fatal("journal failure was not returned")
			}
			intent, pending, err := l.keys.PendingRevocation()
			if err != nil || pending != durable {
				t.Fatal("unexpected durable intent", pending, err)
			}
			if err := l.contentEpoch.Use(func(domaincloud.ContentKeyState) error {
				t.Fatal("old key publication remained admitted after journal failure")
				return nil
			}); !errors.Is(err, domaincloud.ErrKeyEpochHalted) {
				t.Fatal("epoch did not halt", err)
			}
			offer, _ := runtimeOffer(t, "A", a, time.Now().UnixMilli())
			if err := l.answerRekey(offer, func([]byte) error { t.Fatal("A received a key after uncertain revoke"); return nil }); err == nil {
				t.Fatal("uncertain revoke allowed key disclosure")
			}
			changed, err := l.revokeContentViewer("A")
			if err != nil || !changed {
				t.Fatal("retry failed", changed, err)
			}
			current, err := l.contentEpoch.Current()
			if err != nil || current.Epoch != old.Epoch+1 {
				t.Fatal("retry did not rotate exactly once", current.Epoch, err)
			}
			if pending {
				again, err := l.keys.RotateContentKey(intent.Operation, intent.ExpectedEpoch, rand.Reader)
				if err != nil || again.KeyID != current.KeyID {
					t.Fatal("retry replaced the durable operation", err)
				}
			}
			if changed, err := l.revokeContentViewer("A"); err != nil || changed {
				t.Fatal("completed revoke repeated", changed, err)
			}
			final, _ := l.contentEpoch.Current()
			if final.KeyID != current.KeyID {
				t.Fatal("completed retry minted another key")
			}
			if err := l.answerRekey(offer, func([]byte) error { t.Fatal("revoked A received a key"); return nil }); err == nil {
				t.Fatal("A was resurrected")
			}
			offerB, _ := runtimeOffer(t, "B", b, time.Now().UnixMilli())
			answered := false
			if err := l.answerRekey(offerB, func([]byte) error { answered = true; return nil }); err != nil || !answered {
				t.Fatal("readonly B could not recover", err)
			}
		})
	}
}

func TestRuntimePendingRevocationReadAndPinFailuresHalt(t *testing.T) {
	for _, stage := range []string{"journal-read", "pin-write"} {
		t.Run(stage, func(t *testing.T) {
			l, a, _ := runtimeFixture(t)
			old, _ := l.contentEpoch.Current()
			intent, err := l.keys.BeginRevocation("A", old.Epoch)
			if err != nil {
				t.Fatal(err)
			}
			var repair func()
			if stage == "journal-read" {
				path := filepath.Join(l.keys.Dir(), "content-key-revocation-v1")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid journal"), 0600); err != nil {
					t.Fatal(err)
				}
				repair = func() {
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				path := l.pinned.Path()
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				repair = func() {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := l.revokeContentViewer("A"); err == nil {
				t.Fatal("storage failure was ignored")
			}
			if err := l.contentEpoch.Use(func(domaincloud.ContentKeyState) error {
				t.Fatal("pending revoke left publication open")
				return nil
			}); !errors.Is(err, domaincloud.ErrKeyEpochHalted) {
				t.Fatal("pending failure did not halt", err)
			}
			repair()
			if changed, err := l.revokeContentViewer("A"); err != nil || !changed {
				t.Fatal("repair did not resume", changed, err)
			}
			state, err := l.contentEpoch.Current()
			if err != nil || state.Epoch != old.Epoch+1 {
				t.Fatal("repair rotated incorrectly", state.Epoch, err)
			}
			again, err := l.keys.RotateContentKey(intent.Operation, intent.ExpectedEpoch, rand.Reader)
			if err != nil || again.KeyID != state.KeyID {
				t.Fatal("repair replaced intent", err)
			}
			offer, _ := runtimeOffer(t, "A", a, time.Now().UnixMilli())
			if err := l.answerRekey(offer, func([]byte) error { t.Fatal("repair resurrected A"); return nil }); err == nil {
				t.Fatal("revoked A was accepted")
			}
		})
	}
}

// The persisted shape was produced by v2.0.0 (d2ed0c0633dec8dc7c4f0d2b8d78cf229a2795d4)
// PinnedStore.Pin using synthetic data. Only B's public key is replaced per run.
func TestRuntimeHistoricalStrongPinRekeysWithoutChangingScope(t *testing.T) {
	for _, c := range []struct {
		name, scope                                                  string
		pending, revoked, missing, wrongAccount, badKey, wrongSigner bool
		want                                                         bool
	}{
		{name: "v2-empty-scope", want: true}, {name: "operate", scope: "operate", want: true},
		{name: "readonly", scope: "readonly", want: true}, {name: "unknown-scope", scope: "future"},
		{name: "pending", scope: "operate", pending: true}, {name: "revoked", scope: "operate", revoked: true},
		{name: "empty-scope-pending", pending: true}, {name: "empty-scope-revoked", revoked: true},
		{name: "roster-only", missing: true}, {name: "wrong-account", wrongAccount: true},
		{name: "bad-key", badKey: true}, {name: "wrong-signer", wrongSigner: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, a, b := runtimeFixture(t)
			if _, err := l.revokeContentViewer("A"); err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(`{"version":1,"account_id":"account","devices":[{"device_id":"B","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","fingerprint":"synthetic-public-fingerprint","paired_at":1700000000}]}`), &doc); err != nil {
				t.Fatal(err)
			}
			row := doc["devices"].([]any)[0].(map[string]any)
			row["public_key"] = base64.StdEncoding.EncodeToString(b.PublicKey())
			if c.scope != "" {
				row["scope"] = c.scope
			}
			if c.pending {
				row["pairing_pending"] = true
			}
			if c.revoked {
				row["revoked_at"] = 1700000001
			}
			if c.missing {
				doc["devices"] = []any{}
			}
			if c.wrongAccount {
				doc["account_id"] = "other"
			}
			if c.badKey {
				row["public_key"] = "invalid"
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(l.pinned.Path(), raw, 0600); err != nil {
				t.Fatal(err)
			}
			l.pinned = adaptercloud.NewPinnedStore(l.keys.Dir())
			signer := b
			if c.wrongSigner {
				signer = a
			}
			offer, private := runtimeOffer(t, "B", signer, time.Now().UnixMilli())
			var answer []byte
			err = l.answerRekey(offer, func(v []byte) error { answer = bytes.Clone(v); return nil })
			if got := err == nil && len(answer) > 0; got != c.want {
				t.Fatalf("answered=%v want %v: %v", got, c.want, err)
			}
			if c.want {
				machine, _, _ := l.keys.DeviceKey()
				state, err := domaincloud.OpenRekeyAnswer(answer, offer, domaincloud.RekeyAuthority{AccountID: "account", MachineID: "machine", ViewerDeviceID: "B", ActiveViewerKey: b.PublicKey()}, private, machine.PublicKey(), 1, "ms-1", time.Now().UnixMilli())
				current, _ := l.contentEpoch.Current()
				if err != nil || state.Epoch != 2 || !bytes.Equal(state.Key.Bytes(), current.Key.Bytes()) {
					t.Fatalf("epoch 2 recovery: %v", err)
				}
				scope, err := l.pinned.ViewerScope("B")
				want := c.scope
				if want == "" {
					want = "operate"
				}
				if err != nil || string(scope) != want {
					t.Fatalf("scope changed: %s %v", scope, err)
				}
			}
			after, err := os.ReadFile(l.pinned.Path())
			if err != nil || !bytes.Equal(after, raw) {
				t.Fatal("rekey mutated historical pin", err)
			}
		})
	}
}
