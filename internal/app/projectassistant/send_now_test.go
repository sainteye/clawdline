package projectassistant

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// steerSession is a native session that steers as told. native counts the
// steers that reached the agent.
type steerSession struct {
	*runtimeSession
	refuse     error
	outcome    SteerOutcome
	native     *atomic.Int32
	noImages   bool
	noSteering bool
	afterWrite func()
	// lose: the write reaches the agent and its answer is lost.
	lose bool
}

func TestAcceptedFollowUpSettlesAfterTheRequestDisconnects(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx, disconnect := context.WithCancel(t.Context())
	defer disconnect()
	native := &atomic.Int32{}
	f.provider.wrap = func(s *runtimeSession) ProviderSession {
		return &steerSession{runtimeSession: s, native: native, outcome: SteerJoined, afterWrite: disconnect}
	}
	first, err := f.submit(t, "first", "first")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	submission := Submission{
		Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "disconnect", ContentHash: strings.Repeat("a", 64)},
		Message: pa.SendMessage{Fence: f.fence(), Text: "follow up"}, Feature: pa.FeatureState{Enabled: true}, Source: f.source,
		AuthorityCheck: func(ctx context.Context) error { return ctx.Err() },
	}
	receipt, err := f.r.Submit(ctx, submission)
	if err != nil || receipt.State != pa.EffectAccepted || ctx.Err() != context.Canceled {
		t.Fatalf("expected admission followed by disconnect: state=%s request=%v err=%v", receipt.State, ctx.Err(), err)
	}
	turn := f.turn(t, receipt.ResourceID)
	if turn.State != pa.TurnSucceeded || turn.JoinedTurnID != first.ResourceID || turn.Steering {
		t.Fatalf("accepted follow-up did not join: state=%s joined=%q steering=%t", turn.State, turn.JoinedTurnID, turn.Steering)
	}
	replay, err := f.r.Submit(t.Context(), submission)
	if err != nil || replay != receipt || native.Load() != 1 {
		t.Fatalf("retry must replay only the receipt: state=%s native writes=%d err=%v", replay.State, native.Load(), err)
	}
}

func TestPersonFollowUpSteersByBotPreference(t *testing.T) {
	for _, tt := range []struct {
		name         string
		preference   pa.ReplyInput
		permission   bool
		notSupported bool
		noSteering   bool
		refuse       error
		lose         bool
		wantJoined   bool
		wantWrites   int32
		wantFallback pa.SteerFallback
		outcome      SteerOutcome
	}{
		{name: "Bot with no saved choice", wantJoined: true, outcome: SteerJoined},
		{name: "working Bot with pictures", preference: pa.ReplyInputSteer, wantJoined: true, outcome: SteerJoined},
		{name: "Bot set to queue", preference: pa.ReplyInputQueue, outcome: SteerJoined},
		{name: "permission card", permission: true, outcome: SteerJoined, wantFallback: pa.SteerFallbackPermissionPending},
		{name: "unverified picture build", notSupported: true, outcome: SteerJoined, wantFallback: pa.SteerFallbackImagesUnsupported},
		{name: "agent without steering", noSteering: true, outcome: SteerJoined, wantFallback: pa.SteerFallbackAgentUnsupported},
		{name: "adapter deferred input", outcome: SteerNext, wantWrites: 1, wantFallback: pa.SteerFallbackTurnEnded},
		{name: "refused unread", refuse: SteerRefused(errors.New("pipe closed before the write")), wantFallback: pa.SteerFallbackNotSent},
		{name: "refused as unsupported", refuse: SteerRefused(runtimeFailure(pa.Unsupported, "steering")), wantFallback: pa.SteerFallbackImagesUnsupported},
		{name: "answer lost after the write", lose: true, wantWrites: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.store.bot.ReplyInput = tt.preference
			native := &atomic.Int32{}
			f.provider.wrap = func(s *runtimeSession) ProviderSession {
				return &steerSession{runtimeSession: s, native: native, outcome: tt.outcome, noImages: tt.notSupported, noSteering: tt.noSteering, refuse: tt.refuse, lose: tt.lose}
			}
			first, err := f.submit(t, "first", "first")
			if err != nil {
				t.Fatal(err)
			}
			prompt := receivePrompt(t, f.provider)
			if tt.permission {
				offer := pa.PermissionOffer{ID: "offer", TurnID: prompt.Turn.ID, Fence: prompt.Turn.Fence,
					NativeRequestID: "native", Options: []pa.PermissionOption{{ID: "allow", Label: "Allow once", Kind: pa.AllowOnce, Allow: true}}}
				if err := f.provider.session().sink(t.Context(), ProtocolEvent{ID: "offer", Fence: offer.Fence, TurnID: offer.TurnID, Permission: &offer}); err != nil {
					t.Fatal(err)
				}
			}
			picture := []byte("synthetic picture")
			id := strings.Repeat("42", 32)
			f.r.o.Attachments = runtimeAttachments{id: picture}
			submission := Submission{
				Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "follow-up", ContentHash: strings.Repeat("a", 64)},
				Message: pa.SendMessage{Fence: f.fence(), Text: "follow up",
					Attachments: []pa.Attachment{{ID: id, MediaType: "image/png", Bytes: int64(len(picture))}}},
				Feature: pa.FeatureState{Enabled: true}, Source: f.source, AuthorityCheck: func(context.Context) error { return nil },
			}
			receipt, err := f.r.Submit(t.Context(), submission)
			if err != nil {
				t.Fatalf("a follow-up that fell back must not report an error: %v", err)
			}
			turn := f.turn(t, receipt.ResourceID)
			// The same message delivered twice reaches the agent at most once.
			replay, err := f.r.Submit(t.Context(), submission)
			if err != nil || replay != receipt {
				t.Fatalf("idempotent retry: %+v, %v", replay, err)
			}
			if tt.wantJoined {
				if turn.State != pa.TurnSucceeded || turn.JoinedTurnID != first.ResourceID || native.Load() != 1 || turn.SteerFallback != "" {
					t.Fatalf("follow-up did not join the active turn: %+v, native writes %d", turn, native.Load())
				}
				return
			}
			if tt.lose {
				if turn.State != pa.TurnUnknown || turn.Failure == nil || turn.Failure.Field != pa.SteerUnknownField || native.Load() != 1 {
					t.Fatalf("a lost answer must be unknown, never resent: %+v, native writes %d", turn, native.Load())
				}
				return
			}
			if turn.State != pa.TurnQueued || turn.Steering || turn.JoinedTurnID != "" || turn.Failure != nil || native.Load() != tt.wantWrites {
				t.Fatalf("the entire message must queue: %+v, native writes %d", turn, native.Load())
			}
			if turn.SteerFallback != tt.wantFallback {
				t.Fatalf("steer_fallback %q, want %q", turn.SteerFallback, tt.wantFallback)
			}
			f.provider.session().reply <- PromptResult{FinalText: "first"}
			next := receivePrompt(t, f.provider)
			if next.Turn.ID != turn.ID || next.Text != "follow up" || len(next.Images) != 1 || !bytes.Equal(next.Images[0].Data, picture) {
				t.Fatalf("queued follow-up lost content: %+v", next)
			}
			if native.Load() != tt.wantWrites {
				t.Fatalf("the queued message was also steered: native writes %d", native.Load())
			}
		})
	}
}

// Only the person's own words join a reply. Clawdline's notices and input
// without the sender's authority wait for their own turn even when the Bot
// steers; personMessage also excludes rooms, other Bots' requests,
// compaction and lead calls.
func TestOnlyThePersonsMessagesSteer(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*Submission)
	}{
		{name: "host notice", change: func(s *Submission) { s.Notice, s.AuthorityCheck = true, nil }},
		{name: "no sender authority", change: func(s *Submission) { s.AuthorityCheck = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			native := &atomic.Int32{}
			f.provider.wrap = func(s *runtimeSession) ProviderSession {
				return &steerSession{runtimeSession: s, native: native, outcome: SteerJoined}
			}
			if _, err := f.submit(t, "first", "first"); err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			s := Submission{
				Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "other", ContentHash: strings.Repeat("c", 64)},
				Message: pa.SendMessage{Fence: f.fence(), Text: "not the person"},
				Feature: pa.FeatureState{Enabled: true}, Source: f.source, AuthorityCheck: func(context.Context) error { return nil },
			}
			tt.change(&s)
			receipt, err := f.r.Submit(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			if turn := f.turn(t, receipt.ResourceID); native.Load() != 0 || turn.State != pa.TurnQueued || turn.SteerFallback != "" {
				t.Fatalf("a message that is not the person's was steered: %+v, native writes %d", turn, native.Load())
			}
		})
	}
}

func (s *steerSession) SupportsSteering(images bool) bool {
	return !s.noSteering && (!images || !s.noImages)
}

func (s *steerSession) Steer(ctx context.Context, _ PromptRequest) (SteerOutcome, error) {
	if s.refuse != nil {
		return "", s.refuse
	}
	s.native.Add(1)
	if s.afterWrite != nil {
		s.afterWrite()
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	if s.lose {
		return "", errors.New("connection reset after the write")
	}
	return s.outcome, nil
}

// steerFixture is a fixture whose first message is running and whose second
// waits in the queue behind it.
func steerFixture(t *testing.T, refuse error, outcome SteerOutcome) (f *runtimeFixture, running, queued string, native *atomic.Int32, before pa.Fence) {
	t.Helper()
	f = newRuntimeFixture(t)
	native = &atomic.Int32{}
	f.provider.wrap = func(s *runtimeSession) ProviderSession {
		return &steerSession{runtimeSession: s, refuse: refuse, outcome: outcome, native: native}
	}
	before = f.fence()
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	second, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	return f, first.ResourceID, second.ResourceID, native, before
}

func (f *runtimeFixture) fence() pa.Fence {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	return currentFence(f.store.bot, f.store.c)
}

func (f *runtimeFixture) sendNow(id string, fence pa.Fence, turnID string) (pa.Receipt, error) {
	return f.r.SendNow(context.Background(), SendNowRequest{Command: pa.Command{Key: f.key, Operation: pa.SendNowOperation, IdempotencyKey: id, ContentHash: strings.Repeat("b", 64)}, Fence: fence, TurnID: turnID})
}

func (f *runtimeFixture) turn(t *testing.T, id string) pa.Turn {
	t.Helper()
	x, err := f.store.Turn(context.Background(), f.key, id)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func failureField(err error) (pa.ErrorCode, string) {
	var f *pa.Failure
	if errors.As(err, &f) {
		return f.Code, f.Field
	}
	return pa.Code(err), ""
}

// An agent that refuses before anything reaches it leaves the message in the
// queue: no unknown outcome, no halted worker, and the page is told the agent
// cannot steer.
func TestSendNowRefusedBeforeTheAgentKeepsTheMessageQueued(t *testing.T) {
	f, running, queued, native, _ := steerFixture(t, SteerRefused(runtimeFailure(pa.Unsupported, "steering")), "")
	_, err := f.sendNow("now", f.fence(), queued)
	if code, field := failureField(err); code != pa.Unsupported || field != SteeringField {
		t.Fatalf("a refused steer answered %v", err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnQueued || turn.Steering || turn.Failure != nil {
		t.Fatalf("the refused message left the queue: %+v", turn)
	}
	if native.Load() != 0 {
		t.Fatal("the agent received the refused message")
	}
	// The worker carries on: the message runs in its own turn.
	f.provider.session().reply <- PromptResult{FinalText: "first"}
	awaitTurn(t, f, running, pa.TurnSucceeded)
	if q := receivePrompt(t, f.provider); q.Turn.ID != queued || q.Text != "two" {
		t.Fatalf("the queued message did not run next: %+v", q)
	}
	// The same press can come again without colliding with its first records.
	f.provider.session().reply <- PromptResult{FinalText: "second"}
	awaitTurn(t, f, queued, pa.TurnSucceeded)
}

// A press that arrives after its message started answers that it is running.
func TestSendNowOnAMessageAlreadyRunningSucceeds(t *testing.T) {
	f, running, _, native, _ := steerFixture(t, nil, SteerJoined)
	receipt, err := f.sendNow("now", f.fence(), running)
	if err != nil || receipt.State != pa.EffectAccepted || receipt.ResourceID != running {
		t.Fatalf("Send now on a running message: %+v %v", receipt, err)
	}
	if native.Load() != 0 {
		t.Fatal("a running message was steered into itself")
	}
}

// A press that arrives as the running turn ends answers that the message runs
// next, not that Send now is unsupported.
func TestSendNowAsTheRunningTurnEndsSaysTheMessageRunsNext(t *testing.T) {
	f, _, queued, native, _ := steerFixture(t, nil, SteerJoined)
	f.store.mu.Lock()
	f.store.c.ActiveTurnID = ""
	f.store.mu.Unlock()
	receipt, err := f.sendNow("now", f.fence(), queued)
	if err != nil || receipt.State != pa.EffectAccepted || receipt.ResourceID != queued {
		t.Fatalf("Send now as the turn ended: %+v %v", receipt, err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnQueued || turn.Steering || native.Load() != 0 {
		t.Fatalf("the message did not stay queued untouched: %+v", turn)
	}
}

// Every turn start mints a generation, so a phone that read the queue before
// the running turn started still names the same chat; that is enough.
func TestSendNowAcceptsAFenceFromBeforeTheRunningTurnStarted(t *testing.T) {
	f, running, queued, native, before := steerFixture(t, nil, SteerJoined)
	if before.Generation == f.fence().Generation {
		t.Fatal("the fixture did not mint a generation")
	}
	receipt, err := f.sendNow("now", before, queued)
	if err != nil || receipt.State != pa.EffectDelivered {
		t.Fatalf("Send now with an earlier generation: %+v %v", receipt, err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnSucceeded || turn.JoinedTurnID != running || native.Load() != 1 {
		t.Fatalf("the message did not join the running turn: %+v", turn)
	}
	other := before
	other.ConversationID = "another-chat"
	if _, err = f.sendNow("elsewhere", other, queued); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a fence naming another chat answered %v", err)
	}
}

// A press while an earlier Agent process could not be confirmed stopped was
// never accepted; it says busy, not that its effect is unknown.
func TestSendNowAndQueueEditBehindARetainedCleanupAreNotAccepted(t *testing.T) {
	f, p := lifecycleFixture(t)
	p.closeUnknown.Store(true)
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { return !f.r.Usage().Known })
	if _, err = f.sendNow("now", f.fence(), first.ResourceID); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("Send now behind a retained cleanup answered %v", err)
	}
	if _, err = f.editQueue("begin", f.fence(), first.ResourceID, "", "begin", ""); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("a queue edit behind a retained cleanup answered %v", err)
	}
}
