package cloud

// This is an inactive control-message contract. No runtime producer uses it.
import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

const RekeyLifetimeMSLimit = 120_000
const MaxRekeyBodyBytes = 4096
const rekeySafeInteger = 9007199254740991

var ErrRekey = errors.New("content key exchange rejected")

// RekeyAuthority must come from authenticated routing and a currently active,
// non-legacy local pin. Packet fields and account roster membership are NOT authority.
// The runtime must serialize revocation, this check, sealing and delivery.
type RekeyAuthority struct {
	AccountID, MachineID, ViewerDeviceID string
	ActiveViewerKey                      ed25519.PublicKey
}
type RekeyOffer struct {
	Type               string `json:"type"`
	Version            int    `json:"v"`
	AccountID          string `json:"account_id"`
	MachineID          string `json:"machine_id"`
	ViewerDeviceID     string `json:"viewer_device_id"`
	OperationID        string `json:"operation_id"`
	FromEpoch          uint64 `json:"from_epoch"`
	FromKeyID          string `json:"from_key_id"`
	ViewerEphemeralKey string `json:"viewer_ephemeral_key"`
	Nonce              string `json:"nonce"`
	ExpiresAtMS        int64  `json:"expires_at_ms"`
	Signature          string `json:"signature"`
}
type RekeyAnswer struct {
	Type                string     `json:"type"`
	Version             int        `json:"v"`
	Offer               RekeyOffer `json:"offer"`
	OfferHash           string     `json:"offer_hash"`
	SenderDeviceID      string     `json:"sender_device_id"`
	MachineEphemeralKey string     `json:"machine_ephemeral_key"`
	Epoch               uint64     `json:"epoch"`
	KeyID               string     `json:"key_id"`
	SealingNonce        string     `json:"sealing_nonce"`
	Ciphertext          string     `json:"ciphertext"`
	Signature           string     `json:"signature"`
}

// RekeyCanonical returns sorted-key JSON. All wire strings are restricted to
// printable ASCII without HTML-sensitive characters, so Go and JS agree.
func RekeyCanonical(v any) []byte {
	b, _ := json.Marshal(v)
	var m any
	_ = json.Unmarshal(b, &m)
	b, _ = json.Marshal(m)
	return b
}
func rekeyMap(v any, omit ...string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(RekeyCanonical(v), &m)
	for _, k := range omit {
		delete(m, k)
	}
	return m
}
func rekeyBytes(domain string, v any) []byte {
	return append([]byte("clawdline-content-key-"+domain+"-v1\x00"), RekeyCanonical(v)...)
}
func rekeyParse(raw []byte, out any) error {
	if len(raw) > MaxRekeyBodyBytes {
		return ErrRekey
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || !bytes.Equal(raw, RekeyCanonical(out)) {
		return ErrRekey
	}
	return nil
}
func rekeyString(s string) bool {
	if len(s) == 0 || len(s) > maxPairingIDBytes {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 || c == '<' || c == '>' || c == '&' {
			return false
		}
	}
	return true
}
func rekeyB64(s string, n int) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil || len(b) != n || base64.StdEncoding.EncodeToString(b) != s {
		return nil, ErrRekey
	}
	return b, nil
}
func rekeyEncode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
func (o RekeyOffer) valid(now int64) bool {
	for _, s := range []string{o.AccountID, o.MachineID, o.ViewerDeviceID, o.OperationID, o.FromKeyID} {
		if !rekeyString(s) {
			return false
		}
	}
	_, e := rekeyB64(o.ViewerEphemeralKey, 32)
	_, n := rekeyB64(o.Nonce, 32)
	return o.Type == "content_key_offer" && o.Version == 1 && o.FromEpoch > 0 && o.FromEpoch <= rekeySafeInteger && now >= 0 && o.ExpiresAtMS > now && o.ExpiresAtMS <= rekeySafeInteger && o.ExpiresAtMS-now <= RekeyLifetimeMSLimit && e == nil && n == nil
}

// SignRekeyOffer requires a fresh ephemeral private key and random nonce held
// only by the pending operation. It does not authorize the viewer.
func SignRekeyOffer(o RekeyOffer, viewer DeviceKey, now int64) ([]byte, error) {
	if !viewer.Valid() || !o.valid(now) {
		return nil, ErrRekey
	}
	o.Signature = rekeyEncode(viewer.Sign(rekeyBytes("offer", rekeyMap(o, "signature"))))
	return RekeyCanonical(o), nil
}
func VerifyRekeyOffer(raw []byte, a RekeyAuthority, now int64) (RekeyOffer, error) {
	var o RekeyOffer
	if rekeyParse(raw, &o) != nil || !o.valid(now) || o.AccountID != a.AccountID || o.MachineID != a.MachineID || o.ViewerDeviceID != a.ViewerDeviceID || len(a.ActiveViewerKey) != 32 {
		return o, ErrRekey
	}
	sig, e := rekeyB64(o.Signature, 64)
	if e != nil || !ed25519.Verify(a.ActiveViewerKey, rekeyBytes("offer", rekeyMap(o, "signature")), sig) {
		return o, ErrRekey
	}
	return o, nil
}
func rekeyKeyID(epoch uint64, key []byte) string {
	if epoch == 1 {
		return "ms-1"
	}
	h := sha256.Sum256(key)
	return fmt.Sprintf("mk-%d-%x", epoch, h)
}
func rekeyDerive(shared []byte, a RekeyAnswer) []byte {
	h, _ := rekeyB64(a.OfferHash, 32)
	x := hmac.New(sha256.New, h)
	x.Write(shared)
	prk := x.Sum(nil)
	x = hmac.New(sha256.New, prk)
	x.Write(rekeyBytes("kdf", rekeyMap(a, "signature", "ciphertext")))
	x.Write([]byte{1})
	return x.Sum(nil)
}
func rekeyGCM(shared []byte, a RekeyAnswer) (cipher.AEAD, error) {
	b, e := aes.NewCipher(rekeyDerive(shared, a))
	if e != nil {
		return nil, ErrRekey
	}
	return cipher.NewGCM(b)
}

// SealRekeyAnswer verifies authority again. The machine ephemeral key and
// sealing nonce MUST be fresh for each answer; callers use crypto/rand helpers.
// There is no old content key input. state must be the durable current state.
func SealRekeyAnswer(raw []byte, a RekeyAuthority, state ContentKeyState, machine DeviceKey, ephemeral, nonce []byte, now int64) ([]byte, error) {
	o, e := VerifyRekeyOffer(raw, a, now)
	if e != nil || !machine.Valid() || !state.Key.Valid() || state.Epoch < o.FromEpoch || state.Epoch > rekeySafeInteger || state.Epoch == 0 || state.KeyID != rekeyKeyID(state.Epoch, state.Key.Bytes()) || (state.Epoch == o.FromEpoch && state.KeyID != o.FromKeyID) || len(nonce) != 12 {
		return nil, ErrRekey
	}
	pub, e := X25519PublicKey(ephemeral)
	if e != nil {
		return nil, ErrRekey
	}
	peer, _ := rekeyB64(o.ViewerEphemeralKey, 32)
	shared, e := PairingSharedSecret(ephemeral, peer)
	if e != nil {
		return nil, ErrRekey
	}
	hash := sha256.Sum256(raw)
	ans := RekeyAnswer{Type: "content_key_answer", Version: 1, Offer: o, OfferHash: rekeyEncode(hash[:]), SenderDeviceID: a.MachineID, MachineEphemeralKey: rekeyEncode(pub), Epoch: state.Epoch, KeyID: state.KeyID, SealingNonce: rekeyEncode(nonce)}
	g, e := rekeyGCM(shared, ans)
	if e != nil {
		return nil, ErrRekey
	}
	ans.Ciphertext = rekeyEncode(g.Seal(nil, nonce, state.Key.Bytes(), rekeyBytes("aad", rekeyMap(ans, "signature", "ciphertext"))))
	ans.Signature = rekeyEncode(machine.Sign(rekeyBytes("answer", rekeyMap(ans, "signature"))))
	return RekeyCanonical(ans), nil
}

// OpenRekeyAnswer needs the exact locally pending offer, its private ephemeral,
// pairing-pinned machine key, and durable stored epoch/keyID. Consume the pending
// operation atomically with persistence; this stateless API is NOT a replay cache.
func OpenRekeyAnswer(raw, pending []byte, a RekeyAuthority, private []byte, machine ed25519.PublicKey, storedEpoch uint64, storedKeyID string, now int64) (ContentKeyState, error) {
	var ans RekeyAnswer
	o, e := VerifyRekeyOffer(pending, a, now)
	if e != nil || rekeyParse(raw, &ans) != nil || ans.Type != "content_key_answer" || ans.Version != 1 || !bytes.Equal(RekeyCanonical(ans.Offer), pending) || ans.SenderDeviceID != a.MachineID || ans.Epoch < o.FromEpoch || ans.Epoch < storedEpoch || ans.Epoch == 0 || ans.Epoch > rekeySafeInteger || (ans.Epoch == storedEpoch && ans.KeyID != storedKeyID) || (ans.Epoch == o.FromEpoch && ans.KeyID != o.FromKeyID) || len(machine) != 32 {
		return ContentKeyState{}, ErrRekey
	}
	hash := sha256.Sum256(pending)
	if ans.OfferHash != rekeyEncode(hash[:]) {
		return ContentKeyState{}, ErrRekey
	}
	sig, e := rekeyB64(ans.Signature, 64)
	if e != nil || !ed25519.Verify(machine, rekeyBytes("answer", rekeyMap(ans, "signature")), sig) {
		return ContentKeyState{}, ErrRekey
	}
	pub, e := X25519PublicKey(private)
	if e != nil || rekeyEncode(pub) != o.ViewerEphemeralKey {
		return ContentKeyState{}, ErrRekey
	}
	peer, e := rekeyB64(ans.MachineEphemeralKey, 32)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	shared, e := PairingSharedSecret(private, peer)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	nonce, e := rekeyB64(ans.SealingNonce, 12)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	ct, e := rekeyB64(ans.Ciphertext, 48)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	g, e := rekeyGCM(shared, ans)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	key, e := g.Open(nil, nonce, ct, rekeyBytes("aad", rekeyMap(ans, "signature", "ciphertext")))
	if e != nil || rekeyKeyID(ans.Epoch, key) != ans.KeyID {
		return ContentKeyState{}, ErrRekey
	}
	k, e := ContentKeyFromBytes(key)
	if e != nil {
		return ContentKeyState{}, ErrRekey
	}
	return ContentKeyState{Epoch: ans.Epoch, KeyID: ans.KeyID, Key: k}, nil
}
