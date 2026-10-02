package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// goodMilestone is a summary a receiver can continue from: every section,
// links into the project, and nothing copied from a conversation.
const goodMilestone = `# Milestone: the export route answers in pages

## Goal
The export route answers large projects in pages instead of one body.

## Verified decisions
- Pages are 500 rows; measured in internal/export/page_test.go.
- The cursor is the last row id, not an offset.

## Blockers
- The deploy waits on the person's approval of the new route.

## Evidence
- internal/export/page.go
- docs/export.md "Pages"
- ` + "`clawdline item steps 7bd00000-0000-4000-8000-000000000001`" + `

## Next step
Write the console's pager against internal/export/page.go, then ask for the deploy.
`

func problemRules(problems []MilestoneProblem) string {
	var rules []string
	for _, p := range problems {
		rules = append(rules, p.Rule)
	}
	return strings.Join(rules, ",")
}

func TestAMilestoneSummaryWithEverySectionAndLinkedEvidencePasses(t *testing.T) {
	if p := CheckMilestoneSummary([]byte(goodMilestone)); len(p) != 0 {
		t.Fatalf("a good summary was refused: %+v", p)
	}
}

func TestAMilestoneSummaryIsRefusedForEachThingItMustNotBe(t *testing.T) {
	// A credential made at run time, so this file carries none: a known
	// prefix and a value that is neither a fixture nor a pattern.
	sum := sha256.Sum256([]byte("milestone"))
	key := "ghp_" + hex.EncodeToString(sum[:])[:36]
	fence := "```\n" + strings.Repeat("log line\n", milestoneVerbatimLines+2) + "```\n"
	for _, tc := range []struct {
		name, text, rule string
	}{
		{"too long", goodMilestone + strings.Repeat("x", MilestoneSummaryLimit), "too_long"},
		{"missing section", strings.Replace(goodMilestone, "## Blockers\n", "", 1), "section_missing"},
		{"empty section", strings.Replace(goodMilestone, "- The deploy waits on the person's approval of the new route.\n", "", 1), "section_empty"},
		{"unknown section", goodMilestone + "\n## Chat log\nnothing\n", "section_unknown"},
		{"credential", strings.Replace(goodMilestone, "## Next step\n", "## Next step\nUse "+key+" for the push.\n", 1), "credential"},
		{"speaker turn", strings.Replace(goodMilestone, "## Next step\n", "## Next step\nUser: can you also fix the pager?\n", 1), "transcript"},
		{"assistant markup", strings.Replace(goodMilestone, "## Next step\n", "## Next step\n<system-reminder>a reminder</system-reminder>\n", 1), "transcript"},
		{"pasted log", strings.Replace(goodMilestone, "## Next step\n", "## Next step\n"+fence, 1), "verbatim"},
		{"evidence without links", strings.Replace(strings.Replace(strings.Replace(goodMilestone,
			"- internal/export/page.go\n", "- the page code\n", 1),
			"- docs/export.md \"Pages\"\n", "", 1),
			"- `clawdline item steps 7bd00000-0000-4000-8000-000000000001`\n", "", 1), "evidence_unlinked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := CheckMilestoneSummary([]byte(tc.text))
			if !strings.Contains(problemRules(problems), tc.rule) {
				t.Fatalf("problems = %s, want %s", problemRules(problems), tc.rule)
			}
			for _, p := range problems {
				if strings.Contains(p.Detail, key) {
					t.Fatalf("a problem repeated the credential: %q", p.Detail)
				}
			}
		})
	}
}

// The receiver rebuilds its next step from the summary and the evidence it
// links, with nothing else: every link in Evidence is a file it can open, and
// Next step names what to do with one of them.
func TestAReceiverCanRebuildTheNextStepFromTheSummaryAndItsEvidence(t *testing.T) {
	project := t.TempDir()
	for _, f := range []string{"internal/export/page.go", "docs/export.md"} {
		if err := os.MkdirAll(filepath.Join(project, filepath.Dir(f)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := ParseMilestoneSummary([]byte(goodMilestone))
	next := sections["Next step"]
	if next == "" {
		t.Fatal("no next step")
	}
	paths := regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+`).FindAllString(sections["Evidence"], -1)
	if len(paths) < 2 {
		t.Fatalf("evidence named %d paths: %q", len(paths), sections["Evidence"])
	}
	named := false
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(project, p)); err != nil {
			t.Fatalf("evidence %s does not resolve: %v", p, err)
		}
		named = named || strings.Contains(next, p)
	}
	if !named {
		t.Fatalf("next step names no evidence it continues from: %q", next)
	}
}

// The drill the acceptance asks for: a sender holding a deploying item that
// waits on the person's decision, a running child, a child owed a landing
// and a notice not yet acknowledged hands over at a milestone. obligations.md
// names every one of them, the item moves to the receiver with its decision,
// and every task stays the sender's, so the sender's close still finds them.
func TestAMilestoneHandoffKeepsOwnersAndReceiptsTraceable(t *testing.T) {
	b, ctx := newTestBroker(t)
	project := t.TempDir()
	now := time.Now().Truncate(time.Second)
	itemID := "3a100000-0000-4000-8000-000000000001"
	decision := "3a100000-0000-4000-8000-000000000002"
	item := work.ItemV2{ID: itemID, ProjectID: "p", ProjectPath: project, Kind: work.KindFeature,
		Title: "Export pages", Phase: work.PhaseDeploying, Condition: work.ConditionWaitingUser, DecisionID: decision,
		DeploymentPolicy: work.DeployRequired, OwnerSession: rootConversation, CreatedBy: "person",
		CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
	if err := b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		return tx.PutDecision(work.Decision{ID: decision, Session: rootConversation, WorkID: itemID, Project: project,
			Question: "Deploy the export pages?", Options: []work.Option{{ID: "done", Label: "Deployed"}, {ID: "later", Label: "Later"}},
			Default: "later", Blocking: true, State: work.DecisionOpen, Push: work.PushNone, CreatedAt: now, DueAt: now.Add(time.Hour)}, nil, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(item, "person", "{}"); err != nil {
			return err
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: NewUUID(), WorkID: itemID, Mode: "existing_session",
			SessionID: rootConversation, TerminalID: "%1", Assistant: "claude", State: "active",
			HumanActor: "person", CreatedAt: now, UpdatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	running := "3a100000-0000-4000-8000-000000000011"
	owed := "3a100000-0000-4000-8000-000000000012"
	notified := "3a100000-0000-4000-8000-000000000013"
	root := &RootRef{SessionID: rootConversation, Assistant: "claude"}
	for _, r := range []Record{
		{Protocol: Protocol, ID: running, Assistant: "claude", Title: "Pager", State: StateBriefed, Root: root,
			CreatedAt: now, Claims: []string{}, ProjectDir: project, TimeoutMinutes: 240},
		{Protocol: Protocol, ID: owed, Assistant: "claude", Title: "Cursor", State: StateSuccess, Root: root,
			CreatedAt: now, FinishedAt: now, Claims: []string{"a.go"}, ProjectDir: project, TimeoutMinutes: 240,
			Landing: &Landing{State: LandingPending}},
		{Protocol: Protocol, ID: notified, Assistant: "claude", Title: "Docs", State: StateBriefed, Root: root,
			CreatedAt: now, Claims: []string{}, ProjectDir: project, TimeoutMinutes: 240},
	} {
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Settle(ctx, notified, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}

	o := b.gatherMilestoneObligations(ctx, rootConversation, []string{itemID})
	text := renderMilestoneObligations(rootConversation, o)
	for _, want := range []string{itemID, decision, running, owed, notified, "deploying"} {
		if !strings.Contains(text, want) {
			t.Fatalf("obligations.md leaves out %s:\n%s", want, text)
		}
	}
	if o.ItemsUnread || o.TasksUnread || o.NoticeUnread {
		t.Fatalf("a source was unread: %+v", o)
	}
	carried := strings.Join(o.Carried(), ",")
	for _, id := range []string{running, owed, notified} {
		if !strings.Contains(carried, id) {
			t.Fatalf("carried %s, missing %s", carried, id)
		}
	}

	id := "3a100000-0000-4000-8000-000000000020"
	h := Handoff{ID: id, State: HandoffDelivered, ProjectDir: project, FromSession: rootConversation,
		Assistant: "claude", BoardItems: []string{itemID}, Milestone: true, Carried: o.Carried(),
		Opened: &openedSession{TerminalID: "%2"}}
	if err := b.createOpened(ctx, store.TableHandoffs, id, HandoffDelivered, h, "handoff.delivered", now); err != nil {
		t.Fatal(err)
	}
	to := "3a100000-0000-4000-8000-000000000030"
	read := reading{sessions: map[string]session.Session{"%2": {ID: "%2", Assistant: session.AssistantClaude,
		ConversationID: to, State: session.StateIdle, Activity: session.Activity{At: now}}}}
	if n := b.tendHandoffBoards(ctx, read); n != 1 {
		t.Fatalf("moved %d handoffs", n)
	}
	got, err := b.Store.WorkV2Item(ctx, itemID)
	if err != nil || got.OwnerSession != to || got.DecisionID != decision || got.Phase != work.PhaseDeploying {
		t.Fatalf("item after handoff: owner=%s decision=%s phase=%s err=%v", got.OwnerSession, got.DecisionID, got.Phase, err)
	}
	// The person's question goes with the work: still open, now the
	// receiver's, so the answer reaches the Session that continues it.
	var asked work.Decision
	if err := b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		var err error
		asked, err = tx.Decision(decision)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if asked.State != work.DecisionOpen || asked.Session != to {
		t.Fatalf("decision after handoff: state=%s session=%s, want open and %s", asked.State, asked.Session, to)
	}
	stored, err := b.HandoffByID(ctx, id)
	if err != nil || stored.Receiver != to {
		t.Fatalf("receiver = %q (%v), want %s", stored.Receiver, err, to)
	}
	for _, task := range []string{running, owed, notified} {
		r, _, err := b.Record(ctx, task)
		if err != nil || r.Root == nil || r.Root.SessionID != rootConversation {
			t.Fatalf("task %s left its sender: %+v %v", task, r.Root, err)
		}
	}
	unacked, err := b.RootCompletions(ctx, rootConversation)
	if err != nil || len(unacked) != 1 || unacked[0].Record.ID != notified {
		t.Fatalf("the sender's unacknowledged notices = %d (%v), want %s", len(unacked), err, notified)
	}
}

// A milestone handoff that carried no Board item still names its receiver,
// so the ledger can join the two Sessions; an ordinary one is left alone.
func TestAMilestoneHandoffWithNoItemsStillNamesItsReceiver(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Now()
	for _, tc := range []struct {
		id, terminal string
		milestone    bool
	}{
		{"3a100000-0000-4000-8000-000000000040", "%3", true},
		{"3a100000-0000-4000-8000-000000000041", "%4", false},
	} {
		h := Handoff{ID: tc.id, State: HandoffDelivered, FromSession: rootConversation, Assistant: "claude",
			Milestone: tc.milestone, Opened: &openedSession{TerminalID: tc.terminal}}
		if err := b.createOpened(ctx, store.TableHandoffs, tc.id, HandoffDelivered, h, "handoff.delivered", now); err != nil {
			t.Fatal(err)
		}
	}
	read := reading{sessions: map[string]session.Session{
		"%3": {ID: "%3", Assistant: session.AssistantClaude, ConversationID: "3a100000-0000-4000-8000-000000000050",
			State: session.StateIdle, Activity: session.Activity{At: now}},
		"%4": {ID: "%4", Assistant: session.AssistantClaude, ConversationID: "3a100000-0000-4000-8000-000000000051",
			State: session.StateIdle, Activity: session.Activity{At: now}},
	}}
	b.tendHandoffBoards(ctx, read)
	milestone, _ := b.HandoffByID(ctx, "3a100000-0000-4000-8000-000000000040")
	plain, _ := b.HandoffByID(ctx, "3a100000-0000-4000-8000-000000000041")
	if milestone.Receiver != "3a100000-0000-4000-8000-000000000050" || plain.Receiver != "" {
		t.Fatalf("receivers: milestone=%q plain=%q", milestone.Receiver, plain.Receiver)
	}
}

func TestMilestoneLineNamesBothFilesAndWhatStaysWithTheSender(t *testing.T) {
	line := MilestoneHandoffLine("/state/handoffs/x")
	for _, want := range []string{"/state/handoffs/x/handoff.md", "/state/handoffs/x/obligations.md", "Next step", "do not acknowledge or land"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line leaves out %q: %s", want, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Fatal("the line is typed: it is one line")
	}
}
