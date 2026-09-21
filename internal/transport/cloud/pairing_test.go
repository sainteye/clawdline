package cloud

// One pairing against a control plane that behaves like the real one and a
// viewer written in Go.
//
// The viewer is the point. `internal/domain/cloud` already proves this build
// produces the published bytes; what is not provable there is the *order* —
// that the invitation carries the offer, that the machine seals for the offer
// it actually received, that the echoed fingerprint is compared rather than
// displayed, and that a pin only exists once the browser could have the key.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// fakeControlPlane is the three pairing routes, with the rules that matter:
// the invitation secret is only ever seen as a hash, the offer is carried
// opaquely, and `complete` echoes the fingerprint recorded at `start`.
type fakeControlPlane struct {
	mu sync.Mutex

	accountID string
	machineID string

	invitationID   string
	secretHash     string
	encryptedOffer string
	viewerDeviceID string

	pairingID          string
	claimNonce         string
	requesterPrint     string
	delivered          []byte
	deliveredFrom      string
	echoWrongPrint     bool
	invitationTTLHours int
}

func (f *fakeControlPlane) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pairing/invitations/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SecretHash string `json:"secret_hash"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.invitationID, f.secretHash = "inv_1", body.SecretHash
		f.mu.Unlock()
		hours := f.invitationTTLHours
		if hours == 0 {
			hours = 0
		}
		expires := time.Now().Add(5 * time.Minute)
		if hours > 0 {
			expires = time.Now().Add(time.Duration(hours) * time.Hour)
		}
		writeTestJSON(w, map[string]any{
			"status": "pending", "invitation_id": "inv_1",
			"expires_at": expires.Format(time.RFC3339), "expires_in": 300,
		})
	})
	mux.HandleFunc("/v1/pairing/invitations/poll", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		offer, viewer := f.encryptedOffer, f.viewerDeviceID
		f.mu.Unlock()
		if offer == "" {
			w.WriteHeader(http.StatusAccepted)
			writeTestJSON(w, map[string]any{"status": "pending"})
			return
		}
		writeTestJSON(w, map[string]any{
			"status": "ready", "account_id": f.accountID, "machine_id": f.machineID,
			"viewer_device_id": viewer, "encrypted_offer": offer,
		})
	})
	mux.HandleFunc("/v1/pairing/complete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PairingID  string `json:"pairing_id"`
			Ciphertext string `json:"ciphertext"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if body.PairingID != f.pairingID {
			w.WriteHeader(http.StatusNotFound)
			writeTestJSON(w, map[string]any{"error": map[string]any{"code": "unknown_pairing"}})
			return
		}
		raw, err := base64.StdEncoding.DecodeString(body.Ciphertext)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.delivered = raw
		f.deliveredFrom = f.machineID
		print := f.requesterPrint
		if f.echoWrongPrint {
			print = "ZZZZ-ZZZZ-ZZZZ-ZZZZ"
		}
		writeTestJSON(w, map[string]any{"status": "delivered", "fingerprint": print})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// goViewer is the browser half: it builds an offer, seals it under the
// invitation secret, and later opens what the machine left.
type goViewer struct {
	deviceID   string
	signing    domaincloud.DeviceKey
	ephemeral  []byte
	offer      domaincloud.PairingOffer
	claimNonce string
}

func newGoViewer(t *testing.T, accountID, pairingID, claimNonce string, expires time.Time) *goViewer {
	t.Helper()
	signing, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatalf("drawing the viewer's signing key: %v", err)
	}
	ephemeral, err := domaincloud.NewX25519PrivateKey(nil)
	if err != nil {
		t.Fatalf("drawing the viewer's ephemeral key: %v", err)
	}
	public, err := domaincloud.X25519PublicKey(ephemeral)
	if err != nil {
		t.Fatalf("deriving the viewer's ephemeral public key: %v", err)
	}
	pairingNonce := make([]byte, domaincloud.PairingNonceBytes)
	pairingNonce[0] = 0xAB
	viewer := &goViewer{
		deviceID: "dev_browser", signing: signing, ephemeral: ephemeral, claimNonce: claimNonce,
	}
	viewer.offer = domaincloud.PairingOffer{
		PairingID:          pairingID,
		ClaimNonce:         claimNonce,
		PairingNonce:       base64.StdEncoding.EncodeToString(pairingNonce),
		AccountID:          accountID,
		ViewerDeviceID:     viewer.deviceID,
		ViewerSigningKey:   base64.StdEncoding.EncodeToString(signing.PublicKey()),
		ViewerEphemeralKey: base64.StdEncoding.EncodeToString(public),
		ViewerFingerprint:  signing.Fingerprint(),
		ExpiresAt:          expires.UnixMilli(),
	}
	return viewer
}

// pairingHarness is one machine, one control plane and one viewer.
type pairingHarness struct {
	plane   *fakeControlPlane
	server  *httptest.Server
	pairing *Pairing
	pinned  *adaptercloud.PinnedStore
	key     domaincloud.DeviceKey
	secret  domaincloud.ContentKey
	viewer  *goViewer
}

func newPairingHarness(t *testing.T) *pairingHarness {
	t.Helper()
	plane := &fakeControlPlane{
		accountID: "usr_test", machineID: "mac_test",
		pairingID: "pair_1", claimNonce: base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	server := plane.start(t)
	key, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatalf("drawing the machine key: %v", err)
	}
	secret, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatalf("drawing the master secret: %v", err)
	}
	pinned := adaptercloud.NewPinnedStore(t.TempDir())
	viewer := newGoViewer(t, plane.accountID, plane.pairingID, plane.claimNonce, time.Now().Add(5*time.Minute))
	plane.requesterPrint = viewer.offer.ViewerFingerprint

	return &pairingHarness{
		plane: plane, server: server, pinned: pinned, key: key, secret: secret, viewer: viewer,
		pairing: &Pairing{
			AccountID: plane.accountID, MachineID: plane.machineID,
			Credential: "machine-credential", AppOrigin: "https://console.example",
			Client: adaptercloud.NewAccountClient(server.URL),
			Keys: func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
				return key, secret, nil
			},
			Pinned: pinned,
		},
	}
}

// waitFor is the one wait in this file: the poll loop is a goroutine, so a
// test that asserted immediately would be asserting on the request that
// started it.
func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestAPairingCarriesTheAccountKeyToABrowserAndPinsIt(t *testing.T) {
	h := newPairingHarness(t)
	state, err := h.pairing.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	if state.Phase != PairingWaiting {
		t.Fatalf("a fresh invitation is %q, wanted %q", state.Phase, PairingWaiting)
	}
	if !strings.HasPrefix(state.Link, "https://console.example/#pair=") {
		t.Fatalf("the link is %q", state.Link)
	}
	if state.MachineFingerprint != h.key.Fingerprint() {
		t.Fatalf("the link names fingerprint %q, this machine's is %q",
			state.MachineFingerprint, h.key.Fingerprint())
	}

	// The browser reads the link, exactly as `captureCloudPairingInvitation`
	// does: everything after `#pair=`.
	fragment := strings.TrimPrefix(state.Link, "https://console.example/#pair=")
	invitation, err := domaincloud.DecodePairingInvitation(fragment, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("the browser could not read the invitation: %v", err)
	}
	// The control plane was told the hash and nothing else.
	h.plane.mu.Lock()
	storedHash := h.plane.secretHash
	h.plane.mu.Unlock()
	if want := base64.StdEncoding.EncodeToString(invitation.SecretHash()); storedHash != want {
		t.Fatalf("the control plane holds %q, wanted the secret's hash %q", storedHash, want)
	}
	if strings.Contains(storedHash, base64.StdEncoding.EncodeToString(invitation.Secret)) {
		t.Fatal("the control plane was given the invitation secret itself")
	}

	nonce := make([]byte, domaincloud.NonceBytes)
	nonce[0] = 5
	sealed, err := invitation.SealEncryptedOffer(h.viewer.offer.Fragment(), nonce)
	if err != nil {
		t.Fatalf("the browser could not seal its offer: %v", err)
	}
	h.plane.mu.Lock()
	h.plane.encryptedOffer, h.plane.viewerDeviceID = sealed, h.viewer.deviceID
	h.plane.mu.Unlock()

	waitFor(t, "the machine to answer the offer", func() bool {
		return h.pairing.State().Phase == PairingPaired
	})
	final := h.pairing.State()
	if final.ViewerDeviceID != h.viewer.deviceID || final.ViewerFingerprint != h.viewer.offer.ViewerFingerprint {
		t.Fatalf("the pairing named %#v", final)
	}
	if final.Link != "" {
		t.Fatal("a finished pairing is still showing a live link")
	}

	// What the browser can now do: open the wrapper and hold the account key.
	h.plane.mu.Lock()
	delivered := append([]byte(nil), h.plane.delivered...)
	h.plane.mu.Unlock()
	wrapper, err := domaincloud.DecodePairingWrapper(delivered)
	if err != nil {
		t.Fatalf("the delivered blob is not a wrapper: %v", err)
	}
	handover, err := domaincloud.OpenPairingHandover(wrapper, h.viewer.offer, h.viewer.ephemeral,
		h.plane.machineID, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("the browser could not open the handover: %v", err)
	}
	if handover.MasterSecret != base64.StdEncoding.EncodeToString(h.secret.Bytes()) {
		t.Fatal("the browser received a different master secret than this machine holds")
	}
	if handover.MachineSigningKey != base64.StdEncoding.EncodeToString(h.key.PublicKey()) {
		t.Fatal("the browser received a different signing key than this machine signs with")
	}

	// And what this machine can now do: verify that browser.
	key, ok, err := h.pinned.PublicKeyFor(h.viewer.deviceID)
	if err != nil || !ok {
		t.Fatalf("the browser was not pinned: %v %v", ok, err)
	}
	if !key.Equal(h.viewer.signing.PublicKey()) {
		t.Fatal("the pin holds a different key than the offer carried")
	}
}

// The other way in: no invitation, a code the person carried.
func TestAPairingCanBeFinishedFromAPastedCode(t *testing.T) {
	h := newPairingHarness(t)
	state, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment())
	if err != nil {
		t.Fatalf("completing from a code: %v", err)
	}
	if state.Phase != PairingPaired || state.ViewerDeviceID != h.viewer.deviceID {
		t.Fatalf("the pairing did not finish: %#v", state)
	}
	if _, ok, err := h.pinned.PublicKeyFor(h.viewer.deviceID); !ok || err != nil {
		t.Fatalf("the browser was not pinned: %v %v", ok, err)
	}
}

// The check a person cannot make for themselves: the control plane recorded a
// different requester than the code names, so the code is not the one that
// opened this pairing.
func TestAFingerprintTheServiceDoesNotEchoStopsThePairing(t *testing.T) {
	h := newPairingHarness(t)
	h.plane.echoWrongPrint = true
	_, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment())
	if err == nil {
		t.Fatal("a pairing completed with a fingerprint the service did not echo")
	}
	if !strings.Contains(err.Error(), "recorded fingerprint") {
		t.Fatalf("the refusal does not say what disagreed: %v", err)
	}
	if h.pairing.State().Phase != PairingFailed {
		t.Fatalf("the pairing is %q after a refused delivery", h.pairing.State().Phase)
	}
	// Nothing was pinned, because nothing was delivered that this machine
	// could show the person.
	if devices, err := h.pinned.Devices(); err != nil || len(devices) != 0 {
		t.Fatalf("a refused pairing left a pin behind: %#v %v", devices, err)
	}
}

func TestACodeForAnotherAccountIsRefusedBeforeAnythingIsSealed(t *testing.T) {
	h := newPairingHarness(t)
	other := newGoViewer(t, "usr_somebody_else", h.plane.pairingID, h.plane.claimNonce,
		time.Now().Add(5*time.Minute))
	if _, err := h.pairing.Complete(context.Background(), other.offer.Fragment()); err == nil {
		t.Fatal("a code for another account was answered")
	}
	h.plane.mu.Lock()
	delivered := h.plane.delivered
	h.plane.mu.Unlock()
	if delivered != nil {
		t.Fatal("something was delivered for a code belonging to another account")
	}
}

func TestAnExpiredCodeIsRefused(t *testing.T) {
	h := newPairingHarness(t)
	stale := newGoViewer(t, h.plane.accountID, h.plane.pairingID, h.plane.claimNonce,
		time.Now().Add(-time.Minute))
	if _, err := h.pairing.Complete(context.Background(), stale.offer.Fragment()); err == nil {
		t.Fatal("an expired code was answered")
	}
}

// A machine whose clock ran away must not show a code it believes is good for
// hours: the ceiling is this build's, not the control plane's.
func TestAnOverlongInvitationWindowIsCutToThisMachinesCeiling(t *testing.T) {
	h := newPairingHarness(t)
	h.plane.invitationTTLHours = 48
	state, err := h.pairing.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	ceiling := time.Now().Add(adaptercloud.InvitationLifetime).Unix()
	if state.ExpiresAt > ceiling+2 {
		t.Fatalf("the invitation claims to live until %d, past this build's ceiling %d",
			state.ExpiresAt, ceiling)
	}
	h.pairing.Cancel()
}

func TestBeginningASecondPairingReplacesTheFirst(t *testing.T) {
	h := newPairingHarness(t)
	first, err := h.pairing.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	second, err := h.pairing.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning again: %v", err)
	}
	if first.Link == second.Link {
		t.Fatal("the second invitation reused the first one's secret")
	}
	if h.pairing.State().Phase != PairingWaiting {
		t.Fatalf("the replacement is %q", h.pairing.State().Phase)
	}
	if state := h.pairing.Cancel(); state.Phase != PairingIdle {
		t.Fatalf("cancelling left the pairing at %q", state.Phase)
	}
}

func TestRevokingIsLocalAndImmediate(t *testing.T) {
	h := newPairingHarness(t)
	if _, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment()); err != nil {
		t.Fatalf("pairing: %v", err)
	}
	changed, err := h.pairing.Revoke(h.viewer.deviceID)
	if err != nil || !changed {
		t.Fatalf("revoking: %v %v", changed, err)
	}
	refused, err := h.pinned.Refused(h.viewer.deviceID)
	if err != nil || !refused {
		t.Fatalf("the revoked browser is not refused: %v %v", refused, err)
	}
	if changed, err := h.pairing.Revoke("dev_nobody"); err != nil || changed {
		t.Fatalf("revoking an unknown device reported a change: %v %v", changed, err)
	}
}

func TestAPairingWithNoIdentityRefusesRatherThanFails(t *testing.T) {
	var pairing Pairing
	if _, err := pairing.Begin(context.Background()); err != ErrPairingUnavailable {
		t.Fatalf("a link with no identity answered %v", err)
	}
	if _, err := pairing.Complete(context.Background(), "x"); err != ErrPairingUnavailable {
		t.Fatalf("a link with no identity answered %v", err)
	}
}
