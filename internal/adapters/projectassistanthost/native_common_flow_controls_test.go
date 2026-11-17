package projectassistanthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// No installed provider, native opt-in, login or content API is used by any
// control below. Evidence is explicitly labelled deterministic-control.
func commonFlowControlBatch(t *testing.T, limits commonFlowLimits) *commonFlowBatch {
	t.Helper()
	return newCommonFlowBatch(filepath.Join(t.TempDir(), "evidence.json"), "deterministic-control", limits)
}
func commonFlowReadEvidence(t *testing.T, b *commonFlowBatch) commonFlowEvidence {
	t.Helper()
	raw, e := os.ReadFile(b.path)
	if e != nil {
		t.Fatal(e)
	}
	var v commonFlowEvidence
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCommonFlowControlsAuthorizationAndRoot(t *testing.T) {
	parent := t.TempDir()
	for _, tc := range []struct{ name, token, root string }{
		{"missing", "", filepath.Join(parent, "missing")},
		{"wrong", "yes", filepath.Join(parent, "wrong")},
		{"relative", commonFlowAuthorization, "relative"},
		{"existing", commonFlowAuthorization, parent},
		{"unclean", commonFlowAuthorization, parent + "/../root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := commonFlowRoot(tc.token, tc.root); e == nil {
				t.Fatal("unsafe native opt-in/root admitted")
			}
		})
	}
	link := filepath.Join(parent, "dangling")
	if e := os.Symlink(filepath.Join(parent, "absent"), link); e == nil {
		if e = commonFlowRoot(commonFlowAuthorization, link); e == nil {
			t.Fatal("dangling symlink root admitted")
		}
	}
	root := filepath.Join(parent, "fresh")
	if e := commonFlowRoot(commonFlowAuthorization, root); e != nil {
		t.Fatal(e)
	}
	if e := commonFlowRoot(commonFlowAuthorization, root); e == nil {
		t.Fatal("existing state was reused")
	}
	if info, e := os.Stat(root); e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("state root permissions", e)
	}
}

func TestCommonFlowControlsCapsStopBeforeExternalBoundary(t *testing.T) {
	for _, kind := range []string{"open", "prompt", "send_message"} {
		t.Run(kind, func(t *testing.T) {
			b := commonFlowControlBatch(t, commonFlowLimits{1, 1, 1})
			effects := 0
			for i := 0; i < 3; i++ {
				_, e := b.reserve(kind, pa.Fence{}, nil)
				if e == nil {
					effects++
				} else if pa.Code(e) != pa.ExecutionEffectUnknown {
					t.Fatal(e)
				}
			}
			if effects != 1 {
				t.Fatalf("cap crossed external boundary %d times", effects)
			}
			v := commonFlowReadEvidence(t, b)
			if v.Halt != "cap_exhausted:"+kind || len(v.Calls) != 1 {
				t.Fatal("cap evidence missing", v)
			}
			if _, e := b.reserve("another-tool", pa.Fence{}, nil); e == nil {
				t.Fatal("cap did not halt other boundaries")
			}
		})
	}
}

func TestCommonFlowControlsFirstFailureStopsStageAndAgent(t *testing.T) {
	for _, cause := range []string{"failed", "unknown", "real_person_permission_wait"} {
		t.Run(cause, func(t *testing.T) {
			b := commonFlowControlBatch(t, commonFlowFixedLimits())
			calls := 0
			err := b.stage(pa.ClaudeCode, "first", func() error {
				calls++
				_, e := b.reserve("prompt", pa.Fence{}, "persisted request")
				if e != nil {
					return e
				}
				return errors.New(cause)
			})
			if err == nil {
				t.Fatal("failure became success")
			}
			for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
				if e := b.stage(agent, "must-not-run", func() error { calls++; return nil }); e == nil {
					t.Fatal("later stage admitted")
				}
			}
			v := commonFlowReadEvidence(t, b)
			if calls != 1 || len(v.Stages) != 1 || v.Stages[0].Status != "failed" || v.Stages[0].Cause != cause || v.Halt == "" || len(v.Calls) != 1 {
				t.Fatal("failed stage/effect evidence lost", calls, v)
			}
		})
	}
}

func TestCommonFlowControlsEvidenceWriteFailureRefusesEffect(t *testing.T) {
	b := newCommonFlowBatch(filepath.Join(t.TempDir(), "absent", "evidence.json"), "deterministic-control", commonFlowFixedLimits())
	if _, e := b.reserve("open", pa.Fence{}, nil); e == nil {
		t.Fatal("effect admitted without durable evidence")
	}
	if e := b.stage(pa.Codex, "next", func() error { t.Fatal("ran after evidence loss"); return nil }); e == nil {
		t.Fatal("missing evidence did not halt batch")
	}
}

func TestCommonFlowControlsUnsupportedIsNeverPass(t *testing.T) {
	b := commonFlowControlBatch(t, commonFlowFixedLimits())
	if e := b.unsupported(pa.Codex, "fallback", "actual installed adapter exposes no authoritative no-send guarantee"); e != nil {
		t.Fatal(e)
	}
	v := commonFlowReadEvidence(t, b)
	if len(v.Stages) != 1 || v.Stages[0].Status != "unsupported" || v.Stages[0].Cause == "" {
		t.Fatal(v)
	}
}

func TestCommonFlowControlsRemainingAndUnsupportedStop(t *testing.T) {
	b := newCommonFlowBatch(filepath.Join(t.TempDir(), "evidence.json"), "deterministic-control", commonFlowFixedLimits())
	if e := b.stage(pa.ClaudeCode, "capability-admission", func() error { return commonFlowUnsupported("no actual capability") }); e == nil {
		t.Fatal("unsupported stage became pass")
	}
	if e := b.stage(pa.Codex, "main-chat", func() error { t.Fatal("other agent ran"); return nil }); e == nil {
		t.Fatal("other agent admitted")
	}
	v := commonFlowReadEvidence(t, b)
	if len(v.Remaining) != 23 || len(v.Stages) != 1 || v.Stages[0].Status != "unsupported" {
		t.Fatal("unattempted boundaries hidden", v)
	}
	for _, s := range v.Remaining {
		if s.Status != "not-executed" {
			t.Fatal("unattempted stage became pass")
		}
	}
}

func TestCommonFlowControlsWrappedOpenAndPromptCaps(t *testing.T) {
	ctx := context.Background()
	for _, boundary := range []string{"open", "prompt"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := &fixtureProvider{sent: make(chan app.PromptRequest, 4), sessions: make(chan *fixtureSession, 4)}
			batch := commonFlowControlBatch(t, commonFlowLimits{1, 1, 1})
			p := &commonFlowProvider{Provider: fixture, batch: batch}
			q := app.OpenRequest{Bot: pa.Bot{Key: pa.BotKey{BotID: "owned"}}, Binding: pa.ExecutionBinding{Fence: pa.Fence{BindingID: "first", Generation: 1}}}
			s, e := p.Open(ctx, q, func(context.Context, app.ProtocolEvent) error { return nil })
			if e != nil {
				t.Fatal(e)
			}
			if boundary == "open" {
				q.Binding.Fence.BindingID = "second"
				if _, e = p.Open(ctx, q, nil); e == nil {
					t.Fatal("open wrapper crossed cap")
				}
				if fixture.opens.Load() != 1 {
					t.Fatal("native Open executed beyond cap")
				}
			} else {
				if _, e = s.Send(ctx, app.PromptRequest{}); e != nil {
					t.Fatal(e)
				}
				if _, e = s.Send(ctx, app.PromptRequest{}); e == nil {
					t.Fatal("prompt wrapper crossed cap")
				}
				if len(fixture.sent) != 1 {
					t.Fatal("native Send executed beyond cap")
				}
			}
		})
	}
}

func TestCommonFlowControlsSQLiteReceiptAndScope(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	batch := commonFlowControlBatch(t, commonFlowFixedLimits())
	f.o.Provider = func() (app.Provider, error) { return &commonFlowProvider{Provider: f.p, batch: batch}, nil }
	f.start(t)
	f.enable(t)
	b := f.create(t, "control")
	text, answer := "Control request", "control-answer"
	r, err := commonFlowSend(ctx, f, b, "first", text)
	if err != nil {
		t.Fatal(err)
	}
	native := fixtureReceive(t, f.p.sessions)
	q := fixtureReceive(t, f.p.sent)
	if e := native.q.Tools.SendMessage(ctx, "answer", answer); e != nil {
		t.Fatal(e)
	}
	native.reply <- app.PromptResult{FinalText: "Done."}
	x, err := commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
		return len(x.Turns) == 1 && x.Turns[0].State == pa.TurnSucceeded && x.Conversation.ActiveTurnID == "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonFlowReceipt(x, b, r, text, answer); err != nil {
		t.Fatal(err)
	}
	if q.Turn.Fence.Key != b.Key {
		t.Fatal("execution scope changed")
	}
	// These mutations must be rejected by the same acceptance function used by
	// native execution, rather than by a test of source spelling.
	for _, name := range []string{"foreign-bot", "foreign-project", "missing-receipt", "missing-event", "wrong-answer-fence"} {
		t.Run(name, func(t *testing.T) {
			bad := qualificationReopen(t, f, b.Key)
			switch name {
			case "foreign-bot":
				bad.Turns[0].Fence.Key.BotID = "foreign"
			case "foreign-project":
				bad.Turns[0].Fence.Project.CanonicalID = "foreign"
			case "missing-receipt":
				bad.Receipts = nil
			case "missing-event":
				bad.Events = nil
			case "wrong-answer-fence":
				for i := range bad.Messages {
					if bad.Messages[i].Kind == pa.AssistantMessage {
						bad.Messages[i].Fence.Generation++
					}
				}
			}
			if e := commonFlowReceipt(bad, b, r, text, answer); e == nil {
				t.Fatal("corrupt durable evidence accepted")
			}
		})
	}
	// A new query-only connection sees the accepted receipt after closure of
	// the runtime owner; it is not an in-memory provider acknowledgement.
	if err = f.h.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	x = qualificationReopen(t, f, b.Key)
	if err = commonFlowReceipt(x, b, r, text, answer); err != nil {
		t.Fatal(err)
	}
	v := commonFlowReadEvidence(t, batch)
	if v.Opens != 1 || v.Prompts != 1 || v.Tools != 1 {
		t.Fatal("boundary accounting", v)
	}
}

func TestCommonFlowControlsPermissionFactsAndDenial(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "permission-control")
	r, err := commonFlowSend(ctx, f, b, "offer", "Scripted safe request")
	if err != nil {
		t.Fatal(err)
	}
	n := fixtureReceive(t, f.p.sessions)
	q := fixtureReceive(t, f.p.sent)
	kinds := []pa.PermissionOptionKind{pa.AllowOnce, pa.AllowAlways, pa.RejectOnce, pa.RejectAlways}
	offer := pa.PermissionOffer{ID: "four-options", Fence: q.Turn.Fence, TurnID: q.Turn.ID, NativeRequestID: "scripted-request", ExpiresAt: time.Now().Add(time.Minute)}
	for _, kind := range kinds {
		offer.Options = append(offer.Options, pa.PermissionOption{ID: string(kind), Kind: kind, Allow: kind == pa.AllowOnce || kind == pa.AllowAlways, ReadOnly: true, Label: string(kind)})
	}
	if err = n.sink(ctx, app.ProtocolEvent{ID: "offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	x := qualificationReopen(t, f, b.Key)
	if len(x.Permissions) != 1 || !reflect.DeepEqual(x.Permissions[0].Options, offer.Options) || x.Turns[0].State != pa.TurnWaitingPermission {
		t.Fatal("four option facts were changed", x.Permissions)
	}
	s, _ := f.h.Service()
	_, err = s.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "deny"}, pa.PermissionAnswer{Fence: q.Turn.Fence, OfferID: offer.ID, TurnID: q.Turn.ID, OptionID: string(pa.RejectOnce)})
	if err != nil {
		t.Fatal(err)
	}
	n.reply <- app.PromptResult{FinalText: "Denied"}
	fixtureEventually(t, func() bool {
		v := qualificationReopen(t, f, b.Key)
		return v.Turns[0].State == pa.TurnSucceeded && v.Conversation.ActiveTurnID == ""
	})
	x = qualificationReopen(t, f, b.Key)
	if x.Permissions[0].State != pa.PermissionResolved || x.Permissions[0].SelectedOptionID != string(pa.RejectOnce) {
		t.Fatal("denial receipt lost")
	}
	if r.ResourceID != q.Turn.ID {
		t.Fatal("offer changed turn")
	}
	// Installed wrapper observes an offer and seals the whole batch. It must
	// never reuse the scripted denial as permission to answer a real offer.
	batch := commonFlowControlBatch(t, commonFlowFixedLimits())
	p := &commonFlowProvider{Provider: f.p, batch: batch}
	session, e := p.Open(ctx, n.q, func(context.Context, app.ProtocolEvent) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	wrapped := fixtureReceive(t, f.p.sessions)
	if e = wrapped.sink(ctx, app.ProtocolEvent{Permission: &offer, Fence: q.Turn.Fence}); e != nil {
		t.Fatal(e)
	}
	if _, e = session.Send(ctx, q); e == nil {
		t.Fatal("prompt sent after real permission wait")
	}
	if e = session.Answer(ctx, offer.ID, "reject_once"); pa.Code(e) != pa.ScopeDenied {
		t.Fatal("native permission was answered", e)
	}
	if v := commonFlowReadEvidence(t, batch); v.Halt != "real_person_permission_wait" || v.Prompts != 0 {
		t.Fatal(v)
	}
}

func TestCommonFlowControlsForgetRevocationAndLateEvent(t *testing.T) {
	ctx := context.Background()
	n := startNative(t, nil)
	s, _ := n.f.h.Service()
	mem, e := s.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: n.bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "approved"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "approved", Key: n.bot.Key, Project: n.bot.Project, Text: "Approved fixture fact"}})
	if e != nil {
		t.Fatal(e)
	}
	// Forget advances the privacy epoch; it does not itself terminate an
	// already-running main turn. Match the native stage's completed boundary.
	active := fixtureReceive(t, n.f.p.sessions)
	active.reply <- app.PromptResult{FinalText: "Done."}
	fixtureEventually(t, func() bool { return n.f.h.Usage().ActiveActors == 0 })
	if _, e = s.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: n.bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget"}, Project: n.bot.Project, ExpectedRevision: mem.Revision, SourceIDs: []string{"manual:approved"}}); e != nil {
		t.Fatal(e)
	}
	if e = n.tools.SendMessage(ctx, "late", "must never appear"); e == nil {
		t.Fatal("forgotten native tool published")
	}
	if e = n.f.h.engine.Publish(ctx, app.ProtocolEvent{ID: "late-event", Fence: n.prompt.Turn.Fence, TurnID: n.prompt.Turn.ID, Message: "must never appear"}); e == nil {
		t.Fatal("old event accepted")
	}
	n.f.revoked.Store(true)
	before := qualificationReopen(t, n.f, n.bot.Key)
	if _, e = commonFlowSend(ctx, n.f, n.bot, "revoked", "no"); pa.Code(e) != pa.Revoked {
		t.Fatal(e)
	}
	after := qualificationReopen(t, n.f, n.bot.Key)
	if len(after.Turns) != len(before.Turns) || len(after.Messages) != len(before.Messages) || len(after.MemoryEntries) != 1 || !after.MemoryEntries[0].Forgotten {
		t.Fatal("revoked/forgotten state changed")
	}
}

// A native quota error is not the authoritative PreSendRefusal contract.
// The installed adapters intentionally do not manufacture that contract.
func TestCommonFlowControlsSafeSwitchFacts(t *testing.T) {
	f := pa.AttemptFact{Stage: pa.AttemptClosed, CleanupConfirmed: true, Effect: "none", Reason: "usage_limit"}
	if !pa.SafeAttemptClosed(pa.Turn{Attempts: []pa.AttemptFact{f}}) {
		t.Fatal("explicit no-send cleanup fact rejected")
	}
	for _, change := range []func(*pa.AttemptFact){func(f *pa.AttemptFact) { f.SendStarted = true }, func(f *pa.AttemptFact) { f.SendReturned = true }, func(f *pa.AttemptFact) { f.NativeActivity = true }, func(f *pa.AttemptFact) { f.ToolActivity = true }, func(f *pa.AttemptFact) { f.AnswerObserved = true }, func(f *pa.AttemptFact) { f.CleanupConfirmed = false }, func(f *pa.AttemptFact) { f.Effect = "unknown" }, func(f *pa.AttemptFact) { f.Stage = pa.AttemptHeld }, func(f *pa.AttemptFact) { f.Reason = "" }} {
		bad := f
		change(&bad)
		if pa.SafeAttemptClosed(pa.Turn{Attempts: []pa.AttemptFact{bad}}) {
			t.Fatal("unsafe switch fact admitted", bad)
		}
	}
	if pa.SafeAttemptClosed(pa.Turn{}) {
		t.Fatal("missing ledger admitted")
	}
	err := &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: errors.New("fixture")}
	var guarantee *app.PreSendRefusal
	if errors.As(err, &guarantee) {
		t.Fatal("quota detail became no-send guarantee")
	}
	if !strings.Contains(err.Error(), "fixture") {
		t.Fatal("fixture did not execute")
	}
}

type commonFlowRefusalProvider struct {
	*fixtureProvider
	mode    string
	primary pa.Agent
}

func (p commonFlowRefusalProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, e := p.fixtureProvider.Open(ctx, q, sink)
	if e != nil {
		return s, e
	}
	if q.Bot.Agent != p.primary {
		return s, nil
	}
	cause := &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: failure(pa.Unsupported, "scripted-usage")}
	if p.mode == "safe" {
		return s, &app.PreSendRefusal{Failure: cause}
	}
	return s, cause
}

func TestCommonFlowControlsEngineFallbackRefusalAndUnknown(t *testing.T) {
	for _, mode := range []string{"safe", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newHostFixture(t)
			f.o.Provider = func() (app.Provider, error) {
				return commonFlowRefusalProvider{fixtureProvider: f.p, mode: mode, primary: pa.ClaudeCode}, nil
			}
			f.start(t)
			f.enable(t)
			s, _ := f.h.Service()
			key := pa.BotKey{MachineID: f.scope.MachineID, BotID: "fallback"}
			b, _, e := s.Create(ctx, pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "create"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.scope.MachineID, ProjectID: f.scope.ProjectID}, Name: "Fallback control", Agent: pa.ClaudeCode, FallbackAgents: []pa.Agent{pa.Codex}, Appearance: pa.Appearance{Shape: pa.EntryShape, Color: pa.EntryColor}, PermissionPolicy: pa.Ask})
			if e != nil {
				t.Fatal(e)
			}
			_, initial, _, e := s.Get(ctx, b.Key)
			if e != nil {
				t.Fatal(e)
			}
			original := pa.SendMessage{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: initial.ID, BindingID: initial.BindingID, Generation: initial.Generation}, Text: "One deterministic fallback control"}
			command := pa.Command{Key: b.Key, Operation: pa.SendOperation, IdempotencyKey: "one"}
			r, e := s.Send(ctx, command, original)
			if e != nil {
				t.Fatal(e)
			}
			fixtureReceive(t, f.p.sessions) // Primary's cleanup handle, never sent.
			want := pa.TurnUnknown
			if mode == "safe" {
				alternate := fixtureReceive(t, f.p.sessions)
				q := fixtureReceive(t, f.p.sent)
				if alternate.q.Bot.Agent != pa.Codex || q.Turn.ID != r.ResourceID {
					t.Fatal("safe alternate identity/turn changed")
				}
				alternate.reply <- app.PromptResult{FinalText: "Safe alternate"}
				want = pa.TurnSucceeded
			}
			fixtureEventually(t, func() bool {
				x := qualificationReopen(t, f, key)
				return len(x.Turns) == 1 && x.Turns[0].State == want && x.Conversation.ActiveTurnID == ""
			})
			x := qualificationReopen(t, f, key)
			turn := x.Turns[0]
			if mode == "unknown" {
				if f.p.opens.Load() != 1 {
					t.Fatal("unknown opened an alternate")
				}
				select {
				case <-f.p.sent:
					t.Fatal("unknown Open sent a prompt")
				default:
				}
				if turn.Attempts[len(turn.Attempts)-1].Effect != "unknown" {
					t.Fatal("unknown lost ledger")
				}
			}
			if mode == "safe" {
				closed := false
				for _, fact := range turn.Attempts {
					if fact.Agent == pa.ClaudeCode && pa.SafeAttemptClosed(pa.Turn{Attempts: []pa.AttemptFact{fact}}) {
						closed = true
					}
				}
				if !closed || f.p.opens.Load() != 2 {
					t.Fatal("safe switch lacked authoritative no-send cleanup", turn.Attempts)
				}
			}
			// Exact accepted-command readback is allowed; a fresh message would
			// acknowledge unknown in the product, so this control never sends one.
			before := f.p.opens.Load()
			replay, e := s.Send(ctx, command, original)
			if e != nil || replay.ID != r.ID || f.p.opens.Load() != before {
				t.Fatal("durable admission replay ran another attempt", e)
			}
		})
	}
}

func TestCommonFlowControlsConcurrentCapAndWrappedTool(t *testing.T) {
	b := commonFlowControlBatch(t, commonFlowLimits{1, 1, 1})
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := b.reserve("prompt", pa.Fence{}, nil); e == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("concurrent cap expanded", accepted.Load())
	}
	n := startNative(t, nil)
	toolsBatch := commonFlowControlBatch(t, commonFlowLimits{1, 1, 1})
	w := &commonFlowTools{NativeTools: n.tools, batch: toolsBatch, fence: n.prompt.Turn.Fence}
	if e := w.SendMessage(context.Background(), "first", "only one effect"); e != nil {
		t.Fatal(e)
	}
	if e := w.SendMessage(context.Background(), "second", "must not appear"); e == nil {
		t.Fatal("tool wrapper bypassed cap")
	}
	x := qualificationReopen(t, n.f, n.bot.Key)
	answers := 0
	for _, m := range x.Messages {
		if m.Kind == pa.AssistantMessage {
			answers++
			if m.Text != "only one effect" {
				t.Fatal("over-cap tool changed SQLite")
			}
		}
	}
	if answers != 1 {
		t.Fatal("tool effects", answers)
	}
}

// Run the SAME functional stages used by installed execution against scripted
// replies. This proves the harness can actually reach its routine/peer/group
// assertions; it is not evidence of native compatibility.
type commonFlowScriptProvider struct{ *fixtureProvider }
type commonFlowScriptSession struct {
	app.ProviderSession
	id string
}

func (s commonFlowScriptSession) SessionID() string { return s.id }
func (p commonFlowScriptProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, e := p.fixtureProvider.Open(ctx, q, sink)
	if e != nil {
		return s, e
	}
	id := q.Binding.NativeSessionID
	if !q.Resume {
		id = fmt.Sprintf("scripted-%s-%s-%d", q.Bot.Key.BotID, q.Binding.Fence.BindingID, q.Binding.Fence.Generation)
	}
	return commonFlowScriptSession{s, id}, nil
}

func TestCommonFlowControlsCompleteFunctionalStages(t *testing.T) {
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		t.Run(string(agent), func(t *testing.T) {
			root := t.TempDir()
			batch := commonFlowControlBatch(t, commonFlowFixedLimits())
			provider := &fixtureProvider{sent: make(chan app.PromptRequest, pa.MaxPendingTurns), sessions: make(chan *fixtureSession, pa.MaxPendingTurns)}
			stop := make(chan struct{})
			defer close(stop)
			errs := make(chan error, 1)
			go func() {
				for {
					select {
					case <-stop:
						return
					case n := <-provider.sessions:
						var q app.PromptRequest
						select {
						case <-stop:
							return
						case q = <-provider.sent:
						}
						if err := commonFlowScript(context.Background(), n, q, "common-"+string(agent)); err != nil {
							select {
							case errs <- err:
							default:
							}
							n.reply <- app.PromptResult{}
							return
						}
					}
				}
			}()
			commonFlowAgent(t, root, agent, &commonFlowProvider{Provider: commonFlowScriptProvider{provider}, batch: batch}, batch, false)
			select {
			case err := <-errs:
				t.Fatal(err)
			default:
			}
			v := commonFlowReadEvidence(t, batch)
			passes := 0
			for _, s := range v.Stages {
				if s.Status == "pass" {
					passes++
				} else if s.Status != "unsupported" {
					t.Fatal("functional stage incomplete", s)
				}
			}
			if passes != 9 || v.Prompts < 10 || v.Tools < 12 {
				t.Fatal("functional stages not exercised", passes, v.Prompts, v.Tools)
			}
		})
	}
}

func commonFlowScript(ctx context.Context, n *fixtureSession, q app.PromptRequest, stem string) error {
	t := n.q.Tools
	send := func(id, text string) error { return t.SendMessage(ctx, id, text) }
	final := "Done."
	var err error
	switch {
	case q.Turn.Room != nil:
		final = stem + "-room"
	case q.Turn.RoutineRunID != "":
		final = stem + "-routine"
	case q.Turn.Origin == pa.BotRequestOrigin:
		final = stem + "-peer-reviewed"
	case q.Turn.Origin == pa.BotReplyOrigin:
		err = send("peer-summary", stem+"-peer-reviewed")
	case strings.HasPrefix(q.Text, "Read your own memory with"):
		_, err = t.Memory(ctx)
		if err == nil {
			err = send("second", stem+"-approved-token "+stem+"-chat-token")
		}
	case strings.HasPrefix(q.Text, "Read your own memory"):
		_, err = t.Memory(ctx)
		if err == nil {
			err = send("first", stem+"-first")
		}
	case strings.HasPrefix(q.Text, "Use send_file"):
		p := t.(app.OutputTools)
		path := filepath.Join(n.q.Directory, "common.html")
		_, err = p.PublishOutput(ctx, "file", app.OutputRequest{Path: path, Name: "common.html"})
		if err == nil {
			_, err = p.PublishOutput(ctx, "html", app.OutputRequest{Path: path, HTML: true, Title: "Common flow"})
		}
		if err == nil {
			_, err = p.ListArtifacts(ctx)
		}
		if err == nil {
			err = send("outputs", stem+"-outputs")
		}
	case strings.HasPrefix(q.Text, "Call save_routine"):
		p := t.(app.RoutineTools)
		paused := false
		var saved app.RoutineResult
		saved, err = p.Routines(ctx, app.RoutineRequest{Action: "save", IdempotencyKey: "schedule", Name: "Common flow", Instruction: "Return exactly " + stem + "-routine as final text without tools", IntervalSeconds: 3600, Enabled: &paused})
		if err == nil {
			_, err = p.Routines(ctx, app.RoutineRequest{Action: "test", IdempotencyKey: "test", ID: saved.ID})
		}
		if err == nil {
			err = send("routine-ready", stem+"-routine-ready")
		}
	case strings.HasPrefix(q.Text, "Call list_bots"):
		p := t.(app.CollaborationTools)
		_, err = p.ListBots(ctx, pa.BotDirectoryQuery{})
		if err == nil {
			_, err = p.MessageBot(ctx, pa.BotMessageRequest{IdempotencyKey: "peer", TargetBotID: "peer-" + string(n.q.Bot.Agent), Text: "Return exactly " + stem + "-peer-reviewed as your final text"})
		}
		if err == nil {
			err = send("peer-start", stem+"-peer-start")
		}
	case strings.HasPrefix(q.Text, "Read memory using"):
		_, err = t.Memory(ctx)
		if err == nil {
			err = send("after-forget", stem+"-forgotten")
		}
	default:
		return errors.New("unhandled scripted common-flow prompt")
	}
	if err == nil {
		n.reply <- app.PromptResult{FinalText: final}
	}
	return err
}
