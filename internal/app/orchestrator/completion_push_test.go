package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestCompletionPushUsesSessionAndPreference(t *testing.T) {
	var pushes []string
	b := &Broker{
		PushEnabled: func(key string) bool { return key != "smart_notifications" },
		Push: func(_ context.Context, _, body, terminal, _ string) (int, int, error) {
			pushes = append(pushes, terminal+":"+body)
			return 1, 0, nil
		},
	}
	ctx := context.Background()
	e := store.Effect{Kind: EffectSessionDeliveryPush, Subject: "%1", Payload: []byte(`{"terminal":"%1","tag":"delivery","summary":"The work is ready.","language":"en"}`)}
	if got := runCompletionPush(ctx, b, e); got.state != store.EffectDone || len(pushes) != 1 || !strings.HasPrefix(pushes[0], "%1:") || strings.Contains(pushes[0], "The work is ready") {
		t.Fatalf("generic delivery push = %+v, %v", got, pushes)
	}
	b.PushEnabled = func(string) bool { return true }
	if got := runCompletionPush(ctx, b, e); got.state != store.EffectDone || !strings.Contains(pushes[1], "The work is ready") {
		t.Fatalf("detailed delivery push = %+v, %v", got, pushes)
	}
	b.PushEnabled = func(key string) bool { return key != "push_on_delivery" }
	if got := runCompletionPush(ctx, b, e); got.outcome != "disabled_by_user" || len(pushes) != 2 {
		t.Fatalf("disabled delivery = %+v, %v", got, pushes)
	}
}

// A report is a tool call before the final answer. Neither recording the
// receipt nor recovery while the answer is streaming may buzz the phone.
func TestSessionDeliveryWaitsForFreshIdleAfterReport(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Now().UTC()
	b.Clock = func() time.Time { return now }
	row := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation,
		State: session.StateWorking, Evidence: session.EvidenceRegistry,
		Observation: session.Observation{ObservedAt: now, Freshness: session.FreshnessCurrent}}
	b.Live = func(context.Context) []session.Session { return []session.Session{row} }
	var pushes int
	b.PushEnabled = func(string) bool { return true }
	b.Push = func(_ context.Context, _, body, terminal, _ string) (int, int, error) {
		if terminal != "%1" || body != "The release is ready." {
			t.Fatalf("push = %q %q", terminal, body)
		}
		pushes++
		return 1, 0, nil
	}
	receipt, err := b.ReportSessionDelivery(ctx, "%1", "The release is ready.")
	if err != nil || !receipt.Created {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if pushes != 0 {
		t.Fatal("pushed before the final answer was printed")
	}
	events, err := b.Store.LatestEvents(ctx, "session.delivered", []string{"%1"})
	if err != nil || len(events) != 1 {
		t.Fatalf("receipt was not durable: %v %v", events, err)
	}
	// A fixed delay, including the unattended-effect recovery delay, is not
	// evidence that a long final answer finished.
	now = now.Add(10 * time.Minute)
	b.RecoverEffects(ctx)
	if pushes != 0 {
		t.Fatal("recovery pushed while the answer was still streaming")
	}
	// Idle cached before the report, retained after a scan failure, and idle
	// inferred from process presence all fail to prove output completion.
	row.State = session.StateIdle
	b.RecoverEffects(ctx)
	if pushes != 0 {
		t.Fatal("pre-report idle reading released the push")
	}
	row.Observation.ObservedAt = now
	row.Observation.Freshness = session.FreshnessUnverified
	b.RecoverEffects(ctx)
	if pushes != 0 {
		t.Fatal("retained idle reading released the push")
	}
	row.Observation.Freshness = session.FreshnessCurrent
	row.Evidence = session.EvidenceProcess
	b.RecoverEffects(ctx)
	if pushes != 0 {
		t.Fatal("process presence released the push")
	}
	row.Evidence = session.EvidenceRegistry
	for _, state := range []session.State{session.StateWorking, session.StateWaiting, session.StateUnknown} {
		row.State = state
		b.RecoverEffects(ctx)
		if pushes != 0 {
			t.Fatalf("%s released the push", state)
		}
	}
	row.State = session.StateIdle
	b.Pass(ctx)
	b.Pass(ctx)
	if pushes != 1 {
		t.Fatalf("finished answer pushed %d times, want once", pushes)
	}
	effects, err := b.Store.Effects(ctx, EffectSessionDeliveryPush, "%1")
	if err != nil || len(effects) != 1 || effects[0].Attempts != 1 || effects[0].State != store.EffectDone {
		t.Fatalf("outbox = %+v, %v", effects, err)
	}
}

func TestBatchPushWaitsForLastChildAndSendsOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	var pushes []string
	b.Push = func(_ context.Context, _, body, terminal, _ string) (int, int, error) {
		pushes = append(pushes, terminal+":"+body)
		return 1, 0, nil
	}
	b.PushEnabled = func(string) bool { return true }
	for _, id := range []string{"task-one", "task-two"} {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: id,
			State: StateBriefed, CreatedAt: time.Now(), Claims: []string{},
			Root:           &RootRef{SessionID: rootConversation, Assistant: "claude"},
			RootTerminalID: "%1", BatchID: "batch-one"}
		if err := b.save(ctx, r, HashSecret("secret"), "task.queued"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Settle(ctx, "task-one", StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 0 {
		t.Fatalf("batch pushed before its last child: %v", pushes)
	}
	if _, err := b.Settle(ctx, "task-two", StateFailure, "failed", nil); err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 1 || !strings.HasPrefix(pushes[0], "%1:") {
		t.Fatalf("batch completion pushes = %v", pushes)
	}
	if _, err := b.Settle(ctx, "task-two", StateFailure, "failed", nil); err == nil || len(pushes) != 1 {
		t.Fatalf("duplicate settlement pushed again: %v, %v", err, pushes)
	}
}

func TestDeliveryPushRecoveryKeepsTheTurnBoundary(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "attempted"}[started], func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			earlier, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC()
			payload, _ := json.Marshal(completionPush{Terminal: "%1", Conversation: rootConversation, After: at, Tag: "delivery"})
			ids, err := earlier.RecordIntent(ctx, nil, []store.Effect{{Kind: EffectSessionDeliveryPush, Subject: "%1", Payload: payload}})
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if _, err := earlier.StartEffect(ctx, ids[0]); err != nil {
					t.Fatal(err)
				}
			}
			if err := earlier.Close(); err != nil {
				t.Fatal(err)
			}
			later, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer later.Close()
			row := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation,
				State: session.StateWorking, Evidence: session.EvidenceRegistry,
				Observation: session.Observation{ObservedAt: at.Add(time.Second), Freshness: session.FreshnessCurrent}}
			pushes := 0
			b := &Broker{Store: later, Live: func(context.Context) []session.Session { return []session.Session{row} },
				Push: func(context.Context, string, string, string, string) (int, int, error) { pushes++; return 1, 0, nil }}
			b.RecoverEffects(ctx)
			if pushes != 0 {
				t.Fatal("restart pushed before the final answer ended")
			}
			row.State = session.StateIdle
			b.RecoverEffects(ctx)
			b.RecoverEffects(ctx)
			want, state := 1, store.EffectDone
			if started {
				want, state = 0, store.EffectUnknown
			}
			if pushes != want {
				t.Fatalf("pushes=%d, want %d", pushes, want)
			}
			effects, err := later.Effects(ctx, EffectSessionDeliveryPush, "%1")
			if err != nil || len(effects) != 1 || effects[0].State != state {
				t.Fatalf("outbox=%+v, %v", effects, err)
			}
		})
	}
}

func TestDeliveryPushDoesNotFollowAReplacedOrMissingSession(t *testing.T) {
	for _, scenario := range []string{"rebound", "missing", "unbound", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			at := time.Now().UTC()
			b.Clock = func() time.Time { return at }
			pushes := 0
			b.Push = func(context.Context, string, string, string, string) (int, int, error) { pushes++; return 1, 0, nil }
			if _, err := b.ReportSessionDelivery(ctx, "%1", "Finished."); err != nil {
				t.Fatal(err)
			}
			at = at.Add(time.Second)
			row := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation,
				State: session.StateIdle, Evidence: session.EvidenceRegistry,
				Observation: session.Observation{ObservedAt: at, Freshness: session.FreshnessCurrent}}
			b.Live = nil
			inv := session.Inventory{Sessions: []session.Session{row}}
			b.Reading = func(context.Context) session.Inventory { return inv }
			outcome := ""
			switch scenario {
			case "rebound":
				inv.Sessions[0].ConversationID = "another-conversation"
				outcome = "session_rebound"
			case "missing":
				inv.Sessions = nil
			case "unbound":
				inv.Sessions[0].ConversationID = ""
			case "legacy":
				inv.Sessions[0].ConversationID = ""
				payload := []byte(`{"terminal":"%1","tag":"legacy"}`)
				if _, err := b.Store.RecordIntent(ctx, nil, []store.Effect{{Kind: EffectSessionDeliveryPush, Subject: "legacy", Payload: payload}}); err != nil {
					t.Fatal(err)
				}
			}
			b.RecoverEffects(ctx)
			if scenario == "missing" {
				// Incomplete absence held it. A complete reading of the owning source
				// proves it is gone, even if another source is unavailable.
				effects, _ := b.Store.Effects(ctx, EffectSessionDeliveryPush, "%1")
				if len(effects) != 1 || effects[0].State != store.EffectPending {
					t.Fatalf("incomplete absence = %+v", effects)
				}
				inv.Sources = map[string]bool{"tmux": true}
				b.RecoverEffects(ctx)
				outcome = "session_gone"
			}
			if pushes != 0 {
				t.Fatal("pushed to a replacement, absent or unbound Session")
			}
			effects, _ := b.Store.Effects(ctx, EffectSessionDeliveryPush, "%1")
			if outcome != "" && (len(effects) != 1 || effects[0].Outcome != outcome) {
				t.Fatalf("outbox=%+v, want %s", effects, outcome)
			}
			if scenario == "legacy" {
				effects, _ := b.Store.Effects(ctx, EffectSessionDeliveryPush, "legacy")
				if len(effects) != 1 || effects[0].Outcome != "session_binding_missing" {
					t.Fatalf("legacy=%+v", effects)
				}
			}
		})
	}
}

func TestDeliveryPushConcurrentRecoverySendsOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	at := time.Now().UTC()
	b.Clock = func() time.Time { return at }
	if _, err := b.ReportSessionDelivery(ctx, "%1", "Finished."); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation,
			State: session.StateIdle, Evidence: session.EvidenceRegistry, Observation: session.Observation{ObservedAt: at, Freshness: session.FreshnessCurrent}}}
	}
	rows, err := b.Store.Effects(ctx, EffectSessionDeliveryPush, "%1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("outbox=%+v, %v", rows, err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	b.Push = func(context.Context, string, string, string, string) (int, int, error) {
		close(entered)
		<-release
		return 1, 0, nil
	}
	go func() { defer close(done); b.RecoverEffects(ctx) }()
	<-entered
	// A second recovery may already have read the pending row before the
	// first claimed it. It must neither resend nor clear the first hold.
	b.settleDeliveryPush(ctx, rows[0], b.read(ctx))
	b.RecoverEffects(ctx)
	effects, _ := b.Store.Effects(ctx, EffectSessionDeliveryPush, "%1")
	close(release)
	<-done
	if len(effects) != 1 || effects[0].Attempts != 1 {
		t.Fatalf("concurrent outbox=%+v", effects)
	}
	b.RecoverEffects(ctx)
}
