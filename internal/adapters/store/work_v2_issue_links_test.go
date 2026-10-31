package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func issueItem(t *testing.T, s *Store, id string, phase work.Phase) work.ItemV2 {
	t.Helper()
	item := work.ItemV2{ID: id, ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue, Title: "Title " + id,
		Description: "Body", Phase: phase, DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local",
		CreatedAt: time.Unix(5, 0), UpdatedAt: time.Unix(5, 0), Cycle: 1, Version: 1}
	if err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error { return tx.CreateItem(item, "local", `{}`) }); err != nil {
		t.Fatal(err)
	}
	return item
}

func setPhase(t *testing.T, s *Store, id string, phase work.Phase) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE work_v2_items SET phase = ? WHERE id = ?`, string(phase), id); err != nil {
		t.Fatal(err)
	}
}

func pendingIDs(t *testing.T, s *Store, now time.Time) map[string]bool {
	t.Helper()
	due, err := s.PendingIssueLinks(context.Background(), now, 50)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, d := range due {
		out[d.Link.WorkID] = true
		if d.Title == "" || d.Phase == "" {
			t.Fatalf("a due link without its item: %+v", d)
		}
	}
	return out
}

func TestOnlyLinksWhoseItemOwesACallAreDue(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	gh := work.IssueTarget{Provider: work.IssueProviderGitHub, Repo: "octo/widgets"}
	now := time.Unix(1_000, 0)
	for id, phase := range map[string]work.Phase{"assigned": work.PhaseAssigned, "started": work.PhaseImplementing,
		"unlinked": work.PhaseImplementing} {
		item := issueItem(t, s, id, phase)
		if id == "unlinked" {
			continue
		}
		if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.LinkIssue(item, gh, "local", now) }); err != nil {
			t.Fatal(err)
		}
	}
	if got := pendingIDs(t, s, now); len(got) != 1 || !got["started"] {
		t.Fatalf("due %v", got)
	}

	if err := s.RecordIssueSynced(ctx, "started", work.IssueOpen, "#4", "https://github.com/octo/widgets/issues/4", now); err != nil {
		t.Fatal(err)
	}
	if got := pendingIDs(t, s, now); len(got) != 0 {
		t.Fatalf("an open issue on a working item is due: %v", got)
	}
	setPhase(t, s, "started", work.PhaseDone)
	if got := pendingIDs(t, s, now); !got["started"] {
		t.Fatalf("a done item's open issue is not due: %v", got)
	}

	next := now.Add(4 * time.Minute)
	if err := s.RecordIssueFailed(ctx, "started", "github_unreachable", 1, next, now); err != nil {
		t.Fatal(err)
	}
	if got := pendingIDs(t, s, now); len(got) != 0 {
		t.Fatalf("a failed link is due before its retry: %v", got)
	}
	if got := pendingIDs(t, s, next); !got["started"] {
		t.Fatalf("a failed link is not due at its retry: %v", got)
	}
	if ok, err := s.RetryIssueLink(ctx, "started", now); !ok || err != nil {
		t.Fatalf("retry %v %v", ok, err)
	}
	if got := pendingIDs(t, s, now); !got["started"] {
		t.Fatalf("Retry did not make the link due: %v", got)
	}

	links, err := s.IssueLinks(ctx, []string{"started", "assigned", "unlinked"})
	if err != nil || len(links) != 2 || links["started"].ExternalKey != "#4" || links["started"].LastError != "github_unreachable" {
		t.Fatalf("links %+v %v", links, err)
	}
	var events int
	s.db.QueryRow(`SELECT COUNT(*) FROM work_v2_events WHERE work_id = 'started' AND kind LIKE 'issue.%'`).Scan(&events)
	if events != 3 { // linked, synced, failed
		t.Fatalf("%d issue events", events)
	}
	if err := s.RecordIssueFailed(ctx, "started", "github_unreachable", 2, next, now); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM work_v2_events WHERE work_id = 'started' AND kind LIKE 'issue.%'`).Scan(&events)
	if events != 3 {
		t.Fatalf("the same failure again wrote an event: %d", events)
	}
}

func TestALinkedIssueCannotMoveToAnotherTracker(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	item := issueItem(t, s, "a", work.PhaseAssigned)
	gh := work.IssueTarget{Provider: work.IssueProviderGitHub, Repo: "octo/widgets"}
	jira := work.IssueTarget{Provider: work.IssueProviderJira, JiraSite: "https://acme.atlassian.net", JiraProject: "OPS", JiraIssueType: "Task"}
	link := func(target work.IssueTarget) error {
		return s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.LinkIssue(item, target, "local", time.Unix(9, 0)) })
	}
	if err := link(gh); err != nil {
		t.Fatal(err)
	}
	if err := link(jira); err != nil {
		t.Fatalf("changing a target before any issue exists: %v", err)
	}
	if err := s.RecordIssueSynced(ctx, "a", work.IssueOpen, "OPS-1", "https://acme.atlassian.net/browse/OPS-1", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := link(jira); err != nil {
		t.Fatalf("the same target again: %v", err)
	}
	if err := link(gh); !errors.Is(err, ErrIssueLinkMoved) {
		t.Fatalf("moving a created issue: %v", err)
	}
	if r, ok := work.AsRefusalV2(link(work.IssueTarget{Provider: work.IssueProviderGitHub, Repo: "nope"})); !ok || r.Code != "issue_sync_target_invalid" {
		t.Fatalf("an invalid target was linked")
	}
}
