package cloud

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// The browser twin (web/console/src/cloud/browser-invite.ts) reads the same
// file; CLAWDLINE_INVITE_VECTORS=record rewrites it from this code.
const inviteVectorFile = "testdata/browser-invite-v1.json"

type inviteVector struct {
	InviteID       string         `json:"invite_id"`
	Secret         string         `json:"secret"`
	MachineID      string         `json:"machine_id"`
	BrowserScalar  string         `json:"browser_scalar"`
	MachineScalar  string         `json:"machine_scalar"`
	BrowserMessage string         `json:"browser_message"`
	MachineMessage string         `json:"machine_message"`
	ISK            string         `json:"isk"`
	ConfirmKey     string         `json:"confirm_key"`
	HandoverKey    string         `json:"handover_key"`
	Code           string         `json:"code"`
	ConfirmNonce   string         `json:"confirm_nonce"`
	Confirm        InviteConfirm  `json:"confirm"`
	SealedConfirm  string         `json:"sealed_confirm"`
	HandoverNonce  string         `json:"handover_nonce"`
	Handover       InviteHandover `json:"handover"`
	SealedHandover string         `json:"sealed_handover"`
}

func computeInviteVector(t *testing.T, v inviteVector) inviteVector {
	t.Helper()
	prs, ci, sid := InviteCPaceInputs(v.InviteID, v.Secret)
	b, err := NewCPacePartyWithScalar(unhex(t, v.BrowserScalar), prs, ci, sid, InviteBrowserAD, true)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewCPacePartyWithScalar(unhex(t, v.MachineScalar), prs, ci, sid, InviteMachineAD, false)
	if err != nil {
		t.Fatal(err)
	}
	v.BrowserMessage, v.MachineMessage = hex.EncodeToString(b.Message()), hex.EncodeToString(m.Message())
	iskB, err := b.Finish(m.Message(), InviteMachineAD)
	if err != nil {
		t.Fatal(err)
	}
	iskM, err := m.Finish(unhex(t, v.BrowserMessage), InviteBrowserAD)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(iskB, iskM) {
		t.Fatal("browser and machine disagree on the ISK")
	}
	v.ISK = hex.EncodeToString(iskB)
	keys, err := DeriveInviteKeys(iskB, v.InviteID)
	if err != nil {
		t.Fatal(err)
	}
	v.ConfirmKey, v.HandoverKey, v.Code = hex.EncodeToString(keys.Confirm), hex.EncodeToString(keys.Handover), keys.Code
	if v.SealedConfirm, err = SealInviteConfirm(keys, v.Confirm, unhex(t, v.ConfirmNonce)); err != nil {
		t.Fatal(err)
	}
	v.Confirm.V, v.Confirm.Type = 1, inviteConfirmType
	if v.SealedHandover, err = SealInviteHandover(keys, v.Handover, unhex(t, v.HandoverNonce)); err != nil {
		t.Fatal(err)
	}
	v.Handover.V, v.Handover.Type = 1, inviteHandoverType
	return v
}

func TestBrowserInviteVectorsAreWhatThisCodeComputes(t *testing.T) {
	raw, err := os.ReadFile(inviteVectorFile)
	if err != nil {
		t.Fatal(err)
	}
	var want inviteVector
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	got := computeInviteVector(t, want)
	if os.Getenv("CLAWDLINE_INVITE_VECTORS") == "record" {
		out, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(inviteVectorFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("vectors drifted:\n got %s\nwant %s", g, w)
	}
	keys := InviteKeys{Confirm: unhex(t, want.ConfirmKey), Handover: unhex(t, want.HandoverKey)}
	c, err := OpenInviteConfirm(keys, want.SealedConfirm, want.InviteID, want.MachineID, want.Confirm.DeviceID, want.Confirm.PublicKey)
	if err != nil || c.Name != want.Confirm.Name {
		t.Fatalf("confirm round trip: %+v %v", c, err)
	}
	h, err := OpenInviteHandover(keys, want.SealedHandover, want.InviteID)
	if err != nil || h != want.Handover {
		t.Fatalf("handover round trip: %+v %v", h, err)
	}
}

func TestInviteCodesParseAsPeopleTypeThem(t *testing.T) {
	for typed, want := range map[string][2]string{
		"K7M4Q-9X2FTB":   {"K7M4Q", "9X2FTB"},
		"k7m4q 9x2ftb":   {"K7M4Q", "9X2FTB"},
		"K7M4Q9X2FTB":    {"K7M4Q", "9X2FTB"},
		"o1l2i-000000":   {"0112" + "1", "000000"},
		" K7M4Q-9X2FTB ": {"K7M4Q", "9X2FTB"},
	} {
		id, secret, err := ParseInviteCode(typed)
		if err != nil || id != want[0] || secret != want[1] {
			t.Errorf("ParseInviteCode(%q) = %q %q %v, want %q %q", typed, id, secret, err, want[0], want[1])
		}
	}
	for _, bad := range []string{"", "K7M4Q-9X2FT", "K7M4Q-9X2FTBB", "K7M4U-9X2FTB", "K7M4Q-9X2FT!"} {
		if _, _, err := ParseInviteCode(bad); !errors.Is(err, ErrInviteCode) {
			t.Errorf("ParseInviteCode(%q) err = %v, want ErrInviteCode", bad, err)
		}
	}
	secret, err := NewInviteSecret(rand.Reader)
	if err != nil || len(secret) != InviteSecretChars {
		t.Fatalf("NewInviteSecret = %q %v", secret, err)
	}
	if id, s, err := ParseInviteCode(FormatInviteCode("K7M4Q", secret)); err != nil || id != "K7M4Q" || s != secret {
		t.Fatalf("format/parse round trip: %q %q %v", id, s, err)
	}
}

func TestAWrongSecretGivesADifferentComparisonCode(t *testing.T) {
	run := func(secretB, secretM string) (InviteKeys, InviteKeys) {
		prsB, ciB, sidB := InviteCPaceInputs("K7M4Q", secretB)
		prsM, ciM, sidM := InviteCPaceInputs("K7M4Q", secretM)
		b, _ := NewCPaceParty(rand.Reader, prsB, ciB, sidB, InviteBrowserAD, true)
		m, _ := NewCPaceParty(rand.Reader, prsM, ciM, sidM, InviteMachineAD, false)
		ib, _ := b.Finish(m.Message(), InviteMachineAD)
		im, _ := m.Finish(b.Message(), InviteBrowserAD)
		kb, _ := DeriveInviteKeys(ib, "K7M4Q")
		km, _ := DeriveInviteKeys(im, "K7M4Q")
		return kb, km
	}
	if kb, km := run("9X2FTB", "9X2FTB"); kb.Code != km.Code || !bytes.Equal(kb.Handover, km.Handover) {
		t.Fatal("the same secret disagreed")
	}
	if kb, km := run("9X2FTB", "9X2FTC"); bytes.Equal(kb.Handover, km.Handover) {
		t.Fatal("different secrets agreed on a handover key")
	}
}

func TestAnInviteConfirmIsCheckedAgainstWhatCloudReported(t *testing.T) {
	keys := InviteKeys{Confirm: bytes.Repeat([]byte{1}, 32), Handover: bytes.Repeat([]byte{2}, 32)}
	pub := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	c := InviteConfirm{InviteID: "K7M4Q", MachineID: "mac_a", DeviceID: "dev_1", PublicKey: pub, Name: "Linux Chrome"}
	sealed, err := SealInviteConfirm(keys, c, make([]byte, NonceBytes))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenInviteConfirm(keys, sealed, "K7M4Q", "mac_a", "dev_1", pub); err != nil {
		t.Fatalf("matching confirm refused: %v", err)
	}
	if _, err := OpenInviteConfirm(keys, sealed, "K7M4Q", "mac_a", "dev_1", "BAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="); !errors.Is(err, ErrInviteMismatch) {
		t.Fatalf("substituted key err = %v, want ErrInviteMismatch", err)
	}
	if _, err := OpenInviteConfirm(keys, sealed, "K7M4R", "mac_a", "dev_1", pub); !errors.Is(err, ErrInviteSealed) {
		t.Fatalf("other invitation err = %v, want ErrInviteSealed", err)
	}
	wrong := InviteKeys{Confirm: bytes.Repeat([]byte{3}, 32)}
	if _, err := OpenInviteConfirm(wrong, sealed, "K7M4Q", "mac_a", "dev_1", pub); !errors.Is(err, ErrInviteSealed) {
		t.Fatalf("wrong key err = %v, want ErrInviteSealed", err)
	}
}

func TestInviteExpiryChoices(t *testing.T) {
	for choice, want := range map[string]int64{"8h": 28800, "7d": 604800, "never": 0} {
		if got, ok := InviteExpirySeconds(choice); !ok || got != want {
			t.Errorf("InviteExpirySeconds(%q) = %d %v", choice, got, ok)
		}
	}
	if _, ok := InviteExpirySeconds("30d"); ok {
		t.Error("an unknown expiry was accepted")
	}
}
