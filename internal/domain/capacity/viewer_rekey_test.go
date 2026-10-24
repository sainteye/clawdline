package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerContentKeyIDBoundMatchesEnvelope(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/legacy/js/net/cloud-crypto.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`MAX_CONTENT_KEY_ID_BYTES = ([0-9]+)`).FindStringSubmatch(string(raw))
	if len(m) != 2 {
		t.Fatal("missing named browser key ID bound")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n != 128 {
		t.Fatalf("browser limit must be 128: %s", m[1])
	}
	if n != cloud.MaxKeyIDBytes {
		t.Fatalf("browser key ID limit %s, Go envelope limit %d; integrate the root's 128-byte compatibility prerequisite", m[1], cloud.MaxKeyIDBytes)
	}
}

func TestViewerRekeyBounds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/cloud/viewer-rekey.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		symbol, row string
		scale       int64
		unit        Unit
	}{
		{"VIEWER_REKEY_PENDING_LIMIT", "cloud.viewer_rekey_pending", 1, Rows},
		{"VIEWER_REKEY_COOLDOWN_MS", "cloud.viewer_rekey_cooldown", 1000, Seconds},
	} {
		m := regexp.MustCompile(c.symbol + ` = ([0-9_]+)`).FindStringSubmatch(string(raw))
		if len(m) != 2 {
			t.Fatalf("missing %s", c.symbol)
		}
		value, err := strconv.ParseInt(strings.ReplaceAll(m[1], "_", ""), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range Register() {
			if entry.Name == c.row {
				found = true
				if entry.Limit != value/c.scale || entry.Unit != c.unit {
					t.Errorf("%s differs from browser", c.row)
				}
			}
		}
		if !found {
			t.Errorf("unregistered browser bound %s", c.row)
		}
	}
}
