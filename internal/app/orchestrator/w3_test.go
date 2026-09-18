package orchestrator

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// W3 (docs/design-decisions.md §6): landing has one record, and it proves what
// it says — D17, D18, D19, D51 and broker-design #35. Every test names the
// criterion it answers; each "refused" sits beside a request under the same
// conditions that is accepted, so a check that refuses everything cannot pass.

// gitIn runs git in a directory and answers its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFile writes one file on the checked-out branch and commits it.
func commitFile(t *testing.T, repo, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", name)
	gitIn(t, repo, "commit", "-q", "-m", name)
	return gitIn(t, repo, "rev-parse", "HEAD")
}

// eventCount is how many events of one kind a task has, read from the file.
func eventCount(t *testing.T, dir, id, kind string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "clawdline.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE subject = ? AND kind = ?`, id, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func refusalReason(err error) string {
	ref, ok := err.(Refusal)
	if !ok {
		return ""
	}
	reason, _ := ref.Extra["reason"].(string)
	return reason
}

// isolatedTask stores a briefed isolated task whose delivery branch is cut at
// the repository's current HEAD, the way planWorktree records one.
func isolatedTask(t *testing.T, b *Broker, ctx context.Context, repo, id string) Record {
	t.Helper()
	base := gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "branch", BranchName(id), base)
	r := Record{
		Protocol: Protocol, ID: id, Assistant: "claude", Title: "w3", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"}, ProjectDir: repo, Repository: repo,
		Isolation: IsolationWorktree, LeaseScope: LeaseWorktree,
		Worktree: &Worktree{Repository: repo, Path: filepath.Join(b.Dir, "wt", id), Branch: BranchName(id),
			Base: base, Head: base},
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"},
	}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	return r
}

// onBranch commits one file on a branch and comes back to main.
func onBranch(t *testing.T, repo, branch, name string) string {
	t.Helper()
	gitIn(t, repo, "checkout", "-q", branch)
	c := commitFile(t, repo, name, name+"\n")
	gitIn(t, repo, "checkout", "-q", "main")
	return c
}

func land(b *Broker, ctx context.Context, id, state, target, commit string) (Record, error) {
	return b.Land(ctx, id, LandingRequest{State: state, Target: target, Commit: commit, Machine: true})
}

// cutover A5 with D51, for an isolated task; D18 on the same record; G17.
func TestAnIsolatedLandingProvesItsOwnDelivery(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000001-0000-4000-8000-000000000001"
	r := isolatedTask(t, b, ctx, repo, id)
	base := r.Worktree.Base
	delivery := onBranch(t, repo, BranchName(id), "work.go")

	// G17: settling records what the branch holds. Before W3 the head was
	// written once, as the base, and never again.
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Worktree.Head != delivery {
		t.Fatalf("settled head = %q, want the branch's commit %q (base %q)", settled.Worktree.Head, delivery, base)
	}
	if settled.Landing == nil || settled.Landing.State != LandingPending || settled.Landing.Target != "" {
		t.Fatalf("landing opened as %+v, want pending with no target (D19)", settled.Landing)
	}

	// A5: a commit the target does not contain is refused. The delivery is
	// not on main yet.
	if _, err := land(b, ctx, id, "landed", "main", delivery); refusalCode(err) != "unverified_landing" ||
		refusalReason(err) != UnverifiedNotOn {
		t.Fatalf("landing a commit main does not contain answered %v, want unverified_landing/not_on_target", err)
	}

	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", BranchName(id))
	merge := gitIn(t, repo, "rev-parse", "HEAD")

	// D51: the base is on main — it always is — and is refused by name. This
	// is exactly the request the first version accepted.
	if _, err := land(b, ctx, id, "landed", "main", base); refusalCode(err) != "unverified_landing" ||
		refusalReason(err) != UnverifiedPredates {
		t.Fatalf("landing the task's own base answered %v, want unverified_landing/predates_dispatch", err)
	}
	before := rawRecord(t, b.Dir, id)

	// The control: the real delivery, same target, same moment, is accepted.
	landed, err := land(b, ctx, id, "landed", "main", merge)
	if err != nil {
		t.Fatalf("the real delivery was refused: %v", err)
	}
	l := landed.Landing
	if l.State != LandingLanded || l.Commit != merge || l.DeliveryHead != delivery || l.Base != base ||
		l.TargetCommit != merge || l.Target != "main" || l.CorrectedFrom != nil {
		t.Fatalf("landing = %+v", l)
	}
	if got := rawRecord(t, b.Dir, id); got == before {
		t.Fatal("the landing was not written")
	}

	// D18: the same commit again — spelled short — is a replay: no write, no
	// event, and not an error.
	written := rawRecord(t, b.Dir, id)
	events := eventCount(t, b.Dir, id, "landing.landed")
	if _, err := land(b, ctx, id, "landed", "main", merge[:10]); err != nil {
		t.Fatalf("a replay of the recorded landing answered %v", err)
	}
	if got := rawRecord(t, b.Dir, id); got != written {
		t.Fatalf("a replay wrote the record:\nbefore %s\nafter  %s", written, got)
	}
	if n := eventCount(t, b.Dir, id, "landing.landed"); n != events {
		t.Fatalf("a replay added an event: %d → %d", events, n)
	}

	// D18: a different commit that passes the gate is a correction: applied,
	// with what it replaced kept and an event that says so.
	later := commitFile(t, repo, "later.go", "later\n")
	corrected, err := land(b, ctx, id, "landed", "main", later)
	if err != nil {
		t.Fatalf("a correction to a later commit carrying the delivery was refused: %v", err)
	}
	if corrected.Landing.Commit != later || corrected.Landing.CorrectedFrom == nil ||
		corrected.Landing.CorrectedFrom.Commit != merge || corrected.Landing.CorrectedFrom.CorrectedFrom != nil {
		t.Fatalf("corrected landing = %+v from %+v", corrected.Landing, corrected.Landing.CorrectedFrom)
	}
	if n := eventCount(t, b.Dir, id, "landing.corrected"); n != 1 {
		t.Fatalf("landing.corrected events = %d, want 1", n)
	}

	// D18: a different commit that fails the gate is refused, and the record
	// is exactly as it was.
	standing := rawRecord(t, b.Dir, id)
	if _, err := land(b, ctx, id, "landed", "main", base); refusalReason(err) != UnverifiedPredates {
		t.Fatalf("correcting to the base answered %v", err)
	}
	if got := rawRecord(t, b.Dir, id); got != standing {
		t.Fatal("a refused correction changed the record")
	}
	// A different target is another claim, not a correction.
	gitIn(t, repo, "branch", "release", later)
	if _, err := land(b, ctx, id, "landed", "release", later); refusalCode(err) != "landing_conflict" {
		t.Fatalf("landing on another target answered %v, want landing_conflict", err)
	}
	if n := eventCount(t, b.Dir, id, "landing.corrected"); n != 1 {
		t.Fatalf("refusals added corrections: %d", n)
	}
}

// D17: an isolated branch that carries nothing past its base has no delivery
// to prove, even when the target has moved on and the commit named is new.
func TestAnEmptyDeliveryCannotBeLanded(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000002-0000-4000-8000-000000000002"
	r := isolatedTask(t, b, ctx, repo, id)
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	other := commitFile(t, repo, "someone-else.go", "x\n")

	_, err := land(b, ctx, id, "landed", "main", other)
	if refusalReason(err) != UnverifiedNothing {
		t.Fatalf("landing someone else's commit for an empty delivery answered %v, want nothing_delivered", err)
	}
	// The control: once the branch carries work and main has it, the same
	// route with the same target accepts it.
	delivery := onBranch(t, repo, BranchName(id), "work.go")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", BranchName(id))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	landed, err := land(b, ctx, id, "landed", "main", merge)
	if err != nil {
		t.Fatalf("the delivery committed after settlement was refused: %v", err)
	}
	// The proof follows the branch: a root that commits a child's work onto
	// its branch after it settled has added to the delivery (G17).
	if landed.Landing.DeliveryHead != delivery || landed.Worktree.Head != delivery || r.Worktree.Base == delivery {
		t.Fatalf("delivery head %q, worktree head %q, want %q", landed.Landing.DeliveryHead, landed.Worktree.Head, delivery)
	}
	// A commit on main that does not carry the delivery is not it.
	if _, err := land(b, ctx, id, "landed", "main", other); refusalReason(err) != UnverifiedNotCarried {
		t.Fatalf("correcting to a commit without the delivery answered %v", err)
	}
}

// cutover A5 with D51, for a task that writes the shared checkout: the line is
// where the repository stood when it was dispatched.
func TestASharedCheckoutLandingMustPostdateItsDispatch(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	earlier := gitIn(t, repo, "rev-parse", "HEAD")
	base := commitFile(t, repo, "b.go", "package a\n")

	id := "d3000003-0000-4000-8000-000000000003"
	writeBrief(t, b, id, repo, map[string]any{"claims": []string{"a.go"}})
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Generation: inv.Generation, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.DispatchBase != base {
		t.Fatalf("dispatch base = %q, want HEAD at dispatch %q", out.Record.DispatchBase, base)
	}
	if !out.Record.State.Terminal() {
		// No terminal in a test: the tab does not open and the task settles.
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}

	for _, stale := range []string{base, earlier} {
		if _, err := land(b, ctx, id, "landed", "main", stale); refusalReason(err) != UnverifiedPredates {
			t.Fatalf("landing %s, already there at dispatch, answered %v, want predates_dispatch", stale[:7], err)
		}
	}
	gitIn(t, repo, "checkout", "-q", "-b", "side")
	side := commitFile(t, repo, "side.go", "side\n")
	gitIn(t, repo, "checkout", "-q", "main")
	if _, err := land(b, ctx, id, "landed", "main", side); refusalReason(err) != UnverifiedNotOn {
		t.Fatalf("landing a commit main does not contain answered %v, want not_on_target", err)
	}
	// The control: a commit made on main after the dispatch.
	work := commitFile(t, repo, "a.go", "package a // changed\n")
	landed, err := land(b, ctx, id, "landed", "main", work)
	if err != nil {
		t.Fatalf("a commit made after the dispatch was refused: %v", err)
	}
	if landed.Landing.Commit != work || landed.Landing.Base != base || landed.Landing.DeliveryHead != "" {
		t.Fatalf("landing = %+v", landed.Landing)
	}
}

// DG-7: a record that never learned its line cannot be proved landed, and the
// refusal says why.
func TestALandingWithNoRecordedBaseIsRefused(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000004-0000-4000-8000-000000000004"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "pre-W3", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"}, ProjectDir: repo, Repository: repo,
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	work := commitFile(t, repo, "a.go", "package a // changed\n")
	if _, err := land(b, ctx, id, "landed", "main", work); refusalReason(err) != UnverifiedBase {
		t.Fatalf("landing a task with no recorded base answered %v, want base_unknown", err)
	}
	// It can still be closed by the states that prove nothing about git.
	if _, err := land(b, ctx, id, "abandoned", "", ""); err != nil {
		t.Fatalf("abandoning it answered %v", err)
	}
}

// D18 for the declarations: an identical resend writes nothing; a different
// note is a correction that keeps what it replaced.
func TestASettledDeclarationIsCorrectedNotOverwritten(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "d3000005-0000-4000-8000-000000000005"
	finished(t, b, ctx, id)
	if _, err := b.Land(ctx, id, LandingRequest{State: "abandoned", Note: "superseded", Machine: true}); err != nil {
		t.Fatal(err)
	}
	written := rawRecord(t, b.Dir, id)
	if _, err := b.Land(ctx, id, LandingRequest{State: "abandoned", Machine: true}); err != nil {
		t.Fatal(err)
	}
	if got := rawRecord(t, b.Dir, id); got != written {
		t.Fatal("an identical resend of a settled declaration wrote the record")
	}
	r, err := b.Land(ctx, id, LandingRequest{State: "abandoned", Note: "replaced by d3000006", Machine: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Landing.Note != "replaced by d3000006" || r.Landing.CorrectedFrom == nil ||
		r.Landing.CorrectedFrom.Note != "superseded" {
		t.Fatalf("landing = %+v", r.Landing)
	}
	if n := eventCount(t, b.Dir, id, "landing.corrected"); n != 1 {
		t.Fatalf("landing.corrected = %d", n)
	}
	if _, err := b.Land(ctx, id, LandingRequest{State: "nothing_to_land", Machine: true}); refusalCode(err) != "invalid_transition" {
		t.Fatalf("moving a settled landing to another state answered %v", err)
	}
}

// D19: merged is asked of the target the record names, and nothing else. With
// none named it is unknown — and a pending landing is never filed as settled.
func TestTheInventoryReadsTheRecordedTarget(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000007-0000-4000-8000-000000000007"
	r := isolatedTask(t, b, ctx, repo, id)
	// The task's checkout, on disk and clean: the case in which the first
	// version filed a merged branch as droppable.
	gitIn(t, repo, "worktree", "add", "-q", r.Worktree.Path, BranchName(id))
	delivery := commitFile(t, r.Worktree.Path, "work.go", "work\n")
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	// Merged into the main checkout's HEAD, which is what the first version
	// asked about: it filed this row as settled on no decision of anybody's.
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", BranchName(id))
	merge := gitIn(t, repo, "rev-parse", "HEAD")

	row := func() (InventoryRow, map[string]any) {
		t.Helper()
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range InventoryPayload(inv)["unlanded"].([]map[string]any) {
			if item["task"] == id {
				for _, r := range inv.Unlanded {
					if r.Task == id {
						return r, item
					}
				}
			}
		}
		t.Fatalf("task not unlanded: live %d unlanded %d droppable %d", len(inv.Live), len(inv.Unlanded), len(inv.Droppable))
		return InventoryRow{}, nil
	}
	u, wire := row()
	if u.Target != "" || u.Merged != nil || wire["target"] != nil || wire["merged"] != nil {
		t.Fatalf("no target recorded: row target %q merged %v, wire %v/%v — want unknown", u.Target, u.Merged, wire["target"], wire["merged"])
	}
	if u.Head != delivery {
		t.Fatalf("inventory head = %q, want the delivery %q (G17)", u.Head, delivery)
	}

	// The root names a target the delivery is not on: false, and asked of
	// that branch — not of HEAD, which has it.
	gitIn(t, repo, "branch", "release", u.Head+"~1")
	if _, err := b.Land(ctx, id, LandingRequest{State: "pending", Target: "release", Machine: true}); err != nil {
		t.Fatal(err)
	}
	u, wire = row()
	if u.Target != "release" || u.Merged == nil || *u.Merged || wire["merged"] != false {
		t.Fatalf("target release: merged %v wire %v, want false", u.Merged, wire["merged"])
	}
	// Named main, which has it: true — and still unlanded, because the
	// record says pending and the record is the one answer (D01).
	if _, err := b.Land(ctx, id, LandingRequest{State: "pending", Target: "main", Machine: true}); err != nil {
		t.Fatal(err)
	}
	u, wire = row()
	if u.Merged == nil || !*u.Merged || wire["merged"] != true || wire["target"] != "main" {
		t.Fatalf("target main: merged %v wire %v, want true", u.Merged, wire["merged"])
	}
	if _, err := land(b, ctx, id, "landed", "", merge); err != nil {
		t.Fatalf("landing on the recorded target: %v", err)
	}
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range inv.Unlanded {
		if u.Task == id {
			t.Fatal("a landed task is still unlanded")
		}
	}
	// Once the record says landed, a clean checkout of a merged branch is
	// droppable again: the rule was only ever kept from overruling a record.
	//
	// And only once its owner is provably gone (D13, G22, W6): with no
	// reading of the machine the advice is withheld — the control — and it
	// is given when the child's own source answered completely without its
	// tab and no process works inside the checkout.
	for _, d := range inv.Droppable {
		if d.Task == id {
			t.Fatalf("droppable with no reading of the machine: %+v", d)
		}
	}
	if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error {
		r.ChildTerminalID, r.ChildBackend = "%77", "tmux"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1", Assistant: session.AssistantClaude}},
		sources: map[string]bool{"tmux": true}, complete: true})
	b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }
	if inv, err = b.ReadInventory(ctx, repo, nil); err != nil {
		t.Fatal(err)
	}
	dropped := false
	for _, d := range inv.Droppable {
		dropped = dropped || (d.Task == id && d.Why == WhyMergedClean)
	}
	if !dropped {
		t.Fatalf("a landed, merged, clean checkout is not droppable: %+v", inv.Droppable)
	}
}

// broker-design #35: pending, split by whether its owner is here, derived from
// the beat's reading and never written.
func TestAPendingLandingSaysWhetherItsOwnerIsHere(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "d3000008-0000-4000-8000-000000000008"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"},
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Obligation(settled); got != ObligationUnknown {
		t.Fatalf("before any reading: %q, want pending_unknown", got)
	}

	root := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}
	other := session.Session{ID: "%2", Assistant: session.AssistantClaude, ConversationID: "0ther000-0000-4000-8000-000000000000"}
	anonymous := session.Session{ID: "%3", Assistant: session.AssistantCodex}
	cases := []struct {
		name     string
		sessions []session.Session
		ps       bool
		want     Obligation
	}{
		{"root running", []session.Session{root, other}, false, ObligationLive},
		{"process table complete, every assistant named, root absent", []session.Session{other}, true, ObligationOrphaned},
		{"root absent, one assistant nobody can name", []session.Session{other, anonymous}, true, ObligationUnknown},
		{"root absent, process table incomplete", []session.Session{other}, false, ObligationUnknown},
	}
	stored := rawRecord(t, b.Dir, id)
	for _, c := range cases {
		sessions, ps := c.sessions, c.ps
		b.Reading = func(context.Context) session.Inventory {
			return session.Inventory{Sessions: sessions, Complete: false,
				Sources: map[string]bool{"ps": ps, "tmux": true, "iterm": false}}
		}
		b.Pass(ctx)
		if got := b.Obligation(settled); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got := rawRecord(t, b.Dir, id); got != stored {
		t.Fatal("deriving the obligation wrote the record (D04)")
	}

	// A reading that has gone stale answers nothing.
	b.Clock = func() time.Time { return time.Now().Add(presenceFresh + time.Second) }
	if got := b.Obligation(settled); got != ObligationUnknown {
		t.Fatalf("stale reading: %q, want pending_unknown", got)
	}
	b.Clock = nil

	// No root at all: nothing on this machine owes it.
	detached := settled
	detached.Root = nil
	if got := b.Obligation(detached); got != ObligationOrphaned {
		t.Fatalf("no root: %q", got)
	}
	// Any other landing state is not pending, and has no obligation.
	done := settled
	done.Landing = &Landing{State: LandingAbandoned}
	if got := b.Obligation(done); got != "" {
		t.Fatalf("abandoned: %q", got)
	}
}
