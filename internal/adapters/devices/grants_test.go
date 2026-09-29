package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/auth"
)

func grantsFixture(t *testing.T) (*Files, *Grants, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_790_000_000, 0)
	state := auth.State{Devices: []auth.Device{
		{ID: "dev-local", Name: "This machine", Hash: "h1", Caps: auth.NewCaps(auth.Read, auth.Send, auth.Admin),
			Created: now, Approved: true, Local: true},
		{ID: "dev-phone", Name: "phone", Hash: "h2", Caps: auth.NewCaps(auth.Read), Created: now, Approved: true},
	}}
	if err := f.Save(state); err != nil {
		t.Fatal(err)
	}
	return f, OpenGrants(f), dir
}

// Acceptance 5: a grant never changes the device file, and the device file
// still decodes with the decoder of commit 27dbb0f2 — an older daemon, after
// a downgrade or a restore, reads its devices as before.
//
// The decoder is an embedded copy (decodeStoreAt27dbb0f2 below), not the one
// this build uses, so a later change to decodeStore cannot make this pass by
// changing both sides.
func TestAGrantLeavesTheDeviceFileAnOlderDaemonReads(t *testing.T) {
	_, g, dir := grantsFixture(t)
	store := filepath.Join(dir, StoreFile)
	before, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Set("dev-phone", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.Granted("dev-phone"); !ok || err != nil {
		t.Fatalf("granted: %v %v", ok, err)
	}
	after, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the grant changed %s", StoreFile)
	}
	ids, err := decodeStoreAt27dbb0f2(after)
	if err != nil {
		t.Fatalf("the 27dbb0f2 decoder refuses the device file: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("the 27dbb0f2 decoder read %v", ids)
	}
	// And the road not taken, which this test exists to catch: the same
	// grant written as a capability in the device file locks the older
	// daemon out of every device.
	withCap := bytes.Replace(after, []byte(`"read"`), []byte(`"read","terminal"`), 1)
	if bytes.Equal(withCap, after) {
		t.Fatal("the device file has no read capability to extend")
	}
	if _, err := decodeStoreAt27dbb0f2(withCap); err == nil {
		t.Fatal("the 27dbb0f2 decoder read a terminal capability; this test cannot fail")
	}
	info, err := os.Lstat(filepath.Join(dir, GrantsFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("%s is %o", GrantsFile, perm)
	}
}

// The file this writes is the one it reads, a grant is kept at its first
// time, and taking one away or pruning revoked devices leaves the rest.
func TestGrantsRoundTripAndPrune(t *testing.T) {
	f, g, _ := grantsFixture(t)
	first := time.Unix(1_790_000_100, 0)
	if _, err := g.Set("dev-phone", true, first); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Set("dev-tablet", true, first); err != nil {
		t.Fatal(err)
	}
	row, err := g.Set("dev-phone", true, first.Add(time.Hour))
	if err != nil || !row.GrantedAt.Equal(first) {
		t.Fatalf("regranting moved the time: %v %v", row.GrantedAt, err)
	}
	again := OpenGrants(f)
	all, err := again.All()
	if err != nil || len(all) != 2 {
		t.Fatalf("read back %v %v", all, err)
	}
	if err := again.Keep(func(id string) bool { return id == "dev-phone" }); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Granted("dev-tablet"); ok {
		t.Fatal("a pruned device kept its grant")
	}
	if _, err := g.Set("dev-phone", false, first); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Granted("dev-phone"); ok {
		t.Fatal("a grant taken away is still there")
	}
}

// A file this did not write grants nobody, says why, and is not written over.
func TestAnUnreadableGrantsFileGrantsNobody(t *testing.T) {
	for name, body := range map[string]string{
		"not json":        `{"dev-phone":`,
		"an unknown key":  `{"dev-phone":{"granted_at":1,"admin":true}}`,
		"no time":         `{"dev-phone":{}}`,
		"a key not an id": `{"dev phone":{"granted_at":1}}`,
		"not an object":   `["dev-phone"]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, g, dir := grantsFixture(t)
			path := filepath.Join(dir, GrantsFile)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if ok, err := g.Granted("dev-phone"); ok || err == nil {
				t.Fatalf("granted %v, err %v", ok, err)
			}
			if _, err := g.Set("dev-other", true, time.Now()); err == nil {
				t.Fatal("a grant was written over a file that could not be read")
			}
			if got, _ := os.ReadFile(path); string(got) != body {
				t.Fatalf("the file became %s", got)
			}
		})
	}
	t.Run("a link", func(t *testing.T) {
		_, g, dir := grantsFixture(t)
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		_ = os.WriteFile(target, []byte(`{"dev-phone":{"granted_at":1}}`), 0o600)
		if err := os.Symlink(target, filepath.Join(dir, GrantsFile)); err != nil {
			t.Fatal(err)
		}
		if ok, _ := g.Granted("dev-phone"); ok {
			t.Fatal("a grants file that is a link was read")
		}
	})
}

// ---- The device-file decoder as of commit 27dbb0f2 ------------------------
//
// Every check decodeStore, deviceIn.device, passwordIn.password and
// auth.ParseCapability made at 27dbb0f2 (`git show 27dbb0f2:internal/adapters
// /devices/files.go`), restated here against private copies of its types and
// constants, with the result reduced to the ids: it depends on nothing that
// may change in this package. The decoding step that matters — one strict
// object with no unknown key and only the capabilities read, send and admin —
// is kept verbatim.

type fileIn27 struct {
	Version  *int          `json:"version"`
	Devices  *[]deviceIn27 `json:"devices"`
	Password *struct {
		Hash       *string `json:"hash"`
		Salt       *string `json:"salt"`
		Iterations *int    `json:"iterations"`
	} `json:"password"`
}

type deviceIn27 struct {
	ID       *string   `json:"id"`
	Name     *string   `json:"name"`
	Hash     *string   `json:"hash"`
	Caps     *[]string `json:"caps"`
	Created  *float64  `json:"created"`
	LastSeen *float64  `json:"last_seen"`
	Approved *bool     `json:"approved"`
	Local    *bool     `json:"local"`
}

func decodeStoreAt27dbb0f2(data []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file *fileIn27
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("something follows the object")
	}
	switch {
	case file == nil:
		return nil, errors.New("it is null")
	case file.Version == nil:
		return nil, errors.New("it has no version")
	case *file.Version != 1:
		return nil, fmt.Errorf("it is version %d, and this app reads version 1", *file.Version)
	case file.Devices == nil:
		return nil, errors.New("it has no devices array")
	}
	var ids []string
	for i, row := range *file.Devices {
		if row.ID == nil || row.Name == nil || row.Hash == nil || row.Caps == nil || row.Created == nil ||
			row.Approved == nil || row.Local == nil {
			return nil, fmt.Errorf("device %d: a key is missing", i)
		}
		if *row.ID == "" || len(*row.ID) > 128 || !validText(*row.ID) {
			return nil, fmt.Errorf("device %d: the id is not an id", i)
		}
		if len(*row.Name) > 256 {
			return nil, fmt.Errorf("device %d: the name is too long", i)
		}
		if len(*row.Caps) == 0 {
			return nil, fmt.Errorf("device %d: no capabilities", i)
		}
		for _, c := range *row.Caps {
			switch c {
			case "read", "send", "admin":
			default:
				return nil, fmt.Errorf("device %d: unknown capability %q", i, c)
			}
		}
		if *row.Created < 0 || row.LastSeen != nil && *row.LastSeen < 0 {
			return nil, fmt.Errorf("device %d: a time before 1970", i)
		}
		ids = append(ids, *row.ID)
	}
	if p := file.Password; p != nil && (p.Hash == nil || p.Salt == nil || p.Iterations == nil) {
		return nil, errors.New("password: it needs hash, salt and iterations")
	}
	return ids, nil
}
