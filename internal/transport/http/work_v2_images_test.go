package http

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestWorkV2ReferenceImageRouteReadsOnlyDurableBoardBytes(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Unix(1_790_000_000, 0)
	itemID := "32000000-0000-4000-8000-000000000001"
	imageID := "32000000-0000-4000-8000-000000000002"
	item := work.ItemV2{ID: itemID, ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature,
		Title: "Reference", Description: "See the screenshot", Phase: work.PhaseCreated,
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at,
		Cycle: 1, Version: 1}
	data := []byte("png bytes")
	if err := st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.AddImage(work.ImageV2{ID: imageID, WorkID: itemID, Title: "reference.png", MediaType: "image/png",
			Width: 2, Height: 1, CreatedBy: "local", CreatedAt: at}, data)
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, httptest.NewRequest(method, "/v1/work/v2/images/"+imageID, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" ||
			rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s: %d %v", method, rec.Code, rec.Header())
		}
		if method == http.MethodGet && rec.Body.String() != string(data) {
			t.Fatalf("GET body %q", rec.Body.String())
		}
		if method == http.MethodHead && rec.Body.Len() != 0 {
			t.Fatalf("HEAD returned %d bytes", rec.Body.Len())
		}
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, httptest.NewRequest(http.MethodGet,
		"/v1/work/v2/images/32000000-0000-4000-8000-000000000099", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing image: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWorkV2ReferenceImageWritesStayPersonOnlyAndDecodeRasterData(t *testing.T) {
	s := &Server{}
	machine := httptest.NewRequest(http.MethodPost,
		"/v1/work/v2/items/32000000-0000-4000-8000-000000000001/images",
		strings.NewReader(`{"expected_version":1,"title":"x","data_url":"data:image/png;base64,cG5n"}`))
	machine = machine.WithContext(context.WithValue(machine.Context(), accessKey{}, access{machine: true,
		verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Send)}}))
	rec := httptest.NewRecorder()
	s.workV2Route(rec, machine)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "session_cannot_create_item") {
		t.Fatalf("machine upload: %d %s", rec.Code, rec.Body.String())
	}

	person := httptest.NewRequest(http.MethodPost,
		"/v1/work/v2/items/32000000-0000-4000-8000-000000000001/images",
		strings.NewReader(`{"expected_version":1,"title":"x","data_url":"https://example.invalid/x.png"}`))
	person = person.WithContext(context.WithValue(person.Context(), accessKey{}, access{verdict: auth.Verdict{
		Allowed: true, Local: true, Caps: auth.NewCaps(auth.Send)}}))
	rec = httptest.NewRecorder()
	s.workV2Route(rec, person)
	if rec.Code != http.StatusUnsupportedMediaType || !strings.Contains(rec.Body.String(), "unsupported_image") {
		t.Fatalf("remote URL: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWorkV2ImageRouteAlsoReadsDirectTodoReferences(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Unix(1_790_000_000, 0)
	todo := work.DirectTodoV2{ID: "todo-with-image", SessionID: "session-a", Text: "inspect",
		CreatedBy: "local", CreatedAt: at, Version: 1}
	imageID := "32000000-0000-4000-8000-000000000010"
	data := []byte("todo png bytes")
	if err := st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.CreateDirectTodo(todo); err != nil {
			return err
		}
		return tx.AddDirectTodoImage(work.DirectTodoImageV2{ID: imageID, TodoID: todo.ID,
			Title: "todo.png", MediaType: "image/png", Width: 2, Height: 1, CreatedBy: "local", CreatedAt: at}, data)
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, httptest.NewRequest(http.MethodGet, "/v1/work/v2/images/"+imageID, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(data) {
		t.Fatalf("todo image: %d %q", rec.Code, rec.Body.String())
	}
}

// `?size=thumb` is what a Board card and a to-do row ask for: the stored
// picture drawn with its long edge at most 480 pixels, as JPEG. No query is
// the whole PNG, which the full-size viewer asks for; any other query is a
// 400 rather than a quiet full-size answer; and a deleted image is a 404 even
// when its thumbnail is still held.
func TestWorkV2ReferenceImageThumbnailIsSmallAndBounded(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Unix(1_790_000_000, 0)
	itemID := "32000000-0000-4000-8000-000000000021"
	item := work.ItemV2{ID: itemID, ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature,
		Title: "Reference", Description: "See the screenshot", Phase: work.PhaseCreated,
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at,
		Cycle: 1, Version: 1}
	imageID := "32000000-0000-4000-8000-000000000020"
	picture := image.NewNRGBA(image.Rect(0, 0, 1600, 873))
	for y := 0; y < 873; y++ {
		for x := 0; x < 1600; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var stored bytes.Buffer
	if err := png.Encode(&stored, picture); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.AddImage(work.ImageV2{ID: imageID, WorkID: itemID, Title: "reference.png", MediaType: "image/png",
			Width: 1600, Height: 873, CreatedBy: "local", CreatedAt: at}, stored.Bytes())
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, thumbs: artifacts.NewThumbnailCache(1 << 20)}
	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	rec := get("/v1/work/v2/images/" + imageID + "?size=thumb")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %v", rec.Code, rec.Header())
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || format != "jpeg" || cfg.Width != 480 || cfg.Height != 262 {
		t.Fatalf("the thumbnail is %s %dx%d: %v", format, cfg.Width, cfg.Height, err)
	}
	// Small enough to be a small answer on the Cloud reply channel, whose
	// large-answer threshold is 256 KiB of payload.
	if rec.Body.Len() > 192<<10 {
		t.Fatalf("a thumbnail of %d bytes", rec.Body.Len())
	}
	if held, entries, _, _ := s.thumbs.Reading(); entries != 1 || held != int64(rec.Body.Len()) {
		t.Fatalf("the cache holds %d bytes in %d entries", held, entries)
	}

	if full := get("/v1/work/v2/images/" + imageID); full.Code != http.StatusOK ||
		full.Header().Get("Content-Type") != "image/png" || !bytes.Equal(full.Body.Bytes(), stored.Bytes()) {
		t.Fatalf("full: %d %v", full.Code, full.Header())
	}
	for _, query := range []string{"?size=full", "?size=thumb&size=thumb", "?width=10", "?size="} {
		if bad := get("/v1/work/v2/images/" + imageID + query); bad.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", query, bad.Code, bad.Body.String())
		}
	}

	if err := st.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.DeleteImage(imageID)
	}); err != nil {
		t.Fatal(err)
	}
	if gone := get("/v1/work/v2/images/" + imageID + "?size=thumb"); gone.Code != http.StatusNotFound {
		t.Fatalf("a deleted image's thumbnail: %d", gone.Code)
	}
}
