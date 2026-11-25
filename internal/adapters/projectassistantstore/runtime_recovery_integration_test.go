package projectassistantstore

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRuntimeSQLiteFreshContinuation(t *testing.T) {
	for _, topic := range []string{"Research https://example.invalid/topic-alpha", "Banner: 1600 by 900, violet mountains, title Aurora"} {
		for _, interleaved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/interleaved=%t", topic, interleaved), func(t *testing.T) {
				db, s, path := testDB(t)
				b := seed(t, s, "continuation")
				p := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult, 1)}
				r := recoveryEngine(t, s, b, p)
				send := func(id, text string) app.PromptRequest {
					t.Helper()
					input := recoverySubmission(t, s, b, id)
					input.Message.Text = text
					receipt, err := r.Submit(testContext, input)
					if err != nil {
						t.Fatal(err)
					}
					var q app.PromptRequest
					select {
					case q = <-p.sent:
					case <-time.After(3 * time.Second):
						t.Fatal("no actual provider prompt")
					}
					p.result <- app.PromptResult{FinalText: "Acknowledged " + id}
					recoveryAwait(t, func() bool {
						x, e := s.Export(testContext, b.Key)
						if e != nil {
							return false
						}
						for _, turn := range x.Turns {
							if turn.ID == receipt.ResourceID {
								return turn.State == pa.TurnSucceeded
							}
						}
						return false
					})
					return q
				}
				send("topic", topic)
				rounds := 2
				followup := "Please start"
				if interleaved {
					send("unrelated", "Also, hello")
					rounds++
					followup = "Please start the earlier research or banner"
				}
				if err := r.Close(testContext); err != nil {
					t.Fatal(err)
				}
				s = reopenRecoveryStore(t, db, path)
				r = recoveryEngine(t, s, b, p)
				q := send("followup", followup)
				if q.Handoff == nil {
					t.Fatal("fresh prompt omitted handoff")
				}
				opening, err := q.Handoff.Opening(q.Turn, q.Context)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(opening, topic) {
					t.Fatalf("actual fresh provider prompt lost the earlier topic: %s", opening)
				}
				x, err := s.Export(testContext, b.Key)
				if err != nil || len(x.MemoryEntries) != 0 || len(x.Messages) != rounds*2 || p.sends.Load() != int32(rounds) || p.dispatches.Load() != 0 {
					t.Fatalf("continuation changed memory, replayed work or duplicated replies: messages=%d memory=%d sends=%d dispatches=%d err=%v", len(x.Messages), len(x.MemoryEntries), p.sends.Load(), p.dispatches.Load(), err)
				}
			})
		}
	}
}

func TestRuntimeSQLiteBindDuringWorkCompletion(t *testing.T) {
	for _, changeInstructions := range []bool{false, true} {
		t.Run(fmt.Sprintf("changeInstructions=%t", changeInstructions), func(t *testing.T) {
			_, s, _ := testDB(t)
			b := seed(t, s, "binding-race")
			oldFence := populate(t, s, b, pa.EffectAccepted)
			prior, err := s.Turn(testContext, b.Key, "turn")
			if err != nil {
				t.Fatal(err)
			}
			chat, err := s.Conversation(testContext, b.Key, b.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
			prior.State, chat.ActiveTurnID = pa.TurnSucceeded, ""
			if _, err = s.Commit(testContext, app.Mutation{
				Command: command(b.Key, pa.SendOperation, "finish-prior"), ExpectedVersion: 2,
				Turn: &prior, Conversation: &chat, Receipt: pa.Receipt{ID: "finish-prior", State: pa.EffectDelivered},
			}); err != nil {
				t.Fatal(err)
			}
			peer := &recoveryPeer{sent: make(chan app.PromptRequest, 2), result: make(chan app.PromptResult, 1)}
			var projected atomic.Bool
			repo := bindingRaceStore{Store: s, beforeBind: func(ctx context.Context, m app.Mutation) error {
				if !projected.CompareAndSwap(false, true) {
					return nil
				}
				bot := *m.Bot
				if changeInstructions {
					bot.Instructions.Revision++
					bot.Instructions.Text = "Changed instructions"
				}
				link := pa.WorkLink{ID: "work", Fence: oldFence, TurnID: prior.ID, TaskID: "task", State: pa.WorkCompleted, ResultMessageID: "work-result"}
				message := pa.Message{ID: link.ResultMessageID, Fence: oldFence, TurnID: prior.ID, Kind: pa.WorkMessage, Text: "Dispatched work finished", At: time.Now()}
				_, err := s.Commit(ctx, app.Mutation{
					Command: command(b.Key, pa.DispatchOperation, "late-work"), ExpectedVersion: m.ExpectedVersion, Bot: &bot,
					WorkLinks: []pa.WorkLink{link}, Messages: []pa.Message{message},
					Receipt: pa.Receipt{ID: "late-work", State: pa.EffectDelivered},
				})
				return err
			}}
			engine := recoveryEngineWithRepository(t, s, repo, b, peer)
			submission := recoverySubmission(t, s, b, "while-work-finishes")
			receipt, err := engine.Submit(testContext, submission)
			if err != nil {
				t.Fatal(err)
			}
			peer.result <- app.PromptResult{FinalText: "Finished the new turn"}
			var actual pa.Turn
			recoveryAwait(t, func() bool {
				actual, _ = s.Turn(testContext, b.Key, receipt.ResourceID)
				return actual.State == pa.TurnSucceeded || actual.State == pa.TurnUnknown || actual.State == pa.TurnFailed
			})
			x, err := s.Export(testContext, b.Key)
			if err != nil {
				t.Fatal(err)
			}
			if len(x.WorkLinks) != 1 || x.WorkLinks[0].State != pa.WorkCompleted || len(x.Messages) < 3 || x.Messages[2].ID != "work-result" {
				t.Fatal("the concurrent work result was not preserved")
			}
			if changeInstructions {
				if actual.State != pa.TurnUnknown || actual.Failure == nil || actual.Failure.Code != pa.StaleVersion ||
					peer.sends.Load() != 0 || x.Bot.Instructions.Text != "Changed instructions" {
					t.Fatalf("changed instructions were overwritten or stale prompt was sent: turn=%+v sends=%d", actual, peer.sends.Load())
				}
				return
			}
			if actual.State != pa.TurnSucceeded || peer.opens.Load() != 1 || peer.sends.Load() != 1 {
				t.Fatalf("unrelated work result stopped the new turn: state=%s failure=%+v opens=%d sends=%d", actual.State, actual.Failure, peer.opens.Load(), peer.sends.Load())
			}
			if prompt := <-peer.sent; prompt.Turn.ID != actual.ID {
				t.Fatalf("different turn sent: %s", prompt.Turn.ID)
			}
		})
	}
}

type bindingRaceStore struct {
	*Store
	beforeBind func(context.Context, app.Mutation) error
}

func (s bindingRaceStore) Commit(ctx context.Context, m app.Mutation) (pa.Receipt, error) {
	if strings.HasPrefix(m.Command.IdempotencyKey, "runtime-bound-") {
		if err := s.beforeBind(ctx, m); err != nil {
			return pa.Receipt{}, err
		}
	}
	return s.Store.Commit(ctx, m)
}

// This peer never starts a vendor adapter or makes a model request.
type recoveryPeer struct {
	app.WorkBridge
	opens, sends, dispatches atomic.Int32
	sent                     chan app.PromptRequest
	result                   chan app.PromptResult
	unknown                  bool
}

func (*recoveryPeer) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "fixture", Evidence: "synthetic-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true}, nil
}
func (p *recoveryPeer) Open(context.Context, app.OpenRequest, func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.opens.Add(1)
	return p, nil
}
func (*recoveryPeer) SessionID() string { return "synthetic-native" }
func (p *recoveryPeer) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	p.sends.Add(1)
	p.sent <- q
	if p.unknown {
		return nil, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "synthetic_lost_response"}
	}
	return p, nil
}
func (p *recoveryPeer) Wait(ctx context.Context) (app.PromptResult, error) {
	select {
	case result := <-p.result:
		return result, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}
func (*recoveryPeer) Answer(context.Context, string, string) error { return nil }
func (*recoveryPeer) Cancel(context.Context) error                 { return nil }
func (*recoveryPeer) Close(context.Context) error                  { return nil }
func (p *recoveryPeer) Dispatch(context.Context, pa.WorkRequest) (pa.WorkLink, error) {
	p.dispatches.Add(1)
	return pa.WorkLink{}, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "synthetic_broker_response"}
}

type recoveryResolver struct{ scope pa.ProjectScope }

func (r recoveryResolver) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: r.scope, Directory: "/synthetic", Available: true, Current: true}, nil
}

func recoveryEngine(t *testing.T, s *Store, b pa.Bot, peer *recoveryPeer) *app.Engine {
	t.Helper()
	return recoveryEngineWithRepository(t, s, s, b, peer)
}

func recoveryEngineWithRepository(t *testing.T, s *Store, repo app.Repository, b pa.Bot, peer *recoveryPeer) *app.Engine {
	t.Helper()
	var sequence atomic.Int64
	r, err := app.NewRuntime(app.RuntimeOptions{Repository: repo, Memory: s, WorkBridge: peer, ProjectResolver: recoveryResolver{b.Project}, Provider: peer,
		Clock: time.Now, ID: func() string { return fmt.Sprintf("recovery-%d-%d", time.Now().UnixNano(), sequence.Add(1)) },
		Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
			return pa.FeatureState{Enabled: true}, recoverySource(b.Key), nil
		}, Limits: app.RuntimeLimits{MaxBots: 1, PromptTimeout: 3 * time.Second, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(testContext); err != nil {
			t.Error(err)
		}
	})
	return r
}

func recoverySource(key pa.BotKey) pa.SourceStatus {
	return pa.SourceStatus{MachineID: key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
}

func recoverySubmission(t *testing.T, s *Store, b pa.Bot, id string) app.Submission {
	t.Helper()
	c, err := s.Conversation(testContext, b.Key, b.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	return app.Submission{Command: command(b.Key, pa.SendOperation, id), Feature: pa.FeatureState{Enabled: true}, Source: recoverySource(b.Key),
		Message: pa.SendMessage{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, Text: "Synthetic input"}}
}

func recoveryAwait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("recovery condition did not become true")
}

func reopenRecoveryStore(t *testing.T, db *sql.DB, path string) *Store {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.SetMaxOpenConns(1)
	t.Cleanup(func() { reopened.Close() })
	if err := (Installer{}).Install(testContext, reopened); err != nil {
		t.Fatal(err)
	}
	return New(reopened)
}

func TestRuntimeSQLiteReopenRetainsUnknownTurnAndWorkReceipt(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "recovery")
	p := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult, 1)}
	r := recoveryEngine(t, s, b, p)
	input := recoverySubmission(t, s, b, "one")
	accepted, err := r.Submit(testContext, input)
	if err != nil || accepted.State != pa.EffectAccepted {
		t.Fatalf("acceptance: %+v %v", accepted, err)
	}
	var prompt app.PromptRequest
	select {
	case prompt = <-p.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no prompt")
	}
	work := pa.WorkRequest{Actor: pa.AssistantExecutionActor{CapabilityID: "synthetic-host-capability", Fence: prompt.Turn.Fence, ExpiresAt: time.Now().Add(time.Minute), Operations: []pa.ActorOperation{pa.DispatchWork}},
		Command: command(b.Key, pa.DispatchOperation, "work"), TurnID: prompt.Turn.ID, Brief: "Synthetic isolated work", Claims: []string{"owned.go"}}
	_, err = r.DispatchWork(testContext, work)
	requireCode(t, err, pa.ExecutionEffectUnknown)
	// The same key and content goes back to the work bridge, which alone
	// decides from broker facts; the Engine keeps its one durable receipt.
	_, err = r.DispatchWork(testContext, work)
	requireCode(t, err, pa.ExecutionEffectUnknown)
	conflicting := work
	conflicting.Command.ContentHash = command(b.Key, pa.DispatchOperation, "different").ContentHash
	_, err = r.DispatchWork(testContext, conflicting)
	requireCode(t, err, pa.IdempotencyConflict)
	if err := r.Close(testContext); err != nil {
		t.Fatal(err)
	}
	// Model a queue admitted durably before the interrupted execution ended.
	// Recovery must keep it behind the unknown turn after a real pool reopen.
	x, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	queued := pa.Turn{ID: "later-turn", Fence: prompt.Turn.Fence, MessageID: "later-message", State: pa.TurnQueued, IdempotencyKey: "later-before-crash"}
	conversation := x.Conversation
	conversation.LastMessageSeq++
	message := pa.Message{ID: queued.MessageID, Fence: queued.Fence, TurnID: queued.ID, Seq: conversation.LastMessageSeq, Kind: pa.UserMessage, Text: "Synthetic queued input"}
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, queued.IdempotencyKey), ExpectedVersion: x.Bot.Version, Conversation: &conversation, Turn: &queued, Messages: []pa.Message{message}, Receipt: pa.Receipt{ID: "later-accepted", State: pa.EffectAccepted, ResourceID: queued.ID}})
	if err != nil {
		t.Fatal(err)
	}
	s = reopenRecoveryStore(t, db, path)
	before, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	r = recoveryEngine(t, s, b, p)
	if err := r.Recover(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	replayed, err := r.Submit(testContext, input)
	if err != nil || replayed != accepted {
		t.Fatalf("lost acceptance replay: %+v %v", replayed, err)
	}
	input.Command.ContentHash = command(b.Key, pa.SendOperation, "conflict").ContentHash
	_, err = r.Submit(testContext, input)
	requireCode(t, err, pa.IdempotencyConflict)
	_, err = r.Submit(testContext, recoverySubmission(t, s, b, "later"))
	requireCode(t, err, pa.OutcomeUnknown)
	unknown, err := s.Receipt(testContext, work.Command)
	if err != nil || unknown == nil || unknown.State != pa.EffectUnknown {
		t.Fatalf("lost work receipt: %+v %v", unknown, err)
	}
	_, err = s.Observe(testContext, command(b.Key, pa.ObserveOperation, "unknown"), unknown.ID, unknown.EventSeq)
	requireCode(t, err, pa.OutcomeUnknown)
	after, err := s.Export(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("recovery/replay changed durable facts: %v", err)
	}
	turn, err := s.Turn(testContext, b.Key, accepted.ResourceID)
	if err != nil || turn.State != pa.TurnUnknown || p.opens.Load() != 1 || p.sends.Load() != 1 || p.dispatches.Load() != 2 { // the original and one same-key bridge inspection
		t.Fatalf("unknown replay: turn=%+v error=%v opens=%d sends=%d dispatches=%d", turn, err, p.opens.Load(), p.sends.Load(), p.dispatches.Load())
	}
	if pending, err := s.PendingTurns(testContext, b.Key); err != nil || len(pending) != 2 || pending[0].State != pa.TurnUnknown || pending[1].ID != queued.ID || pending[1].State != pa.TurnQueued {
		t.Fatalf("reopened queue lost unknown barrier: %+v %v", pending, err)
	}
}

func TestRuntimeSQLiteReconnectKeepsMessagesEventsAndReadCursor(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "timeline")
	p := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult, 1)}
	r := recoveryEngine(t, s, b, p)
	input := recoverySubmission(t, s, b, "one")
	accepted, err := r.Submit(testContext, input)
	if err != nil {
		t.Fatal(err)
	}
	var prompt app.PromptRequest
	select {
	case prompt = <-p.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no prompt")
	}
	x, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	executing := false
	for _, receipt := range x.Receipts {
		if receipt.ResourceID == accepted.ResourceID && receipt.State == pa.EffectExecuting {
			executing = true
			_, err := s.Observe(testContext, command(b.Key, pa.ObserveOperation, "too-early"), receipt.ID, receipt.EventSeq)
			requireCode(t, err, pa.OutcomeUnknown)
		}
	}
	if !executing {
		t.Fatal("no durable executing receipt before completion")
	}
	event := app.ProtocolEvent{ID: "explicit", Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, Message: "One explicit reply"}
	if err := r.Publish(testContext, event); err != nil {
		t.Fatal(err)
	}
	if err := r.Publish(testContext, event); err != nil {
		t.Fatal(err)
	}
	changed := event
	changed.Message = "Conflicting reply"
	requireCode(t, r.Publish(testContext, changed), pa.IdempotencyConflict)
	stale := event
	stale.ID = "stale"
	stale.Fence.Generation--
	requireCode(t, r.Publish(testContext, stale), pa.StaleGeneration)
	p.result <- app.PromptResult{FinalText: "Final must not duplicate explicit reply"}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	x, err = s.Export(testContext, b.Key)
	if err != nil || len(x.Messages) != 2 {
		t.Fatalf("duplicate bubble facts: messages=%d %v", len(x.Messages), err)
	}
	var delivered pa.Receipt
	for _, receipt := range x.Receipts {
		if receipt.Command.IdempotencyKey == "runtime-finish-"+accepted.ResourceID {
			delivered = receipt
		}
	}
	if delivered.State != pa.EffectDelivered {
		t.Fatalf("terminal receipt: %+v", delivered)
	}
	observeCommand := command(b.Key, pa.ObserveOperation, "observed")
	observed, err := s.Observe(testContext, observeCommand, delivered.ID, delivered.EventSeq)
	if err != nil || observed.State != pa.EffectObserved {
		t.Fatalf("observation: %+v %v", observed, err)
	}
	readCommand := command(b.Key, pa.ReadOperation, "read")
	cursor := pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "synthetic-owner", MessageSeq: x.Conversation.LastMessageSeq}
	if _, err := s.MarkRead(testContext, readCommand, cursor); err != nil {
		t.Fatal(err)
	}
	page, err := s.Read(testContext, pa.EventQuery{Key: b.Key, AfterSeq: delivered.EventSeq, PageSize: pa.MaxEventPageRows})
	if err != nil || len(page.Items) != 1 || page.Items[0].Kind != pa.ReadAdvanced {
		t.Fatalf("metadata became message events: %+v %v", page, err)
	}
	frontier := page.NextSeq
	if err := r.Close(testContext); err != nil {
		t.Fatal(err)
	}
	s = reopenRecoveryStore(t, db, path)
	r = recoveryEngine(t, s, b, p)
	if err := r.Recover(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	if replay, err := r.Submit(testContext, input); err != nil || replay != accepted {
		t.Fatalf("accepted receipt changed on reconnect: %+v %v", replay, err)
	}
	if got, err := s.MarkRead(testContext, readCommand, cursor); err != nil || got != cursor {
		t.Fatalf("read cursor replay: %+v %v", got, err)
	}
	if got, err := s.Observe(testContext, observeCommand, delivered.ID, delivered.EventSeq); err != nil || got != observed {
		t.Fatalf("observed receipt replay: %+v %v", got, err)
	}
	page, err = s.Read(testContext, pa.EventQuery{Key: b.Key, AfterSeq: frontier, PageSize: pa.MaxEventPageRows})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("reconnect invented events: %+v %v", page, err)
	}
	x, err = s.Export(testContext, b.Key)
	if err != nil || len(x.Messages) != 2 || len(x.ReadCursors) != 1 || x.ReadCursors[0] != cursor || p.sends.Load() != 1 || p.opens.Load() != 1 {
		t.Fatalf("reconnect lost cursor or replayed: %+v %v opens=%d sends=%d", x.ReadCursors, err, p.opens.Load(), p.sends.Load())
	}
}

// A restart cut Lynn off mid-request; Clawdline told the person it would
// summarize her reply and then heard nothing. Now the request is still not
// replayed, but its sender gets a reply turn saying it did not finish.
func TestRestartTellsTheSenderThatItsRequestDidNotFinish(t *testing.T) {
	_, s, _ := testDB(t)
	a := seed(t, s, "a")
	populate(t, s, a, pa.EffectAccepted)
	b := seedTeamRecipient(t, s, "b")
	x, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "restart-tell"))
	if err != nil {
		t.Fatal(err)
	}
	// The sender's own turn ended before the restart, as Clawdline's had.
	export, err := s.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	var own pa.Turn
	for _, turn := range export.Turns {
		if turn.ID == "turn" {
			own = turn
		}
	}
	finish := reviewFinish(t, s, a.Key, own, pa.TurnSucceeded)
	finish.Permissions, finish.WorkLinks = nil, nil
	for _, p := range export.Permissions {
		p.State, p.SelectedOptionID = pa.PermissionResolved, "reject"
		finish.Permissions = append(finish.Permissions, p)
	}
	if _, err = s.Commit(testContext, finish); err != nil {
		t.Fatal(err)
	}
	reviewStart(t, s, b.Key, x.RequestTurnID)
	peer := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult, 1)}
	r := recoveryEngine(t, s, b, peer)
	if err = r.Recover(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	select {
	case q := <-peer.sent:
		if q.Turn.Fence.Key != a.Key || q.Turn.Origin != pa.BotReplyOrigin || q.Turn.ExchangeID != x.ID || !strings.Contains(q.Text, "did not finish") || !strings.Contains(q.Text, "Do not resend the request") {
			t.Fatalf("the sender's reply turn: key=%v origin=%s text=%q", q.Turn.Fence.Key, q.Turn.Origin, q.Text)
		}
		peer.result <- app.PromptResult{FinalText: "The review did not finish; shall I ask again?"}
	case <-time.After(3 * time.Second):
		t.Fatal("the sender was never told that its request did not finish")
	}
	got, err := s.Exchange(testContext, a.Key, x.ID)
	if err != nil || got.Outcome != pa.ExchangeUnknown {
		t.Fatal(got, err)
	}
}
