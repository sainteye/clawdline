package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func key(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func payload() []byte { return []byte("the archive") }

func manifestBytes(t *testing.T) []byte {
	t.Helper()
	sum := sha256.Sum256(payload())
	m := Manifest{Version: "v0.10.0", Commit: strings.Repeat("a", 40), CommittedAt: "2026-10-06T00:00:00Z",
		Channel: "stable", Artifacts: []Artifact{{OS: "linux", Arch: "amd64", Kind: KindDaemon,
			Name: "clawdline_v0.10.0_linux_amd64.tar.gz", URL: "https://example.invalid/a.tar.gz",
			Size: int64(len(payload())), SHA256: hex.EncodeToString(sum[:])}}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sigs(t *testing.T, s ...Signature) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestOnlyAManifestSignedByATrustedKeyIsOpened(t *testing.T) {
	pub, priv := key(t)
	_, otherPriv := key(t)
	m := manifestBytes(t)

	got, err := Open(m, sigs(t, Sign(m, priv)), []ed25519.PublicKey{pub})
	if err != nil || got.Version != "v0.10.0" {
		t.Fatalf("a good signature: %v %+v", err, got)
	}
	// A rotation: signed by an unknown key and a trusted one.
	if _, err := Open(m, sigs(t, Sign(m, otherPriv), Sign(m, priv)), []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("one trusted signature among several: %v", err)
	}
	cases := []struct {
		name, want string
		m, s       []byte
		keys       []ed25519.PublicKey
	}{
		{"no signature file", CodeManifestUnsigned, m, nil, []ed25519.PublicKey{pub}},
		{"empty list", CodeManifestUnsigned, m, []byte("[]"), []ed25519.PublicKey{pub}},
		{"untrusted key", CodeSignatureInvalid, m, sigs(t, Sign(m, otherPriv)), []ed25519.PublicKey{pub}},
		{"tampered manifest", CodeSignatureInvalid, bytes.Replace(m, []byte("v0.10.0"), []byte("v0.10.1"), 1), sigs(t, Sign(m, priv)), []ed25519.PublicKey{pub}},
		{"no trusted keys", CodeNoTrustedKey, m, sigs(t, Sign(m, priv)), nil},
		{"garbage signatures", CodeSignatureInvalid, m, []byte("{"), []ed25519.PublicKey{pub}},
	}
	for _, c := range cases {
		if _, err := Open(c.m, c.s, c.keys); code(err) != c.want {
			t.Errorf("%s: got %v, want %s", c.name, err, c.want)
		}
	}
}

func TestASignedManifestThatIsNotARealReleaseIsRefused(t *testing.T) {
	pub, priv := key(t)
	for name, mut := range map[string]func(*Manifest){
		"bad version":  func(m *Manifest) { m.Version = "0.10" },
		"short commit": func(m *Manifest) { m.Commit = "abc" },
		"no sha":       func(m *Manifest) { m.Artifacts[0].SHA256 = "" },
		"path in name": func(m *Manifest) { m.Artifacts[0].Name = "../x" },
		"unknown kind": func(m *Manifest) { m.Artifacts[0].Kind = "plugin" },
		"bad min":      func(m *Manifest) { m.MinVersion = "latest" },
		"zero size":    func(m *Manifest) { m.Artifacts[0].Size = 0 },
	} {
		var m Manifest
		_ = json.Unmarshal(manifestBytes(t), &m)
		mut(&m)
		b, _ := json.Marshal(m)
		if _, err := Open(b, sigs(t, Sign(b, priv)), []ed25519.PublicKey{pub}); err == nil {
			t.Errorf("%s: opened", name)
		}
	}
}

func TestAnArtifactMustBeExactlyWhatTheManifestSays(t *testing.T) {
	pub, priv := key(t)
	mb := manifestBytes(t)
	m, err := Open(mb, sigs(t, Sign(mb, priv)), []ed25519.PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Artifact("linux", "amd64", KindDaemon)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Artifact("darwin", "arm64", KindApp); code(err) != CodeNoArtifact {
		t.Fatalf("missing platform: %v", err)
	}
	var out bytes.Buffer
	if err := a.Check(bytes.NewReader(payload()), &out); err != nil || out.String() != string(payload()) {
		t.Fatalf("the right bytes: %v %q", err, out.String())
	}
	if err := a.Check(bytes.NewReader(payload()[:3]), nil); code(err) != CodeArtifactSize {
		t.Fatalf("truncated: %v", err)
	}
	if err := a.Check(bytes.NewReader(append(payload(), 'x')), nil); code(err) != CodeArtifactSize {
		t.Fatalf("longer: %v", err)
	}
	wrong := []byte("the archivX")
	if err := a.Check(bytes.NewReader(wrong), nil); code(err) != CodeArtifactSHA256 {
		t.Fatalf("same size, other bytes: %v", err)
	}
}

func TestVersionsCompareAsReleasesDo(t *testing.T) {
	order := []string{"v0.9.1", "v0.10.0-beta.1", "v0.10.0-beta.2", "v0.10.0-beta.10", "v0.10.0", "v0.10.1", "v1.0.0"}
	for i := range order {
		for j := range order {
			a, err := ParseVersion(order[i])
			if err != nil {
				t.Fatal(err)
			}
			b, _ := ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
		if v, _ := ParseVersion(order[i]); v.String() != order[i] {
			t.Errorf("round trip %s → %s", order[i], v)
		}
	}
	for _, bad := range []string{"", "0.10.0", "v0.10", "v01.2.3", "v1.2.3-", "devel+abc1234", "v1.2.x"} {
		if _, err := ParseVersion(bad); code(err) != CodeVersionUnparseable {
			t.Errorf("%q parsed", bad)
		}
	}
}

// A key that does not decode would be dropped by TrustedKeys without a word,
// and every release signed by it refused on every machine.
func TestEveryCompiledInKeyIsAnEd25519PublicKey(t *testing.T) {
	if len(releaseKeys) == 0 {
		t.Fatal("no release key is compiled in")
	}
	n := 0
	for _, k := range TrustedKeys() {
		if len(k) == ed25519.PublicKeySize {
			n++
		}
	}
	if extraKey == "" && n != len(releaseKeys) {
		t.Fatalf("%d of %d compiled-in keys decode", n, len(releaseKeys))
	}
}
