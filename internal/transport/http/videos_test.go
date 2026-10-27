package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVideoPublicationNeedsExplicitRangeAndReturnsOnlyChunkJSON(t *testing.T) {
	root := t.TempDir()
	data := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'}, make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(root, "clip.mp4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		query  string
		status int
	}{{"", 400}, {"?offset=0&revision=", 200}, {"?offset=0&offset=0&revision=", 400}, {"?offset=0&revision=&extra=1", 400}, {"?offset=1&revision=", 416}, {"?offset=0&revision=stale", 409}} {
		r := httptest.NewRequest("GET", "/v1/sessions/s/documents/project/clip.mp4"+row.query, nil)
		w := httptest.NewRecorder()
		(&Server{}).servePublishedFile(w, r, root, "project", "", "clip.mp4")
		if w.Code != row.status {
			t.Fatalf("%s: %d %s", row.query, w.Code, w.Body)
		}
		if row.status == 200 {
			var body map[string]any
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["path"] != "clip.mp4" || body["media_type"] != "video/mp4" || body["byte_count"] != float64(len(data)) {
				t.Fatal(w.Body)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal(w.Header())
			}
		}
	}
}
