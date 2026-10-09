package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// slowPane is a pane whose listing answers after delay — iTerm2's list Apple
// Event on a busy machine, which every pinned read used to wait behind.
type slowPane struct {
	*pane
	delay time.Duration
	asked atomic.Int32
}

func (p *slowPane) Inventory(ctx context.Context) (session.Inventory, error) {
	p.asked.Add(1)
	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
	}
	inv, err := p.pane.Inventory(ctx)
	inv.Provenance = "tmux"
	return inv, err
}

const slowListing = 400 * time.Millisecond

// slowPinnedServer is a daemon with one Claude pane behind a slow listing,
// the shared reading in front of it, and the pane's current generation.
func slowPinnedServer(t *testing.T) (*Server, *slowPane, string) {
	t.Helper()
	if swiftstore.ProcessStart(os.Getpid()).IsZero() {
		t.Skip("this platform has no kernel process-start reader")
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, PID: os.Getpid(), State: session.StateIdle}}
	s := paneServer(t, p)
	slow := &slowPane{pane: p, delay: slowListing}
	s.terminals = []ports.TerminalHost{slow}
	s.inventory = app.Inventory{Terminals: s.terminals, Screen: p}
	s.readings = app.NewInventoryReading(s.inventory.Read, 0)
	s.executionMachineID = "mac_a"
	ctx := context.Background()
	// The generation is learned from a reading of its own, so the shared one
	// holds nothing yet.
	inv := s.inventory.Read(ctx)
	items := inv.Assistants()
	if len(items) != 1 {
		t.Fatalf("assistant reading: %+v", inv)
	}
	gen := s.observeExecutions(ctx, inv, items, []swiftstore.Live{liveOf(items[0])})["%19"]
	if gen == "" {
		t.Fatal("no generation for the pane")
	}
	slow.asked.Store(0)
	return s, slow, gen
}

func pinned(method, target, gen string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("X-Clawdline-Target-Machine", "mac_a")
	req.Header.Set("X-Clawdline-Execution-Generation", gen)
	return req
}

// A pinned transcript read with a scan that finished a moment ago is answered
// without waiting for the slow listing. The same check made the way every one
// was before — against a scan of its own — waits the whole listing.
func TestAPinnedTranscriptReadDoesNotWaitForTheSlowListing(t *testing.T) {
	s, slow, gen := slowPinnedServer(t)
	ctx := context.Background()

	start := time.Now()
	if err := s.AdmitExecutionTarget(ctx, "mac_a", "%19", gen); err != nil {
		t.Fatal(err)
	}
	before := time.Since(start)

	scans := s.readings.Counts().Scans
	rec := httptest.NewRecorder()
	start = time.Now()
	s.transcriptRoute(rec, pinned(http.MethodGet, "/v1/transcript?session=%2519", gen))
	after := time.Since(start)
	t.Logf("pinned transcript read, listing %v slow: fresh check %v, after a scan 0 s ago %v", slowListing, before, after)
	if rec.Code != http.StatusOK {
		t.Fatalf("pinned read: %d %s", rec.Code, rec.Body.String())
	}
	if before < slowListing {
		t.Fatalf("the fresh check did not wait for the listing: %v", before)
	}
	if after >= slowListing/2 {
		t.Fatalf("the pinned read waited for the listing: %v", after)
	}
	if got := s.readings.Counts().Scans - scans; got != 0 {
		t.Fatalf("the pinned read took %d scans", got)
	}
	if got := slow.asked.Load(); got != 1 {
		t.Fatalf("the listing was asked %d times, want 1", got)
	}
}

// With no recent scan, the transcript route's two pin checks — before and
// after the page is read — are one scan between them, not two.
func TestTwoPinChecksInOneReadAreOneScan(t *testing.T) {
	s, slow, gen := slowPinnedServer(t)
	rec := httptest.NewRecorder()
	s.transcriptRoute(rec, pinned(http.MethodGet, "/v1/transcript?session=%2519", gen))
	if rec.Code != http.StatusOK {
		t.Fatalf("pinned read: %d %s", rec.Code, rec.Body.String())
	}
	if got := s.readings.Counts().Scans; got != 1 {
		t.Fatalf("one pinned read took %d scans, want 1", got)
	}
	if got := slow.asked.Load(); got != 1 {
		t.Fatalf("the listing was asked %d times, want 1", got)
	}
}

// A pinned write still waits for a scan taken for it even when a recent one is
// held, and the action then finds its target in that same scan instead of
// taking another.
func TestAPinnedWriteWaitsForOneFreshScan(t *testing.T) {
	s, slow, gen := slowPinnedServer(t)
	ctx := context.Background()
	s.readings.Fresh(ctx)
	scans := s.readings.Counts().Scans

	req := pinned(http.MethodPost, "/v1/sessions/%2519/send", gen)
	rec := httptest.NewRecorder()
	start := time.Now()
	if !s.admitPinnedSessionRequest(rec, req) {
		t.Fatalf("pinned write refused: %d %s", rec.Code, rec.Body.String())
	}
	if took := time.Since(start); took < slowListing {
		t.Fatalf("the write was admitted on a held reading in %v", took)
	}
	if _, err := s.actions().Find(req.Context(), "%19"); err != nil {
		t.Fatal(err)
	}
	if got := s.readings.Counts().Scans - scans; got != 1 {
		t.Fatalf("the pinned write took %d scans, want 1", got)
	}
	if got := slow.asked.Load(); got != 2 {
		t.Fatalf("the listing was asked %d times, want 2 (the held scan and the write's)", got)
	}
}

// A recent scan answers the check, not the question: a generation that is
// not the running one is refused exactly as before.
func TestARecentScanStillRefusesAChangedGeneration(t *testing.T) {
	s, _, _ := slowPinnedServer(t)
	s.readings.Fresh(context.Background())
	rec := httptest.NewRecorder()
	s.transcriptRoute(rec, pinned(http.MethodGet, "/v1/transcript?session=%2519", "not-this-one"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "execution_generation_changed" {
		t.Fatalf("a changed generation was read: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.transcriptRoute(rec, pinned(http.MethodGet, "/v1/transcript?session=%2520", "any"))
	if rec.Code != http.StatusNotFound || codeOf(t, rec) != "execution_target_missing" {
		t.Fatalf("a missing target was read: %d %s", rec.Code, rec.Body.String())
	}
}
