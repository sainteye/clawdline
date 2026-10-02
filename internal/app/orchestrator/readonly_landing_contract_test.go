package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dispatchShared dispatches a shared-tree task with these claims and settles
// it, as a test with no terminal does.
func dispatchShared(t *testing.T, b *Broker, ctx context.Context, repo, id string, claims []string) Dispatched {
	t.Helper()
	writeBrief(t, b, id, repo, map[string]any{"claims": claims})
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Generation: inv.Generation, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Record.State.Terminal() {
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// The read-only contract (2026-10-02): `"claims": []` is an answer, not an
// omission, so it is not warned about; only a known, non-empty write set opens
// a landing obligation in the shared tree; and a shared task that committed
// nothing may say so — while an isolated one whose own branch or checkout
// shows work is still refused.

// ① An explicit empty write set is not "claims missing".
func TestAnExplicitlyEmptyClaimListIsNotWarnedAbout(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "c0a10000-0000-4000-8000-000000000001"
	out := dispatchShared(t, b, ctx, repo, id, []string{})
	for _, w := range out.Warnings {
		if w.Code == "claims_missing" {
			t.Fatalf("an explicit claims: [] was warned as missing: %+v", w)
		}
	}
	if out.Record.Claims == nil {
		t.Fatal("the explicit empty list was stored as unknown")
	}
}

// ② A shared task that declared nothing owes no landing when it ends; one that
// declared paths does.
func TestOnlyADeclaredSharedWriteSetOpensALanding(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	for _, c := range []struct {
		id     string
		claims []string
		owes   bool
	}{
		{"c0a10000-0000-4000-8000-000000000002", []string{}, false},
		{"c0a10000-0000-4000-8000-000000000003", []string{"src/"}, true},
	} {
		dispatchShared(t, b, ctx, repo, c.id, c.claims)
		r, _, err := b.Record(ctx, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if owes := r.Landing != nil; owes != c.owes {
			t.Fatalf("claims %v: landing owed = %v, want %v (%+v)", c.claims, owes, c.owes, r.Landing)
		}
	}
}

// ③ The case from the report: a shared task declared src/, committed nothing,
// and its root says so. The declaration alone is not evidence of a write.
func TestASharedTaskThatCommittedNothingMayRecordNothingToLand(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "c0a10000-0000-4000-8000-000000000004"
	dispatchShared(t, b, ctx, repo, id, []string{"src/"})
	first, err := land(b, ctx, id, string(LandingNothingToLand), "", "")
	if err != nil {
		t.Fatalf("nothing_to_land for a shared task with no commit: %v", err)
	}
	if first.Landing == nil || first.Landing.State != LandingNothingToLand {
		t.Fatalf("landing = %+v", first.Landing)
	}
	// Idempotent: the same claim again changes nothing and is not refused.
	again, err := land(b, ctx, id, string(LandingNothingToLand), "", "")
	if err != nil {
		t.Fatalf("repeating nothing_to_land: %v", err)
	}
	if !again.Landing.At.Equal(first.Landing.At) {
		t.Fatalf("a replay moved the landing: %v → %v", first.Landing.At, again.Landing.At)
	}
}

// ④ A read-only isolated child whose branch and checkout hold nothing never
// owes a landing: it is recorded nothing_to_land as it ends, not left pending
// for a beat or a root to clear.
func TestAReadOnlyWorktreeOpensNoLandingObligation(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "c0a10000-0000-4000-8000-000000000007"
	readOnlyTask(t, b, ctx, repo, id, true)
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingNothingToLand || l.Note != landingEmptyNote {
		t.Fatalf("landing = %s (%q), want nothing_to_land at settlement", l.State, l.Note)
	}
	// Idempotent: the root saying it again is a no-op, not a refusal.
	if _, err := land(b, ctx, id, string(LandingNothingToLand), "", ""); err != nil {
		t.Fatalf("repeating nothing_to_land: %v", err)
	}
}

// ⑤ The failure path that must stay: a read-only child that wrote anyway —
// a commit on its branch, or a change in its checkout — owes the landing, its
// note names the evidence, and it cannot claim it wrote nothing. One that was
// cancelled is not read at all, and unread is not empty.
func TestAReadOnlyWorktreeThatWroteKeepsItsEvidence(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)

	committed := "c0a10000-0000-4000-8000-000000000005"
	r := readOnlyTask(t, b, ctx, repo, committed, true)
	if err := os.WriteFile(filepath.Join(r.Worktree.Path, "new.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r.Worktree.Path, "add", "new.go")
	gitIn(t, r.Worktree.Path, "commit", "-q", "-m", "work")

	dirty := "c0a10000-0000-4000-8000-000000000006"
	d := readOnlyTask(t, b, ctx, repo, dirty, true)
	if err := os.WriteFile(filepath.Join(d.Worktree.Path, "scratch.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ id, evidence string }{
		{committed, "commit(s)"},
		{dirty, "uncommitted changes"},
	} {
		if _, err := b.Settle(ctx, c.id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
		l := landingOf(t, b, ctx, c.id)
		if l.State != LandingPending || !strings.Contains(l.Note, "declared no writes, but") ||
			!strings.Contains(l.Note, c.evidence) {
			t.Fatalf("%s: landing = %s (%q), want pending naming %q", c.id, l.State, l.Note, c.evidence)
		}
		if _, err := land(b, ctx, c.id, string(LandingNothingToLand), "", ""); refusalCode(err) != "wrote_to_repository" {
			t.Fatalf("%s: nothing_to_land answered %v, want wrote_to_repository", c.id, err)
		}
	}

	cancelled := "c0a10000-0000-4000-8000-000000000008"
	readOnlyTask(t, b, ctx, repo, cancelled, true)
	if _, err := b.Settle(ctx, cancelled, StateCancelled, "stopped", nil); err != nil {
		t.Fatal(err)
	}
	if l := landingOf(t, b, ctx, cancelled); l.State != LandingPending {
		t.Fatalf("a cancelled read-only child: landing = %s, want pending", l.State)
	}
}
