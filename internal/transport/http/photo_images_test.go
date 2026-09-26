package http

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A photograph is stored as the JPEG it is, and both picture routes answer it
// under image/jpeg: that header is the media type the Cloud line carries to
// the phone (cloudops.shapeImage), so a wrong one here is a wrong one there.
func TestAStoredPhotographIsServedAsAJPEG(t *testing.T) {
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 6, 4)), nil); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(src, photo.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir()), swift: swiftstore.OpenImages(t.TempDir())}}
	stored, err := s.pictures.store.ImportPaths(context.Background(), []string{src}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.imageRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/artifacts/images/"+stored[0].Artifact.ID, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!bytes.HasPrefix(rec.Body.Bytes(), []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatalf("session image: %d %v", rec.Code, rec.Header())
	}
	if ref := s.pictures.resolve(stored[0].Artifact.ID, time.Now()); ref.MediaType != "image/jpeg" {
		t.Fatalf("the reference a transcript carries says %q", ref.MediaType)
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Unix(1_790_000_000, 0)
	itemID, imageID := "33000000-0000-4000-8000-000000000001", "33000000-0000-4000-8000-000000000002"
	normalized, err := artifacts.Normalize(context.Background(), photo.Bytes(), artifacts.ProductionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(work.ItemV2{ID: itemID, ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature,
			Title: "Reference", Description: "See the photograph", Phase: work.PhaseCreated,
			DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at,
			Cycle: 1, Version: 1}, "local", `{}`); err != nil {
			return err
		}
		return tx.AddImage(work.ImageV2{ID: imageID, WorkID: itemID, Title: "photo.jpg", MediaType: normalized.MediaType,
			Width: normalized.Width, Height: normalized.Height, CreatedBy: "local", CreatedAt: at}, normalized.Data)
	}); err != nil {
		t.Fatal(err)
	}
	s = &Server{store: st}
	for _, path := range []string{"/v1/work/v2/images/" + imageID, "/v1/work/v2/images/" + imageID + "?size=thumb"} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" ||
			!bytes.HasPrefix(rec.Body.Bytes(), []byte{0xFF, 0xD8, 0xFF}) {
			t.Fatalf("%s: %d %v", path, rec.Code, rec.Header())
		}
	}
}
