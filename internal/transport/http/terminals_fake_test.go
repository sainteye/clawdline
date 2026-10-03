package http

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// fakeTerms is a terminal server in memory: what the service asks of it,
// answered at once unless a test slows or breaks it.
type fakeTerms struct {
	mu    sync.Mutex
	terms map[terminal.ID]*fakeTerm
	next  int
	// keysDelay holds every Keys call this long; keysHang makes the next
	// one wait until its context ends, the shape of a tmux that never said.
	keysDelay    time.Duration
	keysHang     bool
	frameEntered chan struct{}
	frameRelease chan struct{}
	typed        []string
}

type fakeTerm struct {
	t     terminal.Terminal
	rev   uint64
	lines []string
	subs  map[chan struct{}]struct{}
}

func newFakeTerms() *fakeTerms { return &fakeTerms{terms: map[terminal.ID]*fakeTerm{}} }

func (f *fakeTerms) Open(_ context.Context, req ports.OpenTerminal) (terminal.Terminal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := terminal.NewID()
	t := terminal.Terminal{ID: id, ProjectID: req.ProjectID, Created: time.Now(), Status: terminal.Running,
		Cols: req.Cols, Rows: req.Rows, Dir: req.ProjectPath}
	f.terms[id] = &fakeTerm{t: t, rev: 1, subs: map[chan struct{}]struct{}{}}
	return t, nil
}

func (f *fakeTerms) List(context.Context) ([]terminal.Terminal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []terminal.Terminal{}
	for _, t := range f.terms {
		out = append(out, t.t)
	}
	return out, nil
}

func (f *fakeTerms) get(id terminal.ID) (*fakeTerm, error) {
	t := f.terms[id]
	if t == nil {
		return nil, terminal.Refuse(terminal.CodeClosed, "there is no such terminal")
	}
	return t, nil
}

func (f *fakeTerms) Frame(_ context.Context, id terminal.ID) (terminal.Frame, error) {
	if f.frameEntered != nil {
		select {
		case f.frameEntered <- struct{}{}:
		default:
		}
		<-f.frameRelease
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.get(id)
	if err != nil {
		return terminal.Frame{}, err
	}
	return terminal.Frame{Rev: strconv.FormatUint(t.rev, 10), At: time.Now(), Cols: t.t.Cols, Rows: t.t.Rows,
		Lines: append([]string(nil), t.lines...)}, nil
}

func (f *fakeTerms) Keys(ctx context.Context, id terminal.ID, data []byte) error {
	f.mu.Lock()
	delay, hang := f.keysDelay, f.keysHang
	f.keysHang = false
	_, err := f.get(id)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if hang {
		<-ctx.Done()
		return ctx.Err()
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	f.mu.Lock()
	f.typed = append(f.typed, string(data))
	f.mu.Unlock()
	f.draw(id, string(data))
	return nil
}

func (f *fakeTerms) Paste(_ context.Context, id terminal.ID, text string) error {
	f.mu.Lock()
	_, err := f.get(id)
	if err == nil {
		f.typed = append(f.typed, "paste:"+text)
	}
	f.mu.Unlock()
	if err == nil {
		f.draw(id, text)
	}
	return err
}

// draw is the screen moving: a new line, a new revision and a wake.
func (f *fakeTerms) draw(id terminal.ID, line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.terms[id]
	if t == nil {
		return
	}
	t.lines = append(t.lines, line)
	t.rev++
	for c := range t.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

func (f *fakeTerms) Resize(_ context.Context, id terminal.ID, cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.get(id)
	if err != nil {
		return err
	}
	t.t.Cols, t.t.Rows = cols, rows
	return nil
}

func (f *fakeTerms) Close(_ context.Context, id terminal.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.get(id)
	if err != nil {
		return err
	}
	delete(f.terms, id)
	for c := range t.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeTerms) History(_ context.Context, id terminal.ID, lines int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.get(id)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), t.lines...), nil
}

func (f *fakeTerms) HistoryBounded(ctx context.Context, id terminal.ID, lines, maxBytes int) ([]string, error) {
	return f.History(ctx, id, lines)
}

func (f *fakeTerms) Changed(_ context.Context, id terminal.ID) (<-chan struct{}, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.get(id)
	if err != nil {
		return nil, func() {}, err
	}
	c := make(chan struct{}, 1)
	t.subs[c] = struct{}{}
	return c, func() {
		f.mu.Lock()
		delete(t.subs, c)
		f.mu.Unlock()
	}, nil
}

func (f *fakeTerms) typedNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typed...)
}

var _ ports.OwnedTerminals = (*fakeTerms)(nil)

// termFixture is a whole handler, gate included, over a terminal host.
type termFixture struct {
	t       *testing.T
	s       *Server
	h       http.Handler
	local   string
	machine string
	dir     string
}

func newTermFixture(t *testing.T, host ports.OwnedTerminals) *termFixture {
	t.Helper()
	p := waitingPane("%4", "")
	s, h, local, machine := wholeServer(t, touchPane{p}, touchPane{p})
	s.term.host = host
	project := t.TempDir()
	s.term.project = func(_ context.Context, id string) (string, bool) { return project, id == "prj_test" }
	return &termFixture{t: t, s: s, h: h, local: local, machine: machine, dir: s.cfg.Dir}
}

// device pairs a reader or a sender. Senders inherit terminal access.
func (f *termFixture) device(name string, granted bool) (id, token string) {
	f.t.Helper()
	caps := auth.NewCaps(auth.Read)
	if granted {
		caps = auth.NewCaps(auth.Read, auth.Send)
	}
	id, token, err := f.s.gate().auth.AddDevice(name, caps, false)
	if err != nil {
		f.t.Fatal(err)
	}
	if granted {
		f.grant(id, true)
	}
	return id, token
}

func (f *termFixture) grant(id string, on bool) {
	f.t.Helper()
	body := `{"grant":false}`
	if on {
		body = `{"grant":true}`
	}
	rec := f.do(http.MethodPost, "/v1/auth/devices/"+id+"/terminal", f.local, body)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("grant %s %v answered %d %s", id, on, rec.Code, rec.Body)
	}
}

func (f *termFixture) do(method, path, token, body string) *httptest.ResponseRecorder {
	headers := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return call{method: method, path: path, body: body, headers: headers}.do(f.h)
}

func (f *termFixture) open(token string) contract.Terminal {
	f.t.Helper()
	rec := f.do(http.MethodPost, "/v1/terminals", token, `{"project_id":"prj_test","cols":80,"rows":24}`)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("open answered %d %s", rec.Code, rec.Body)
	}
	var out contract.Terminal
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *termFixture) control(id, token, client, action string) (contract.TerminalControl, *httptest.ResponseRecorder) {
	f.t.Helper()
	rec := f.do(http.MethodPost, "/v1/terminals/"+id+"/control", token,
		`{"action":"`+action+`","client":"`+client+`"}`)
	var out contract.TerminalControl
	if rec.Code == http.StatusOK {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return out, rec
}

func (f *termFixture) input(id, token, client string, epoch, seq int64, data string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(contract.TerminalInputRequest{Epoch: epoch, Client: client, Seq: seq,
		Data: b64(data)})
	return f.do(http.MethodPost, "/v1/terminals/"+id+"/input", token, string(b))
}

func termCode(rec *httptest.ResponseRecorder) string {
	var r contract.TerminalRefusal
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	return string(r.Error)
}

// sse is one stream read on its own goroutine, its events in order.
type sse struct {
	events chan sseEvent
	cancel func()
}

type sseEvent struct {
	name string
	data string
	at   time.Time
}

func (f *termFixture) stream(srv *httptest.Server, id, token, client string) (*sse, int) {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/terminals/"+id+"/stream?client="+client, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		f.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		var r contract.TerminalRefusal
		_ = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		cancel()
		return &sse{events: nil, cancel: func() {}}, resp.StatusCode
	}
	out := &sse{events: make(chan sseEvent, 256), cancel: cancel}
	go func() {
		defer resp.Body.Close()
		defer close(out.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		var e sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			case line == "" && e.name != "":
				e.at = time.Now()
				out.events <- e
				e = sseEvent{}
			}
		}
	}()
	f.t.Cleanup(cancel)
	return out, http.StatusOK
}

// until reads events until one matches, and fails after wait.
func (s *sse) until(t *testing.T, wait time.Duration, what string, ok func(sseEvent) bool) sseEvent {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case e, open := <-s.events:
			if !open {
				t.Fatalf("the stream ended before %s", what)
			}
			if ok(e) {
				return e
			}
		case <-deadline:
			t.Fatalf("no %s within %v", what, wait)
		}
	}
}

// ended is whether the stream closes within wait, and when.
func (s *sse) ended(t *testing.T, wait time.Duration) (time.Time, []sseEvent) {
	t.Helper()
	var seen []sseEvent
	deadline := time.After(wait)
	for {
		select {
		case e, open := <-s.events:
			if !open {
				return time.Now(), seen
			}
			seen = append(seen, e)
		case <-deadline:
			t.Fatalf("the stream was still open after %v; saw %v", wait, seen)
		}
	}
}

func (f *termFixture) server() *httptest.Server {
	srv := httptest.NewServer(f.h)
	f.t.Cleanup(srv.Close)
	return srv
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
