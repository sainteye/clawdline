package projectassistantstore

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAPictureIsKeptByDigestAndReadBackOnlyUnchanged(t *testing.T) {
	store := Attachments{Dir: t.TempDir()}
	key := pa.BotKey{MachineID: "machine", BotID: "bot"}
	data := tinyPNG(t)
	a, err := store.Put(key, "image/png", data)
	if err != nil || len(a.ID) != 64 || a.Bytes != int64(len(data)) {
		t.Fatalf("put: %+v %v", a, err)
	}
	again, err := store.Put(key, "image/png", data)
	if err != nil || again != a {
		t.Fatalf("the same picture was not stored once: %+v %v", again, err)
	}
	got, err := store.Read(key, a)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("read back", err)
	}
	if got, mediaType, err := store.ReadByID(key, a.ID); err != nil || mediaType != "image/png" || !bytes.Equal(got, data) {
		t.Fatalf("read by id: %s %v", mediaType, err)
	}
	if _, err := store.Read(pa.BotKey{MachineID: "machine", BotID: "other"}, a); pa.Code(err) != pa.NotFound {
		t.Fatalf("another Bot read the picture: %v", err)
	}
	if _, _, err := store.ReadByID(key, "../"+a.ID); pa.Code(err) != pa.Invalid {
		t.Fatalf("a path-like id was accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.botDir(key), a.ID), append(data, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(key, a); pa.Code(err) != pa.Invalid {
		t.Fatalf("a changed file was read: %v", err)
	}
	if _, err := store.Put(key, "text/plain", data); pa.Code(err) != pa.Invalid {
		t.Fatalf("a non-picture type was stored: %v", err)
	}
}
