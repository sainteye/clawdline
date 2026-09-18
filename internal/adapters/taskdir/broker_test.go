package taskdir

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stage leaves what a child whose shell died between validating and renaming
// leaves: result.json.tmp and the marker binding its bytes.
func stage(t *testing.T, r Root, id string, body []byte) {
	t.Helper()
	dir := r.Path(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json.tmp"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	marker := `{"clawdline_protocol":1,"task_id":"` + id + `","finalization_ready":true,"result_sha256":"` +
		hex.EncodeToString(sum[:]) + `"}`
	if err := os.WriteFile(filepath.Join(dir, "result.json.ready"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ⑥ Adoption creates result.json only where there is none. The first version
// renamed the validated tmp over the name, so a child that renamed its own
// result in the meantime had it replaced by the older copy (D16).
func TestAdoptionNeverOverwritesAResult(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "11111111-1111-4111-8111-111111111111"
	stale := []byte(`{"status":"failure","summary":"validated first"}`)
	stage(t, r, id, stale)
	_, body, ok := r.ReadReady(id)
	if !ok {
		t.Fatal("the marker was not believed")
	}
	// The child's own rename lands between the broker's look and its adoption.
	own := []byte(`{"status":"success","summary":"the child's own"}`)
	final := filepath.Join(r.Path(id), "result.json")
	if err := os.WriteFile(final, own, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AdoptReady(id, body); !errors.Is(err, ErrResultExists) {
		t.Fatalf("adopting over an existing result answered %v, want ErrResultExists", err)
	}
	if got, _ := os.ReadFile(final); string(got) != string(own) {
		t.Fatalf("result.json became %s", got)
	}
	if _, err := os.Stat(filepath.Join(r.Path(id), "result.json.adopting")); !os.IsNotExist(err) {
		t.Error("the staging file was left behind")
	}
}

// With nothing there, adoption publishes exactly the bytes the marker bound,
// and takes the marker away.
func TestAdoptionPublishesTheBoundBytes(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "22222222-2222-4222-8222-222222222222"
	want := []byte(`{"status":"success","summary":"validated"}`)
	stage(t, r, id, want)
	_, body, ok := r.ReadReady(id)
	if !ok {
		t.Fatal("the marker was not believed")
	}
	if err := r.AdoptReady(id, body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(r.Path(id), "result.json"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("result.json = %s (%v)", got, err)
	}
	for _, left := range []string{"result.json.ready", "result.json.tmp", "result.json.adopting"} {
		if _, err := os.Stat(filepath.Join(r.Path(id), left)); !os.IsNotExist(err) {
			t.Errorf("%s was left behind", left)
		}
	}
	info, _ := os.Stat(filepath.Join(r.Path(id), "result.json"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("result.json mode %v", info.Mode().Perm())
	}
}

// accepted.json is read only with a secret in it.
func TestAcceptedFileNeedsASecret(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "33333333-3333-4333-8333-333333333333"
	path := filepath.Join(r.Path(id), "accepted.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.ReadAccepted(id); ok {
		t.Fatal("no file read as a receipt")
	}
	_ = os.WriteFile(path, []byte(`{"note":"hi"}`), 0o600)
	if _, ok := r.ReadAccepted(id); ok {
		t.Fatal("a receipt with no secret was read")
	}
	_ = os.WriteFile(path, []byte(`{"task_secret":"s"}`), 0o600)
	if got, ok := r.ReadAccepted(id); !ok || got.Secret != "s" {
		t.Fatalf("got %+v %v", got, ok)
	}
}
