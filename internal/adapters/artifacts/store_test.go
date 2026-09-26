package artifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// pngClaiming is a PNG signature and an IHDR that claim a size, and nothing
// else: enough for the dimensions to be read, nowhere near enough to decode.
func pngClaiming(w, h uint32) []byte {
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0})
	var out bytes.Buffer
	out.Write([]byte("\x89PNG\r\n\x1a\n"))
	_ = binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()))
	chunk := append([]byte("IHDR"), ihdr.Bytes()...)
	out.Write(chunk)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

func TestDecodeDataURLTakesOnlyBase64DataURLs(t *testing.T) {
	if b, ok := DecodeDataURL("data:image/png;base64,aGVs\nbG8="); !ok || string(b) != "hello" {
		t.Fatalf("%q %v", b, ok)
	}
	for _, s := range []string{"", "file:///etc/passwd", "http://x/y.png", "data:image/png,hello", "data:image/png;base64", "data:;base64,a"} {
		if _, ok := DecodeDataURL(s); ok {
			t.Fatalf("%q was taken", s)
		}
	}
}

func TestNormalizeBoundsBeforeDecoding(t *testing.T) {
	_, err := Normalize(context.Background(), pngClaiming(20_000, 1), ProductionPolicy)
	if ref, ok := err.(Refusal); !ok || ref.Code != "image_too_large" || ref.Status != 413 {
		t.Fatalf("err %v", err)
	}
	_, err = Normalize(context.Background(), pngClaiming(8000, 8000), ProductionPolicy)
	if ref, ok := err.(Refusal); !ok || ref.Code != "image_too_large" {
		t.Fatalf("pixels: %v", err)
	}
	_, err = Normalize(context.Background(), []byte("not a picture at all"), ProductionPolicy)
	if ref, ok := err.(Refusal); !ok || ref.Code != "unsupported_image" || ref.Status != 415 {
		t.Fatalf("junk: %v", err)
	}
	gif, _ := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAP///wAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw==")
	n, err := Normalize(context.Background(), gif, ProductionPolicy)
	if err != nil || n.Width != 1 || n.Height != 1 || !bytes.HasPrefix(n.Data, []byte("\x89PNG")) {
		t.Fatalf("gif: %v %+v", err, n.Width)
	}
}

func TestStoreImportsLooksUpAndExpires(t *testing.T) {
	src := t.TempDir()
	s := NewStore(t.TempDir())
	now := time.Unix(1_800_000_000, 0)
	stored, err := s.ImportPaths(context.Background(), []string{writePNG(t, src, "a.png", 4, 3)}, now)
	if err != nil || len(stored) != 1 {
		t.Fatalf("%v %v", stored, err)
	}
	a := stored[0].Artifact
	if !IsID(a.ID) || a.Width != 4 || a.Height != 3 || a.ExpiresAt != now.Unix()+ProductionPolicy.TTLSeconds {
		t.Fatalf("%+v", a)
	}
	if Marker(a.ID) != `<clawdline-image id="`+a.ID+`">` {
		t.Fatal(Marker(a.ID))
	}
	for _, p := range []string{s.Dir, s.imagePath(a), s.metadataPath(a.ID)} {
		info, _ := os.Stat(p)
		want := os.FileMode(0o600)
		if p == s.Dir {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s is %v", p, info.Mode())
		}
	}
	// The record is the Swift app's format.
	raw, _ := os.ReadFile(s.metadataPath(a.ID))
	var m map[string]map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["artifact"]["mediaType"] != "image/png" || m["artifact"]["byteCount"] == nil {
		t.Fatalf("%s", raw)
	}

	if f := s.Lookup(a.ID, now); f.State != Live || len(f.Data) != a.ByteCount {
		t.Fatalf("live: %+v", f.State)
	}
	if f := s.Lookup("1a000000-0000-4000-8000-000000000001", now); f.State != Missing {
		t.Fatalf("unknown id: %v", f.State)
	}
	if f := s.Lookup("../../etc/passwd", now); f.State != Missing {
		t.Fatalf("not an id: %v", f.State)
	}
	later := now.Add(25 * time.Hour)
	if f := s.Liveness(a.ID, later); f.State != Expired {
		t.Fatalf("expired: %v", f.State)
	}
	// Expiry leaves a tombstone, so the id stays "expired" rather than "unknown".
	if _, err := os.Stat(s.imagePath(a)); !os.IsNotExist(err) {
		t.Fatalf("picture kept after expiry: %v", err)
	}
	if f := s.Lookup(a.ID, later); f.State != Expired {
		t.Fatalf("tombstone: %v", f.State)
	}
	// And the tombstone goes after its week.
	s.Delete(nil, later.Add(8*24*time.Hour))
	if f := s.Liveness(a.ID, later.Add(8*24*time.Hour)); f.State != Missing {
		t.Fatalf("reaped: %v", f.State)
	}
}

// A file that is not the size its record says is not that picture.
func TestStoreLookupRefusesATamperedFile(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Now()
	stored, err := s.ImportPaths(context.Background(), []string{writePNG(t, t.TempDir(), "a.png", 2, 2)}, now)
	if err != nil {
		t.Fatal(err)
	}
	id := stored[0].Artifact.ID
	if err := os.WriteFile(stored[0].File, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f := s.Lookup(id, now); f.State != Expired {
		t.Fatalf("%v", f.State)
	}
}

// Every refusal happens before anything is written: a bad second path leaves
// no first picture behind.
func TestStoreRefusesABatchWhole(t *testing.T) {
	src := t.TempDir()
	good := writePNG(t, src, "good.png", 2, 2)
	link := filepath.Join(src, "link.png")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(src, "junk.png")
	_ = os.WriteFile(junk, []byte("nope"), 0o600)
	s := NewStore(t.TempDir())
	cases := map[string][]string{
		"invalid_image_path": {good, "relative.png"},
		"unsupported_image":  {good, junk},
		"bad_request":        {good, good, good, good, good, good, good},
	}
	cases["invalid_image_path"] = []string{good, link}
	for code, paths := range cases {
		_, err := s.ImportPaths(context.Background(), paths, time.Now())
		if ref, ok := err.(Refusal); !ok || ref.Code != code {
			t.Fatalf("%s: %v", code, err)
		}
	}
	for _, p := range []string{"relative.png", src + "/../" + filepath.Base(src) + "/good.png", filepath.Join(src, "missing.png")} {
		_, err := s.ImportPaths(context.Background(), []string{p}, time.Now())
		if ref, ok := err.(Refusal); !ok || ref.Code != "invalid_image_path" {
			t.Fatalf("%s: %v", p, err)
		}
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 0 {
		t.Fatalf("left behind: %d", len(entries))
	}
}

func TestPathsFromIsClosed(t *testing.T) {
	for _, body := range []string{`[]`, `{}`, `[{"path":""}]`, `[{"path":"/a","x":1}]`, `[{"path":1}]`, `["/a"]`} {
		if _, err := PathsFrom(json.RawMessage(body), ProductionPolicy); err == nil {
			t.Fatalf("%s was taken", body)
		}
	}
	got, err := PathsFrom(json.RawMessage(`[{"path":"/a.png"},{"path":"/b.png"}]`), ProductionPolicy)
	if err != nil || strings.Join(got, ",") != "/a.png,/b.png" {
		t.Fatalf("%v %v", got, err)
	}
}

// The drop cache keeps the newest by name and removes only its own names.
func TestDropsPruneAndDiscardOnlyTheirOwn(t *testing.T) {
	d := &Drops{Dir: filepath.Join(t.TempDir(), "drops"), Keep: 2}
	start := time.Unix(1_800_000_000, 0)
	var paths []string
	for i := range 3 {
		p, err := d.Store([]byte{byte(i)}, start.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if !dropName.MatchString(filepath.Base(p)) {
			t.Fatalf("name %s", p)
		}
		paths = append(paths, p)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("the oldest survived the prune")
	}
	foreign := filepath.Join(d.Dir, "keep-me.png")
	_ = os.WriteFile(foreign, []byte("x"), 0o600)
	outside := writePNG(t, t.TempDir(), "clawdline-20260101-000000-000-1a000000-0000-4000-8000-000000000001.png", 1, 1)
	d.Discard([]string{paths[1], foreign, outside, d.Dir + "/../x"})
	if _, err := os.Stat(paths[1]); !os.IsNotExist(err) {
		t.Fatal("discard kept its own file")
	}
	for _, p := range []string{foreign, outside, paths[2]} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed", p)
		}
	}
}

// limits N15: a picture let go at the store's limit is counted against the
// limit that made it go, the store hears of every picture it keeps (which is
// how the register warns before the limit is reached), and the tombstone says
// why, so whoever asks for it later is told it was let go to make room.
func TestAPictureLetGoAtTheLimitIsCountedAndSaysWhy(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Policy.MaxCount = 3
	stored := 0
	s.OnStored(func() { stored++ })
	src := writePNG(t, t.TempDir(), "a.png", 2, 2)
	now := time.Now()
	var first string
	for i := 0; i < 4; i++ {
		got, err := s.ImportPaths(context.Background(), []string{src}, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got[0].Artifact.ID
		}
		count, _ := s.Readings(now)
		if want := int64(min(i+1, 3)); count.Used != want {
			t.Fatalf("after %d stores the row reads %d, want %d", i+1, count.Used, want)
		}
	}
	if stored != 4 {
		t.Fatalf("heard %d stores, want 4", stored)
	}
	count, bytes := s.Readings(now)
	if count.Counters.Evicted != 1 || bytes.Counters.Evicted != 0 || count.Counters.LastActionAt.IsZero() {
		t.Fatalf("count row %+v, bytes row %+v; want one eviction, by count", count.Counters, bytes.Counters)
	}
	if bytes.Used <= 0 {
		t.Fatalf("bytes row reads %d", bytes.Used)
	}
	found := s.Lookup(first, now.Add(5*time.Second))
	if found.State != Expired || !found.Evicted {
		t.Fatalf("the picture let go reads %+v; want expired and evicted", found)
	}
}

// limits N16: every picture the drop cache removes was typed into a prompt as
// a path, so each removal is counted and the row is measurable before it.
func TestDropsCountWhatTheyPrune(t *testing.T) {
	d := &Drops{Dir: filepath.Join(t.TempDir(), "drops"), Keep: 2}
	if r := d.Reading(); !r.Known || r.Used != 0 {
		t.Fatalf("an empty cache reads %+v", r)
	}
	heard := 0
	d.OnStored(func() { heard++ })
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := d.Store([]byte("png"), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	r := d.Reading()
	if r.Used != 2 || r.Counters.Evicted != 1 || heard != 3 {
		t.Fatalf("after three stores into two: %+v, heard %d", r, heard)
	}
}
