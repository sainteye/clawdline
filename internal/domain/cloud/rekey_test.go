package cloud

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

type rekeyVector struct {
	Offer          string `json:"offer"`
	Answer         string `json:"answer"`
	ViewerSeed     string `json:"viewer_seed"`
	MachineSeed    string `json:"machine_seed"`
	ViewerPrivate  string `json:"viewer_private"`
	MachinePrivate string `json:"machine_private"`
	ContentKey     string `json:"content_key"`
	SealingNonce   string `json:"sealing_nonce"`
}

func rekeySetup(t *testing.T) (rekeyVector, RekeyAuthority, DeviceKey, DeviceKey, []byte, []byte, ContentKeyState) {
	t.Helper()
	b, e := os.ReadFile("testdata/rekey-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var f rekeyVector
	if json.Unmarshal(b, &f) != nil {
		t.Fatal("fixture")
	}
	dec := func(s string, n int) []byte {
		b, e := rekeyB64(s, n)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	v, _ := DeviceKeyFromSeed(dec(f.ViewerSeed, 32))
	m, _ := DeviceKeyFromSeed(dec(f.MachineSeed, 32))
	k, _ := ContentKeyFromBytes(dec(f.ContentKey, 32))
	return f, RekeyAuthority{"account-test", "machine-test", "viewer-test", v.PublicKey()}, v, m, dec(f.ViewerPrivate, 32), dec(f.MachinePrivate, 32), ContentKeyState{2, rekeyKeyID(2, k.Bytes()), k}
}
func TestRekeyCrossLanguage(t *testing.T) {
	f, a, v, m, vp, mp, state := rekeySetup(t)
	now := int64(1700000000000)
	var o RekeyOffer
	if json.Unmarshal([]byte(f.Offer), &o) != nil {
		t.Fatal("offer")
	}
	offer, e := SignRekeyOffer(o, v, now)
	if e != nil || string(offer) != f.Offer {
		t.Fatalf("offer bytes differ: %v", e)
	}
	n, _ := rekeyB64(f.SealingNonce, 12)
	ans, e := SealRekeyAnswer(offer, a, state, m, mp, n, now)
	if e != nil || string(ans) != f.Answer {
		t.Fatalf("answer bytes differ: %v", e)
	}
	got, e := OpenRekeyAnswer([]byte(f.Answer), offer, a, vp, m.PublicKey(), 1, "ms-1", now)
	if e != nil || !bytes.Equal(got.Key.Bytes(), state.Key.Bytes()) {
		t.Fatalf("JS answer did not open: %v", e)
	}
}
func TestRekeyRejects(t *testing.T) {
	f, a, v, m, vp, mp, state := rekeySetup(t)
	now := int64(1700000000000)
	offer := []byte(f.Offer)
	answer := []byte(f.Answer)
	for _, field := range []string{"account_id", "machine_id", "viewer_device_id", "operation_id", "nonce", "viewer_ephemeral_key", "expires_at_ms", "signature", "type", "v", "from_epoch", "from_key_id"} {
		t.Run("offer/"+field, func(t *testing.T) {
			var o map[string]any
			_ = json.Unmarshal(offer, &o)
			switch field {
			case "expires_at_ms":
				o[field] = float64(now + 1)
			case "v", "from_epoch":
				o[field] = float64(2)
			default:
				o[field] = "tampered"
			}
			if _, e := VerifyRekeyOffer(RekeyCanonical(o), a, now); e == nil {
				t.Fatal("accepted modified offer")
			}
		})
	}
	for _, field := range []string{"sender_device_id", "offer_hash", "machine_ephemeral_key", "epoch", "key_id", "sealing_nonce", "ciphertext", "signature", "type", "v", "extra"} {
		t.Run("answer/"+field, func(t *testing.T) {
			var o map[string]any
			_ = json.Unmarshal(answer, &o)
			if field == "epoch" {
				o[field] = float64(1)
			} else if field == "v" {
				o[field] = float64(2)
			} else {
				o[field] = "tampered"
			}
			if _, e := OpenRekeyAnswer(RekeyCanonical(o), offer, a, vp, m.PublicKey(), 1, "ms-1", now); e == nil {
				t.Fatal("accepted modified answer")
			}
		})
	}
	a2 := a
	a2.ActiveViewerKey = nil
	if _, e := VerifyRekeyOffer(offer, a2, now); e == nil {
		t.Fatal("revoked pin accepted")
	}
	if _, e := OpenRekeyAnswer(answer, offer, a, mp, m.PublicKey(), 1, "ms-1", now); e == nil {
		t.Fatal("wrong recipient opened")
	}
	if _, e := OpenRekeyAnswer(answer, offer, a, vp, m.PublicKey(), 3, "newer", now); e == nil {
		t.Fatal("rollback")
	}
	if _, e := VerifyRekeyOffer(offer, a, now+120000); e == nil {
		t.Fatal("expired")
	}
	var o RekeyOffer
	_ = json.Unmarshal(offer, &o)
	o.ExpiresAtMS = now + RekeyLifetimeMSLimit + 1
	if _, e := SignRekeyOffer(o, v, now); e == nil {
		t.Fatal("TTL")
	}
	for _, bad := range []string{"{}", string(offer) + " ", `{"unknown":1}`, string(bytes.Repeat([]byte("x"), MaxRekeyBodyBytes+1))} {
		if _, e := VerifyRekeyOffer([]byte(bad), a, now); e == nil {
			t.Fatal("malformed")
		}
	}
	state.KeyID = "ms-2"
	n, _ := rekeyB64(f.SealingNonce, 12)
	if _, e := SealRekeyAnswer(offer, a, state, m, mp, n, now); e == nil {
		t.Fatal("fake key id")
	}
}
