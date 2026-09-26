package http

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

type landingGit struct {
	resolved map[string]string
	on       map[string]bool
}

func (g landingGit) ValidBranchName(_ context.Context, name string) bool {
	return name == "main" || name == "origin"
}

func (g landingGit) ResolveCommit(_ context.Context, _, revision string) (string, error) {
	if value := g.resolved[revision]; value != "" {
		return value, nil
	}
	return "", errors.New("unknown revision")
}

func (g landingGit) IsAncestor(_ context.Context, _, commit, target string) (bool, error) {
	return g.on[commit+".."+target], nil
}

func directLandingItem() app.WorkV2View {
	return app.WorkV2View{Item: work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectPath: "/project",
		OwnerSession: "session-a"}, Assignments: []work.AssignmentV2{{Mode: "existing_session", SessionID: "session-a", State: "active"}}}
}

func rootAssignmentLandingItem(rootAssignment string) app.WorkV2View {
	return app.WorkV2View{Item: work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectPath: "/project",
		OwnerSession: "session-a"}, Assignments: []work.AssignmentV2{{Mode: "new_session", SessionID: "session-a",
		RootAssignment: rootAssignment, State: "active"}}}
}

func TestDirectLandingResolvesLocalAndPublishedTargets(t *testing.T) {
	g := landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true, "commit-a..remote-head": true}}
	got, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a", "/project",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"})
	if refusal != nil || got == nil || got.Commit != "commit-a" || got.TargetCommit != "local-head" || got.RemoteCommit != "remote-head" {
		t.Fatalf("verified landing: %+v %v", got, refusal)
	}
}

func TestDirectLandingAcceptsResolvedRootAssignmentOwner(t *testing.T) {
	g := landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true, "commit-a..remote-head": true}}
	got, refusal := verifyWorkV2DirectLanding(context.Background(), g, rootAssignmentLandingItem("root-a"), "session-a", "/project",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"})
	if refusal != nil || got == nil || got.Commit != "commit-a" {
		t.Fatalf("Root Assignment landing: %+v %v", got, refusal)
	}
}

func TestDirectLandingRefusesUnresolvedNewSessionAssignment(t *testing.T) {
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), landingGit{}, rootAssignmentLandingItem(""), "session-a", "/project",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"}); refusal == nil || refusal.Code != "direct_landing_not_applicable" {
		t.Fatalf("new Session without Root Assignment: %v", refusal)
	}
}

func TestDirectLandingRefusesAnotherSessionAndUnpublishedCommit(t *testing.T) {
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), landingGit{}, directLandingItem(), "session-b", "/project",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"}); refusal == nil || refusal.Code != "direct_landing_not_applicable" {
		t.Fatalf("foreign Session: %v", refusal)
	}
	g := landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true}}
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a", "/project",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"}); refusal == nil || refusal.Code != "landing_not_published" {
		t.Fatalf("unpublished commit: %v", refusal)
	}
}

// onlyIn is landingGit answering only for one repository, so a lookup in any
// other repository finds nothing.
type onlyIn struct {
	landingGit
	repo string
}

func (g onlyIn) ResolveCommit(ctx context.Context, repo, revision string) (string, error) {
	if repo != g.repo {
		return "", errors.New("not this repository")
	}
	return g.landingGit.ResolveCommit(ctx, repo, revision)
}

// A backend item whose change landed as a frontend commit is verified in the
// repository the caller resolved, and the receipt says which one; the item's
// own repository does not hold the commit and is refused.
func TestDirectLandingInAnotherProjectIsVerifiedThereAndNamed(t *testing.T) {
	g := onlyIn{landingGit: landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true, "commit-a..remote-head": true}}, repo: "/frontend"}
	ask := &workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin", Project: "frontend-id"}
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a", "/project",
		ask); refusal == nil || refusal.Code != "landing_commit_unresolved" {
		t.Fatalf("own repository: %+v", refusal)
	}
	got, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a", "/frontend", ask)
	if refusal != nil || got.Commit != "commit-a" || got.Repository != "/frontend" {
		t.Fatalf("other repository: %+v %+v", got, refusal)
	}
	same, refusal := verifyWorkV2DirectLanding(context.Background(), onlyIn{landingGit: g.landingGit, repo: "/project"},
		directLandingItem(), "session-a", "/project", ask)
	if refusal != nil || same.Repository != "" {
		t.Fatalf("own repository named as another: %+v %+v", same, refusal)
	}
}
