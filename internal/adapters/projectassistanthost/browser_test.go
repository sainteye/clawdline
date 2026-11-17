package projectassistanthost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// scriptedChrome stands in for the native host and extension: it is not Chrome.
// It keeps one page per tab with a click counter and a text field.
type scriptedChrome struct {
	b      *browserbridge.Bridge
	next   int
	clicks map[int]int
	text   map[int]string
	ops    []string
}

func newScriptedChrome(t *testing.T, profiles *browserbridge.Profiles) *scriptedChrome {
	t.Helper()
	b, err := profiles.Channel(browserpolicy.Isolated)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.Poll(ctx, browserbridge.HostInfo{Mode: browserpolicy.Isolated, Version: "scripted"})
	return &scriptedChrome{b: b, next: 30, clicks: map[int]int{}, text: map[int]string{}}
}

// serve answers every queued command once.
func (c *scriptedChrome) serve(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	commands := c.b.Poll(ctx, browserbridge.HostInfo{Mode: browserpolicy.Isolated, Version: "scripted"})
	for _, command := range commands {
		c.ops = append(c.ops, command.Op)
		tab := command.Tab
		var value any = map[string]bool{"ok": true}
		switch command.Op {
		case "open":
			c.next++
			tab = c.next
		case "click":
			c.clicks[tab]++
		case "type":
			var a struct{ Text string }
			_ = json.Unmarshal(command.Args, &a)
			c.text[tab] = a.Text
		case "read":
			value = map[string]any{"clicks": c.clicks[tab], "text": c.text[tab]}
		case "screenshot":
			value = map[string]string{"data": base64.StdEncoding.EncodeToString([]byte("\xff\xd8scripted\xff\xd9")), "url": "http://127.0.0.1/page"}
		}
		raw, _ := json.Marshal(value)
		if err := c.b.Deliver([]browserbridge.Result{{ID: command.ID, OK: true, Tab: tab, Value: raw}}); err != nil {
			t.Fatal(err)
		}
	}
	return len(commands)
}

type browserCall struct {
	value json.RawMessage
	err   error
}

// run starts one tool call, waits for it to queue, answers it and returns.
func (c *scriptedChrome) run(t *testing.T, tools app.BrowserTools, op, args string) browserCall {
	t.Helper()
	done := make(chan browserCall, 1)
	go func() {
		q := app.BrowserCommand{Op: op}
		if args != "" {
			q.Args = json.RawMessage(args)
		}
		r, err := tools.BrowserCommand(context.Background(), q)
		done <- browserCall{r.Value, err}
	}()
	for {
		select {
		case r := <-done:
			return r
		default:
		}
		if c.b.Status().Pending > 0 {
			c.serve(t)
			return fixtureReceive(t, done)
		}
	}
}

func browserFixture(t *testing.T) (*hostFixture, *browserbridge.Profiles, *scriptedChrome) {
	t.Helper()
	f := newHostFixture(t)
	profiles, err := browserbridge.OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := browserpolicy.Policy{Order: []string{browserpolicy.Isolated, browserpolicy.UserChrome, browserpolicy.Native}, Automatic: map[string]bool{browserpolicy.Isolated: true, browserpolicy.UserChrome: false, browserpolicy.Native: false}}
	f.o.Browser = &BrowserAccess{
		Status: func(ctx context.Context, owner string) (app.BrowserStatus, error) {
			mode, st, _ := profiles.SelectedStatus(owner, policy)
			out := app.BrowserStatus{Mode: mode, Connected: st.Connected, Tabs: []app.BrowserTab{}}
			for _, tab := range st.Tabs {
				out.Tabs = append(out.Tabs, app.BrowserTab{Tab: tab.Tab, Mode: tab.Mode, Current: tab.Current})
			}
			return out, nil
		},
		Command: func(ctx context.Context, owner string, q app.BrowserCommand) (app.BrowserResult, error) {
			v, err := profiles.DoOwned(ctx, owner, q.Op, q.Args, policy, q.Mode)
			return app.BrowserResult{Value: v}, err
		},
	}
	return f, profiles, newScriptedChrome(t, profiles)
}

func browserToolsOf(t *testing.T, s *fixtureSession) app.BrowserTools {
	t.Helper()
	tools, ok := s.q.Tools.(app.BrowserTools)
	if !ok {
		t.Fatal("Host tools have no browser tools")
	}
	return tools
}

// A Bot's main-chat turn drives the real Profiles and Bridge under a
// Host-derived owner, keeps that owner across turns, and loses authority with
// each turn it no longer runs.
func TestHostBotBrowserToolsUseTheEngineUnderAStableOwner(t *testing.T) {
	f, profiles, chrome := browserFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := browserToolsOf(t, first)
	owner := BrowserOwner(first.q.Binding.Fence, first.q.Binding.ForgottenThrough)

	steps := []struct{ op, args string }{
		{"open", `{"url":"http://127.0.0.1:9/page"}`}, {"read", ""}, {"click", `{"label":"Count"}`},
		{"type", `{"text":"Ada"}`}, {"read", ""}, {"screenshot", ""},
	}
	var last json.RawMessage
	for _, step := range steps {
		r := chrome.run(t, tools, step.op, step.args)
		if r.err != nil {
			t.Fatalf("%s: %v", step.op, r.err)
		}
		if step.op == "read" {
			last = r.value
		}
	}
	if string(last) != `{"clicks":1,"text":"Ada"}` {
		t.Fatalf("page after click and type = %s", last)
	}
	if got := chrome.b.OwnerOf(31); got != owner {
		t.Fatalf("tab owner = %q, want the Host-derived %q", got, owner)
	}
	rows, _ := chrome.b.Records(owner, 20)
	if len(rows) != len(steps) {
		t.Fatalf("audit rows = %d, want %d: %+v", len(rows), len(steps), rows)
	}
	status, err := tools.BrowserStatus(context.Background())
	if err != nil || len(status.Tabs) != 1 || status.Tabs[0].Tab != 31 {
		t.Fatalf("status = %+v %v", status, err)
	}
	first.reply <- app.PromptResult{FinalText: "Done"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	// The retained tools of the finished turn admit nothing.
	before := len(chrome.ops)
	if _, err := tools.BrowserCommand(context.Background(), app.BrowserCommand{Op: "read"}); err == nil {
		t.Fatal("a finished turn's browser tool still worked")
	}
	if chrome.b.Status().Pending != 0 || len(chrome.ops) != before {
		t.Fatal("a finished turn's command was admitted")
	}

	// The next turn of the same conversation reaches its own stable tab.
	if _, e := f.send(t, b, "two"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	second := fixtureReceive(t, f.p.sessions)
	if again := BrowserOwner(second.q.Binding.Fence, second.q.Binding.ForgottenThrough); again != owner {
		t.Fatalf("owner changed across turns: %s → %s", owner, again)
	}
	next := browserToolsOf(t, second)
	if r := chrome.run(t, next, "read", ""); r.err != nil || string(r.value) != `{"clicks":1,"text":"Ada"}` {
		t.Fatalf("second turn read = %s %v", r.value, r.err)
	}

	// Another Bot of the same Project cannot reach it.
	other := f.create(t, "other")
	_ = other
	if BrowserOwner(pa.Fence{Key: other.Key, Project: other.Project, ConversationID: second.q.Binding.Fence.ConversationID}, 0) == owner {
		t.Fatal("two Bots share an owner")
	}

	// A revoked source refuses the next effect without admitting it.
	f.revoked.Store(true)
	before = len(chrome.ops)
	if _, err := next.BrowserCommand(context.Background(), app.BrowserCommand{Op: "click", Args: json.RawMessage(`{"label":"Count"}`)}); err == nil {
		t.Fatal("a revoked source still drove the browser")
	}
	if chrome.b.Status().Pending != 0 || len(chrome.ops) != before {
		t.Fatal("a revoked source's command was admitted")
	}
	f.revoked.Store(false)
	_ = profiles
	second.reply <- app.PromptResult{FinalText: "Done"}
}

// Ending the execution withdraws a command still waiting for the host.
func TestHostSealWithdrawsAQueuedBrowserCommand(t *testing.T) {
	f, _, chrome := browserFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := browserToolsOf(t, first)
	if r := chrome.run(t, tools, "open", `{"url":"http://127.0.0.1:9/page"}`); r.err != nil {
		t.Fatal(r.err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := tools.BrowserCommand(context.Background(), app.BrowserCommand{Op: "click", Args: json.RawMessage(`{"label":"Pay"}`)})
		done <- err
	}()
	fixtureEventually(t, func() bool { return chrome.b.Status().Pending == 1 })
	first.reply <- app.PromptResult{FinalText: "Done"}
	err := fixtureReceive(t, done)
	if !errors.Is(err, browserbridge.Cancelled) {
		t.Fatalf("err = %v, want %s", err, browserbridge.Cancelled)
	}
	if n := chrome.serve(t); n != 0 {
		t.Fatalf("a sealed execution's click was handed to the host (%d)", n)
	}
	if chrome.clicks[31] != 0 {
		t.Fatal("the click happened")
	}
}

func TestBrowserOwnerSeparatesEveryAuthoritativeBoundary(t *testing.T) {
	base := pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}, Project: pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "c", Kind: pa.RepositoryProject}, ConversationID: "conv", BindingID: "bind", Generation: 1}
	owner := BrowserOwner(base, 0)
	if len(owner) > 128 || owner[:len(BrowserOwnerPrefix)] != BrowserOwnerPrefix {
		t.Fatalf("owner %q", owner)
	}
	same := base
	same.BindingID, same.Generation = "other-binding", 9
	if BrowserOwner(same, 0) != owner {
		t.Fatal("a new generation or binding lost the Bot's tabs")
	}
	changes := map[string]func(*pa.Fence){
		"machine":      func(f *pa.Fence) { f.Key.MachineID = "m2"; f.Project.MachineID = "m2" },
		"bot":          func(f *pa.Fence) { f.Key.BotID = "b2" },
		"project":      func(f *pa.Fence) { f.Project.ProjectID = "p2" },
		"canonical":    func(f *pa.Fence) { f.Project.CanonicalID = "c2" },
		"conversation": func(f *pa.Fence) { f.ConversationID = "conv2" },
	}
	for name, change := range changes {
		f := base
		change(&f)
		if BrowserOwner(f, 0) == owner {
			t.Fatalf("%s did not change the owner", name)
		}
	}
	if BrowserOwner(base, 1) == owner {
		t.Fatal("forgetting did not change the owner")
	}
	// Field boundaries are length-prefixed: shifting text between fields differs.
	a, b := base, base
	a.Key.BotID, a.ConversationID = "bx", "y"
	b.Key.BotID, b.ConversationID = "b", "xy"
	if BrowserOwner(a, 0) == BrowserOwner(b, 0) {
		t.Fatal("field boundaries collide")
	}
}

func TestBrowserToolsRefuseRoutineSlotsAndBotTurns(t *testing.T) {
	if pa.Code(browserScope(pa.ExecutionBinding{SessionSlot: "routine-1"}, pa.Turn{})) != pa.ScopeDenied {
		t.Fatal("a routine slot borrowed the main lane's browser")
	}
	for _, origin := range []pa.TurnOrigin{pa.BotRequestOrigin, pa.BotReplyOrigin} {
		if pa.Code(browserScope(pa.ExecutionBinding{}, pa.Turn{Origin: origin})) != pa.ScopeDenied {
			t.Fatalf("%s turn reached the browser", origin)
		}
	}
	if pa.Code(browserScope(pa.ExecutionBinding{}, pa.Turn{Origin: pa.HostNoticeOrigin, Room: &pa.RoomTicket{}})) != pa.ScopeDenied {
		t.Fatal("a room host notice reached the browser")
	}
	if browserScope(pa.ExecutionBinding{}, pa.Turn{Origin: pa.HostNoticeOrigin}) != nil {
		t.Fatal("the main lane host notice was refused")
	}
	if browserScope(pa.ExecutionBinding{}, pa.Turn{Origin: pa.UserOrigin}) != nil {
		t.Fatal("the person's own turn was refused")
	}
}

// slowCleanup holds a session's native cleanup open, as a process that is
// still exiting does. The runtime closes a session before it cancels the
// turn's context, so this is the interval in which only the seal acts.
type slowCleanup struct {
	app.Provider
	started chan struct{}
	release chan struct{}
}

func (p *slowCleanup) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, err := p.Provider.Open(ctx, q, sink)
	if s == nil {
		return s, err
	}
	return &slowSession{ProviderSession: s, p: p}, err
}

type slowSession struct {
	app.ProviderSession
	p *slowCleanup
}

func (s *slowSession) Close(ctx context.Context) error {
	close(s.p.started)
	<-s.p.release
	return s.ProviderSession.Close(ctx)
}

// While native cleanup is still running the turn's context is alive; the seal
// alone must withdraw a command that is still waiting for the host, so the
// host cannot pick it up during cleanup.
func TestHostSealWithdrawsAQueuedCommandDuringNativeCleanup(t *testing.T) {
	f, _, chrome := browserFixture(t)
	slow := &slowCleanup{Provider: f.p, started: make(chan struct{}), release: make(chan struct{})}
	f.o.Provider = func() (app.Provider, error) { return slow, nil }
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := browserToolsOf(t, first)
	if r := chrome.run(t, tools, "open", `{"url":"http://127.0.0.1:9/page"}`); r.err != nil {
		t.Fatal(r.err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := tools.BrowserCommand(context.Background(), app.BrowserCommand{Op: "click", Args: json.RawMessage(`{"label":"Pay"}`)})
		done <- err
	}()
	fixtureEventually(t, func() bool { return chrome.b.Status().Pending == 1 })
	first.reply <- app.PromptResult{FinalText: "Done"}
	fixtureReceive(t, slow.started)
	defer close(slow.release)
	select {
	case err := <-done:
		if !errors.Is(err, browserbridge.Cancelled) {
			t.Fatalf("err = %v, want %s", err, browserbridge.Cancelled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the queued click outlived the closed session")
	}
	if n := chrome.serve(t); n != 0 || chrome.clicks[31] != 0 {
		t.Fatalf("the host received %d commands during cleanup; clicks=%d", n, chrome.clicks[31])
	}
}

// A source that went read-only (offline for writes) and a feature the person
// turned off each refuse the next browser effect of the running turn, and the
// host receives nothing.
func TestHostBrowserRefusesAReadOnlySourceAndADisabledFeature(t *testing.T) {
	f, _, chrome := browserFixture(t)
	var offline atomic.Bool
	source := f.o.Source
	f.o.Source = func(ctx context.Context, key pa.BotKey, write bool) (pa.SourceStatus, error) {
		s, err := source(ctx, key, write)
		if offline.Load() {
			s.State, s.Writable = pa.SourceOffline, false
		}
		return s, err
	}
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := browserToolsOf(t, first)
	if r := chrome.run(t, tools, "open", `{"url":"http://127.0.0.1:9/page"}`); r.err != nil {
		t.Fatal(r.err)
	}
	refused := func(why string) {
		t.Helper()
		before := len(chrome.ops)
		_, err := tools.BrowserCommand(context.Background(), app.BrowserCommand{Op: "click", Args: json.RawMessage(`{"label":"Pay"}`)})
		if err == nil {
			t.Fatalf("%s: the click was admitted", why)
		}
		if chrome.b.Status().Pending != 0 || len(chrome.ops) != before {
			t.Fatalf("%s: the host received a command", why)
		}
		t.Logf("%s: %v", why, err)
	}
	offline.Store(true)
	refused("read-only source")
	if pa.Code(func() error { _, e := tools.BrowserStatus(context.Background()); return e }()) == "" {
		t.Fatal("a read-only source still read the browser status")
	}
	offline.Store(false)
	if err := f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	refused("disabled feature")
}

// A Bot that hands a page to the person must keep the tab: release would drop
// it as owner and the person could no longer reach it from the Bot's chat.
func TestHostBotBrowserReleaseKeepsTheTabForThePerson(t *testing.T) {
	f, _, chrome := browserFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := browserToolsOf(t, first)
	owner := BrowserOwner(first.q.Binding.Fence, first.q.Binding.ForgottenThrough)
	if r := chrome.run(t, tools, "open", `{"url":"http://127.0.0.1:9/page"}`); r.err != nil {
		t.Fatal(r.err)
	}
	before := len(chrome.ops)
	r := chrome.run(t, tools, "release", "")
	if r.err != nil {
		t.Fatalf("release: %v", r.err)
	}
	if len(chrome.ops) != before {
		t.Fatal("release reached the browser engine")
	}
	if got := chrome.b.OwnerOf(31); got != owner {
		t.Fatalf("tab owner after release = %q, want the Bot's %q", got, owner)
	}
	if !strings.Contains(string(r.value), "kept") {
		t.Fatalf("release answer does not say the tab was kept: %s", r.value)
	}
	status, err := tools.BrowserStatus(context.Background())
	if err != nil || len(status.Tabs) != 1 {
		t.Fatalf("status after release = %+v %v", status, err)
	}
	first.reply <- app.PromptResult{FinalText: "Done"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
}
