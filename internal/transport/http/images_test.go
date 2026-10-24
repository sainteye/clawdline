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

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
)

func picturesFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir())}}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	src := filepath.Join(t.TempDir(), "a.png")
	_ = os.WriteFile(src, buf.Bytes(), 0o600)
	stored, err := s.pictures.store.ImportPaths(context.Background(), []string{src}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s, stored[0].Artifact.ID
}

func TestImageRouteReadsOwnStore(t *testing.T) {
	s, own := picturesFixture(t)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.imageRoute(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get("/v1/artifacts/images/" + own); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" ||
		w.Header().Get("Cache-Control") != "private, no-store" || !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("own: %d %v", w.Code, w.Header())
	}
	for path, code := range map[string]int{
		"/v1/artifacts/images/1a000000-0000-4000-8000-000000000001": 404,
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

func TestPinnedImageCannotBypassSessionAdmission(t *testing.T) {
	s, own := picturesFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/artifacts/images/"+own+"?session=%2519", nil)
	request.Header.Set("X-Clawdline-Target-Machine", "machine-a")
	request.Header.Set("X-Clawdline-Execution-Generation", strings.Repeat("a", 32))
	recorder := httptest.NewRecorder()
	s.imageRoute(recorder, request)
	if recorder.Code == http.StatusOK || bytes.HasPrefix(recorder.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("unverified Session received image bytes: %d %s", recorder.Code, recorder.Body.String())
	}
	request.URL.RawQuery = ""
	recorder = httptest.NewRecorder()
	s.imageRoute(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "execution_target_required") {
		t.Fatalf("pinned image without Session: %d %s", recorder.Code, recorder.Body.String())
	}
}

// Only the machine's own token stores pictures.
func TestImagesRouteRefusesADevice(t *testing.T) {
	s, _ := picturesFixture(t)
	w := httptest.NewRecorder()
	s.imagesRoute(w, httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", strings.NewReader(`{"images":[{"path":"/tmp/a.png"}]}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// A marker resolves to a picture, an expired one or an unknown one; an
// envelope's pictures are served as described.
func TestWireArtifacts(t *testing.T) {
	s, own := picturesFixture(t)
	unknown := "1a000000-0000-4000-8000-000000000001"
	e := transcript.Entry{
		ArtifactIDs: []string{own, unknown},
		Artifacts:   []transcript.ImageRef{{ID: unknown, MediaType: "image/png", ByteCount: 9, Width: 1, Height: 1, ExpiresAt: 5}},
	}
	got := s.pictures.wireArtifacts(e, time.Now())
	if len(got) != 3 || got[0].ID != unknown || got[0].ByteCount != 9 || got[0].State != "" {
		t.Fatalf("envelope first: %+v", got)
	}
	if got[1].ID != own || got[1].Width != 3 || got[1].State != "" {
		t.Fatalf("live: %+v", got[1])
	}
	if got[2].State != contract.ImageAbsenceUnknown || got[2].ExpiresAt != 1 || got[2].ByteCount != 0 {
		t.Fatalf("unknown: %+v", got[2])
	}
	later := time.Now().Add(48 * time.Hour)
	if r := s.pictures.resolve(own, later); r.State != contract.ImageAbsenceExpired {
		t.Fatalf("expired: %+v", r)
	}
	if s.pictures.wireArtifacts(transcript.Entry{}, later) != nil {
		t.Fatal("an entry with no pictures carries none")
	}
}

// The retired Swift app's picture cache is not read: an id only it holds is
// unknown to both picture routes, wherever that cache is.
func TestTheRetiredAppsPicturesAreNotRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_SESSION_IMAGE_DIR", dir)
	id := "1a000000-0000-4000-8000-000000000004"
	_ = os.WriteFile(filepath.Join(dir, id+".json"), []byte(`{"artifact":{"byteCount":3,"height":1,"expiresAt":4102444800,"id":"`+id+`","mediaType":"image\/png","width":1},"createdAt":1}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, id+".png"), []byte("png"), 0o600)
	s := &Server{pictures: newPictures(t.TempDir())}
	w := httptest.NewRecorder()
	s.imageRoute(w, httptest.NewRequest(http.MethodGet, "/v1/artifacts/images/"+id, nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "artifact_not_found") {
		t.Fatalf("route: %d %s", w.Code, w.Body.String())
	}
	if r := s.pictures.resolve(id, time.Now()); r.State != contract.ImageAbsenceUnknown {
		t.Fatalf("reference: %+v", r)
	}
}

func TestTranscriptImageThumbnailKeepsTheOriginalAndExpiresWithIt(t *testing.T) {
	s, id := picturesFixture(t)
	get := func(query string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		s.imageRoute(out, httptest.NewRequest(http.MethodGet, "/v1/artifacts/images/"+id+query, nil))
		return out
	}
	original := get("")
	thumb := get("?size=thumb")
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if thumb.Code != http.StatusOK || thumb.Header().Get("Content-Type") != "image/jpeg" || err != nil || format != "jpeg" || cfg.Width != 3 || cfg.Height != 2 {
		t.Fatalf("thumbnail: status=%d config=%+v format=%s err=%v", thumb.Code, cfg, format, err)
	}
	if again := get(""); !bytes.Equal(again.Body.Bytes(), original.Body.Bytes()) {
		t.Fatal("thumbnail changed the original")
	}
	for _, query := range []string{"?size=", "?size=full", "?size=thumb&size=thumb"} {
		if out := get(query); out.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %s: %d", query, out.Code)
		}
	}
	s.pictures.store.Delete([]string{id}, time.Now())
	if out := get("?size=thumb"); out.Code != http.StatusGone {
		t.Fatalf("deleted thumbnail: %d", out.Code)
	}
}
