package devices

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// A device file that is not exactly what Save writes is refused, and left as
// it was, byte for byte.
func TestLoadIsStrict(t *testing.T) {
	h := digest("x")
	dev := `{"id":"d1","name":"n","hash":"` + h + `","caps":["read"],"created":1,"approved":true,"local":false}`
	pw := `"hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","salt":"c2FsdHNhbHRzYWx0c2FsdA=="`
	for name, body := range map[string]string{
		"null":            `null`,
		"empty object":    `{}`,
		"array":           `[]`,
		"string":          `"x"`,
		"syntax":          `{not-json`,
		"no version":      `{"devices":[]}`,
		"version 2":       `{"version":2,"devices":[]}`,
		"version 1.0":     `{"version":1.0,"devices":[]}`,
		"devices null":    `{"version":1,"devices":null}`,
		"no devices":      `{"version":1}`,
		"unknown key":     `{"version":1,"devices":[],"write":true}`,
		"trailing":        `{"version":1,"devices":[]} {}`,
		"null device":     `{"version":1,"devices":[null]}`,
		"no id":           `{"version":1,"devices":[{"name":"n","hash":"` + h + `","caps":["read"],"created":1,"approved":true,"local":false}]}`,
		"empty id":        `{"version":1,"devices":[{"id":"","name":"n","hash":"` + h + `","caps":["read"],"created":1,"approved":true,"local":false}]}`,
		"no approved":     `{"version":1,"devices":[{"id":"d1","name":"n","hash":"` + h + `","caps":["read"],"created":1,"local":false}]}`,
		"unknown cap":     `{"version":1,"devices":[{"id":"d1","name":"n","hash":"` + h + `","caps":["root"],"created":1,"approved":true,"local":false}]}`,
		"no caps":         `{"version":1,"devices":[{"id":"d1","name":"n","hash":"` + h + `","caps":[],"created":1,"approved":true,"local":false}]}`,
		"negative time":   `{"version":1,"devices":[{"id":"d1","name":"n","hash":"` + h + `","caps":["read"],"created":-1,"approved":true,"local":false}]}`,
		"hash as number":  `{"version":1,"devices":[{"id":"d1","name":"n","hash":5,"caps":["read"],"created":1,"approved":true,"local":false}]}`,
		"long name":       `{"version":1,"devices":[{"id":"d1","name":"` + strings.Repeat("n", 300) + `","hash":"` + h + `","caps":["read"],"created":1,"approved":true,"local":false}]}`,
		"no iterations":   `{"version":1,"devices":[` + dev + `],"password":{` + pw + `}}`,
		"float iteration": `{"version":1,"devices":[` + dev + `],"password":{` + pw + `,"iterations":6e5}}`,
		"not base64":      `{"version":1,"devices":[],"password":{"hash":"!!","salt":"!!","iterations":600000}}`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, StoreFile)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := Open(dir)
		if err != nil {
			t.Fatalf("%s: open: %v", name, err)
		}
		if _, err := f.Load(); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: %v", name, err)
		}
		if after, _ := os.ReadFile(path); string(after) != body {
			t.Errorf("%s: the file changed", name)
		}
	}

	dir := t.TempDir()
	body := `{"version":1,"devices":[` + dev + `],"password":{` + pw + `,"iterations":600000}}`
	if err := os.WriteFile(filepath.Join(dir, StoreFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _ := Open(dir)
	state, err := f.Load()
	if err != nil || len(state.Devices) != 1 || state.Password == nil || state.Password.Iterations != 600000 {
		t.Fatalf("a sound file: %+v %v", state, err)
	}
}

// Save and Load agree: what this package writes, it reads.
func TestSaveThenLoad(t *testing.T) {
	f, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := auth.State{Devices: []auth.Device{{ID: "a", Name: "", Hash: digest("a"), Caps: auth.NewCaps(auth.Read), Approved: true}}}
	if err := f.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := f.Load()
	if err != nil || len(got.Devices) != 1 || got.Devices[0].Name != "?" {
		t.Fatalf("%+v %v", got, err)
	}
}

// A link in place of any of the four files stops Open, and whatever it points
// at is neither tightened nor changed.
func TestOpenRefusesALinkedFile(t *testing.T) {
	for _, name := range credentialFiles {
		root := t.TempDir()
		dir := filepath.Join(root, "state")
		outside := filepath.Join(root, "outside")
		if err := os.WriteFile(outside, []byte("SENTINEL\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../outside", filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir); !errors.Is(err, ErrNotRegular) {
			t.Errorf("%s as a link: %v", name, err)
		}
		info, _ := os.Stat(outside)
		data, _ := os.ReadFile(outside)
		if info.Mode().Perm() != 0o644 || string(data) != "SENTINEL\n" {
			t.Errorf("%s: the target became %v %q", name, info.Mode().Perm(), data)
		}
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, StoreFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrNotRegular) {
		t.Errorf("a directory named %s: %v", StoreFile, err)
	}
}

// A link put there after Open is refused where it is met.
func TestALinkMadeLaterIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	token := strings.Repeat("t", 43)
	if err := os.WriteFile(outside, []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range credentialFiles {
		if err := os.Symlink("../outside", filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	f.Audit("device.add", map[string]string{"device": "x"})
	if _, err := f.Load(); !errors.Is(err, ErrUnreadable) || !errors.Is(err, ErrNotRegular) {
		t.Errorf("load: %v", err)
	}
	if _, err := f.ReadLocalToken(); !errors.Is(err, ErrNotRegular) {
		t.Errorf("local token: %v", err)
	}
	if got, err := f.MachineToken(); err == nil || got != "" {
		t.Errorf("machine token through a link: %q %v", got, err)
	}
	data, _ := os.ReadFile(outside)
	info, _ := os.Stat(outside)
	if string(data) != token || info.Mode().Perm() != 0o644 {
		t.Fatalf("the target became %v %q", info.Mode().Perm(), data)
	}
}

// The audit keeps a bounded piece of any value, cut between characters.
func TestAuditClipsValues(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.Audit("password.fail", map[string]string{"device": strings.Repeat("é", 10_000)})
	data, err := os.ReadFile(filepath.Join(dir, AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 2*auditFieldLimit || !utf8.Valid(data) {
		t.Fatalf("an audit line of %d bytes (valid UTF-8: %v)", len(data), utf8.Valid(data))
	}
	if info, _ := os.Stat(filepath.Join(dir, AuditFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode %v", info.Mode().Perm())
	}
}
