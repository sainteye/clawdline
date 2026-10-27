package documents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func videoFixture(t *testing.T, root, name string, size int64) string {
	t.Helper()
	path := filepath.Join(root, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	// ISO BMFF ftyp, with a supported MP4 compatible brand.
	if _, err := f.WriteAt([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'}, 0); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestVideoChunksAreBoundedAndVersioned(t *testing.T) {
	root := t.TempDir()
	path := videoFixture(t, root, "clip.mp4", MaximumVideoChunkBytes+17)
	first, err := ReadVideo(context.Background(), root, "clip.mp4", 0, "")
	if err != nil || len(first.Data) != MaximumVideoChunkBytes || first.TotalBytes != MaximumVideoChunkBytes+17 || first.Revision == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := ReadVideo(context.Background(), root, "clip.mp4", MaximumVideoChunkBytes, first.Revision)
	if err != nil || !bytes.Equal(second.Data, make([]byte, 17)) {
		t.Fatalf("last: %+v %v", second, err)
	}
	// Ordinary text readers still refuse the same publication.
	if _, _, _, err := Read(root, "clip.mp4"); err != NotFound {
		t.Fatal(err)
	}
	changed := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVideo(context.Background(), root, "clip.mp4", MaximumVideoChunkBytes, first.Revision); err != VideoChanged {
		t.Fatalf("changed: %v", err)
	}
}
func TestVideoRefusesEscapeWrongTypeOversizeAndBadRanges(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	videoFixture(t, root, "clip.mp4", MaximumVideoChunkBytes+17)
	external := videoFixture(t, outside, "secret.mp4", 100)
	if err := os.Symlink(external, filepath.Join(root, "link.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fake.mp4"), []byte("<script>alert(1)</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	videoFixture(t, root, "large.mp4", MaximumVideoBytes+1)
	for _, row := range []struct {
		name     string
		offset   int64
		revision string
		want     error
	}{
		{"../secret.mp4", 0, "", NotFound}, {"link.mp4", 0, "", NotFound}, {".hidden.mp4", 0, "", NotFound},
		{"clip.mp4", -1, "", VideoRange}, {"clip.mp4", 1, "", VideoRange}, {"clip.mp4", MaximumVideoChunkBytes, "", VideoRange},
		{"clip.mp4", 2 * MaximumVideoChunkBytes, "stale", VideoRange}, {"fake.mp4", 0, "", VideoInvalid}, {"large.mp4", 0, "", TooLarge},
	} {
		t.Run(row.name+"/"+row.revision, func(t *testing.T) {
			if _, err := ReadVideo(context.Background(), root, row.name, row.offset, row.revision); err != row.want {
				t.Fatalf("got %v want %v", err, row.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadVideo(ctx, root, "clip.mp4", 0, ""); err != context.Canceled {
		t.Fatal(err)
	}
	rows, _ := Walk(context.Background(), root)
	for _, row := range rows {
		if row.Path == "large.mp4" || row.Path == "link.mp4" {
			t.Fatal(row)
		}
	}
}
