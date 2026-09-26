package http

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// limits N19: a stream that is not reading is owed the newest revision of each
// screen, not the first sixteen. It used to be a channel of sixteen whose
// seventeenth send went to `default` and nowhere, so the page stopped on a
// screen that had moved on and nothing counted it.
func TestAStreamThatFellBehindIsToldTheNewestRevision(t *testing.T) {
	bus := newScreenBus()
	_, sub := bus.subscribe()
	for i := 0; i < 100; i++ {
		bus.publish("%1", fmt.Sprintf("rev-%02d", i))
	}
	bus.publish("%2", "other")
	frames := bus.take(sub)
	if len(frames) != 2 || frames[0].ID != "%1" || frames[0].Revision != "rev-99" || frames[1].ID != "%2" {
		t.Fatalf("the stream was handed %+v; want %%1 at rev-99, then %%2", frames)
	}
	r := bus.reading()
	if r.Counters.Coalesced != 99 || r.Counters.Disconnected != 0 {
		t.Fatalf("reading %+v; want 99 coalesced", r.Counters)
	}
	if more := bus.take(sub); len(more) != 0 {
		t.Fatalf("taken twice: %+v", more)
	}
}

// Past the register's limit of different screens waiting, the stream is ended
// — counted, and the page reconnects and reads afresh — while a stream that
// keeps up is not touched.
func TestAStreamTooFarBehindIsEndedAndCounted(t *testing.T) {
	bus := newScreenBus()
	bus.limit = 3
	_, slow := bus.subscribe()
	_, quick := bus.subscribe()
	for i := 0; i < 4; i++ {
		bus.publish(fmt.Sprintf("%%%d", i), "r")
		if got := bus.take(quick); len(got) != 1 {
			t.Fatalf("the stream that keeps up was handed %+v", got)
		}
	}
	select {
	case <-slow.gone:
	default:
		t.Fatal("a stream with more screens waiting than the limit was not ended")
	}
	select {
	case <-quick.gone:
		t.Fatal("the stream that kept up was ended")
	default:
	}
	if r := bus.reading(); r.Counters.Disconnected != 1 || !strings.HasPrefix(r.Note, "1 stream") {
		t.Fatalf("reading %+v", r)
	}
}

// limits N15, N16: the register measures a picture store the moment it grows,
// not a tick later, so the warning comes before the write that lets the oldest
// go. The beat here ticks once an hour; only the nudge can have measured.
func TestAPictureStoredIsMeasuredAtOnce(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_TICK", "1h")
	s := capacityServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartCapacity(ctx)
	waitFor := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			d, _ := s.capacityDiagnostics()
			for _, e := range d.Entries {
				if e.Name == capacity.ArtifactsDrops && e.Used != nil && *e.Used == want {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("artifacts.drops never read %d", want)
	}
	waitFor(0)
	// artifacts.drops reads bytes: the three this picture holds.
	if _, err := s.pictures.drops.Store([]byte("png"), time.Now()); err != nil {
		t.Fatal(err)
	}
	waitFor(3)
}

// A picture let go to make room answers with the same code as one whose day
// was up, and a sentence that says which.
func TestAnEvictedPictureSaysItWasLetGo(t *testing.T) {
	s, own, _ := picturesFixture(t)
	s.pictures.store.Policy.MaxCount = 1
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	src := filepath.Join(t.TempDir(), "b.png")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	// A second picture into a store that keeps one: the fixture's is the oldest.
	if _, err := s.pictures.store.ImportPaths(context.Background(), []string{src}, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.imageRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/artifacts/images/"+own, nil))
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "artifact_expired") ||
		!strings.Contains(rec.Body.String(), "let go to make room") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
