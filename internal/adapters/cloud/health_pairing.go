package cloud

// The machine half of identity pairing (internal/domain/cloud/health_pairing.go)
// over Cloud's strict routes, and the record of which content-key epoch this
// machine's master secret is.
//
// The record is written only from a confirm receipt in which Cloud says it
// recorded the content_key_grant this machine attested. Until then the epoch
// is unknown and owner-sealed diagnostics refuse with key_handover_required:
// an old master secret with no handover behind it gets no epoch by default.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// Identity pairing bounds.
const (
	// IdentityPollInterval is the wait between two polls of one phase.
	IdentityPollInterval = time.Second
	// MaxIdentityPollAttempts bounds the polls for one phase; Cloud's pairing
	// lives at most 600 s, so more would only poll an expired row.
	MaxIdentityPollAttempts = 600
	// IdentityRequestTimeout is one request's deadline.
	IdentityRequestTimeout = 15 * time.Second
	// ContentEpochFile is the epoch record, beside the key files.
	ContentEpochFile    = "content-key-epoch-v1.json"
	maxEpochRecordBytes = 4 << 10
)

// Identity pairing failures a caller branches on.
var (
	ErrIdentityPollExhausted = errors.New("the identity pairing phase did not arrive before the poll bound")
	ErrIdentityGrantMissing  = errors.New("Cloud did not record the content_key_grant this machine attested")
	ErrIdentityNoMaster      = errors.New("this machine holds no account master secret to hand over")
)

// CloudRefusal is a typed Cloud error: status and stable code, never the message.
type CloudRefusal struct {
	Status int
	Code   string
}

func (e *CloudRefusal) Error() string { return fmt.Sprintf("cloud refused: %d %s", e.Status, e.Code) }

// IdentityStartReceipt is Cloud's `start` answer, the parts this side uses.
type IdentityStartReceipt struct {
	QR       json.RawMessage `json:"qr"`
	Identity struct {
		IdentityEpoch   int64 `json:"identity_epoch"`
		MachineKeyEpoch int64 `json:"machine_key_epoch"`
	} `json:"identity"`
}

// IdentityPhaseReceipt is Cloud's phase-write answer.
type IdentityPhaseReceipt struct {
	Status          string `json:"status"`
	Phase           string `json:"phase"`
	PhaseSHA256     string `json:"phase_sha256"`
	IdentityEpoch   int64  `json:"identity_epoch"`
	MachineKeyEpoch int64  `json:"machine_key_epoch"`
	ViewerKeyEpoch  int64  `json:"viewer_key_epoch"`
	ContentKeyEpoch int64  `json:"content_key_epoch"`
	ContentKeyGrant *struct {
		Status          string `json:"status"`
		ViewerDeviceID  string `json:"viewer_device_id"`
		ViewerKeyEpoch  int64  `json:"viewer_key_epoch"`
		ContentKeyEpoch int64  `json:"content_key_epoch"`
		IdentityEpoch   int64  `json:"identity_epoch"`
	} `json:"content_key_grant,omitempty"`
}

// identityPoll is Cloud's poll answer.
type identityPoll struct {
	Status      string          `json:"status"`
	Blob        json.RawMessage `json:"blob"`
	PhaseSHA256 string          `json:"phase_sha256"`
}

// IdentityClient calls the three identity routes with the machine credential.
type IdentityClient struct {
	BaseURL    string
	HTTP       *http.Client
	Credential func(ctx context.Context) (string, error)
}

func (c *IdentityClient) post(ctx context.Context, path string, body []byte, out any) (int, error) {
	cred, err := c.Credential(ctx)
	if err != nil || cred == "" {
		return 0, &CloudRefusal{Status: 401, Code: "no_machine_credential"}
	}
	ctx, cancel := context.WithTimeout(ctx, IdentityRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: IdentityRequestTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return res.StatusCode, err
	}
	if len(raw) > maxResponseBytes {
		return res.StatusCode, fmt.Errorf("identity pairing answer past %d bytes", maxResponseBytes)
	}
	if res.StatusCode >= 300 {
		var r struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &r)
		return res.StatusCode, &CloudRefusal{Status: res.StatusCode, Code: r.Error.Code}
	}
	return res.StatusCode, json.Unmarshal(raw, out)
}

func phaseBody(claimNonce string, w cloud.PairingWrapper, attestation string) []byte {
	m := map[string]cloud.Value{"claim_nonce": cloud.Str(claimNonce), "blob": w.Value()}
	if attestation != "" {
		m["handover_attestation"] = cloud.Str(attestation)
	}
	return cloud.CanonicalBytes(cloud.Object(m))
}

// WritePhase writes one phase, with the attestation on confirm only.
func (c *IdentityClient) WritePhase(ctx context.Context, claimNonce string, w cloud.PairingWrapper, attestation string) (IdentityPhaseReceipt, error) {
	var r IdentityPhaseReceipt
	_, err := c.post(ctx, "/v1/pairing/identity/phases/"+w.Phase, phaseBody(claimNonce, w, attestation), &r)
	return r, err
}

// PollPhase answers the wrapper and Cloud's digest, or ok=false while pending.
func (c *IdentityClient) PollPhase(ctx context.Context, pairingID, phase, claimNonce string) (cloud.PairingWrapper, string, bool, error) {
	body := cloud.CanonicalBytes(cloud.Object(map[string]cloud.Value{
		"pairing_id": cloud.Str(pairingID), "phase": cloud.Str(phase), "claim_nonce": cloud.Str(claimNonce),
	}))
	var p identityPoll
	status, err := c.post(ctx, "/v1/pairing/identity/poll", body, &p)
	if err != nil {
		return cloud.PairingWrapper{}, "", false, err
	}
	if status == http.StatusAccepted || p.Status != "ready" {
		return cloud.PairingWrapper{}, "", false, nil
	}
	w, err := cloud.DecodePairingWrapper(p.Blob)
	if err != nil {
		return cloud.PairingWrapper{}, "", false, err
	}
	return w, p.PhaseSHA256, true, nil
}

// MARK: content-key epoch record

// ContentEpoch is the persisted record.
type ContentEpoch struct {
	V               int    `json:"v"`
	ContentKeyEpoch int64  `json:"content_key_epoch"`
	KeyID           string `json:"key_id"`
	AccountID       string `json:"account_id"`
	MachineID       string `json:"machine_id"`
	PairingID       string `json:"pairing_id"`
	RecordedAtMS    int64  `json:"recorded_at_ms"`
}

// ContentEpochStore reads and writes the record in one directory.
type ContentEpochStore struct{ Dir string }

// Load answers the epoch only when the record names this account and the
// master secret it is given; anything else is unknown, never a default.
func (s ContentEpochStore) Load(accountID string, master []byte) (int64, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, ContentEpochFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if len(raw) > maxEpochRecordBytes {
		return 0, false, fmt.Errorf("content-key epoch record past %d bytes", maxEpochRecordBytes)
	}
	var rec ContentEpoch
	if err := json.Unmarshal(raw, &rec); err != nil {
		return 0, false, err
	}
	id, err := cloud.ContentKeyID(master)
	if err != nil {
		return 0, false, nil
	}
	if rec.V != 1 || rec.ContentKeyEpoch < 1 || rec.KeyID != id || rec.AccountID != accountID {
		return 0, false, nil
	}
	return rec.ContentKeyEpoch, true, nil
}

// Save writes the record atomically (temp file, fsync, rename).
func (s ContentEpochStore) Save(rec ContentEpoch) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".epoch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.Dir, ContentEpochFile)); err != nil {
		return err
	}
	directory, err := os.Open(s.Dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// Forget removes the record (sign-out, account change).
func (s ContentEpochStore) Forget() error {
	err := os.Remove(filepath.Join(s.Dir, ContentEpochFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// MARK: the machine run

// MachineIdentityPairing runs one pairing from this machine. Every input is
// injected: the tests and the end-to-end experiment use synthetic keys and
// never read a real key file.
type MachineIdentityPairing struct {
	// Authorize serializes only key reads, disclosure, and confirmed local commit.
	// Polls and human decisions must never run inside this callback.
	Authorize    func(context.Context, func() error) error
	ContentState func() (cloud.ContentKeyState, error)
	Reserve      func(cloud.IdentityOffer) error
	Complete     func(IdentityPairingResult) error
	Client       *IdentityClient
	MachineKey   cloud.DeviceKey
	// Master reads the account master secret (cloudkeys.Files.MasterSecret).
	Master func() ([]byte, bool, error)
	Epochs ContentEpochStore
	// Show receives the QR fragment for the person to carry to the viewer.
	Show func(fragment string)
	// Approve asks the person on this machine to compare the viewer's
	// fingerprint and the short code. Nothing is handed over unless it
	// answers true; there is no automatic approval.
	Approve func(ctx context.Context, viewerFingerprint, code string) (bool, error)
	Now     func() time.Time
	Rand    io.Reader
	Sleep   func(ctx context.Context, d time.Duration) error
}

// IdentityPairingResult is what a completed run proved.
type IdentityPairingResult struct {
	PairingID         string
	ViewerDeviceID    string
	ViewerFingerprint string
	ViewerSigningKey  string
	ContentKeyEpoch   int64
	IdentityEpoch     int64
}

func (m *MachineIdentityPairing) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *MachineIdentityPairing) sleep(ctx context.Context, d time.Duration) error {
	if m.Sleep != nil {
		return m.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (m *MachineIdentityPairing) random(n int) ([]byte, error) {
	r := m.Rand
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func (m *MachineIdentityPairing) poll(ctx context.Context, q cloud.IdentityPairingQR, phase string) (cloud.PairingWrapper, string, error) {
	for attempt := 0; attempt < MaxIdentityPollAttempts; attempt++ {
		w, digest, ok, err := m.Client.PollPhase(ctx, q.PairingID, phase, q.ClaimNonce)
		if err != nil {
			return cloud.PairingWrapper{}, "", err
		}
		if ok {
			return w, digest, nil
		}
		if err := m.sleep(ctx, IdentityPollInterval); err != nil {
			return cloud.PairingWrapper{}, "", err
		}
	}
	return cloud.PairingWrapper{}, "", ErrIdentityPollExhausted
}

// Run performs start → (offer) → grant → (activate) → attested confirm and
// records the epoch only from Cloud's grant receipt.
func (m *MachineIdentityPairing) Run(ctx context.Context, accountID string) (IdentityPairingResult, error) {
	var master []byte
	var epoch int64
	var known bool
	var initial cloud.ContentKeyState
	err := m.authorized(ctx, func() error {
		if m.ContentState != nil {
			state, err := m.ContentState()
			if err != nil {
				return err
			}
			if _, err := cloud.NewKeyEpoch(state); err != nil {
				return err
			}
			if state.Epoch > math.MaxInt64 {
				return cloud.ErrIdentityEpoch
			}
			initial = state
			master = bytes.Clone(state.Key.Bytes())
			epoch = int64(state.Epoch)
			return m.Epochs.CheckRuntime(accountID, state)
		}
		var ok bool
		var err error
		master, ok, err = m.Master()
		if err != nil {
			return err
		}
		if !ok || len(master) != cloud.ContentKeyBytes {
			return ErrIdentityNoMaster
		}
		epoch, known, err = m.Epochs.Load(accountID, master)
		if err == nil && !known {
			epoch = 1
		}
		return err
	})
	defer clear(master)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	// A state reader cannot silently change the selected key mid-transcript.
	guarded := func(fn func() error) error {
		return m.authorized(ctx, func() error {
			if m.ContentState != nil {
				state, err := m.ContentState()
				if err != nil {
					return err
				}
				if state.Epoch != initial.Epoch || state.KeyID != initial.KeyID || !bytes.Equal(state.Key.Bytes(), master) {
					return cloud.ErrRekey
				}
			}
			return fn()
		})
	}
	ephemeral, err := cloud.NewX25519Private(m.Rand)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	defer clear(ephemeral)
	ephemeralPublic, err := cloud.X25519PublicKey(ephemeral)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	pairingNonce, err := m.random(cloud.PairingNonceBytes)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	keyID, err := cloud.ContentKeyID(master)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	startBody, err := cloud.IdentityStartBody(m.MachineKey, ephemeralPublic, pairingNonce, epoch, "ck-"+keyID[:16])
	if err != nil {
		return IdentityPairingResult{}, err
	}
	var start IdentityStartReceipt
	if _, err := m.Client.post(ctx, "/v1/pairing/identity/start", startBody, &start); err != nil {
		return IdentityPairingResult{}, err
	}
	q, err := cloud.DecodeIdentityPairingQR(start.QR, m.now().UnixMilli())
	if err != nil {
		return IdentityPairingResult{}, err
	}
	if q.AccountID != accountID || q.MachineSigningKey != base64.StdEncoding.EncodeToString(m.MachineKey.PublicKey()) ||
		q.MachineEphemeralKey != base64.StdEncoding.EncodeToString(ephemeralPublic) {
		return IdentityPairingResult{}, cloud.ErrIdentityTranscript
	}
	// The signed invitation expires independently of the wrapper's lifetime.
	var cancel context.CancelFunc
	ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(q.ExpiresAt))
	defer cancel()
	if m.Show != nil {
		m.Show(q.Fragment())
	}

	offerWrapper, offerDigest, err := m.poll(ctx, q, cloud.IdentityPhaseOffer)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	if cloud.PhaseDigest(offerWrapper) != offerDigest {
		return IdentityPairingResult{}, cloud.ErrIdentityPhaseDigest
	}
	offerClear, err := cloud.OpenIdentityPhase(q, offerWrapper, cloud.IdentityPhaseOffer, offerWrapper.SenderDeviceID, offerWrapper.EphemeralKey, ephemeral)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	offer, err := cloud.DecodeIdentityOffer(offerClear, q, offerWrapper)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	if m.Approve == nil {
		return IdentityPairingResult{}, cloud.ErrPairingNotApproved
	}
	approved, err := m.Approve(ctx, offer.ViewerFingerprint, q.ShortCode(offer.ViewerSigningKey, offer.ViewerEphKey))
	if err != nil {
		return IdentityPairingResult{}, err
	}
	if !approved {
		return IdentityPairingResult{}, cloud.ErrPairingNotApproved
	}

	var grantReceipt IdentityPhaseReceipt
	var grantDigest string
	err = guarded(func() error {
		if m.Reserve != nil {
			if err := m.Reserve(offer); err != nil {
				return err
			}
		}
		grantNonce, err := m.random(cloud.NonceBytes)
		if err != nil {
			return err
		}
		grant := cloud.IdentityGrant{
			PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID,
			MachineSigningKey: q.MachineSigningKey, MachineFingerprint: q.MachineFingerprint,
			ViewerDeviceID: offer.ViewerDeviceID, OfferSHA256: offerDigest,
			ContentKeyEpoch: epoch, IdentityEpoch: start.Identity.IdentityEpoch,
			MasterSecret: base64.StdEncoding.EncodeToString(master),
		}
		grantPlain := grant.Canonical()
		grantWrapper, err := cloud.SealIdentityPhase(q, cloud.IdentityPhaseGrant, q.MachineID, ephemeral, offer.ViewerEphKey, grantNonce, grantPlain)
		clear(grantPlain)
		if err != nil {
			return err
		}
		grantReceipt, err = m.Client.WritePhase(ctx, q.ClaimNonce, grantWrapper, "")
		if err != nil {
			return err
		}
		grantDigest = cloud.PhaseDigest(grantWrapper)
		if grantReceipt.PhaseSHA256 != grantDigest || grantReceipt.ContentKeyEpoch != epoch {
			return cloud.ErrIdentityPhaseDigest
		}

		return nil
	})
	if err != nil {
		return IdentityPairingResult{}, err
	}

	activateWrapper, activateDigest, err := m.poll(ctx, q, cloud.IdentityPhaseActivate)
	if err != nil {
		return IdentityPairingResult{}, err
	}
	var result IdentityPairingResult
	err = guarded(func() error {
		confirmNonce, err := m.random(cloud.NonceBytes)
		if err != nil {
			return err
		}
		attested, err := cloud.AttestVerifiedActivate(cloud.ActivateCheck{
			QR: q, Offer: offer, GrantSHA256: grantDigest, ActivateWrapper: activateWrapper, ActivatePhaseSHA: activateDigest,
			MachineEphemeral: ephemeral, Master: master, ContentKeyEpoch: epoch, IdentityEpoch: start.Identity.IdentityEpoch,
			MachineKeyEpoch: start.Identity.MachineKeyEpoch, ViewerKeyEpoch: grantReceipt.ViewerKeyEpoch, ConfirmNonce: confirmNonce,
		}, m.MachineKey)
		if err != nil {
			return err
		}
		confirmReceipt, err := m.Client.WritePhase(ctx, q.ClaimNonce, attested.Confirm, attested.Attestation)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		g := confirmReceipt.ContentKeyGrant
		if g == nil || g.Status != "recorded" || g.ViewerDeviceID != offer.ViewerDeviceID || g.ContentKeyEpoch != epoch ||
			g.IdentityEpoch != start.Identity.IdentityEpoch {
			return ErrIdentityGrantMissing
		}
		if !known || m.ContentState != nil {
			if err := m.Epochs.Save(ContentEpoch{V: 1, ContentKeyEpoch: epoch, KeyID: keyID, AccountID: q.AccountID,
				MachineID: q.MachineID, PairingID: q.PairingID, RecordedAtMS: m.now().UnixMilli()}); err != nil {
				return err
			}
		}
		result = IdentityPairingResult{PairingID: q.PairingID, ViewerDeviceID: offer.ViewerDeviceID,
			ViewerFingerprint: offer.ViewerFingerprint, ViewerSigningKey: offer.ViewerSigningKey,
			ContentKeyEpoch: epoch, IdentityEpoch: start.Identity.IdentityEpoch}
		if m.Complete != nil {
			return m.Complete(result)
		}
		return nil
	})
	if err != nil {
		return IdentityPairingResult{}, err
	}
	return result, nil
}

func (m *MachineIdentityPairing) authorized(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.Authorize != nil {
		return m.Authorize(ctx, fn)
	}
	return fn()
}

// CheckRuntime bridges an old attested epoch without allowing it to reset the
// durable runtime floor. Older records describe historical keys, not the new key.
func (s ContentEpochStore) CheckRuntime(accountID string, state cloud.ContentKeyState) error {
	raw, err := os.ReadFile(filepath.Join(s.Dir, ContentEpochFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(raw) > maxEpochRecordBytes {
		return cloud.ErrContentKeyState
	}
	var rec ContentEpoch
	if json.Unmarshal(raw, &rec) != nil || rec.V != 1 || rec.AccountID != accountID || rec.ContentKeyEpoch < 1 || rec.KeyID == "" {
		return cloud.ErrContentKeyState
	}
	if uint64(rec.ContentKeyEpoch) > state.Epoch {
		return cloud.ErrContentKeyConflict
	}
	id, err := cloud.ContentKeyID(state.Key.Bytes())
	if err != nil {
		return err
	}
	if uint64(rec.ContentKeyEpoch) == state.Epoch && rec.KeyID != id {
		return cloud.ErrContentKeyConflict
	}
	return nil
}
