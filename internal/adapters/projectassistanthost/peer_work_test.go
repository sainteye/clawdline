package projectassistanthost

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type peerWorkTools interface {
	Dispatch(context.Context, string, string, string, []string) (pa.WorkLink, error)
	Status(context.Context, string) (pa.WorkLink, error)
	MessageWork(context.Context, string, string, string) (pa.WorkMessageReceipt, error)
	MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
}

var launchedSecret = regexp.MustCompile(`TASK_SECRET=([0-9a-f]{64})`)

// A Bot answering another Bot's message_bot runs in a private thread worker.
// That turn still reads and adds to the work this same Bot dispatched from its
// own chat, and still cannot reach the work any other Bot dispatched.
func TestPeerRequestTurnReachesOnlyItsOwnBotsDispatchedWork(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxBots = 4
	var mu sync.Mutex
	var typed []string
	f.o.Broker.Type = func(_ context.Context, _, text string) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, text)
		return nil
	}
	// relayed returns the Session messages typed so far, not briefings.
	relayed := func() []transcript.Entry {
		mu.Lock()
		defer mu.Unlock()
		var out []transcript.Entry
		for _, line := range typed {
			if m, ok := transcript.SessionMessage(line, 1); ok {
				out = append(out, m)
			}
		}
		return out
	}
	f.start(t)
	f.enable(t)
	atlas, lens := f.create(t, "atlas"), f.create(t, "lens")

	// Atlas dispatches from its own chat, and the work Session signs in.
	if _, err := f.send(t, atlas, "dispatch"); err != nil {
		t.Fatal(err)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	own, ok := main.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("Atlas's chat has no work tools")
	}
	link, err := own.Dispatch(ctx, "work", "Show unconfirmed delivery", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	// The broker types the briefing, carrying the secret, into the worker.
	var secret []string
	fixtureEventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range typed {
			if secret = launchedSecret.FindStringSubmatch(line); secret != nil {
				typed = nil
				return true
			}
		}
		return false
	})
	if _, err = f.o.Broker.Accept(ctx, link.TaskID, secret[1]); err != nil {
		t.Fatal(err)
	}
	main.reply <- app.PromptResult{FinalText: "Dispatched."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Export(ctx, atlas.Key)
		return err == nil && turn.Conversation.ActiveTurnID == ""
	})

	// Lens dispatches work of its own, then asks Atlas to pass on a spec.
	if _, err = f.send(t, lens, "ask"); err != nil {
		t.Fatal(err)
	}
	asker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	lensTools, ok := asker.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("Lens's chat has no work tools")
	}
	foreign, err := lensTools.Dispatch(ctx, "lens-work", "Check the delivery copy", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := lensTools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: atlas.Key.BotID, Text: "Please add the new delivery spec to your running task.", IdempotencyKey: "spec"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("request was not assigned to Atlas's thread worker: %+v, %v", x, err)
	}
	asker.reply <- app.PromptResult{FinalText: "Asked Atlas."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, lens.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.Origin != pa.BotRequestOrigin || worker.q.Bot.Key == atlas.Key {
		t.Fatalf("not a peer-request worker turn: %+v", request.Turn)
	}
	peer, ok := worker.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("the peer-request turn has no work tools")
	}

	read, err := peer.Status(ctx, link.TaskID)
	if err != nil || read.TaskID != link.TaskID || read.State != pa.WorkExecuting {
		t.Fatalf("work_status on Atlas's own work from the peer-request turn: %+v, %v", read, err)
	}
	sent, err := peer.MessageWork(ctx, link.TaskID, "Show 'delivery not confirmed' when no receipt arrives.", "spec-1")
	if err != nil || sent.Stage != pa.WorkMessageDelivered {
		t.Fatalf("message_work on Atlas's own work from the peer-request turn: %+v, %v", sent, err)
	}
	lines := relayed()
	if len(lines) != 1 {
		t.Fatalf("typed %d messages, want 1: %+v", len(lines), lines)
	}
	m := lines[0]
	if !strings.Contains(m.Text, "來自 Bot atlas（派工者）") || !strings.Contains(m.Text, "轉述 Bot lens 的請求") ||
		!strings.Contains(m.Text, "relaying a request from Bot lens") || !strings.Contains(m.Text, "not the person's authorization") ||
		!strings.HasSuffix(m.Text, "Show 'delivery not confirmed' when no receipt arrives.") {
		t.Fatalf("the worker cannot tell the message relays another Bot's request: %+v", m)
	}

	// Lens's work stays Lens's, even from inside Atlas's peer-request turn.
	var refused *pa.Failure
	if _, err = peer.Status(ctx, foreign.TaskID); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "work_dispatched_by_another_bot" {
		t.Fatalf("work_status reached another Bot's work: %v", err)
	}
	if _, err = peer.MessageWork(ctx, foreign.TaskID, "Not yours.", "spec-2"); !errors.As(err, &refused) || refused.Field != "work_dispatched_by_another_bot" {
		t.Fatalf("message_work reached another Bot's work: %v", err)
	}
	if n := len(relayed()); n != 1 {
		t.Fatalf("a refused message was typed: %d", n)
	}
	worker.reply <- app.PromptResult{FinalText: "Passed the spec to my running task."}
}

// A Bot answering another Bot's message_bot does the requested work itself:
// it dispatches into its own Project and drives tabs it opened in that turn.
// Another Project still needs the person, who cannot be asked in a peer turn.
func TestPeerRequestTurnDispatchesAndUsesItsOwnBrowser(t *testing.T) {
	ctx := context.Background()
	f, _, chrome := browserFixture(t)
	f.o.Limits.MaxBots = 4
	f.start(t)
	f.enable(t)
	atlas, lens := f.create(t, "atlas"), f.create(t, "lens")

	if _, err := f.send(t, lens, "ask"); err != nil {
		t.Fatal(err)
	}
	asker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	lensTools := asker.q.Tools.(peerWorkTools)
	x, err := lensTools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: atlas.Key.BotID, Text: "Please dispatch the fix and take a 390px screenshot.", IdempotencyKey: "fix"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("request was not assigned to Atlas's thread worker: %+v, %v", x, err)
	}
	asker.reply <- app.PromptResult{FinalText: "Asked Atlas."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, lens.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.Origin != pa.BotRequestOrigin || worker.q.Bot.Key == atlas.Key {
		t.Fatalf("not a peer-request worker turn: %+v", request.Turn)
	}
	peer := worker.q.Tools.(peerWorkTools)

	link, err := peer.Dispatch(ctx, "peer-work", "Fix the peer-turn tools", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil || link.TaskID == "" {
		t.Fatalf("dispatch_work from the peer-request turn: %+v, %v", link, err)
	}
	var refused *pa.Failure
	other := worker.q.Tools.(interface {
		DispatchToProject(context.Context, string, string, string, []string, string) (pa.WorkLink, error)
	})
	if _, err = other.DispatchToProject(ctx, "peer-other", "Elsewhere", "Change only a.go.", []string{"a.go"}, "elsewhere"); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "dispatch_project_direct" {
		t.Fatalf("a peer-request turn dispatched into another Project: %v", err)
	}

	tools := browserToolsOf(t, worker)
	if r := chrome.run(t, tools, "open", `{"url":"http://127.0.0.1:9/page"}`); r.err != nil {
		t.Fatalf("browser open from the peer-request turn: %v", r.err)
	}
	if r := chrome.run(t, tools, "screenshot", ""); r.err != nil {
		t.Fatalf("browser screenshot from the peer-request turn: %v", r.err)
	}
	owner := BrowserOwner(worker.q.Binding.Fence, worker.q.Binding.ForgottenThrough)
	if got := chrome.b.OwnerOf(31); got != owner {
		t.Fatalf("tab owner = %q, want the worker's own %q", got, owner)
	}
	status, err := tools.BrowserStatus(ctx)
	if err != nil || len(status.Tabs) != 1 || status.Tabs[0].Tab != 31 {
		t.Fatalf("browser_status from the peer-request turn: %+v, %v", status, err)
	}
	worker.reply <- app.PromptResult{FinalText: "Dispatched and captured."}
}

// Work a Bot dispatches while answering another Bot belongs to its main chat:
// a notice there names who asked and the task, the task attaches to it, the
// main chat's work_status reads it and its completion wakes the main chat,
// not the hidden thread that dispatched it.
func TestPeerDispatchedWorkLandsInTheMainChat(t *testing.T) {
	ctx := context.Background()
	f, _, _ := browserFixture(t)
	f.o.Limits.MaxBots = 4
	f.start(t)
	f.enable(t)
	atlas, lens := f.create(t, "atlas"), f.create(t, "lens")
	if _, err := f.send(t, lens, "ask"); err != nil {
		t.Fatal(err)
	}
	asker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := asker.q.Tools.(peerWorkTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: atlas.Key.BotID, Text: "Please dispatch the fix.", IdempotencyKey: "fix"})
	if err != nil {
		t.Fatal(err)
	}
	asker.reply <- app.PromptResult{FinalText: "Asked Atlas."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, lens.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if worker.q.Bot.Key == atlas.Key {
		t.Fatal("the request did not run in Atlas's thread worker")
	}
	tools := worker.q.Tools.(peerWorkTools)
	link, err := tools.Dispatch(ctx, "peer-work", "Fix the header", "Change only a.go.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	// The same dispatch again is the same task and the same single notice.
	again, err := tools.Dispatch(ctx, "peer-work", "Fix the header", "Change only a.go.", []string{"a.go"})
	if err != nil || again.TaskID != link.TaskID {
		t.Fatalf("repeated dispatch: %+v, %v", again, err)
	}
	main, err := f.h.store.Export(ctx, atlas.Key)
	if err != nil {
		t.Fatal(err)
	}
	hidden, _ := f.h.store.Export(ctx, worker.q.Bot.Key)
	if link.Fence.Key != atlas.Key || len(main.WorkLinks) != 1 || len(hidden.WorkLinks) != 0 {
		t.Fatalf("task owned by %s; main chat links=%d, hidden thread links=%d", link.Fence.Key.BotID, len(main.WorkLinks), len(hidden.WorkLinks))
	}
	var notices []pa.Message
	for _, m := range main.Messages {
		if m.Kind == pa.NoticeMessage && m.TurnID == link.TurnID {
			notices = append(notices, m)
		}
	}
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "lens") || !strings.Contains(notices[0].Text, "Fix the header") || !strings.Contains(notices[0].Text, "應 Bot lens 的請求") {
		t.Fatalf("main-chat notice: %+v", notices)
	}
	// The worker can still read the work it dispatched for its owner.
	if got, err := tools.Status(ctx, link.TaskID); err != nil || got.TaskID != link.TaskID {
		t.Fatalf("work_status from the thread worker: %+v, %v", got, err)
	}
	worker.reply <- app.PromptResult{FinalText: "Dispatched."}
	// Atlas's answer goes back to Lens as a reply turn.
	reply := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if reply.q.Bot.Key != lens.Key {
		t.Fatalf("the answer went to %s, not Lens", reply.q.Bot.Key.BotID)
	}
	reply.reply <- app.PromptResult{FinalText: "Thanks."}
	fixtureEventually(t, func() bool {
		x, err := f.h.store.Export(ctx, lens.Key)
		return err == nil && x.Conversation.ActiveTurnID == ""
	})

	if _, err := f.send(t, atlas, "how is the fix going?"); err != nil {
		t.Fatal(err)
	}
	chat := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if got, err := chat.q.Tools.(peerWorkTools).Status(ctx, link.TaskID); err != nil || got.TaskID != link.TaskID {
		t.Fatalf("work_status from Atlas's main chat: %+v, %v", got, err)
	}
	chat.reply <- app.PromptResult{FinalText: "Still running."}
	fixtureEventually(t, func() bool {
		x, err := f.h.store.Export(ctx, atlas.Key)
		return err == nil && x.Conversation.ActiveTurnID == ""
	})

	if _, err := f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Header fixed."}); err != nil {
		t.Fatal(err)
	}
	wake := fixtureReceive(t, f.p.sent)
	if pa.WorkCompletionTask(wake.Turn) != link.TaskID || wake.Turn.Fence.Key != atlas.Key {
		t.Fatalf("the review turn ran in %s, not Atlas's main chat: %+v", wake.Turn.Fence.Key.BotID, wake.Turn)
	}
}

// peerFolders resolves "own" to the fixture's Project and "other" to a
// different registered repository, as the person's team folders would.
type peerFolders struct{ fixtureResolver }

func (r peerFolders) ResolveTeamFolder(ctx context.Context, machine, folder string) (pa.ProjectResolution, error) {
	switch folder {
	case "own":
		return r.Resolve(ctx, pa.ProjectSelection{MachineID: machine, ProjectID: r.f.scope.ProjectID})
	case "other":
		s := r.f.scope
		s.ProjectID, s.CanonicalID = "other-project", "other-canonical"
		return pa.ProjectResolution{Scope: s, Directory: r.f.repo + "-other", Current: true, Available: true}, nil
	}
	return pa.ProjectResolution{}, failure(pa.NotFound, "project")
}

// A Bot answering another Bot may dispatch read-only work, such as an
// acceptance Session that only takes screenshots, by naming its own Project
// with no claims. The work belongs to its main chat like any peer dispatch.
// Another Project, and the options that need the person, stay refused.
func TestPeerRequestTurnDispatchesReadOnlyWorkInItsOwnProject(t *testing.T) {
	ctx := context.Background()
	f, _, _ := browserFixture(t)
	f.o.Projects = peerFolders{fixtureResolver{f}}
	f.o.Limits.MaxBots, f.o.Limits.MaxProcesses = 4, 3
	f.start(t)
	f.enable(t)
	atlas, lens := f.create(t, "atlas"), f.create(t, "lens")
	if _, err := f.send(t, lens, "ask"); err != nil {
		t.Fatal(err)
	}
	asker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := asker.q.Tools.(peerWorkTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: atlas.Key.BotID, Text: "Please take the acceptance screenshots.", IdempotencyKey: "shots"})
	if err != nil {
		t.Fatal(err)
	}
	asker.reply <- app.PromptResult{FinalText: "Asked Atlas."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, lens.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.Origin != pa.BotRequestOrigin || worker.q.Bot.Key == atlas.Key {
		t.Fatalf("not a peer-request worker turn: %+v", request.Turn)
	}
	tools := worker.q.Tools.(interface {
		DispatchToProject(context.Context, string, string, string, []string, string) (pa.WorkLink, error)
		DispatchWithCompletion(context.Context, string, string, string, []string, string, string, string) (pa.WorkLink, error)
	})

	var refused *pa.Failure
	for _, folder := range []string{"other", "missing"} {
		if _, err = tools.DispatchToProject(ctx, "peer-"+folder, "Elsewhere", "Read only.", []string{}, folder); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "dispatch_project_direct" {
			t.Fatalf("a peer-request turn dispatched into project %q: %v", folder, err)
		}
	}
	if _, err = tools.DispatchWithCompletion(ctx, "peer-callback", "Shots", "Read only.", []string{}, "", "", ""); !errors.As(err, &refused) || refused.Field != "work_completion_origin" {
		t.Fatalf("a peer-request turn asked for a completion turn: %v", err)
	}

	link, err := tools.DispatchToProject(ctx, "peer-read", "Take acceptance screenshots", "Read only: take 390px screenshots, change nothing.", []string{}, "own")
	if err != nil || link.TaskID == "" {
		t.Fatalf("read-only dispatch in the Bot's own Project from a peer-request turn: %+v, %v", link, err)
	}
	main, err := f.h.store.Export(ctx, atlas.Key)
	if err != nil {
		t.Fatal(err)
	}
	hidden, _ := f.h.store.Export(ctx, worker.q.Bot.Key)
	if link.Fence.Key != atlas.Key || len(main.WorkLinks) != 1 || len(hidden.WorkLinks) != 0 {
		t.Fatalf("task owned by %s; main chat links=%d, hidden thread links=%d", link.Fence.Key.BotID, len(main.WorkLinks), len(hidden.WorkLinks))
	}
	worker.reply <- app.PromptResult{FinalText: "Dispatched the screenshots."}
}
