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

func TestDirectLandingResolvesLocalAndPublishedTargets(t *testing.T) {
	g := landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true, "commit-a..remote-head": true}}
	got, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"})
	if refusal != nil || got == nil || got.Commit != "commit-a" || got.TargetCommit != "local-head" || got.RemoteCommit != "remote-head" {
		t.Fatalf("verified landing: %+v %v", got, refusal)
	}
}

func TestDirectLandingRefusesAnotherSessionAndUnpublishedCommit(t *testing.T) {
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), landingGit{}, directLandingItem(), "session-b",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"}); refusal == nil || refusal.Code != "direct_landing_not_applicable" {
		t.Fatalf("foreign Session: %v", refusal)
	}
	g := landingGit{resolved: map[string]string{
		"delivery": "commit-a", "refs/heads/main": "local-head", "refs/remotes/origin/main": "remote-head",
	}, on: map[string]bool{"commit-a..local-head": true}}
	if _, refusal := verifyWorkV2DirectLanding(context.Background(), g, directLandingItem(), "session-a",
		&workV2LandingRequest{Commit: "delivery", Target: "main", Remote: "origin"}); refusal == nil || refusal.Code != "landing_not_published" {
		t.Fatalf("unpublished commit: %v", refusal)
	}
}
