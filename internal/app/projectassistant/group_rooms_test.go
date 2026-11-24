package projectassistant_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

var roomCtx = context.Background()
var roomAt = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

// The synthetic participant follows the host's responsibility instruction;
// absent that instruction both participants defer, reproducing the greeting.
// This tests the host/provider boundary, not an installed model's compliance.
type responsibleRoomPeer struct {
	*roomPeer
	pass, fail, quota, refuse map[string]bool
	probes                    map[string]int
}

func (p *responsibleRoomPeer) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	if p.refuse[b.Key.BotID] {
		p.mu.Lock()
		p.probes[b.Key.BotID]++
		executionProbe := p.probes[b.Key.BotID] > 1
		p.mu.Unlock()
		if executionProbe {
			return pa.Capabilities{}, &pa.Failure{Code: pa.ScopeDenied, Field: "synthetic_execution"}
		}
	}
	return p.roomPeer.Capabilities(ctx, b)
}

func (p *responsibleRoomPeer) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	inner, err := p.roomPeer.Open(ctx, q, sink)
	if err != nil {
		return nil, err
	}
	return &responsibleRoomSession{roomSession: inner.(*roomSession), owner: p}, nil
}

type responsibleRoomSession struct {
	*roomSession
	owner *responsibleRoomPeer
}

func (s *responsibleRoomSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if _, err := s.roomSession.Send(ctx, q); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *responsibleRoomSession) Wait(context.Context) (app.PromptResult, error) {
	if s.owner.quota[s.bot] {
		return app.PromptResult{}, &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "synthetic_quota_after_send"}}
	}
	if s.owner.fail[s.bot] {
		return app.PromptResult{}, fmt.Errorf("synthetic provider unavailable")
	}
	if !s.owner.pass[s.bot] && strings.Contains(s.last.Text, "You are responsible for acknowledging or answering the latest user message") {
		return app.PromptResult{FinalText: "嗨！我在這裡，可以一起討論。"}, nil
	}
	return app.PromptResult{FinalText: pa.RoomPass}, nil
}

func TestRoomGreetingHasAResponsibleParticipant(t *testing.T) {
	for _, tc := range []struct {
		name, text, answer        string
		pass, fail, quota, refuse map[string]bool
	}{
		{name: "greeting", text: "嗨", answer: "a"},
		{name: "question", text: "可以幫我看看嗎？", answer: "a"},
		{name: "first explicitly passes", text: "嗨", pass: map[string]bool{"a": true}, answer: "b"},
		{name: "first fails", text: "嗨", fail: map[string]bool{"a": true}, answer: "b"},
		{name: "known quota with unknown effect", text: "嗨", quota: map[string]bool{"a": true}, answer: "b"},
		{name: "execution refused before sending", text: "嗨", refuse: map[string]bool{"a": true}, answer: "b"},
		{name: "targeted", text: "@Bob 嗨", answer: "b"},
		{name: "everyone explicitly passes", text: "嗨", pass: map[string]bool{"a": true, "b": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRoomHarness(t, "Alice", "Bob")
			_ = h.engine.Close(roomCtx)
			id := func() string { return fmt.Sprintf("responsible-%d", h.ids.Add(1)) }
			authorize := func(_ context.Context, k pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
				return pa.FeatureState{Enabled: true}, roomSource(k.MachineID), nil
			}
			var err error
			provider := &responsibleRoomPeer{roomPeer: h.peer, pass: tc.pass, fail: tc.fail, quota: tc.quota, refuse: tc.refuse, probes: map[string]int{}}
			h.engine, err = app.NewRuntime(app.RuntimeOptions{Repository: h.bots, Memory: h.bots, WorkBridge: h.peer, Provider: provider, ProjectResolver: roomResolver{h.scope}, Clock: time.Now, ID: id, Authorize: authorize, Rooms: h.driver, Limits: app.RuntimeLimits{MaxBots: 8, MaxProcesses: 4, PromptTimeout: 5 * time.Second, CleanupTimeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = h.engine.Close(roomCtx) })
			h.driver.Attach(h.engine)
			g := h.group(t, "g", h.members...)
			if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "greeting"), tc.text, nil, pa.BotKey{}, nil); err != nil {
				t.Fatal(err)
			}
			h.driver.Idle()
			lines := h.lines(t, g)
			want := 1
			if tc.answer != "" {
				want = 2
			}
			if len(lines) != want || lines[0].Text != tc.text || (want == 2 && lines[1].Author.BotID != tc.answer) {
				t.Fatalf("responsible participant %q, room lines: %+v", tc.answer, lines)
			}
			for _, bot := range h.members {
				turns := h.roomTurns(t, bot)
				if len(turns) > 1 {
					t.Fatalf("member was retried: %+v", turns)
				}
				if tc.fail[bot.Key.BotID] && (len(turns) != 1 || turns[0].State != pa.TurnUnknown || turns[0].Failure == nil || pa.Code(turns[0].Failure) != pa.OutcomeUnknown) {
					t.Fatalf("failure lost its terminal diagnosis: %+v", turns)
				}
			}
			if tc.text == "@Bob 嗨" && len(h.peer.asked("a")) != 0 {
				t.Fatal("explicit target widened to another participant")
			}
			page, err := h.groups.History(roomCtx, g, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			wantMembers := 2
			if tc.text == "@Bob 嗨" {
				wantMembers = 1
			}
			if len(page.MemberOutcomes) != wantMembers || len(page.Activity) != 0 {
				t.Fatalf("terminal room outcomes: %+v", page)
			}
			for _, got := range page.MemberOutcomes {
				switch id := got.Member.BotID; {
				case tc.refuse[id]:
					if got.State != pa.TurnFailed || got.FailureCode != pa.ScopeDenied || got.Disposition != "" || len(h.peer.asked(id)) != 0 {
						t.Fatalf("pre-send refusal was not preserved: %+v", got)
					}
				case tc.fail[id] || tc.quota[id]:
					if got.State != pa.TurnUnknown || got.Disposition != "" || (got.FailureField == "provider_usage_limit") != tc.quota[id] {
						t.Fatalf("room outcome lost its actual diagnosis: %+v", got)
					}
				case id == tc.answer:
					if got.State != pa.TurnSucceeded || got.Disposition != "published" || got.ReplySeq != lines[1].Seq {
						t.Fatalf("room outcome lost its published line: %+v", got)
					}
				default:
					if got.State != pa.TurnSucceeded || got.Disposition != "quiet" || got.ReplySeq != 0 || got.FailureField != "" {
						t.Fatalf("room outcome mislabeled a quiet turn: %+v", got)
					}
				}
			}
		})
	}
}

// Codync's room values; MaxMembers is this test's own bound.
var roomLimits = pa.GroupLimits{MaxMembers: 8, MaxRounds: 3, MaxReplies: 10, MaxLateTurns: 3}

func openRoomDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err = (projectassistantstore.Installer{}).Install(roomCtx, db); err != nil {
		t.Fatal(err)
	}
	if err = (projectassistantstore.GroupInstaller{}).Install(roomCtx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func roomCommand(key pa.BotKey, op pa.Operation, id string) pa.Command {
	return pa.Command{Key: key, Operation: op, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(id)))}
}

func storeBot(t *testing.T, s *projectassistantstore.Store, id, name string, scope pa.ProjectScope) pa.Bot {
	t.Helper()
	b := pa.Bot{Key: pa.BotKey{MachineID: scope.MachineID, BotID: id}, Project: scope, ConversationID: "chat/" + id, Name: name, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1}, Version: 1, CreatedAt: roomAt, UpdatedAt: roomAt}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	summary := pa.BotSummary{Bot: b}
	_, err := s.Commit(roomCtx, app.Mutation{Command: roomCommand(b.Key, pa.CreateOperation, "create/"+id), Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "created/" + id, State: pa.EffectDelivered, ResourceID: id}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "created/" + id, Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.BotCreated, Bot: &summary}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Real Bot rows, membership validation, group state, a whole room turn with
// serial speakers and passes, then reopening the database.
func TestGroupRoomFromStoredBotsThroughReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rooms.db")
	db := openRoomDB(t, path)
	bots := projectassistantstore.New(db)
	scope := pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "repo", Kind: pa.RepositoryProject}
	alice := storeBot(t, bots, "a", "Alice", scope)
	bob := storeBot(t, bots, "b", "Bob", scope)
	rooms, err := app.NewGroupRooms(projectassistantstore.NewGroupStore(db), roomLimits, func() time.Time { return roomAt })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.NewGroupRooms(projectassistantstore.NewGroupStore(db), pa.GroupLimits{}, time.Now); pa.Code(err) != pa.Invalid {
		t.Fatal("rooms without supplied limits")
	}
	person := app.PersonAuthority{Principal: "person", Machine: "m"}
	gkey := pa.BotKey{MachineID: "m", BotID: "g"}
	draft := pa.GroupDraft{Key: gkey, Project: scope, ConversationID: "room/g", Name: "Pair", Members: []pa.BotKey{alice.Key, bob.Key}}
	if _, err = rooms.Create(roomCtx, app.PersonAuthority{}, roomCommand(gkey, pa.CreateOperation, "c"), draft); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("create without the person's authority: %v", err)
	}
	r, err := rooms.Create(roomCtx, person, roomCommand(gkey, pa.CreateOperation, "c"), draft)
	if err != nil || r.ResourceID != "g" {
		t.Fatal(err)
	}
	other := draft
	other.Name = "Changed"
	if _, err = rooms.Create(roomCtx, person, roomCommand(gkey, pa.CreateOperation, "c"), other); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("changed payload under the same key: %v", err)
	}

	// One room turn: round 0 asks both; Alice names Bob, but Bob was shown
	// that line when asked and passed, so round 1 asks nobody and ends it.
	step, err := rooms.PersonWrote(roomCtx, gkey, 0)
	if err != nil || step.Turn == nil {
		t.Fatal(err)
	}
	group, _ := projectassistantstore.NewGroupStore(db).Group(roomCtx, gkey)
	catalog := pa.GroupCatalog{Bots: map[pa.BotKey]pa.Bot{alice.Key: alice, bob.Key: bob}}
	speakers := pa.RoomSpeakers(group, catalog)
	room := []pa.RoomEntry{{Seq: 1, Kind: pa.RoomPersonEntry, Text: "Plan the release"}}
	asked := map[pa.BotKey]int64{}
	answers := map[pa.BotKey][]string{alice.Key: {"Draft ready, @bob please review"}, bob.Key: {"(pass)"}}
	var run pa.RoomRun
	seq := int64(1)
	for !run.Done {
		for _, sp := range pa.RoomSpeakersFor(speakers, room, false, run.Round, asked) {
			ticket, err := rooms.Ask(roomCtx, gkey, sp.Key, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = rooms.Admit(roomCtx, ticket); err != nil {
				t.Fatal(err)
			}
			asked[sp.Key] = seq
			text := answers[sp.Key][0]
			answers[sp.Key] = answers[sp.Key][1:]
			seq++
			entry, ok, err := rooms.Publish(roomCtx, ticket, seq, text)
			if err != nil {
				t.Fatal(err)
			}
			var said *pa.RoomEntry
			if ok {
				room = append(room, entry)
				said = &entry
			}
			if !run.Answered(said, roomLimits) {
				break
			}
		}
		run.EndRound(roomLimits)
	}
	if run.Round != 1 || run.Replies != 1 || run.Last == nil || run.Last.Author != alice.Key {
		t.Fatalf("run %+v", run)
	}
	if len(answers[bob.Key]) != 0 {
		t.Fatal("Bob was not asked")
	}
	step, err = rooms.TurnEnded(roomCtx, gkey, step.State.Revision, step.Turn.Epoch)
	if err != nil || step.Turn != nil {
		t.Fatal(err)
	}

	// A busy member's late ticket, then the group's Stop, survive reopen.
	late, err := rooms.Ask(roomCtx, gkey, bob.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := rooms.Stop(roomCtx, gkey, step.State.Revision)
	if err != nil || !stop.Stopped || len(stop.Cancel) != 2 {
		t.Fatalf("stop %+v %v", stop, err)
	}
	db.Close()
	db = openRoomDB(t, path)
	defer db.Close()
	rooms, _ = app.NewGroupRooms(projectassistantstore.NewGroupStore(db), roomLimits, func() time.Time { return roomAt })
	if _, _, err = rooms.Publish(roomCtx, late, 99, "done at last"); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("late answer after group Stop and reopen: %v", err)
	}
	if _, err = rooms.LateAnswer(roomCtx, gkey, stop.State.Revision, 10); err != nil {
		t.Fatal(err)
	}
	if again, err := rooms.Create(roomCtx, person, roomCommand(gkey, pa.CreateOperation, "c"), draft); err != nil || again != r {
		t.Fatalf("replay after reopen: %+v %v", again, err)
	}
	if _, err = rooms.Delete(roomCtx, person, roomCommand(gkey, pa.DeleteOperation, "d"), 1); err != nil {
		t.Fatal(err)
	}
	if b, err := projectassistantstore.New(db).Bot(roomCtx, alice.Key); err != nil || b.ConversationID != alice.ConversationID {
		t.Fatalf("member after group delete: %v", err)
	}
}
