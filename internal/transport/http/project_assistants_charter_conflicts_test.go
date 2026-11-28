package http

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type conflictFixture struct {
	*paServerFixture
	ctx     context.Context
	store   *projectassistantstore.Store
	bot     pa.Bot
	version int64
}

func newConflictFixture(t *testing.T, instructions string) *conflictFixture {
	t.Helper()
	f := newPAServerFixture(t, true)
	// Sessions open and wait, so a fix turn is accepted and stays queued or running.
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			return &paServerSession{sent: make(chan struct{})}, nil
		}}, nil
	}
	f.enable(t)
	create := f.create()
	create.Settings.Instructions = instructions
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	ctx := context.Background()
	store := f.s.projectAssistantFloorsStore()
	bot, err := store.Bot(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	c := &conflictFixture{paServerFixture: f, ctx: ctx, store: store, bot: bot}
	saved := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan,
		map[string]any{"machine_id": f.key.MachineID, "project_id": bot.Project.ProjectID, "idempotency_key": "charter-1", "expected_version": 0, "text": "## Releases\n\nMerge to main without asking.", "reason": "first"}))
	c.version = saved.Version
	return c
}

func (c *conflictFixture) report(t *testing.T, source pa.CharterConflictSource, sentence, suggested string) pa.CharterConflict {
	t.Helper()
	path := ""
	if source == pa.ConflictInWorkspaceFile {
		path = "docs/playbook.md"
	}
	r, err := c.store.ReportCharterConflict(c.ctx, c.key.MachineID, pa.CharterConflict{ProjectID: c.bot.Project.ProjectID, BotID: c.key.BotID, TurnID: "turn-" + sentence,
		Version: c.version, Source: source, Path: path, BotSentence: sentence, CharterSentence: "Merge to main without asking.", Section: "Releases", Suggested: suggested, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (c *conflictFixture) resolve(t *testing.T, id, action, key string) contract.ProjectAssistantCharterConflictsResponse {
	t.Helper()
	return paCharterAnswer[contract.ProjectAssistantCharterConflictsResponse](t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/conflict-resolve", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": c.bot.Project.ProjectID, "idempotency_key": key, "conflict_id": id, "action": action}))
}

func conflictState(t *testing.T, answer contract.ProjectAssistantCharterConflictsResponse, id string) contract.ProjectAssistantCharterConflictState {
	t.Helper()
	for _, c := range answer.Conflicts {
		if c.ID == id {
			return c.State
		}
	}
	return ""
}

// fixTurn finds the Bot's queued turn that carries the report as its mark.
func (c *conflictFixture) fixTurn(t *testing.T, report string) *pa.Turn {
	t.Helper()
	export, err := c.store.Export(c.ctx, c.key)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range export.Turns {
		if turn.CharterFix != nil && turn.CharterFix.ReportID == report {
			return &turn
		}
	}
	return nil
}

// Following the charter replaces the Bot's sentence itself when it appears
// exactly once in its instructions; twice, it asks the Bot instead, as the
// person's own typed turn carrying the report.
func TestFollowingTheCharterRewritesInstructionsOnlyWhenTheSentenceIsThereOnce(t *testing.T) {
	c := newConflictFixture(t, "Ask before every merge. Keep replies short.")
	once := c.report(t, pa.ConflictInInstructions, "Ask before every merge.", "Merge to main without asking.")
	answer := c.resolve(t, once.ID, "follow_charter", "follow-1")
	if conflictState(t, answer, once.ID) != contract.ProjectAssistantCharterConflictStateFixed {
		t.Fatalf("a sentence found once is fixed by the daemon: %+v", answer.Conflicts)
	}
	bot, err := c.store.Bot(c.ctx, c.key)
	if err != nil || bot.Instructions.Text != "Merge to main without asking. Keep replies short." {
		t.Fatalf("instructions after follow_charter: %q %v", bot.Instructions.Text, err)
	}
	if c.fixTurn(t, once.ID) != nil {
		t.Fatalf("a daemon fix sends the Bot nothing")
	}
	// The same key answers the same outcome; another key on a handled report is stale.
	if again := c.resolve(t, once.ID, "follow_charter", "follow-1"); conflictState(t, again, once.ID) != contract.ProjectAssistantCharterConflictStateFixed {
		t.Fatalf("a repeated choice: %+v", again.Conflicts)
	}
	paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/conflict-resolve", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": c.bot.Project.ProjectID, "idempotency_key": "follow-2", "conflict_id": once.ID, "action": "dismiss"}), 409, pa.StaleVersion)

	// A sentence found twice is left for the Bot.
	c = newConflictFixture(t, "Keep replies short. Ask first. Keep replies short.")
	twice := c.report(t, pa.ConflictInInstructions, "Keep replies short.", "")
	answer = c.resolve(t, twice.ID, "follow_charter", "follow-3")
	if conflictState(t, answer, twice.ID) != contract.ProjectAssistantCharterConflictStateHandedToBot {
		t.Fatalf("a sentence found twice goes to the Bot: %+v", answer.Conflicts)
	}
	turn := c.fixTurn(t, twice.ID)
	if turn == nil || turn.Origin != pa.UserOrigin || turn.CharterFix.BotSentence != "Keep replies short." {
		t.Fatalf("the Bot gets a typed fix turn: %+v", turn)
	}
	if bot, _ := c.store.Bot(c.ctx, c.key); bot.Instructions.Text != "Keep replies short. Ask first. Keep replies short." {
		t.Fatalf("instructions are left alone when the Bot is asked: %q", bot.Instructions.Text)
	}
}

// A short memory fact holding the sentence once is rewritten through the
// memory save path; a sentence that is nowhere in memory goes to the Bot.
func TestFollowingTheCharterRewritesAMemoryFactOrAsksTheBot(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	current, err := c.store.Current(c.ctx, c.key)
	if err != nil {
		t.Fatal(err)
	}
	seed := contract.ProjectAssistantMemoryWriteRequest{Command: c.command(pa.MemorySaveOperation, "seed-fact"), ExpectedRevision: current.Revision,
		Entry: contract.ProjectAssistantMemoryEntry{ID: "fact-1", Key: paWireBotKey(c.key), Project: paWireProjectScope(c.bot.Project), Text: "Deploys need the person's approval. Use pnpm.", Sources: []contract.ProjectAssistantMemorySource{}}}
	paServerOK(t, c.request(t, "POST", "/v1/project-assistants/"+c.key.BotID+"/memory", c.local, seed))
	fact := c.report(t, pa.ConflictInMemory, "Deploys need the person's approval.", "")
	answer := c.resolve(t, fact.ID, "follow_charter", "memory-1")
	if conflictState(t, answer, fact.ID) != contract.ProjectAssistantCharterConflictStateFixed {
		t.Fatalf("a memory fact holding the sentence once is fixed: %+v", answer.Conflicts)
	}
	entries, err := c.store.MemoryEntries(c.ctx, c.key)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, e := range entries {
		if e.ID == "fact-1" && !e.Forgotten {
			text = e.Text
		}
	}
	if text != "Use pnpm." {
		t.Fatalf("the fact after removing the sentence: %q (%+v)", text, entries)
	}
	missing := c.report(t, pa.ConflictInMemory, "Never touch main.", "")
	answer = c.resolve(t, missing.ID, "follow_charter", "memory-2")
	if conflictState(t, answer, missing.ID) != contract.ProjectAssistantCharterConflictStateHandedToBot || c.fixTurn(t, missing.ID) == nil {
		t.Fatalf("a sentence not in memory goes to the Bot: %+v", answer.Conflicts)
	}
}

// change_charter marks the report handed to a charter change; dismiss
// removes it from the list. The list counts Bots that checked the current
// version and reported nothing, and the roster listing counts open reports.
func TestChangeCharterAndDismissOverHTTPAndCloud(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	one := c.report(t, pa.ConflictInWorkspaceFile, "Ask before merging.", "")
	two := c.report(t, pa.ConflictInInstructions, "Ask before pushing.", "")
	list := paCharterAnswer[contract.ProjectAssistantChartersResponse](t, c.request(t, "GET", chartersPath+"?machine_id="+c.key.MachineID, c.readonly, nil))
	if len(list.Charters) != 1 || list.Charters[0].OpenConflicts != 2 {
		t.Fatalf("the listing counts open reports: %+v", list.Charters)
	}
	if err := c.store.RecordCharterCheck(c.ctx, c.key.MachineID, c.bot.Project.ProjectID, "other-bot", c.version, "t", time.Now()); err != nil {
		t.Fatal(err)
	}
	answer := c.resolve(t, one.ID, "change_charter", "change-1")
	if conflictState(t, answer, one.ID) != contract.ProjectAssistantCharterConflictStateHandedToCharter || c.fixTurn(t, one.ID) != nil {
		t.Fatalf("change_charter: %+v", answer.Conflicts)
	}

	// The same choice as Cloud words: an operating pinned device resolves, a
	// read-only one only reads.
	c.pin(t, "owner", adaptercloud.ScopeOperate, false)
	c.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	bridge := cloudops.Bridge{MachineID: c.identity.MachineID, AllowCommands: func() bool { return true }, Router: cloudtransport.Router{
		Handler: c.h, Authorize: cloudtransport.LocalAuthorizer(c.local, c.machine)}}
	sequence := uint64(0)
	word := func(sender, op, request string, fields map[string]any) cloudops.Answer {
		body := map[string]any{"type": "project-assistants." + op, "session": cloudops.MachineReplySession, "request": request, "machine_id": c.key.MachineID, "project_id": c.bot.Project.ProjectID}
		maps.Copy(body, fields)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		sequence++
		return bridge.Handle(c.ctx, cloudops.Command{Channel: "ctl/" + c.key.MachineID, Class: cloudops.ClassCtl, Sender: sender, Sequence: sequence, Plaintext: raw})
	}
	dismiss := map[string]any{"idempotency_key": "dismiss-1", "conflict_id": two.ID, "action": "dismiss"}
	if a := word("reader", "charter-conflict-resolve", "dismiss-1", dismiss); a.Status < 400 {
		t.Fatalf("a read-only device resolved a report: %d %s", a.Status, a.Payload)
	}
	if a := word("owner", "charter-conflict-resolve", "dismiss-1", dismiss); a.Status != 200 {
		t.Fatalf("Cloud resolve: %d %s %s %s", a.Status, a.Code, a.Field, a.Payload)
	}
	read := word("reader", "charter-conflicts", "read-1", nil)
	var envelope struct {
		Body contract.ProjectAssistantCharterConflictsResponse `json:"body"`
	}
	if read.Status != 200 || json.Unmarshal(read.Payload, &envelope) != nil {
		t.Fatalf("Cloud read: %d %s", read.Status, read.Payload)
	}
	got := envelope.Body
	if conflictState(t, got, two.ID) != contract.ProjectAssistantCharterConflictStateDismissed || conflictState(t, got, one.ID) != contract.ProjectAssistantCharterConflictStateHandedToCharter {
		t.Fatalf("a dismissed report is available in history: %+v %s", got.Conflicts, read.Payload)
	}
	// other-bot is not a visible Bot of the Project; the fixture Bot reported.
	if got.CheckedWithoutReport != 0 || got.Version != c.version || got.Limits.Open != pa.MaxCharterConflicts {
		t.Fatalf("the count of Bots that checked without reporting: %+v", got)
	}
	if strings.Contains(string(read.Payload), `"state":"open"`) {
		t.Fatalf("no report is open: %s", read.Payload)
	}
}
