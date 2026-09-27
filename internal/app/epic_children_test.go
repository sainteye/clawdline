package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// implementingEpic is an Epic owned by session-a whose plan was reviewed and
// which has entered implementing: the state its owner may break it up in.
func implementingEpic(t *testing.T, w *WorkSystemV2, clock *epicClock) WorkV2View {
	t.Helper()
	epic := assignedEpic(t, w)
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatal(err)
	}
	review := reviewTask(fmt.Sprintf("7e000000-0000-4000-8000-%012x", clock.at.UnixNano()%1e12), epic.Item.ID, clock.at)
	putTask(t, w.Store, review)
	if err := addDoc(w, &epic, work.DocumentPlanReview, review.ID); err != nil {
		t.Fatal(err)
	}
	if err := advanceTo(w, &epic, work.PhaseImplementing); err != nil {
		t.Fatal(err)
	}
	return epic
}

func epicChild(w *WorkSystemV2, epic *WorkV2View, session string, kind work.Kind, steps ...string) (WorkV2View, error) {
	v, err := w.CreateEpicChild(context.Background(), NewEpicChildV2{EpicID: epic.Item.ID,
		ExpectedVersion: epic.Item.Version, SessionID: session, Kind: kind, Title: "Part " + string(kind),
		Description: "- first\n- second", Steps: steps})
	if err == nil {
		fresh, readErr := w.Item(context.Background(), epic.Item.ID)
		if readErr != nil {
			return v, readErr
		}
		epic.Item = fresh.Item
	}
	return v, err
}

// The owner of an Epic past its plan gate creates Feature and Issue items
// under it: each names the Epic as its parent and the owner as its creator,
// is in the Epic's Project, unassigned, with the owner's explicit steps or —
// when it named none — its description's list once it is assigned. The Epic
// records each child it gained.
func TestAnEpicsOwnerCreatesFeatureAndIssueChildren(t *testing.T) {
	w, clock := newEpicTest(t)
	ctx := context.Background()
	epic := implementingEpic(t, w, clock)
	before := epic.Item.Version
	feature, err := epicChild(w, &epic, "session-a", work.KindFeature, "one", "two", "three")
	if err != nil {
		t.Fatal(err)
	}
	i := feature.Item
	if i.ParentID != epic.Item.ID || i.ProjectID != epic.Item.ProjectID || i.ProjectPath != epic.Item.ProjectPath ||
		i.OwnerSession != "" || i.Phase != work.PhaseCreated || i.CreatedBy != work.EpicOwnerActor("session-a") ||
		i.CreatedVia == nil || i.CreatedVia.Epic != epic.Item.ID || i.CreatedVia.Session != "session-a" || i.CreatedVia.Run != "" {
		t.Fatalf("child: %+v via %+v", i, i.CreatedVia)
	}
	if len(feature.Steps) != 3 || feature.Steps[2].Title != "three" {
		t.Fatalf("explicit steps: %+v", feature.Steps)
	}
	if epic.Item.Version != before+1 {
		t.Fatalf("the Epic's version %d, want %d", epic.Item.Version, before+1)
	}
	events, err := w.Store.WorkV2Events(ctx, epic.Item.ID, 0, 100)
	if err != nil || events[len(events)-1].Kind != "epic.child_created" ||
		!strings.Contains(events[len(events)-1].Payload, i.ID) {
		t.Fatalf("the Epic's record: %+v %v", events, err)
	}
	issue, err := epicChild(w, &epic, "session-a", work.KindIssue)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Steps) != 0 {
		t.Fatalf("an unassigned child seeded steps before its assignment: %+v", issue.Steps)
	}
	assigned, err := w.Assign(ctx, issue.Item.ID, AssignWorkV2{ExpectedVersion: issue.Item.Version,
		Mode: "existing_session", SessionID: "session-b", Actor: work.EpicOwnerActor("session-a"), EpicOwner: "session-a"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.Item.OwnerSession != "session-b" || len(assigned.Steps) != 2 {
		t.Fatalf("assigned child: %+v steps %d", assigned.Item, len(assigned.Steps))
	}
}

// Only the owner of an open Epic past its plan gate creates its children,
// and only a Feature or an Issue; every refusal writes nothing.
func TestOnlyTheOwnerOfAPlannedOpenEpicCreatesChildren(t *testing.T) {
	w, clock := newEpicTest(t)
	ctx := context.Background()
	epic := implementingEpic(t, w, clock)
	_, err := epicChild(w, &epic, "session-b", work.KindFeature)
	refusedAsWork(t, err, "not_epic_owner")
	_, err = epicChild(w, &epic, "", work.KindFeature)
	refusedAsWork(t, err, "not_epic_owner")
	for _, kind := range []work.Kind{work.KindEpic, work.KindPlan, work.KindRefactor, "wish"} {
		_, err = epicChild(w, &epic, "session-a", kind)
		refusedAsWork(t, err, "child_kind_not_allowed")
	}
	stale := epic
	stale.Item.Version--
	_, err = epicChild(w, &stale, "session-a", work.KindFeature)
	refusedAsWork(t, err, "version_conflict")

	planned := assignedEpic(t, w)
	_, err = epicChild(w, &planned, "session-a", work.KindFeature)
	refusedAsWork(t, err, "epic_not_planned")

	feature := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(ctx, feature.Item.ID, AssignWorkV2{ExpectedVersion: feature.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = epicChild(w, &owned, "session-a", work.KindIssue)
	refusedAsWork(t, err, "parent_not_epic")

	if _, err := w.Cancel(ctx, epic.Item.ID, epic.Item.Version, "local", "dropped", nil); err != nil {
		t.Fatal(err)
	}
	closed, _ := w.Item(ctx, epic.Item.ID)
	_, err = epicChild(w, &closed, "session-a", work.KindFeature)
	refusedAsWork(t, err, "item_terminal")

	items, _, err := w.Store.WorkV2Items(ctx, "", "", "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ParentID != "" {
			t.Fatalf("a refused create wrote a child: %+v", it)
		}
	}
}

// One Epic holds at most work.EpicChildLimit children, open or closed; the
// next is refused epic_children_full and nothing is written.
func TestAnEpicHoldsABoundedNumberOfChildren(t *testing.T) {
	w, clock := newEpicTest(t)
	epic := implementingEpic(t, w, clock)
	for n := 0; n < work.EpicChildLimit; n++ {
		if _, err := epicChild(w, &epic, "session-a", work.KindIssue); err != nil {
			t.Fatalf("child %d: %v", n+1, err)
		}
	}
	_, err := epicChild(w, &epic, "session-a", work.KindIssue)
	refusedAsWork(t, err, "epic_children_full")
	items, _, err := w.Store.WorkV2Items(context.Background(), "", "", "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	for _, it := range items {
		if it.ParentID == epic.Item.ID {
			children++
		}
	}
	if children != work.EpicChildLimit {
		t.Fatalf("children %d, want %d", children, work.EpicChildLimit)
	}
}

// The Epic's owner (re)assigns a child of its Epic; another Session may not,
// and an item with no parent Epic stays the person's to assign.
func TestOnlyTheEpicsOwnerAssignsItsChildren(t *testing.T) {
	w, clock := newEpicTest(t)
	ctx := context.Background()
	epic := implementingEpic(t, w, clock)
	child, err := epicChild(w, &epic, "session-a", work.KindFeature)
	if err != nil {
		t.Fatal(err)
	}
	assign := func(item WorkV2View, owner, to string) (WorkV2View, error) {
		return w.Assign(ctx, item.Item.ID, AssignWorkV2{ExpectedVersion: item.Item.Version, Mode: "existing_session",
			SessionID: to, Actor: work.EpicOwnerActor(owner), EpicOwner: owner}, false, nil)
	}
	_, err = assign(child, "session-b", "session-b")
	refusedAsWork(t, err, "not_epic_owner")
	toB, err := assign(child, "session-a", "session-b")
	if err != nil {
		t.Fatal(err)
	}
	toSelf, err := assign(toB, "session-a", "session-a")
	if err != nil || toSelf.Item.OwnerSession != "session-a" {
		t.Fatalf("reassigned to the Epic's owner: %+v %v", toSelf.Item, err)
	}
	loose := createWorkV2Test(t, w, work.KindFeature)
	_, err = assign(loose, "session-a", "session-b")
	refusedAsWork(t, err, "not_epic_child")
}

// An Epic is not moved to done while a child is open; once every child is
// closed it is.
func TestAnEpicIsNotDoneWhileAChildIsOpen(t *testing.T) {
	w, clock := newEpicTest(t)
	ctx := context.Background()
	epic := implementingEpic(t, w, clock)
	child, err := epicChild(w, &epic, "session-a", work.KindIssue)
	if err != nil {
		t.Fatal(err)
	}
	if err := advanceTo(w, &epic, work.PhaseVerifying); err != nil {
		t.Fatal(err)
	}
	move := func(c AdvanceWorkV2) error {
		c.ExpectedVersion, c.SessionID, c.Actor = epic.Item.Version, "session-a", "session-a"
		v, err := w.Advance(ctx, epic.Item.ID, c, nil)
		if err == nil {
			epic.Item = v.Item
		}
		return err
	}
	if err := move(AdvanceWorkV2{Next: work.PhaseMerging, Verification: "tests"}); err != nil {
		t.Fatal(err)
	}
	landing := &VerifiedLandingV2{Commit: "c", Target: "main", TargetCommit: "c", Remote: "origin", RemoteCommit: "c"}
	if err := move(AdvanceWorkV2{Next: work.PhaseDeploying, Landing: landing}); err != nil {
		t.Fatal(err)
	}
	err = move(AdvanceWorkV2{Next: work.PhaseDone, NoDeploymentReason: "library"})
	refusedAsWork(t, err, "epic_children_open")
	if !strings.Contains(err.Error(), "1 child") {
		t.Fatalf("the refusal does not count the open children: %v", err)
	}
	if _, err := w.Cancel(ctx, child.Item.ID, child.Item.Version, "local", "not needed", nil); err != nil {
		t.Fatal(err)
	}
	if err := move(AdvanceWorkV2{Next: work.PhaseDone, NoDeploymentReason: "library"}); err != nil {
		t.Fatalf("an Epic whose children are closed: %v", err)
	}
}
