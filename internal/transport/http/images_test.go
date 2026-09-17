package http

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/artifacts"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/contract"
)

func picturesFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	swiftDir := t.TempDir()
	s := &Server{pictures: pictures{
		store: artifacts.NewStore(t.TempDir()),
		swift: swiftstore.OpenImages(swiftDir),
	}}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	src := filepath.Join(t.TempDir(), "a.png")
	_ = os.WriteFile(src, buf.Bytes(), 0o600)
	stored, err := s.pictures.store.ImportPaths(context.Background(), []string{src}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	swiftID := "2bf6b711-ab17-4fd2-8a4a-d76d920ddd06"
	_ = os.WriteFile(filepath.Join(swiftDir, swiftID+".json"), []byte(`{"artifact":{"byteCount":3,"height":1,"expiresAt":4102444800,"id":"`+swiftID+`","mediaType":"image\/png","width":1},"createdAt":1}`), 0o600)
	_ = os.WriteFile(filepath.Join(swiftDir, swiftID+".png"), []byte("png"), 0o600)
	return s, stored[0].Artifact.ID, swiftID
}

func TestImageRouteReadsOwnStoreThenSwifts(t *testing.T) {
	s, own, swift := picturesFixture(t)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.imageRoute(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get("/v1/artifacts/images/" + own); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" ||
		w.Header().Get("Cache-Control") != "private, no-store" || !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("own: %d %v", w.Code, w.Header())
	}
	if w := get("/v1/artifacts/images/" + swift); w.Code != 200 || w.Body.String() != "png" {
		t.Fatalf("swift: %d %q", w.Code, w.Body.String())
	}
	for path, code := range map[string]int{
		"/v1/artifacts/images/0a6473d6-af38-49ea-8a7c-21607322bb3f": 404,
		"/v1/artifacts/images/":            404,
		"/v1/artifacts/images/a/b":         404,
		"/v1/artifacts/images/..%2f..%2fx": 404,
	} {
		if w := get(path); w.Code != code || !strings.Contains(w.Body.String(), "artifact_not_found") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	s.pictures.store.Delete([]string{own}, time.Now())
	if w := get("/v1/artifacts/images/" + own); w.Code != 410 || !strings.Contains(w.Body.String(), "artifact_expired") {
		t.Fatalf("deleted: %d %s", w.Code, w.Body.String())
	}
}

// Only the machine's own token stores pictures.
func TestImagesRouteRefusesADevice(t *testing.T) {
	s, _, _ := picturesFixture(t)
	w := httptest.NewRecorder()
	s.imagesRoute(w, httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", strings.NewReader(`{"images":[{"path":"/tmp/a.png"}]}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// A marker resolves to a picture, an expired one or an unknown one; an
// envelope's pictures are served as described.
func TestWireArtifacts(t *testing.T) {
	s, own, swift := picturesFixture(t)
	unknown := "0a6473d6-af38-49ea-8a7c-21607322bb3f"
	e := transcript.Entry{
		ArtifactIDs: []string{own, swift, unknown},
		Artifacts:   []transcript.ImageRef{{ID: unknown, MediaType: "image/png", ByteCount: 9, Width: 1, Height: 1, ExpiresAt: 5}},
	}
	got := s.pictures.wireArtifacts(e, time.Now())
	if len(got) != 4 || got[0].ID != unknown || got[0].ByteCount != 9 || got[0].State != "" {
		t.Fatalf("envelope first: %+v", got)
	}
	if got[1].ID != own || got[1].Width != 3 || got[1].State != "" || got[2].ID != swift || got[2].State != "" {
		t.Fatalf("live: %+v", got[1:3])
	}
	if got[3].State != contract.ImageAbsenceUnknown || got[3].ExpiresAt != 1 || got[3].ByteCount != 0 {
		t.Fatalf("unknown: %+v", got[3])
	}
	later := time.Now().Add(48 * time.Hour)
	if r := s.pictures.resolve(own, later); r.State != contract.ImageAbsenceExpired {
		t.Fatalf("expired: %+v", r)
	}
	if s.pictures.wireArtifacts(transcript.Entry{}, later) != nil {
		t.Fatal("an entry with no pictures carries none")
	}
}
