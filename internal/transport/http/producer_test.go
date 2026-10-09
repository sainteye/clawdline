package http

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// openStream reads one `/v1/events` connection and reports each `sessions`
// frame's line on the channel.
func openStream(t *testing.T, url string) <-chan string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	res, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan string, 64)
	go func() {
		defer res.Body.Close()
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 0, 1<<16), 1<<22)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "sessions":
				select {
				case frames <- strings.TrimPrefix(line, "data: "):
				default:
				}
			}
		}
	}()
	return frames
}

func streamServer(t *testing.T, p *pane) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("CLAWDLINE_NEXT_STREAM", "250ms")
	s := paneServer(t, p)
	s.screenBus = newScreenBus()
	ts := httptest.NewServer(http.HandlerFunc(s.ownEvents))
	t.Cleanup(ts.Close)
	return s, ts
}

// Three tabs used to be three builds of the machine every tick, and the Cloud
// publisher a fourth.
func TestEveryStreamReadsOneBuildPerTick(t *testing.T) {
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, State: session.StateIdle}}
	s, ts := streamServer(t, p)
	const tabs = 3
	streams := make([]<-chan string, tabs)
	for i := range streams {
		streams[i] = openStream(t, ts.URL)
	}
	for i, frames := range streams {
		select {
		case <-frames:
		case <-time.After(5 * time.Second):
			t.Fatalf("stream %d sent no first frame", i)
		}
	}
	before := s.lists().sessions.builds.Load()
	time.Sleep(1250 * time.Millisecond) // five ticks
	// The route and the publisher are answered from the same product.
	rec := httptest.NewRecorder()
	s.sessions(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))
	if _, ok := s.CloudSessions(context.Background()); !ok {
		t.Fatal("the publisher's read was refused")
	}
	built := s.lists().sessions.builds.Load() - before
	if built < 3 || built > 7 {
		t.Errorf("%d builds in five ticks for %d streams, a route read and a publisher read; want one a tick", built, tabs)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"%19"`) {
		t.Errorf("the route did not answer the shared list: %d %s", rec.Code, rec.Body.String())
	}
}

// A working session's clock moves every second; a stream does not send the
// list again for that alone, and does when the line says something new.
func TestAStreamDoesNotSendTheClockAlone(t *testing.T) {
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, State: session.StateWorking,
		Line: "Thinking… (1m 12s · ↑ 3k tokens)"}}
	_, ts := streamServer(t, p)
	frames := openStream(t, ts.URL)
	select {
	case first := <-frames:
		if !strings.Contains(first, `"working_since"`) {
			t.Errorf("the working row has no working_since: %s", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no first frame")
	}
	p.mu.Lock()
	p.s.Line = "Thinking… (1m 14s · ↑ 3k tokens)"
	p.mu.Unlock()
	select {
	case frame := <-frames:
		t.Fatalf("the clock alone was sent: %s", frame)
	case <-time.After(1 * time.Second):
	}
	p.mu.Lock()
	p.s.Line = "Thinking… (1m 16s · ↑ 8k tokens)"
	p.mu.Unlock()
	select {
	case frame := <-frames:
		if !strings.Contains(frame, "8k tokens") {
			t.Errorf("the frame is not the new line: %s", frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a line that said something new was not sent")
	}
}

func TestWorkingSinceIsSteadyWithinATurnAndMovesWithANewOne(t *testing.T) {
	var clock workingClock
	at := time.Unix(10_000, 0)
	first := clock.since("%1", "Thinking… (1m 0s)", at)
	if first != 10_000-60 {
		t.Fatalf("since = %d", first)
	}
	// One second of reading jitter is the same turn.
	if got := clock.since("%1", "Thinking… (1m 3s)", at.Add(4*time.Second)); got != first {
		t.Errorf("one turn's start wandered: %d then %d", first, got)
	}
	// A clock back at a few seconds is a new turn.
	if got := clock.since("%1", "Thinking… (2s)", at.Add(30*time.Second)); got != 10_000+28 {
		t.Errorf("a new turn kept the old start: %d", got)
	}
	if got := clock.since("%1", "Thinking…", at); got != 0 {
		t.Errorf("a line with no clock has a start: %d", got)
	}
}

// An old page reads the row by the keys it knows; the new one is additive.
func TestAnOldShapedPageStillReadsTheRow(t *testing.T) {
	row := sessionRowWire{SessionRow: contract.SessionRow{ID: "%19", State: contract.SessionStateWorking,
		Line: "Thinking… (1m 12s)", WorkingSince: 1_000}}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Line  string `json:"line"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&old); err != nil {
		t.Fatal(err)
	}
	if old.ID != "%19" || old.State != "working" || old.Line != "Thinking… (1m 12s)" {
		t.Errorf("an old reader lost the row: %+v from %s", old, body)
	}
	// And a row with no clock carries no key at all.
	plain, _ := json.Marshal(sessionRowWire{SessionRow: contract.SessionRow{ID: "%2"}})
	if strings.Contains(string(plain), "working_since") {
		t.Errorf("a row with no clock carries working_since: %s", plain)
	}
}

// An unchanged scan opens no write transaction; a changed one does.
func TestAnUnchangedExecutionScanIsNotWritten(t *testing.T) {
	if swiftstore.ProcessStart(os.Getpid()).IsZero() {
		t.Skip("this platform has no kernel process-start reader")
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, PID: os.Getpid(), State: session.StateIdle}}
	s := paneServer(t, p)
	s.executionMachineID = "mac_a"
	ctx := context.Background()
	observe := func() map[string]string {
		inv := s.freshReading(ctx)
		items := inv.Assistants()
		lives := make([]swiftstore.Live, len(items))
		for i, item := range items {
			lives[i] = liveOf(item)
		}
		return s.observeExecutions(ctx, inv, items, lives)
	}
	first := observe()
	if first["%19"] == "" {
		t.Fatalf("no generation: %v", first)
	}
	_, skippedBefore := s.executions.reading()
	second := observe()
	held, skipped := s.executions.reading()
	if skipped != skippedBefore+1 {
		t.Errorf("an unchanged scan went to the store (skipped %d → %d)", skippedBefore, skipped)
	}
	if second["%19"] != first["%19"] || held != 1 {
		t.Errorf("the remembered answer differs: %v then %v (held %d)", first, second, held)
	}
	// The answer is a copy: a caller cannot edit what the next one is told.
	second["%19"] = "edited"
	if third := observe(); third["%19"] != first["%19"] {
		t.Errorf("a caller's edit reached the memo: %v", third)
	}

	// A new terminal is a changed scan, and is written.
	p.mu.Lock()
	p.s.ID = "%20"
	p.mu.Unlock()
	_, skippedBefore = s.executions.reading()
	fourth := observe()
	if _, skipped := s.executions.reading(); skipped != skippedBefore || fourth["%20"] == "" {
		t.Errorf("a changed scan was answered from the memo: %v", fourth)
	}
	if n, err := s.store.ExecutionCount(ctx, "mac_a"); err != nil || n != 1 {
		t.Errorf("the store holds %d execution(s) (%v); the proven-absent one should be gone", n, err)
	}
}

// The Cloud publisher reads the first task page on every pass. Inside one tick
// that page is the stream's product; any other page is built as it was.
func TestTheFirstTaskPageIsTheSharedProduct(t *testing.T) {
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, State: session.StateIdle}}
	s, _ := streamServer(t, p)
	read := func(query string) int {
		rec := httptest.NewRecorder()
		s.tasksList(rec, httptest.NewRequest(http.MethodGet, "/v1/orchestrator/tasks"+query, nil))
		return rec.Code
	}
	before := s.lists().tasks.builds.Load()
	for i := 0; i < 4; i++ {
		if code := read("?limit=50"); code != http.StatusOK {
			t.Fatalf("the first page answered %d", code)
		}
	}
	if built := s.lists().tasks.builds.Load() - before; built != 1 {
		t.Errorf("four reads of the first page inside one tick built it %d times", built)
	}
	if code := read("?limit=50&state=running"); code != http.StatusOK {
		t.Fatalf("a filtered first page answered %d", code)
	}
	before = s.lists().tasks.builds.Load()
	if code := read("?limit=10"); code != http.StatusOK {
		t.Fatalf("another page answered %d", code)
	}
	if built := s.lists().tasks.builds.Load() - before; built != 0 {
		t.Errorf("another page was answered from the product (%d builds of it)", built)
	}
}
