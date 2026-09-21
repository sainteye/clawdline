package http

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// On 2026-09-21 a daemon ran for seven and a half hours without
// CLAWDLINE_NEXT_WEB. `/` said "this daemon does not own that route yet", which
// was not true — `/` is the console, and the daemon had not been told where its
// files were. It now says that, by name; a /v1 route nobody has written still
// says not_implemented.
func TestADaemonWithNoWebRootSaysSoAtItsFrontDoor(t *testing.T) {
	h, token := upstreamFixture(t, config.Load())

	for _, path := range []string{"/", "/index.html", "/app/js/main.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:7757"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var refusal struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		if rec.Code != http.StatusNotImplemented || refusal.Error != "no_web_root" ||
			!strings.Contains(refusal.Detail, "CLAWDLINE_NEXT_WEB") {
			t.Errorf("%s answered %d: %s", path, rec.Code, rec.Body)
		}
	}

	if rec := askUnowned(t, h, token); !strings.Contains(rec.Body.String(), `"not_implemented"`) {
		t.Errorf("an unowned /v1 route: %d %s", rec.Code, rec.Body)
	}

	// And /v1/diagnostics, this machine's own reading, carries the same answer.
	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics", nil)
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var d contract.Diagnostics
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("diagnostics: %d %s", rec.Code, rec.Body)
	}
	if d.Console.State != contract.ConsoleStateNone || d.Console.Root != "" ||
		!strings.Contains(d.Console.Detail, "501 no_web_root") {
		t.Errorf("diagnostics.console: %+v", d.Console)
	}
}

// Health is not where this is answered, and the reason is written into the
// contract (ConsoleDiagnostics): a missing local page stops nothing a paired
// phone uses. This pins that the choice was made, not forgotten.
func TestHealthStaysAboutTheDaemonWhenThereIsNoConsole(t *testing.T) {
	h, _ := upstreamFixture(t, config.Load())
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Host = "127.0.0.1:7757"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var health contract.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil || !health.OK {
		t.Fatalf("health with no console: %d %s", rec.Code, rec.Body)
	}
}

func TestTheConsoleIsReadAsServedNoneOrBroken(t *testing.T) {
	if c := consoleReading(""); c.State != contract.ConsoleStateNone {
		t.Errorf("no root: %+v", c)
	}

	empty := t.TempDir()
	c := consoleReading(empty)
	if c.State != contract.ConsoleStateBroken || c.Root != empty || !strings.Contains(c.Detail, "500 no_document") {
		t.Errorf("a root with no document: %+v", c)
	}
	if line := consoleLogLine(c); !strings.HasPrefix(line, "console: BROKEN") {
		t.Errorf("%q", line)
	}

	if err := os.WriteFile(filepath.Join(empty, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := consoleReading(empty); c.State != contract.ConsoleStateServed || consoleLogLine(c) != "console: served from "+empty {
		t.Errorf("a root with a document: %+v", c)
	}
}

// The line is under `listening`, because that line is the one somebody reads
// after a restart, and nothing in between may push it out of sight.
func TestTheConsoleLineIsTheLineAfterListening(t *testing.T) {
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	t.Setenv("CLAWDLINE_NEXT_WEB", "")
	t.Setenv(config.UpstreamPortEnv, "")
	cfg := config.Load()
	cfg.Dir = filepath.Join(t.TempDir(), "next")
	cfg.Port = 0
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.store.Close() })

	var buf syncBuffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ln, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "console:") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	_ = ln.Close()
	<-done

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "clawdline-go listening on http://"+ln.Addr().String()) {
			continue
		}
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], "console: NONE") ||
			!strings.Contains(lines[i+1], "CLAWDLINE_NEXT_WEB is not set") {
			t.Fatalf("the line after listening is not the console:\n%s", buf.String())
		}
		return
	}
	t.Fatalf("no listening line:\n%s", buf.String())
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
