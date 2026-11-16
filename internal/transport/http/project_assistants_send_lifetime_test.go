package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// sendLifetimeGate sits in front of the real handler on a real TCP listener.
// Armed, it lets the source admit a send and then holds the HTTP answer, or
// holds one snapshot read before the source sees it, until the test releases
// it or the client goes away.
type sendLifetimeGate struct {
	f             *paServerFixture
	mu            sync.Mutex
	armed, held   string
	release       chan struct{}
	posts, gone   atomic.Int32
	providerSends atomic.Int32
}

func (g *sendLifetimeGate) wait(r *http.Request, what string) bool {
	g.mu.Lock()
	release := make(chan struct{})
	g.armed, g.held, g.release = "", what, release
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.held = ""; g.mu.Unlock() }()
	select {
	case <-release:
		return true
	case <-r.Context().Done():
		g.gone.Add(1)
		return false
	}
}

func (g *sendLifetimeGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/fixture/arm", "/fixture/release":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		g.mu.Lock()
		if r.URL.Path == "/fixture/arm" {
			g.armed = r.URL.Query().Get("what")
		} else if g.release != nil {
			close(g.release)
			g.release = nil
		}
		g.mu.Unlock()
		w.WriteHeader(204)
		return
	case "/fixture/state":
		x, err := store.New(g.f.s.store.ProjectAssistantDatabase()).Export(r.Context(), g.f.key)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		g.mu.Lock()
		state := map[string]any{"held": g.held, "posts": g.posts.Load(), "client_gone": g.gone.Load(), "provider_sends": g.providerSends.Load(), "turns": len(x.Turns)}
		g.mu.Unlock()
		kinds := map[pa.MessageKind]int{}
		for _, m := range x.Messages {
			kinds[m.Kind]++
		}
		state["user"], state["assistant"] = kinds[pa.UserMessage], kinds[pa.AssistantMessage]
		_ = json.NewEncoder(w).Encode(state)
		return
	}
	g.mu.Lock()
	armed := g.armed
	g.mu.Unlock()
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/messages") {
		g.posts.Add(1)
		if armed == "send" {
			rec := httptest.NewRecorder()
			g.f.h.ServeHTTP(rec, r)
			if !g.wait(r, "send") {
				return
			}
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
	}
	if armed == "snapshot" && strings.HasSuffix(r.URL.Path, "/view") && r.URL.Query().Get("mode") == "snapshot" && !g.wait(r, "snapshot") {
		return
	}
	g.f.h.ServeHTTP(w, r)
}

type sendLifetimePending struct{ text string }

func (p sendLifetimePending) Wait(context.Context) (app.PromptResult, error) {
	return app.PromptResult{FinalText: "Scripted reply to " + p.text}, nil
}

func newSendLifetimeFixture(t *testing.T) (*sendLifetimeGate, *httptest.Server) {
	t.Helper()
	f := newPAServerFixture(t, true)
	g := &sendLifetimeGate{f: f}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			return &fallbackHTTPSession{send: func(p app.PromptRequest) (app.PendingPrompt, error) {
				g.providerSends.Add(1)
				return sendLifetimePending{text: p.Text}, nil
			}}, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	server := httptest.NewServer(g)
	t.Cleanup(server.Close)
	return g, server
}

func (g *sendLifetimeGate) state(t *testing.T) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "/fixture/state", nil))
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func sendLifetimeAwait(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The production reproducer at the source: the source admits one send and
// completes its turn even though the client that sent it stopped listening
// before the answer. Its durable receipt is what a client can still match.
func TestProjectAssistantSendLifetimeSourceKeepsAdmittedTurnWhenClientLeaves(t *testing.T) {
	g, server := newSendLifetimeFixture(t)
	f := g.f
	got := f.get(t, f.local)
	q := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "left-before-answer"), Text: "First message",
		Fence: paWireFence(pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation})}
	raw, _ := json.Marshal(q)
	g.mu.Lock()
	g.armed = "send"
	g.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/project-assistants/"+f.key.BotID+"/messages", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+f.local)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", q.Command.IdempotencyKey)
	done := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(r)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	sendLifetimeAwait(t, "source admission", func() bool { return g.state(t)["held"] == "send" })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("client should have lost its answer: %v", err)
	}
	sendLifetimeAwait(t, "client departure", func() bool { return g.gone.Load() == 1 })
	sendLifetimeAwait(t, "assistant reply", func() bool { return g.state(t)["assistant"] == float64(1) })
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, receipt := range x.Receipts {
		if receipt.Command.IdempotencyKey == q.Command.IdempotencyKey {
			matches++
			if receipt.ResourceID == "" {
				t.Fatalf("receipt without its turn: %+v", receipt)
			}
		}
	}
	if s := g.state(t); matches != 1 || s["user"] != float64(1) || s["provider_sends"] != float64(1) || s["posts"] != float64(1) {
		t.Fatalf("one admitted send: receipts=%d state=%v", matches, s)
	}
}

// The actual Bots UI and Cloud SourceClient in a real browser against this
// source. Set CLAWDLINE_SEND_LIFETIME_E2E_DIR to an empty artifact directory
// and BOTS_BROWSER_CLAWDLINE_DIR to the browser broker's directory.
func TestProjectAssistantSendLifetimeBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_SEND_LIFETIME_E2E_DIR")
	if dir == "" {
		t.Skip("CLAWDLINE_SEND_LIFETIME_E2E_DIR is not set")
	}
	g, server := newSendLifetimeFixture(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "../../../web/console/src/project-assistants/send-lifetime.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+g.f.local, "BOTS_MACHINE="+g.f.key.MachineID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	if e := os.WriteFile(filepath.Join(dir, "browser.log"), log, 0o600); e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
	t.Logf("final source state: %v", g.state(t))
}
