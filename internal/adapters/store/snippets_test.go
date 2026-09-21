package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/snippet"
)

func openSnippetStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, context.Background()
}

func globalFields(title, body string) snippet.Fields {
	return snippet.Fields{Title: title, HasTitle: true, Body: body, HasBody: true,
		Scope: snippet.Global, HasScope: true}
}

func projectFields(title, body, project string) snippet.Fields {
	f := globalFields(title, body)
	f.Scope, f.Project, f.HasProject = snippet.Project, project, true
	return f
}

// Roomy limits, so a test about one rule is not also a test about the other.
var roomy = snippet.Limits{Total: 100, Scope: 50}

func TestAMachineWithNoSnippetsAnswersAnEmptyList(t *testing.T) {
	st, ctx := openSnippetStore(t)
	rows, err := st.Snippets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a fresh store held %d snippet(s)", len(rows))
	}
	total, group, name, err := st.SnippetCounts(ctx)
	if err != nil || total != 0 || group != 0 || name != "" {
		t.Fatalf("%d %d %q %v", total, group, name, err)
	}
}

// Each new snippet lands after the last one in its own group, and the two
// groups count separately: a project's first snippet is not put after the
// global ones.
func TestANewSnippetLandsAtTheEndOfItsOwnGroup(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	first, ref, err := st.CreateSnippet(ctx, "id-1", globalFields("one", "a"), roomy, now)
	if err != nil || ref != nil {
		t.Fatal(err, ref)
	}
	second, _, err := st.CreateSnippet(ctx, "id-2", globalFields("two", "b"), roomy, now)
	if err != nil {
		t.Fatal(err)
	}
	mine, _, err := st.CreateSnippet(ctx, "id-3", projectFields("three", "c", "/p"), roomy, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Position != 100 || second.Position != 200 {
		t.Errorf("global: %d %d", first.Position, second.Position)
	}
	if mine.Position != 100 {
		t.Errorf("a project's first snippet was placed after the global ones: %d", mine.Position)
	}
}

func TestASnippetTheStoreRefusesIsNotWritten(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	_, ref, err := st.CreateSnippet(ctx, "id-1", snippet.Fields{Title: "t", HasTitle: true}, roomy, now)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeMalformed {
		t.Fatalf("a body with no scope was accepted: %v", ref)
	}
	rows, _ := st.Snippets(ctx)
	if len(rows) != 0 {
		t.Fatalf("%d row(s) written by a refused create", len(rows))
	}
	// And the refused create did not spend the disk brake, so the next ten
	// honest writes still go through.
	for i := 0; i < snippetWriteLimit; i++ {
		if _, ref, err := st.CreateSnippet(ctx, "ok-"+string(rune('a'+i)), globalFields("t", "b"), roomy, now); err != nil || ref != nil {
			t.Fatalf("write %d: %v %v", i, err, ref)
		}
	}
}

func TestTheMachinesLimitRefusesAndNothingIsLetGo(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	limits := snippet.Limits{Total: 2, Scope: 50}
	for i, id := range []string{"a", "b"} {
		// One window per write: the brake is not what this test is about.
		if _, ref, err := st.CreateSnippet(ctx, id, globalFields("t", "b"), limits,
			now.Add(time.Duration(i)*time.Hour)); err != nil || ref != nil {
			t.Fatal(err, ref)
		}
	}
	_, ref, err := st.CreateSnippet(ctx, "c", globalFields("t", "b"), limits, now.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeLimitReached || ref.Status != 409 {
		t.Fatalf("a third snippet was accepted past a limit of two: %v", ref)
	}
	rows, _ := st.Snippets(ctx)
	if len(rows) != 2 {
		t.Fatalf("%d row(s) left; nothing may be let go to make room", len(rows))
	}
}

func TestAGroupsLimitRefusesThatGroupAndNotTheOther(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	limits := snippet.Limits{Total: 100, Scope: 1}
	if _, ref, err := st.CreateSnippet(ctx, "g", globalFields("t", "b"), limits, now); err != nil || ref != nil {
		t.Fatal(err, ref)
	}
	_, ref, err := st.CreateSnippet(ctx, "g2", globalFields("t", "b"), limits, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeLimitReached {
		t.Fatalf("a full group accepted one more: %v", ref)
	}
	if _, ref, err := st.CreateSnippet(ctx, "p", projectFields("t", "b", "/p"), limits,
		now.Add(2*time.Hour)); err != nil || ref != nil {
		t.Fatalf("one group being full refused another: %v %v", err, ref)
	}
}

func TestASnippetMovingIntoAFullGroupIsRefusedAndOneStayingIsNot(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	limits := snippet.Limits{Total: 100, Scope: 1}
	if _, _, err := st.CreateSnippet(ctx, "g", globalFields("global one", "b"), limits, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateSnippet(ctx, "p", projectFields("project one", "b", "/p"), limits,
		now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	move := snippet.Fields{Scope: snippet.Global, HasScope: true}
	_, ref, err := st.UpdateSnippet(ctx, "p", move, limits, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeLimitReached {
		t.Fatalf("a move into a full group was accepted: %v", ref)
	}
	// Staying where it is never meets the group's limit, even when that group
	// is full — which it is, of this one snippet.
	rename := snippet.Fields{Title: "renamed", HasTitle: true}
	saved, ref, err := st.UpdateSnippet(ctx, "p", rename, limits, now.Add(3*time.Hour))
	if err != nil || ref != nil {
		t.Fatalf("a rename in a full group was refused: %v %v", err, ref)
	}
	if saved.Title != "renamed" || saved.Position != 100 {
		t.Errorf("%+v", saved)
	}
}

func TestASnippetThatChangesGroupLandsAtTheEndOfTheNewOne(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	for i, id := range []string{"g1", "g2"} {
		if _, _, err := st.CreateSnippet(ctx, id, globalFields("t", "b"), roomy,
			now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateSnippet(ctx, "p1", projectFields("t", "b", "/p"), roomy,
		now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	moved, ref, err := st.UpdateSnippet(ctx, "p1",
		snippet.Fields{Scope: snippet.Global, HasScope: true}, roomy, now.Add(3*time.Hour))
	if err != nil || ref != nil {
		t.Fatal(err, ref)
	}
	if moved.Scope != snippet.Global || moved.Project != "" || moved.Position != 300 {
		t.Fatalf("%+v", moved)
	}
}

func TestASnippetNobodyHasIsNotFoundRatherThanMade(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	_, ref, err := st.UpdateSnippet(ctx, "nobody", globalFields("t", "b"), roomy, now)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeNotFound || ref.Status != 404 {
		t.Fatalf("a save invented a snippet: %v", ref)
	}
	ref, err = st.DeleteSnippet(ctx, "nobody", now)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeNotFound {
		t.Fatalf("deleting nothing said it worked: %v", ref)
	}
	rows, _ := st.Snippets(ctx)
	if len(rows) != 0 {
		t.Fatalf("%d row(s)", len(rows))
	}
}

func TestOneGroupIsReorderedWholeOrNotAtAll(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	for i, id := range []string{"a", "b", "c"} {
		if _, _, err := st.CreateSnippet(ctx, id, globalFields("t"+id, "b"), roomy,
			now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateSnippet(ctx, "p", projectFields("t", "b", "/p"), roomy,
		now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The project snippet is not a member of the global group, so an order
	// naming it is refused rather than moving it.
	_, ref, err := st.OrderSnippets(ctx, snippet.Global, "", []string{"a", "b", "p"}, now.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.Code != snippet.CodeScopeMismatch {
		t.Fatalf("an order naming another group's snippet was accepted: %v", ref)
	}
	ordered, ref, err := st.OrderSnippets(ctx, snippet.Global, "", []string{"c", "a", "b"}, now.Add(5*time.Hour))
	if err != nil || ref != nil {
		t.Fatal(err, ref)
	}
	if len(ordered) != 3 || ordered[0].ID != "c" {
		t.Fatalf("%+v", ordered)
	}
	rows, _ := st.Snippets(ctx)
	seen := []string{}
	for _, r := range rows {
		if r.Scope == snippet.Global {
			seen = append(seen, r.ID)
		}
	}
	if len(seen) != 3 || seen[0] != "c" || seen[1] != "a" || seen[2] != "b" {
		t.Errorf("read back as %v", seen)
	}
	// One press, one token: reordering three rows did not spend three.
	if _, _, err := st.CreateSnippet(ctx, "after", globalFields("t", "b"), roomy, now.Add(5*time.Hour)); err != nil {
		t.Fatalf("the order spent more than one token: %v", err)
	}
}

func TestTheBrakeRefusesTheEleventhWriteInItsWindowAndTheNextWindowLetsItThrough(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < snippetWriteLimit; i++ {
		if _, ref, err := st.CreateSnippet(ctx, "id-"+string(rune('a'+i)), globalFields("t", "b"),
			roomy, now); err != nil || ref != nil {
			t.Fatalf("write %d: %v %v", i, err, ref)
		}
	}
	_, _, err := st.CreateSnippet(ctx, "one-too-many", globalFields("t", "b"), roomy, now)
	if !errors.Is(err, SnippetRateLimited) {
		t.Fatalf("the eleventh write in the window was let through: %v", err)
	}
	rows, _ := st.Snippets(ctx)
	if len(rows) != snippetWriteLimit {
		t.Fatalf("%d row(s); a braked write must write nothing", len(rows))
	}
	if _, ref, err := st.CreateSnippet(ctx, "later", globalFields("t", "b"), roomy,
		now.Add(snippetWriteWindow+time.Second)); err != nil || ref != nil {
		t.Fatalf("the next window did not let a write through: %v %v", err, ref)
	}
}

// The brake's window is rows in the store rather than a list in memory, so a
// machine that was just told to slow down is still slowed down after a restart.
func TestTheBrakeSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < snippetWriteLimit; i++ {
		if _, _, err := st.CreateSnippet(ctx, "id-"+string(rune('a'+i)), globalFields("t", "b"),
			roomy, now); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, _, err := again.CreateSnippet(ctx, "after-restart", globalFields("t", "b"), roomy, now); !errors.Is(err, SnippetRateLimited) {
		t.Fatalf("a restart gave the brake back: %v", err)
	}
}

func TestTheCountsNameTheFullestGroup(t *testing.T) {
	st, ctx := openSnippetStore(t)
	now := time.Unix(1_700_000_000, 0)
	if _, _, err := st.CreateSnippet(ctx, "g", globalFields("t", "b"), roomy, now); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"p1", "p2"} {
		if _, _, err := st.CreateSnippet(ctx, id, projectFields("t", "b", "/p"), roomy,
			now.Add(time.Duration(i+1)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	total, fullest, name, err := st.SnippetCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || fullest != 2 || name != "project /p" {
		t.Fatalf("%d %d %q", total, fullest, name)
	}
}
