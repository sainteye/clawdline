package taskdir

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// limits N9: result.json was read whole, whatever its size. A file past the
// bound is refused, typed, and nothing of it is parsed; one at the bound is
// read as before.
func TestAResultPastTheBoundIsRefusedWhole(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "22222222-2222-4222-8222-222222222222"
	dir := r.Path(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid JSON all the way through, so only the size can refuse it.
	head := []byte(`{"clawdline_protocol":1,"status":"success","summary":"`)
	tail := []byte(`"}`)
	pad := bytes.Repeat([]byte("x"), resultLimit+1-len(head)-len(tail))
	big := append(append(append([]byte{}, head...), pad...), tail...)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, body, err := r.ReadResult(id); !errors.Is(err, ErrTooLarge) || body != nil {
		t.Fatalf("a result of %d bytes: %v (%d bytes back)", len(big), err, len(body))
	}
	if _, ok := r.Result(id); ok {
		t.Fatal("the older reader read a result past the bound")
	}

	fits := append(append(append([]byte{}, head...), pad[1:]...), tail...)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), fits, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _, err := r.ReadResult(id); err != nil || got.Status != "success" {
		t.Fatalf("a result at the bound: %v %+v", err, got.Status)
	}
}

// A note past its bound is not a note: progress.json and accepted.json are one
// sentence and one secret.
func TestANotePastTheBoundIsNotRead(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "33333333-3333-4333-8333-333333333333"
	dir := r.Path(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	big := append([]byte(`{"task_secret":"s","note":"`), bytes.Repeat([]byte("n"), noteLimit)...)
	big = append(big, `"}`...)
	for _, name := range []string{"progress.json", "accepted.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), big, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, ok := r.ReadProgress(id); ok {
		t.Error("a progress file past the bound was read")
	}
	if _, ok := r.ReadAccepted(id); ok {
		t.Error("an accepted file past the bound was read")
	}
}
