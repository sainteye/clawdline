package projectassistantstore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func reviewAdmission(t *testing.T, s *Store, a, b pa.Bot, id string) app.ExchangeAdmission {
	t.Helper()
	ax, err := s.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	bx, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	x := pa.Exchange{ID: id, Project: a.Project, Source: pa.ExchangeEndpoint{Key: a.Key, ConversationID: a.ConversationID}, Target: pa.ExchangeEndpoint{Key: b.Key, ConversationID: b.ConversationID}, SourceTurnID: "turn", RequestTurnID: id + "-request-turn", Request: "Review the synthetic change.", Outcome: pa.ExchangeQueued, Revision: 1, Hops: 1}
	return app.ExchangeAdmission{SourceFence: exchangeFence(aggregate{Bot: ax.Bot, Conversation: ax.Conversation}), SourceTurnID: "turn", SourceVersion: ax.Bot.Version, TargetVersion: bx.Bot.Version, Command: command(a.Key, pa.SendOperation, id), Exchange: x, At: time.Now()}
}

func reviewStart(t *testing.T, s *Store, key pa.BotKey, id string) pa.Turn {
	t.Helper()
	x, err := s.Export(testContext, key)
	if err != nil {
		t.Fatal(err)
	}
	var turn pa.Turn
	for _, v := range x.Turns {
		if v.ID == id {
			turn = v
		}
	}
	turn.State = pa.TurnRunning
	turn.Instructions = x.Bot.Instructions
	turn.MemoryRevision = x.Memory.Revision
	c := x.Conversation
	c.ActiveTurnID = turn.ID
	m := exchangeMutation(aggregate{Bot: x.Bot, Conversation: x.Conversation}, id+"-start", time.Now(), &turn, nil)
	m.Conversation = &c
	if _, err = s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	if turn.Origin == pa.BotRequestOrigin {
		if err = s.StartExchange(testContext, turn.Fence, turn.ID, turn.ExchangeID); err != nil {
			t.Fatal(err)
		}
	}
	return turn
}

func reviewFinish(t *testing.T, s *Store, key pa.BotKey, turn pa.Turn, state pa.TurnState) app.Mutation {
	t.Helper()
	x, err := s.Export(testContext, key)
	if err != nil {
		t.Fatal(err)
	}
	turn.State = state
	c := x.Conversation
	if c.ActiveTurnID == turn.ID {
		c.ActiveTurnID = ""
	}
	m := exchangeMutation(aggregate{Bot: x.Bot, Conversation: x.Conversation}, turn.ID+"-finish", time.Now(), &turn, nil)
	m.Conversation = &c
	return m
}

func seedTeamRecipient(t *testing.T, s *Store, id string) pa.Bot {
	t.Helper()
	b := seed(t, s, id)
	b.ManageTeam = true
	b.Version++
	cmd := command(b.Key, pa.EditOperation, "manage-team")
	if _, err := s.Commit(testContext, app.Mutation{Command: cmd, ExpectedVersion: b.Version - 1, Bot: &b,
		Receipt: pa.Receipt{ID: "team-recipient", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollaborationOrdinaryRecipientKeepsItsActiveChat(t *testing.T) {
	_, s, _ := testDB(t)
	sender := seed(t, s, "sender")
	receiver := seed(t, s, "receiver")
	collaborationActiveSource(t, s, sender)
	collaborationActiveSource(t, s, receiver)
	q := reviewAdmission(t, s, sender, receiver, "background-exchange")
	x, err := s.AdmitExchange(testContext, q)
	if err != nil || x.RequestWorkerID == "" || x.Recipient().Key == receiver.Key || !x.RequestAt.Equal(q.At) || !x.ReplyAt.IsZero() {
		t.Fatalf("ordinary recipient did not delegate: %+v, %v", x, err)
	}
	worker, err := s.StartBackgroundTask(testContext, receiver.Key, time.Now())
	if err != nil || worker != x.Recipient().Key {
		t.Fatalf("active chat blocked the peer job: %v, %v", worker, err)
	}
	active := reviewStart(t, s, worker, x.RequestTurnID)
	finished := reviewFinish(t, s, worker, active, pa.TurnSucceeded)
	x, err = s.FinishExchange(testContext, finished, "Independent answer.")
	if err != nil || x.Outcome != pa.ExchangeCompleted || x.Delivery != "queued" ||
		!x.RequestAt.Equal(q.At) || !x.ReplyAt.Equal(finished.Bot.UpdatedAt) {
		t.Fatalf("peer result did not return to its sender: %+v, %v", x, err)
	}
	owner, err := s.Export(testContext, receiver.Key)
	if err != nil || owner.Conversation.ActiveTurnID != "turn" || len(owner.Turns) != 1 {
		t.Fatalf("peer job changed the user's active conversation: %+v, %v", owner.Conversation, err)
	}
	last := owner.Messages[len(owner.Messages)-1]
	if last.Exchange == nil || last.Exchange.Reply != "Independent answer." || last.Kind != pa.NoticeMessage ||
		!last.Exchange.RequestAt.Equal(q.At) || !last.Exchange.ReplyAt.Equal(finished.Bot.UpdatedAt) {
		t.Fatalf("recipient did not retain the attributed result: %+v", last)
	}
	private, err := s.Export(testContext, worker)
	if err != nil || private.Messages[len(private.Messages)-1].Text != "Independent answer." {
		t.Fatalf("worker transcript did not retain its answer: %+v, %v", private.Messages, err)
	}
	waiting, err := s.AdmitExchange(testContext, reviewAdmission(t, s, sender, receiver, "waiting-peer-job"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteBot(testContext, receiver.Key); err != nil {
		t.Fatal(err)
	}
	got, err := s.Exchange(testContext, sender.Key, waiting.ID)
	if err != nil || got.Outcome != pa.ExchangeCancelled || got.Delivery != "dropped" {
		t.Fatalf("deleting the recipient left the accepted peer job outstanding: %+v, %v", got, err)
	}
}

func TestCollaborationRecipientRosterDoesNotCountExchangeNotices(t *testing.T) {
	for _, managesTeam := range []bool{false, true} {
		t.Run(fmt.Sprintf("manage-team=%t", managesTeam), func(t *testing.T) {
			db, s, _ := testDB(t)
			views := newTestViews(t, db)
			sender := seed(t, s, "sender")
			var recipient pa.Bot
			if managesTeam {
				recipient = seedTeamRecipient(t, s, "recipient")
			} else {
				recipient = seed(t, s, "recipient")
				collaborationActiveSource(t, s, recipient)
			}
			collaborationActiveSource(t, s, sender)
			q := rosterQuery()
			assertUnread := func(want int64) {
				t.Helper()
				list, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}, q.ReaderID)
				if err != nil {
					t.Fatal(err)
				}
				legacy, streamed := int64(-1), int64(-1)
				for _, row := range list.Items {
					if row.Bot.Key == recipient.Key {
						legacy = row.UnreadCount
					}
				}
				for _, change := range rosterRead(t, views, q).Frame.Changes {
					if change.Key == recipient.Key && change.Summary != nil {
						streamed = change.Summary.UnreadCount
					}
				}
				if legacy != want || streamed != want {
					t.Fatalf("recipient unread legacy=%d stream=%d, want %d", legacy, streamed, want)
				}
			}
			// The seeded message is the person's own, which is never unread,
			// so exchange cards must keep the count at zero.
			directUnread := int64(0)
			assertUnread(directUnread)
			x, err := s.AdmitExchange(testContext, reviewAdmission(t, s, sender, recipient, "hidden-unread"))
			if err != nil {
				t.Fatal(err)
			}
			assertUnread(directUnread)
			worker := recipient.Key
			if !managesTeam {
				worker, err = s.StartBackgroundTask(testContext, recipient.Key, time.Now())
				if err != nil {
					t.Fatal(err)
				}
			}
			active := reviewStart(t, s, worker, x.RequestTurnID)
			if _, err = s.FinishExchange(testContext, reviewFinish(t, s, worker, active, pa.TurnSucceeded), "Synthetic answer"); err != nil {
				t.Fatal(err)
			}
			assertUnread(directUnread)
			export, err := s.Export(testContext, recipient.Key)
			if err != nil || export.Messages[len(export.Messages)-1].Exchange == nil {
				t.Fatal("recipient exchange card disappeared", err)
			}
			if directUnread > 0 {
				_, err = s.MarkRead(testContext, command(recipient.Key, pa.ReadOperation, "explicit-read"), pa.ReadCursor{
					Key: recipient.Key, ConversationID: recipient.ConversationID, ReaderID: q.ReaderID, MessageSeq: 1,
				})
				if err != nil {
					t.Fatal(err)
				}
				assertUnread(0)
			}
		})
	}
}

func TestCollaborationAdmissionAndCompletionAreTwoBotTransactions(t *testing.T) {
	db, s, _ := testDB(t)
	a := seed(t, s, "a")
	populate(t, s, a, pa.EffectAccepted)
	b := seedTeamRecipient(t, s, "b")
	q := reviewAdmission(t, s, a, b, "one-exchange")
	before := databaseFacts(t, db)
	if _, err := db.Exec(`CREATE TRIGGER fail_admission BEFORE INSERT ON project_assistant_exchanges BEGIN SELECT RAISE(ABORT,'admission fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitExchange(testContext, q); err == nil {
		t.Fatal("fault accepted")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_admission`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, databaseFacts(t, db)) {
		t.Fatal("admission left partial aggregates/events/receipts")
	}
	x, err := s.AdmitExchange(testContext, q)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.AdmitExchange(testContext, q)
	if err != nil || replay.ID != x.ID {
		t.Fatal("admission replay", replay, err)
	}
	bad := q
	bad.Command.ContentHash = strings.Repeat("f", 64)
	_, err = s.AdmitExchange(testContext, bad)
	requireCode(t, err, pa.IdempotencyConflict)
	target := reviewStart(t, s, b.Key, x.RequestTurnID)
	m := reviewFinish(t, s, b.Key, target, pa.TurnSucceeded)
	before = databaseFacts(t, db)
	if _, err = db.Exec(`CREATE TRIGGER fail_completion BEFORE UPDATE ON project_assistant_exchanges WHEN NEW.outstanding=0 BEGIN SELECT RAISE(ABORT,'completion fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishExchange(testContext, m, "One peer review."); err == nil {
		t.Fatal("completion fault accepted")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_completion`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, databaseFacts(t, db)) {
		t.Fatal("completion dropped work or left a duplicate source turn")
	}
	x, err = s.FinishExchange(testContext, m, "One peer review.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishExchange(testContext, m, "One peer review."); err != nil {
		t.Fatal(err)
	}
	ax, err := s.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	var replies int
	for _, turn := range ax.Turns {
		if turn.Origin == pa.BotReplyOrigin {
			replies++
			if turn.ExchangeID != x.ID || turn.Hops != 1 {
				t.Fatal(turn)
			}
		}
	}
	if replies != 1 || x.Outcome != pa.ExchangeCompleted || x.Delivery != "queued" {
		t.Fatal(replies, x)
	}
	if total, largest, err := s.CollaborationUsage(testContext, a.Key.MachineID); err != nil || total != 0 || largest != 0 {
		t.Fatal("capacity not released once", total, largest, err)
	}
	archive, err := s.ExportArchive(testContext, a.Key)
	if err != nil || len(archive.Data.Exchanges) != 1 {
		t.Fatal("archive dropped exchange", err)
	}
	_, targetStore, _ := testDB(t)
	requireCode(t, targetStore.RestoreArchive(testContext, archive), pa.Unsupported)
}

func TestCollaborationDirectoryScopeHiddenAndLateFence(t *testing.T) {
	_, s, _ := testDB(t)
	a := seed(t, s, "a")
	f := populate(t, s, a, pa.EffectAccepted)
	b := seed(t, s, "b")
	page, err := s.Directory(testContext, f, "turn", pa.BotDirectoryQuery{PageSize: 1})
	if err != nil || len(page.Bots) != 1 || page.Bots[0].ID != b.Key.BotID {
		t.Fatal(page, err)
	}
	b.Hidden = true
	b.Version = 2
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "hide"), ExpectedVersion: 1, Bot: &b, Receipt: pa.Receipt{ID: "hide", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	page, err = s.Directory(testContext, f, "turn", pa.BotDirectoryQuery{})
	if err != nil || len(page.Bots) != 0 {
		t.Fatal(page, err)
	}
	_, err = s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "hidden"))
	requireCode(t, err, pa.ScopeDenied)
	current, err := s.Bot(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	current.Lead = true
	current.Version++
	if _, err = s.Commit(testContext, app.Mutation{Command: command(a.Key, pa.EditOperation, "make-lead"), ExpectedVersion: current.Version - 1, Bot: &current, DesignateLead: true, Receipt: pa.Receipt{ID: "make-lead", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	page, err = s.Directory(testContext, f, "turn", pa.BotDirectoryQuery{})
	if err != nil || len(page.Bots) != 1 || page.Bots[0].ID != b.Key.BotID {
		t.Fatal(page, err)
	}
	x, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "lead-hidden"))
	if err != nil || x.Target.Key != b.Key {
		t.Fatal(x, err)
	}
	f.Generation++
	_, err = s.Directory(testContext, f, "turn", pa.BotDirectoryQuery{})
	requireCode(t, err, pa.StaleGeneration)
}

func TestCollaborationStopRestartAndDeletedSenderKeepSurvivingFacts(t *testing.T) {
	for _, action := range []string{"sender_stop", "recipient_stop", "restart_queued", "restart_running", "delete_sender"} {
		t.Run(action, func(t *testing.T) {
			_, s, _ := testDB(t)
			a := seed(t, s, "a")
			populate(t, s, a, pa.EffectAccepted)
			b := seedTeamRecipient(t, s, "b")
			x, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "lifetime"))
			if err != nil {
				t.Fatal(err)
			}
			var sequence int
			id := func() string { sequence++; return fmt.Sprint("cleanup-", sequence) }
			switch action {
			case "sender_stop":
				if err = app.QuiesceHistory(testContext, s, time.Now, id, a.Key); err != nil {
					t.Fatal(err)
				}
				x, err = s.Exchange(testContext, b.Key, x.ID)
				if err != nil || x.Outcome != pa.ExchangeQueued {
					t.Fatal("sender cancelled accepted recipient", x, err)
				}
			case "recipient_stop":
				if err = app.QuiesceHistory(testContext, s, time.Now, id, b.Key); err != nil {
					t.Fatal(err)
				}
				// The sender hears that the request is over (Codync: "or why
				// it couldn't finish"), instead of waiting for nothing.
				x, err = s.Exchange(testContext, a.Key, x.ID)
				if err != nil || x.Outcome != pa.ExchangeCancelled || x.Delivery != "queued" || x.ReplyTurnID == "" {
					t.Fatal(x, err)
				}
			case "restart_queued", "restart_running":
				if action == "restart_running" {
					reviewStart(t, s, b.Key, x.RequestTurnID)
				}
				peer := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult)}
				r := recoveryEngine(t, s, b, peer)
				if err = r.Recover(testContext, b.Key); err != nil {
					t.Fatal(err)
				}
				if err = r.ResumeInterrupted(testContext, b.Key); err != nil {
					t.Fatal(err)
				}
				x, err = s.Exchange(testContext, a.Key, x.ID)
				if err != nil || x.Outstanding() || peer.opens.Load() != 0 || peer.sends.Load() != 0 {
					t.Fatal("request replayed", x, err)
				}
				if action == "restart_running" && x.Outcome != pa.ExchangeUnknown {
					t.Fatal("unknown erased", x)
				}
				// The request is never replayed, but its sender is told it
				// did not finish, so it can tell the person.
				if action == "restart_running" && (x.Delivery != "queued" || x.ReplyTurnID == "") {
					t.Fatal("restart left the sender waiting for a reply that cannot come", x)
				}
			case "delete_sender":
				if err = s.DeleteBot(testContext, a.Key); err != nil {
					t.Fatal(err)
				}
				target := reviewStart(t, s, b.Key, x.RequestTurnID)
				x, err = s.FinishExchange(testContext, reviewFinish(t, s, b.Key, target, pa.TurnSucceeded), "Surviving review")
				if err != nil || x.Outcome != pa.ExchangeCompleted || x.Delivery != "dropped" {
					t.Fatal(x, err)
				}
				out, err := s.Export(testContext, b.Key)
				if err != nil || len(out.Exchanges) != 1 || out.Exchanges[0].Reply != "Surviving review" {
					t.Fatal("surviving history", out.Exchanges, err)
				}
			}
		})
	}
}

func TestCollaborationCapacityCountsAcceptedRequests(t *testing.T) {
	_, s, _ := testDB(t)
	a := seed(t, s, "a")
	populate(t, s, a, pa.EffectAccepted)
	for i := 0; i < pa.MaxCollaborationPerSender; i++ {
		b := seed(t, s, fmt.Sprint("b-", i))
		if _, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, fmt.Sprint("exchange-", i))); err != nil {
			t.Fatal(err)
		}
	}
	if total, largest, err := s.CollaborationUsage(testContext, a.Key.MachineID); err != nil || total != pa.MaxCollaborationPerSender || largest != pa.MaxCollaborationPerSender {
		t.Fatal(total, largest, err)
	}
	b := seed(t, s, "overflow")
	_, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "overflow"))
	requireCode(t, err, pa.CapacityFull)
}

func TestCollaborationGlobalPressureDoesNotRejectUnrelatedHistory(t *testing.T) {
	_, s, _ := testDB(t)
	var senders []pa.Bot
	for i := 0; i < 4; i++ {
		a := seed(t, s, fmt.Sprint("sender-", i))
		collaborationActiveSource(t, s, a)
		senders = append(senders, a)
		for j := 0; j < pa.MaxCollaborationPerSender; j++ {
			b := seed(t, s, fmt.Sprint("target-", i, "-", j))
			if _, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, fmt.Sprint("global-", i, "-", j))); err != nil {
				t.Fatal(err)
			}
		}
	}
	total, largest, err := s.CollaborationUsage(testContext, senders[0].Key.MachineID)
	if err != nil || total != pa.MaxCollaborationOutstanding || largest != pa.MaxCollaborationPerSender {
		t.Fatal(total, largest, err)
	}
	a := seed(t, s, "extra-source")
	collaborationActiveSource(t, s, a)
	b := seed(t, s, "extra-target")
	before, err := s.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "global-overflow"))
	requireCode(t, err, pa.CapacityFull)
	after, err := s.Export(testContext, a.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("backpressure changed unrelated chat", err)
	}
	x, err := s.Export(testContext, senders[0].Key)
	if err != nil || len(x.Exchanges) != pa.MaxCollaborationPerSender {
		t.Fatal("history unavailable at capacity", err)
	}
}

func collaborationActiveSource(t *testing.T, s *Store, b pa.Bot) {
	t.Helper()
	f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "binding", Generation: 1}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, BindingID: f.BindingID, Generation: 1, MemoryRevision: 1, ActiveTurnID: "turn"}
	turn := pa.Turn{ID: "turn", Fence: f, MessageID: "input-" + b.Key.BotID, State: pa.TurnRunning, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "input"}
	msg := pa.Message{ID: turn.MessageID, Fence: f, Seq: 1, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Synthetic review input", At: b.CreatedAt}
	binding := pa.ExecutionBinding{Fence: f, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
	_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "input"), ExpectedVersion: 1, Conversation: &c, Binding: &binding, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "sent", State: pa.EffectAccepted, ResourceID: turn.ID}, Events: []pa.Event{{Version: pa.ContractVersion, ID: msg.ID, Fence: f, Kind: pa.MessageCreated, Message: &msg}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollaborationFullSourceQueueRetainsCompletedReview(t *testing.T) {
	s, a, _ := collaborationWaitingReplyFixture(t)
	if promoted, err := s.PromoteExchangeReply(testContext, a.Key, time.Now()); err != nil || promoted {
		t.Fatal("overfilled source queue", promoted, err)
	}
	queued, err := s.Turn(testContext, a.Key, "queued-1")
	if err != nil {
		t.Fatal(err)
	}
	m := reviewFinish(t, s, a.Key, queued, pa.TurnCancelled)
	if _, err = s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	if promoted, err := s.PromoteExchangeReply(testContext, a.Key, time.Now()); err != nil || !promoted {
		t.Fatal("completed reply did not enter available space", promoted, err)
	}
	if promoted, err := s.PromoteExchangeReply(testContext, a.Key, time.Now()); err != nil || promoted {
		t.Fatal("reply promoted twice", promoted, err)
	}
	ax, err := s.Export(testContext, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	replies := 0
	for _, turn := range ax.Turns {
		if turn.Origin == pa.BotReplyOrigin {
			replies++
		}
	}
	if replies != 1 || ax.Exchanges[0].Delivery != "queued" || ax.Exchanges[0].Reply != "One retained review." {
		t.Fatal("lost or duplicated promotion", replies, ax.Exchanges)
	}
}
func collaborationWaitingReplyFixture(t *testing.T) (*Store, pa.Bot, pa.Exchange) {
	t.Helper()
	_, s, _ := testDB(t)
	a := seed(t, s, "a")
	populate(t, s, a, pa.EffectAccepted)
	b := seedTeamRecipient(t, s, "b")
	x, err := s.AdmitExchange(testContext, reviewAdmission(t, s, a, b, "full-source"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < pa.MaxAutomationBacklog; i++ {
		ax, e := s.Export(testContext, a.Key)
		if e != nil {
			t.Fatal(e)
		}
		f := exchangeFence(aggregate{Bot: ax.Bot, Conversation: ax.Conversation})
		turn := pa.Turn{ID: fmt.Sprint("queued-", i), Fence: f, MessageID: fmt.Sprint("queued-input-", i), State: pa.TurnQueued, IdempotencyKey: fmt.Sprint("queued-", i)}
		msg := pa.Message{ID: turn.MessageID, Fence: f, Seq: ax.Conversation.LastMessageSeq + 1, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Already accepted user input", At: time.Now()}
		if _, e = s.Commit(testContext, app.Mutation{Command: command(a.Key, pa.SendOperation, turn.ID), ExpectedVersion: ax.Bot.Version, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: turn.ID, State: pa.EffectAccepted}}); e != nil {
			t.Fatal(e)
		}
	}
	target := reviewStart(t, s, b.Key, x.RequestTurnID)
	x, err = s.FinishExchange(testContext, reviewFinish(t, s, b.Key, target, pa.TurnSucceeded), "One retained review.")
	if err != nil || x.Outcome != pa.ExchangeCompleted || x.Delivery != "waiting" {
		t.Fatal("full source queue discarded completed review", x, err)
	}
	bx, err := s.Export(testContext, b.Key)
	if err != nil || bx.Turns[0].State != pa.TurnSucceeded {
		t.Fatal("recipient completion was not committed", err)
	}
	ax, err := s.Export(testContext, a.Key)
	if err != nil || len(ax.Turns) != pa.MaxAutomationBacklog {
		t.Fatal("source queue was overfilled", err)
	}
	if total, _, err := s.CollaborationUsage(testContext, a.Key.MachineID); err != nil || total != 0 {
		t.Fatal("completed review retained execution capacity", total, err)
	}
	return s, a, x
}

func TestCollaborationStopDropsAndRestartKeepsWaitingReply(t *testing.T) {
	for _, phase := range []string{"stop", "shutdown", "recovery"} {
		t.Run(phase, func(t *testing.T) {
			s, a, x := collaborationWaitingReplyFixture(t)
			n := 0
			id := func() string { n++; return fmt.Sprint("quiesce-source-", n) }
			switch phase {
			case "shutdown":
				if err := app.QuiesceHistoryFor(testContext, s, time.Now, id, a.Key, app.RestartField); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := app.QuiesceHistory(testContext, s, time.Now, id, a.Key); err != nil {
					t.Fatal(err)
				}
			case "recovery":
				peer := &recoveryPeer{sent: make(chan app.PromptRequest, 1), result: make(chan app.PromptResult)}
				r := recoveryEngine(t, s, a, peer)
				if err := r.Recover(testContext, a.Key); err != nil {
					t.Fatal(err)
				}
				if peer.opens.Load() != 0 || peer.sends.Load() != 0 {
					t.Fatal("recovery invoked native work", peer.opens.Load(), peer.sends.Load())
				}
			}
			// An explicit Stop drops the unstarted reply; a restart keeps it
			// waiting for queue space, as Codync resumes reply turns.
			want := "dropped"
			if phase != "stop" {
				want = "waiting"
			}
			got, err := s.Exchange(testContext, a.Key, x.ID)
			if err != nil || got.Outcome != pa.ExchangeCompleted || got.Delivery != want || got.Reply != "One retained review." {
				t.Fatal("stop must drop and restart must keep the unstarted reply, without losing the review", got, err)
			}
		})
	}
}

func TestCrossWorkspaceCollaborationKeepsBothWorkspaceFences(t *testing.T) {
	_, store, _ := testDB(t)
	senderKey := pa.BotKey{MachineID: "machine", BotID: "personal"}
	workspace := pa.PersonalScope(pa.WorkspaceForCreate(command(senderKey, pa.CreateOperation, "create")))
	sender := seedWithScope(t, store, senderKey.BotID, workspace)
	if sender.Lead {
		t.Fatal("the sender must be an ordinary Bot, not the Lead")
	}
	target := seed(t, store, "specialist")
	fence := populate(t, store, sender, pa.EffectAccepted)
	directory, err := store.Directory(testContext, fence, "turn", pa.BotDirectoryQuery{})
	if err != nil || len(directory.Bots) != 1 || directory.Bots[0].ID != target.Key.BotID {
		t.Fatalf("cross-workspace directory: %+v %v", directory, err)
	}
	request := reviewAdmission(t, store, sender, target, "cross-workspace")
	request.Exchange.TargetProject = target.Project
	exchange, err := store.AdmitExchange(testContext, request)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.StartBackgroundTask(testContext, target.Key, time.Now())
	if err != nil || worker != exchange.Recipient().Key {
		t.Fatalf("recipient Task: %v %v", worker, err)
	}
	active := reviewStart(t, store, worker, exchange.RequestTurnID)
	if active.Fence.Project != target.Project || active.Fence.Project == sender.Project {
		t.Fatal("recipient inherited sender workspace")
	}
	exchange, err = store.FinishExchange(testContext, reviewFinish(t, store, worker, active, pa.TurnSucceeded), "Synthetic result")
	if err != nil || exchange.Project != sender.Project || exchange.DestinationProject() != target.Project || exchange.Delivery != "queued" {
		t.Fatalf("reply did not return to the sender: %+v %v", exchange, err)
	}
}
