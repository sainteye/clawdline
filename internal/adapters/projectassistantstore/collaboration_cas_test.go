package projectassistantstore

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Interleave a real two-Bot SQL admission after the source worker has built
// its local mutation. The failed CAS must rebuild only local facts, never IO.
type collaborationRaceStore struct {
	*Store
	stage    string
	injected atomic.Bool
}

func (s *collaborationRaceStore) Commit(ctx context.Context, m app.Mutation) (pa.Receipt, error) {
	match := s.stage == "finish" && strings.HasPrefix(m.Command.IdempotencyKey, "runtime-finish-") || s.stage == "event" && strings.HasPrefix(m.Command.IdempotencyKey, "runtime-event-")
	if m.Command.Key.BotID == "a" && match && s.injected.CompareAndSwap(false, true) {
		a, err := s.Export(ctx, m.Command.Key)
		if err != nil {
			return pa.Receipt{}, err
		}
		b, err := s.Export(ctx, pa.BotKey{MachineID: m.Command.Key.MachineID, BotID: "b"})
		if err != nil {
			return pa.Receipt{}, err
		}
		x := pa.Exchange{ID: "interleaved-review", Project: a.Bot.Project, Source: pa.ExchangeEndpoint{Key: a.Bot.Key, ConversationID: a.Conversation.ID}, Target: pa.ExchangeEndpoint{Key: b.Bot.Key, ConversationID: b.Conversation.ID}, SourceTurnID: a.Conversation.ActiveTurnID, RequestTurnID: "interleaved-request", Request: "Review the synthetic change.", Outcome: pa.ExchangeQueued, Revision: 1, Hops: 1}
		_, err = s.AdmitExchange(ctx, app.ExchangeAdmission{SourceFence: exchangeFence(aggregate{Bot: a.Bot, Conversation: a.Conversation}), SourceTurnID: x.SourceTurnID, SourceVersion: a.Bot.Version, TargetVersion: b.Bot.Version, Command: command(a.Bot.Key, pa.SendOperation, "interleaved-admit"), Exchange: x, At: time.Now()})
		if err != nil {
			return pa.Receipt{}, err
		}
	}
	return s.Store.Commit(ctx, m)
}

func TestCollaborationSourceCASRebuildsFactsWithoutAnotherSend(t *testing.T) {
	for _, stage := range []string{"finish", "event"} {
		t.Run(stage, func(t *testing.T) {
			_, store, _ := testDB(t)
			a := seed(t, store, "a")
			seed(t, store, "b")
			repo := &collaborationRaceStore{Store: store, stage: stage}
			peer := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult, 1)}
			var ids atomic.Int64
			r, err := app.NewRuntime(app.RuntimeOptions{Repository: repo, Memory: store, WorkBridge: peer, ProjectResolver: recoveryResolver{a.Project}, Provider: peer, Clock: time.Now, ID: func() string { return fmt.Sprintf("race-%d", ids.Add(1)) }, Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
				return pa.FeatureState{Enabled: true}, recoverySource(a.Key), nil
			}, Limits: app.RuntimeLimits{MaxBots: 2, MaxProcesses: 1, PromptTimeout: 3 * time.Second, CleanupTimeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Close(testContext); err != nil {
					t.Error(err)
				}
			})
			receipt, err := r.Submit(testContext, recoverySubmission(t, store, a, "input"))
			if err != nil {
				t.Fatal(err)
			}
			var prompt app.PromptRequest
			select {
			case prompt = <-peer.sent:
			case <-time.After(3 * time.Second):
				t.Fatal("no native Send")
			}
			if stage == "event" {
				event := app.ProtocolEvent{ID: "visible", Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, Message: "One answer."}
				if err = r.Publish(testContext, event); err != nil {
					t.Fatalf("interleaved event lost: %v", err)
				}
				if err = r.Publish(testContext, event); err != nil {
					t.Fatalf("event replay: %v", err)
				}
			}
			peer.result <- app.PromptResult{FinalText: "One answer."}
			recoveryAwait(t, func() bool {
				turn, e := store.Turn(testContext, a.Key, receipt.ResourceID)
				return e == nil && (turn.State == pa.TurnSucceeded || turn.State == pa.TurnUnknown)
			})
			x, err := store.Export(testContext, a.Key)
			if err != nil {
				t.Fatal(err)
			}
			if len(x.Turns) != 1 || x.Turns[0].State != pa.TurnSucceeded || len(x.Exchanges) != 1 || !repo.injected.Load() {
				t.Fatalf("completion lost to append: turns=%+v exchanges=%+v", x.Turns, x.Exchanges)
			}
			var users, answers, notices int
			for i, m := range x.Messages {
				if m.Seq != int64(i+1) {
					t.Fatal("sequence", x.Messages)
				}
				switch m.Kind {
				case pa.UserMessage:
					users++
				case pa.AssistantMessage:
					answers++
				case pa.NoticeMessage:
					notices++
				}
			}
			if users != 1 || answers != 1 || notices != 1 || peer.opens.Load() != 1 || peer.sends.Load() != 1 {
				t.Fatal("duplicate or lost effect", users, answers, notices, peer.opens.Load(), peer.sends.Load())
			}
			b, err := store.Export(testContext, pa.BotKey{MachineID: a.Key.MachineID, BotID: "b"})
			if err != nil || len(b.Turns) != 1 || b.Turns[0].State != pa.TurnQueued {
				t.Fatal("other Bot facts lost", b.Turns, err)
			}
		})
	}
}
