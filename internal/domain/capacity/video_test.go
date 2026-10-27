package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/documents"
)

// These are a wire and memory contract shared by two independent runtimes.
func TestVideoByteBoundsAgreeAcrossReaders(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/legacy/js/net/document-links.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		symbol, name string
		bytes        int64
	}{
		{"VIDEO_MAX_BYTES", "artifacts.video_bytes", documents.MaximumVideoBytes},
		{"VIDEO_CHUNK_BYTES", "artifacts.video_chunk_bytes", documents.MaximumVideoChunkBytes},
	} {
		match := regexp.MustCompile(row.symbol + ` = ([0-9]+);`).FindSubmatch(raw)
		if len(match) != 2 {
			t.Fatalf("missing browser bound %s", row.symbol)
		}
		value, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil || value != row.bytes {
			t.Fatalf("%s: browser=%d daemon=%d", row.name, value, row.bytes)
		}
		found := false
		for _, entry := range Register() {
			if entry.Name == row.name {
				found = true
				if entry.Limit != value {
					t.Fatalf("%s register=%d browser=%d", row.name, entry.Limit, value)
				}
			}
		}
		if !found {
			t.Fatalf("unregistered video bound %s", row.name)
		}
	}
}
