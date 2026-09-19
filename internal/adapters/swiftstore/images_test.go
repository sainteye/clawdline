package swiftstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Swift app's picture store is read and never written: an expired record
// is answered as expired and left exactly as it was, with its picture.
func TestSwiftImagesAreReadOnly(t *testing.T) {
	dir := t.TempDir()
	live := "1a000000-0000-4000-8000-000000000002"
	gone := "1a000000-0000-4000-8000-000000000003"
	stale := "1a000000-0000-4000-8000-000000000004"
	bad := "1a000000-0000-4000-8000-000000000005"
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Spelled as Swift's JSONEncoder writes them, escaped slash and all.
	write(live+".json", `{"artifact":{"byteCount":5,"height":358,"expiresAt":1789698504,"id":"`+live+`","mediaType":"image\/png","width":1192},"createdAt":1789612104.8378658}`)
	write(live+".png", "12345")
	write(gone+".json", `{"deletedAt":1789189040.154844,"createdAt":1789100175.053155,"artifact":{"width":1128,"byteCount":5,"mediaType":"image\/png","height":1047,"expiresAt":1789186575,"id":"`+gone+`"}}`)
	write(stale+".json", `{"artifact":{"byteCount":5,"height":1,"expiresAt":1700000000,"id":"`+stale+`","mediaType":"image\/png","width":1},"createdAt":1699999000}`)
	write(stale+".png", "12345")
	write(bad+".json", `{"artifact":{"byteCount":5,"height":1,"expiresAt":1789698504,"id":"`+bad+`","mediaType":"image\/jpeg","width":1},"createdAt":1}`)
	before := snapshot(t, dir)

	s := OpenImages(dir)
	now := time.Unix(1789612200, 0)
	if rec, data, state := s.Lookup(live, now); state != ImageLive || string(data) != "12345" || rec.Width != 1192 {
		t.Fatalf("live: %v %q %+v", state, data, rec)
	}
	if _, state := s.Liveness(gone, now); state != ImageExpired {
		t.Fatalf("tombstone: %v", state)
	}
	if _, _, state := s.Lookup(stale, now); state != ImageExpired {
		t.Fatalf("stale: %v", state)
	}
	for _, id := range []string{bad, "1a000000-0000-4000-8000-000000000001", "../secrets", "1A000000-0000-4000-8000-000000000002"} {
		if _, state := s.Liveness(id, now); state != ImageMissing {
			t.Fatalf("%s: %v", id, state)
		}
	}
	// A picture that is not the size its record says is not that picture.
	write(live+".png", "1234")
	if _, _, state := s.Lookup(live, now); state != ImageExpired {
		t.Fatalf("short: %v", state)
	}
	write(live+".png", "12345")
	if after := snapshot(t, dir); after != before {
		t.Fatalf("the store changed:\n%s\n%s", before, after)
	}
	if _, state := OpenImages(filepath.Join(dir, "absent")).Liveness(live, now); state != ImageMissing {
		t.Fatalf("absent dir: %v", state)
	}
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		out += e.Name() + "=" + string(data) + "\n"
	}
	return out
}
