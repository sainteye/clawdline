package cloud

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/gtank/ristretto255"
)

type cpaceVectors struct {
	PrependLen []struct{ Input, Output string } `json:"prepend_len"`
	LVCat      struct {
		Inputs []string `json:"inputs"`
		Output string   `json:"output"`
	} `json:"lv_cat"`
	Generator struct {
		PRS, CI, SID    string
		GeneratorString string `json:"generator_string"`
		Hash, G         string
	} `json:"generator"`
	Exchange struct {
		ADa, Ya, YaElem, ADb, Yb, YbElem, K string
		TranscriptIR                        string `json:"transcript_ir"`
		ISKIR                               string `json:"isk_ir"`
	} `json:"exchange"`
	Invalid struct {
		S      string   `json:"s"`
		Points []string `json:"points"`
	} `json:"invalid"`
}

// The vector file spells a scalar ya and its element Ya with keys that differ
// only in case, which encoding/json would merge; read those two by hand.
func loadCPaceVectors(t *testing.T) cpaceVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/cpace-ristretto255-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v cpaceVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var exact struct {
		Exchange map[string]string `json:"exchange"`
	}
	if err := json.Unmarshal(raw, &exact); err != nil {
		t.Fatal(err)
	}
	e := exact.Exchange
	v.Exchange.ADa, v.Exchange.Ya, v.Exchange.YaElem = e["ada"], e["ya"], e["Ya"]
	v.Exchange.ADb, v.Exchange.Yb, v.Exchange.YbElem = e["adb"], e["yb"], e["Yb"]
	v.Exchange.K = e["K"]
	return v
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestCPaceStringUtilitiesMatchDraftVectors(t *testing.T) {
	v := loadCPaceVectors(t)
	for _, c := range v.PrependLen {
		if got := hex.EncodeToString(CPacePrependLen(unhex(t, c.Input))); got != c.Output {
			t.Errorf("prepend_len(%s) = %s, want %s", c.Input, got, c.Output)
		}
	}
	var parts [][]byte
	for _, in := range v.LVCat.Inputs {
		parts = append(parts, unhex(t, in))
	}
	if got := hex.EncodeToString(CPaceLVCat(parts...)); got != v.LVCat.Output {
		t.Errorf("lv_cat = %s, want %s", got, v.LVCat.Output)
	}
	// LEB128 past one byte: the draft's 128-byte case.
	long := make([]byte, 128)
	if got := CPacePrependLen(long)[:2]; !bytes.Equal(got, []byte{0x80, 0x01}) {
		t.Errorf("prepend_len(128 bytes) prefix = %x, want 8001", got)
	}
}

func TestCPaceGeneratorMatchesDraftVector(t *testing.T) {
	g := loadCPaceVectors(t).Generator
	prs, ci, sid := unhex(t, g.PRS), unhex(t, g.CI), unhex(t, g.SID)
	if got := hex.EncodeToString(CPaceGeneratorString(prs, ci, sid)); got != g.GeneratorString {
		t.Fatalf("generator_string = %s\nwant %s", got, g.GeneratorString)
	}
	if got := hex.EncodeToString(CPaceGenerator(prs, ci, sid)); got != g.G {
		t.Fatalf("g = %s, want %s", got, g.G)
	}
}

func TestCPaceExchangeMatchesDraftVectors(t *testing.T) {
	v := loadCPaceVectors(t)
	prs, ci, sid := unhex(t, v.Generator.PRS), unhex(t, v.Generator.CI), unhex(t, v.Generator.SID)
	x := v.Exchange
	a, err := NewCPacePartyWithScalar(unhex(t, x.Ya), prs, ci, sid, unhex(t, x.ADa), true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewCPacePartyWithScalar(unhex(t, x.Yb), prs, ci, sid, unhex(t, x.ADb), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(a.Message()); got != x.YaElem {
		t.Fatalf("Ya = %s, want %s", got, x.YaElem)
	}
	if got := hex.EncodeToString(b.Message()); got != x.YbElem {
		t.Fatalf("Yb = %s, want %s", got, x.YbElem)
	}
	if got := hex.EncodeToString(CPaceTranscriptIR(a.Message(), unhex(t, x.ADa), b.Message(), unhex(t, x.ADb))); got != x.TranscriptIR {
		t.Fatalf("transcript_ir = %s, want %s", got, x.TranscriptIR)
	}
	scalar, _ := ristretto255.NewScalar().SetCanonicalBytes(unhex(t, x.Ya))
	if got := hex.EncodeToString(CPaceScalarMultVfy(scalar, b.Message())); got != x.K {
		t.Fatalf("K = %s, want %s", got, x.K)
	}
	iskA, err := a.Finish(b.Message(), unhex(t, x.ADb))
	if err != nil {
		t.Fatal(err)
	}
	iskB, err := b.Finish(unhex(t, x.YaElem), unhex(t, x.ADa))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(iskA) != x.ISKIR || hex.EncodeToString(iskB) != x.ISKIR {
		t.Fatalf("ISK A=%x B=%x, want %s", iskA, iskB, x.ISKIR)
	}
}

func TestCPaceRefusesInvalidAndIdentityPeers(t *testing.T) {
	v := loadCPaceVectors(t)
	s, err := ristretto255.NewScalar().SetCanonicalBytes(unhex(t, v.Invalid.S))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range v.Invalid.Points {
		if got := CPaceScalarMultVfy(s, unhex(t, p)); got != nil {
			t.Errorf("scalar_mult_vfy(s, %s) = %x, want the identity refusal", p, got)
		}
		party, err := NewCPaceParty(rand.Reader, []byte("pw"), nil, []byte("sid"), nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := party.Finish(unhex(t, p), nil); !errors.Is(err, ErrCPaceInvalidPeer) {
			t.Errorf("Finish(%s) err = %v, want ErrCPaceInvalidPeer", p, err)
		}
	}
	party, _ := NewCPaceParty(rand.Reader, []byte("pw"), nil, []byte("sid"), nil, true)
	if _, err := party.Finish([]byte{1, 2, 3}, nil); !errors.Is(err, ErrCPaceInvalidPeer) {
		t.Errorf("short element err = %v, want ErrCPaceInvalidPeer", err)
	}
}

func TestCPaceWrongPasswordYieldsDifferentKeys(t *testing.T) {
	sid := []byte("invite-sid")
	a, _ := NewCPaceParty(rand.Reader, []byte("K7M4Q9"), nil, sid, []byte("browser"), true)
	b, _ := NewCPaceParty(rand.Reader, []byte("K7M4Q8"), nil, sid, []byte("machine"), false)
	ka, errA := a.Finish(b.Message(), []byte("machine"))
	kb, errB := b.Finish(a.Message(), []byte("browser"))
	if errA != nil || errB != nil {
		t.Fatalf("finish errors: %v %v", errA, errB)
	}
	if bytes.Equal(ka, kb) {
		t.Fatal("different passwords produced the same ISK")
	}
	c, _ := NewCPaceParty(rand.Reader, []byte("K7M4Q9"), nil, sid, []byte("browser"), true)
	d, _ := NewCPaceParty(rand.Reader, []byte("K7M4Q9"), nil, sid, []byte("machine"), false)
	kc, _ := c.Finish(d.Message(), []byte("machine"))
	kd, _ := d.Finish(c.Message(), []byte("browser"))
	if !bytes.Equal(kc, kd) {
		t.Fatal("the same password produced different ISKs")
	}
	if _, err := c.Finish(d.Message(), []byte("machine")); !errors.Is(err, ErrCPaceScalar) {
		t.Fatalf("a second Finish err = %v, want ErrCPaceScalar", err)
	}
}

func TestCPaceRefusesAZeroOrNonCanonicalScalar(t *testing.T) {
	if _, err := NewCPacePartyWithScalar(make([]byte, 32), []byte("pw"), nil, nil, nil, true); !errors.Is(err, ErrCPaceScalar) {
		t.Fatalf("zero scalar err = %v", err)
	}
	big := bytes.Repeat([]byte{0xff}, 32)
	if _, err := NewCPacePartyWithScalar(big, []byte("pw"), nil, nil, nil, true); !errors.Is(err, ErrCPaceScalar) {
		t.Fatalf("non-canonical scalar err = %v", err)
	}
}
