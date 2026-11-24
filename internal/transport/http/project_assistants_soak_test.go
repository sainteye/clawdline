package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// There is deliberately no installed-provider branch, executable, or vendor
// configuration in this fixture. The opt-in changes duration, never authority.
type paSoakPeer struct {
	sends, opens, closes atomic.Int32
	resume               bool
	lose                 atomic.Bool
	lostResponse         chan struct{}
	unknownSent          chan struct{}
}

func (p *paSoakPeer) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	caps := paServerCaps(b)
	caps.Resume = p.resume
	return caps, nil
}
func (p *paSoakPeer) Open(context.Context, app.OpenRequest, func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.opens.Add(1)
	return p, nil
}
func (*paSoakPeer) SessionID() string { return "scripted-soak-only" }
func (p *paSoakPeer) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	p.sends.Add(1)
	if p.lose.Load() {
		close(p.unknownSent)
	}
	return paSoakPending{text: "Answer to " + q.Text, lost: p.lose.Load(), release: p.lostResponse}, nil
}
func (*paSoakPeer) Answer(context.Context, string, string) error { return nil }
func (*paSoakPeer) Cancel(context.Context) error                 { return nil }
func (p *paSoakPeer) Close(context.Context) error                { p.closes.Add(1); return nil }

type paSoakPending struct {
	text    string
	lost    bool
	release <-chan struct{}
}

func (p paSoakPending) Wait(ctx context.Context) (app.PromptResult, error) {
	if p.lost {
		select {
		case <-p.release:
		case <-ctx.Done():
			return app.PromptResult{}, ctx.Err()
		}
		return app.PromptResult{}, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "soak_lost_execution_response"}
	}
	return app.PromptResult{FinalText: p.text}, nil
}

type paSoakHTTP struct {
	fixture *paServerFixture
	server  *httptest.Server
	client  *http.Client
}

func (h *paSoakHTTP) reconnect() {
	if h.client != nil {
		h.client.CloseIdleConnections()
	}
	h.client = &http.Client{Timeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond,
		Transport: &http.Transport{Proxy: nil}}
}

func (h *paSoakHTTP) request(t *testing.T, method, path string, body any, out any) int {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+h.fixture.local)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		var envelope struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Idempotency-Key", envelope.Command.IdempotencyKey)
	}
	resp, err := h.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		t.Fatalf("decode HTTP %d: %s", resp.StatusCode, b)
	}
	if resp.StatusCode != 200 {
		t.Logf("HTTP %d: %s", resp.StatusCode, b)
	}
	return resp.StatusCode
}

func (h *paSoakHTTP) get(t *testing.T) contract.ProjectAssistantGetResponse {
	t.Helper()
	var got contract.ProjectAssistantGetResponse
	if h.request(t, "GET", "/v1/project-assistants/"+h.fixture.key.BotID+"?machine_id="+h.fixture.key.MachineID, nil, &got) != 200 {
		t.Fatal("get failed")
	}
	return got
}

func (h *paSoakHTTP) send(t *testing.T, id string) (contract.ProjectAssistantSendRequest, contract.ProjectAssistantReceipt) {
	t.Helper()
	g := h.get(t)
	f := h.fixture
	q := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, id), Text: id,
		Fence: paWireFence(pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID, BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation})}
	var out contract.ProjectAssistantReceiptResponse
	if h.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", q, &out) != 200 {
		t.Fatal("send not accepted")
	}
	if out.Receipt.State != contract.ProjectAssistantEffectStateAccepted {
		t.Fatalf("not accepted: %+v", out)
	}
	return q, out.Receipt
}

func paSoakAwait(t *testing.T, f *paServerFixture, id string, state pa.TurnState) app.Export {
	t.Helper()
	deadline := time.NewTimer(time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		for _, turn := range x.Turns {
			if turn.ID == id && turn.State == state {
				return x
			}
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatalf("turn %s did not reach %s: %+v", id, state, x.Turns)
		}
	}
}

func TestProjectAssistantHTTPDurableSoakControl(t *testing.T) { paRunHTTPDurableSoak(t, 2, 0) }

func TestProjectAssistantHTTPDurableSoakTenMinutes(t *testing.T) {
	if os.Getenv("CLAWDLINE_BOTS_SOAK_817B8489") != "1" {
		t.Skip("task-specific ten-minute synthetic HTTP soak is opt-in")
	}
	paRunHTTPDurableSoak(t, 60, 10*time.Minute)
}

func paRunHTTPDurableSoak(t *testing.T, count int, duration time.Duration) {
	started := time.Now()
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	f := newPAServerFixture(t, true)
	p := &paSoakPeer{lostResponse: make(chan struct{}), unknownSent: make(chan struct{})}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	h := &paSoakHTTP{fixture: f, server: httptest.NewServer(f.h)}
	h.reconnect()
	t.Cleanup(func() { h.client.CloseIdleConnections(); h.server.Close() })
	if h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": true}, nil) != 200 {
		t.Fatal("enable")
	}
	if h.request(t, "POST", "/v1/project-assistants", f.create(), nil) != 200 {
		t.Fatal("create")
	}
	g := h.get(t)
	var snapshot contract.ProjectAssistantReadViewResponse
	if h.request(t, "GET", paViewURL(f.key, g.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), nil, &snapshot) != 200 {
		t.Fatal("snapshot")
	}
	frontier := snapshot.Head.EventFrontier
	seenEvents, seenReceipts := map[string]bool{}, map[string]bool{}
	reconnects := 0
	readEvents := func() {
		t.Helper()
		for {
			var page contract.ProjectAssistantReadViewResponse
			if h.request(t, "GET", paViewURL(f.key, g.Conversation.ID, pa.ReadViewEvents, snapshot.SourceEpoch, "", frontier, 256), nil, &page) != 200 {
				t.Fatal("event continuity")
			}
			for _, item := range page.Items {
				if item.Event == nil || item.Event.Seq <= frontier || seenEvents[item.ID] {
					t.Fatal("duplicate/out-of-order source event")
				}
				seenEvents[item.ID] = true
			}
			if len(page.Items) > 0 && page.NextSeq <= frontier {
				t.Fatal("cursor did not advance")
			}
			frontier = page.NextSeq
			if !page.HasMore {
				break
			}
		}
	}
	var x app.Export
	for i := 0; i < count; i++ {
		// Fixed planned wall-clock pacing, not a virtual scheduler or busy loop.
		if wait := time.Until(started.Add(time.Duration(i) * duration / time.Duration(count))); wait > 0 {
			time.Sleep(wait)
		}
		q, receipt := h.send(t, fmt.Sprintf("soak-input-%03d", i))
		if seenReceipts[receipt.ID] {
			t.Fatal("duplicate receipt")
		}
		seenReceipts[receipt.ID] = true
		x = paSoakAwait(t, f, receipt.ResourceID, pa.TurnSucceeded)
		// Drop the prior TCP connection, then query the same accepted command.
		h.reconnect()
		reconnects++
		var replay contract.ProjectAssistantReceiptResponse
		if h.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", q, &replay) != 200 || replay.Receipt != receipt {
			t.Fatal("reconnect changed durable receipt")
		}
		readEvents()
		t.Logf("checkpoint=%d/%d at=%s sends=%d source_frontier=%d events=%d", i+1, count, time.Now().UTC().Format(time.RFC3339Nano), p.sends.Load(), frontier, len(seenEvents))
	}
	if wait := time.Until(started.Add(duration)); wait > 0 {
		time.Sleep(wait)
	}
	// One final-boundary failure: Send has happened; Wait loses its execution
	// response. Never retry the provider or manufacture a successful answer.
	p.lose.Store(true)
	unknownQ, unknownReceipt := h.send(t, "soak-final-unknown")
	select {
	case <-p.unknownSent:
	case <-time.After(time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond):
		t.Fatal("unknown prompt never sent")
	}
	// A genuinely admitted queue remains behind the lost execution result.
	_, queuedReceipt := h.send(t, "soak-queued-before-unknown")
	close(p.lostResponse)
	x = paSoakAwait(t, f, unknownReceipt.ResourceID, pa.TurnUnknown)
	readEvents()
	if p.sends.Load() != int32(count+1) {
		t.Fatal("unexpected scripted sends")
	}
	if len(x.Messages) != 2*count+2 || len(x.Turns) != count+2 {
		t.Fatalf("duplicate/missing durable facts: messages=%d turns=%d", len(x.Messages), len(x.Turns))
	}
	user, answers, unknown := 0, 0, 0
	ids, texts := map[string]bool{}, map[string]bool{}
	for _, m := range x.Messages {
		if ids[m.ID] || texts[m.Text] {
			t.Fatal("duplicate user input or answer")
		}
		ids[m.ID], texts[m.Text] = true, true
		if m.Kind == pa.UserMessage {
			user++
		}
		if m.Kind == pa.AssistantMessage {
			answers++
		}
	}
	for _, turn := range x.Turns {
		if turn.State == pa.TurnUnknown {
			unknown++
			if turn.Failure == nil || turn.Failure.Code != pa.ExecutionEffectUnknown {
				t.Fatal("lost execution failure provenance")
			}
		}
	}
	if user != count+2 || answers != count || unknown != 1 {
		t.Fatalf("wrong counts: user=%d answers=%d unknown=%d", user, answers, unknown)
	}
	for _, e := range x.Events {
		if e.Seq > snapshot.Head.EventFrontier && !seenEvents[e.ID] {
			t.Fatalf("source event lost on reconnect: %s", e.ID)
		}
	}
	assertBlocked := func() {
		t.Helper()
		latest := h.get(t)
		q := unknownQ
		q.Command = f.command(pa.SendOperation, "soak-blocked-after-unknown")
		q.Text = "must not execute"
		q.Fence = paWireFence(pa.Fence{Key: f.key, Project: paDomainProjectScope(latest.Bot.Project), ConversationID: latest.Conversation.ID, BindingID: latest.Conversation.BindingID, Generation: latest.Conversation.Generation})
		var failure contract.ProjectAssistantErrorResponse
		if h.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", q, &failure) != http.StatusInternalServerError || string(failure.Error.Code) != string(pa.OutcomeUnknown) {
			t.Fatalf("unknown allowed effects: %+v", failure)
		}
		if p.sends.Load() != int32(count+1) {
			t.Fatal("extra Send after unknown")
		}
	}
	assertBlocked()
	if err := f.s.StopProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	if usage := f.s.projectAssistants.host.Usage(); !usage.Known || usage.Processes != 0 || usage.ActiveActors != 0 {
		t.Fatalf("cleanup retained owned resources: %+v", usage)
	}
	// The shared fixture's deferred Stop otherwise inspects the old, closed
	// pool again. Release only the already successfully closed fixture owner.
	f.s.projectAssistants.host = nil
	before, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	h.client.CloseIdleConnections()
	h.server.Close()
	// Orderly shutdown keeps the unsent queue and its unknown predecessor.
	if err := f.s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(config.Config{Dir: f.s.cfg.Dir, Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	f.s = reopened
	reopened.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	t.Cleanup(func() { _ = reopened.StopProjectAssistants(context.Background()); _ = reopened.store.Close() })
	if err := reopened.StartProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewServer(reopened.Handler())
	h.reconnect()
	var replay contract.ProjectAssistantReceiptResponse
	if h.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", unknownQ, &replay) != 200 || replay.Receipt != unknownReceipt {
		t.Fatal("unknown acceptance receipt changed across reopen")
	}
	after, err := featurestore.New(reopened.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Messages, after.Messages) || !reflect.DeepEqual(before.Turns, after.Turns) || !reflect.DeepEqual(before.Receipts, after.Receipts) {
		t.Fatal("cleanup/reopen changed durable facts")
	}
	receiptIDs := map[string]bool{}
	for _, receipt := range after.Receipts {
		if receiptIDs[receipt.ID] {
			t.Fatal("duplicate durable receipt")
		}
		receiptIDs[receipt.ID] = true
	}
	if p.sends.Load() != int32(count+1) {
		t.Fatal("reopen replayed provider")
	}
	if status := h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": false}, nil); status != 200 {
		t.Fatalf("explicit disable: HTTP %d", status)
	}
	cleaned, err := featurestore.New(reopened.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range cleaned.Turns {
		if turn.ID == unknownReceipt.ResourceID && turn.State != pa.TurnUnknown {
			t.Fatal("cleanup erased unknown")
		}
		if turn.ID == queuedReceipt.ResourceID && turn.State != pa.TurnCancelled {
			t.Fatal("cleanup did not cancel unsent queue")
		}
	}
	if p.sends.Load() != int32(count+1) {
		t.Fatal("cleanup replayed provider")
	}
	evidence := map[string]any{"test": t.Name(), "started_at": started.UTC(), "finished_at": time.Now().UTC(), "wall_seconds": time.Since(started).Seconds(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "submitted": count + 2, "queued_before_unknown": 1, "completed_send_receipts": len(seenReceipts), "completed": count, "unknown": unknown, "scripted_sends": p.sends.Load(), "opens": p.opens.Load(), "closes": p.closes.Load(), "user_messages": user, "answers": answers, "source_events_observed": len(seenEvents), "source_frontier": frontier, "receipts": len(after.Receipts), "tcp_reconnects": reconnects, "durable_reopens": 1, "unknown_survived_reopen": true, "extra_send_after_unknown": false, "resources": "process RSS/CPU and OS census not measured; provider has no subprocess", "native_vendor_requests": 0, "paid_api_requests": 0}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SOAK_EVIDENCE %s", raw)
}
