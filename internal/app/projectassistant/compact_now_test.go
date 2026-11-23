package projectassistant

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// compactingSession reports native context as the ACP client does: a usage
// reading for every turn, and a compaction only for a /compact prompt while
// the agent is scripted to perform one.
type compactingSession struct {
	*runtimeSession
	report *atomic.Bool
	// usageFirst scripts Codex's order: the reading after the compaction
	// arrives before its compaction_update, so the agent reports that reading
	// as the "before" and nothing after it.
	usageFirst *atomic.Bool
	mu         sync.Mutex
	turn   string
	fence  pa.Fence
	comp   bool
}

func (s *compactingSession) Send(ctx context.Context, q PromptRequest) (PendingPrompt, error) {
	s.mu.Lock()
	s.turn, s.fence, s.comp = q.Turn.ID, q.Turn.Fence, q.Compact && s.report.Load()
	s.mu.Unlock()
	return s.runtimeSession.Send(ctx, q)
}

func (s *compactingSession) NativeContext() NativeContextState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := NativeContextState{TurnID: s.turn, Fence: s.fence, NativeSessionID: s.SessionID(), UsageKnown: true, UsedTokens: 120_000, WindowTokens: 200_000}
	if s.comp {
		state.UsedTokens = 30_000
		state.CompactedTurnID, state.Compactions, state.CompactedAfter = s.turn, 1, 30_000
		if s.usageFirst != nil && s.usageFirst.Load() {
			state.CompactedBefore, state.CompactedAfter = 30_000, 0
		}
	}
	return state
}

func newCompactFixture(t *testing.T) (*runtimeFixture, *atomic.Bool) {
	f, report, _ := newOrderedCompactFixture(t)
	return f, report
}

func newOrderedCompactFixture(t *testing.T) (*runtimeFixture, *atomic.Bool, *atomic.Bool) {
	f := newRuntimeFixture(t)
	report, usageFirst := &atomic.Bool{}, &atomic.Bool{}
	report.Store(true)
	f.provider.wrap = func(s *runtimeSession) ProviderSession {
		return &compactingSession{runtimeSession: s, report: report, usageFirst: usageFirst}
	}
	return f, report, usageFirst
}

func (f *runtimeFixture) compact(id string) (pa.Receipt, error) {
	f.store.mu.Lock()
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	return f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("c", 64)},
		Message: pa.SendMessage{Fence: fence, Text: compactRequestText}, Feature: pa.FeatureState{Enabled: true}, Source: f.source, Compact: true})
}

// finishOrdinaryTurn leaves the Bot idle with a native session to compact.
func finishOrdinaryTurn(t *testing.T, f *runtimeFixture, text string) {
	t.Helper()
	a, err := f.submit(t, text, "turn-"+text)
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "ok"}
	awaitTurn(t, f, a.ResourceID, pa.TurnSucceeded)
	eventually(t, func() bool { f.store.mu.Lock(); defer f.store.mu.Unlock(); return f.store.c.ActiveTurnID == "" })
}

func TestCompactNowIsRefusedUnlessTheBotIsIdleWithASession(t *testing.T) {
	f, _ := newCompactFixture(t)
	if _, err := f.compact("before-session"); pa.Code(err) != pa.Unsupported || !strings.Contains(err.Error(), compactNoSession) {
		t.Fatalf("compacting a Bot with no native session: %v", err)
	}
	finishOrdinaryTurn(t, f, "one")
	running, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	if _, err = f.compact("while-running"); pa.Code(err) != pa.CapacityFull || !strings.Contains(err.Error(), compactBusy) {
		t.Fatalf("compacting a working Bot: %v", err)
	}
	f.provider.session().reply <- PromptResult{FinalText: "ok"}
	awaitTurn(t, f, running.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	for _, turn := range f.store.turns {
		if turn.Origin == pa.CompactOrigin {
			t.Fatal("a refused compaction left a turn")
		}
	}
}

func TestCompactNowSendsOnlyTheCommandAndTheBotKeepsReceiving(t *testing.T) {
	f, _ := newCompactFixture(t)
	finishOrdinaryTurn(t, f, "one")
	receipt, err := f.compact("compact-1")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	if !q.Compact || q.Text != compactCommand || q.Turn.Origin != pa.CompactOrigin || q.Handoff != nil || q.MemoryChanged || q.Compacted {
		t.Fatalf("compact prompt carried more than the command: %+v", q)
	}
	opened := f.provider.opens[len(f.provider.opens)-1]
	if !opened.Resume {
		t.Fatal("a compaction opened a fresh session")
	}
	f.provider.session().reply <- PromptResult{FinalText: "Compacted. The agent's own words."}
	awaitTurn(t, f, receipt.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	var notice *pa.Message
	for i, m := range f.store.messages {
		if m.ID == compactionNoticePrefix+receipt.ResourceID {
			notice = &f.store.messages[i]
		}
		if m.ID == "reply-"+receipt.ResourceID {
			t.Fatal("the agent's compaction output became a chat reply")
		}
	}
	f.store.mu.Unlock()
	if notice == nil || !strings.Contains(notice.Text, "120K → 30K") {
		t.Fatalf("summarized notice with before and after: %+v", notice)
	}

	// The Bot keeps receiving: the next message opens a new client and
	// repeats memory once, as after the agent's own compaction.
	next, err := f.submit(t, "after", "after")
	if err != nil {
		t.Fatal(err)
	}
	q = receivePrompt(t, f.provider)
	if q.Text != "after" || !q.Compacted || q.Compact {
		t.Fatalf("message after compaction: %+v", q)
	}
	f.provider.session().reply <- PromptResult{FinalText: "fine"}
	awaitTurn(t, f, next.ResourceID, pa.TurnSucceeded)
	if f.provider.count() != 3 {
		t.Fatalf("opens=%d, want one client per turn", f.provider.count())
	}
}

func TestCompactNowThatReportsNothingFailsTypedAndDoesNotBlock(t *testing.T) {
	f, report := newCompactFixture(t)
	finishOrdinaryTurn(t, f, "one")
	report.Store(false)
	receipt, err := f.compact("compact-silent")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "I could not compact."}
	awaitTurn(t, f, receipt.ResourceID, pa.TurnFailed)
	turn, _ := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
	if turn.Failure == nil || turn.Failure.Code != pa.Unsupported || turn.Failure.Field != compactNotReported {
		t.Fatalf("failure: %+v", turn.Failure)
	}
	next, err := f.submit(t, "after", "after")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	if q.Compacted {
		t.Fatal("memory was repeated for a compaction that did not happen")
	}
	f.provider.session().reply <- PromptResult{FinalText: "fine"}
	awaitTurn(t, f, next.ResourceID, pa.TurnSucceeded)
}

func TestCompactNowWithTheSameKeyRunsOnce(t *testing.T) {
	f, _ := newCompactFixture(t)
	finishOrdinaryTurn(t, f, "one")
	first, err := f.compact("compact-once")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	again, err := f.compact("compact-once")
	if err != nil || again.ResourceID != first.ResourceID {
		t.Fatalf("repeated key: %+v %v", again, err)
	}
	f.provider.session().reply <- PromptResult{}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	if after, err := f.compact("compact-once"); err != nil || after.ResourceID != first.ResourceID {
		t.Fatalf("repeated key after completion: %+v %v", after, err)
	}
	select {
	case q := <-f.provider.sent:
		t.Fatalf("a repeated key sent another prompt: %+v", q)
	default:
	}
	if f.provider.count() != 2 {
		t.Fatalf("opens=%d", f.provider.count())
	}
}

// Live Codex (codex-acp 2.6.0, 2026-11-23) sent the post-compaction usage
// before compaction_update, so the notice said only "summarized" although the
// meter had read 28.7K before and 27.4K after. The before of a manual
// compaction is the reading the person saw before asking; the after is the
// turn's last reading.
func TestCompactNowShowsBeforeAndAfterWhenUsageArrivesFirst(t *testing.T) {
	f, _, usageFirst := newOrderedCompactFixture(t)
	finishOrdinaryTurn(t, f, "one")
	usageFirst.Store(true)
	receipt, err := f.compact("compact-usage-first")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{}
	awaitTurn(t, f, receipt.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	for _, m := range f.store.messages {
		if m.ID == compactionNoticePrefix+receipt.ResourceID {
			if !strings.Contains(m.Text, "120K → 30K") {
				t.Fatalf("notice without before and after: %q", m.Text)
			}
			return
		}
	}
	t.Fatal("no summarized notice")
}
