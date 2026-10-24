//go:build darwin || linux

package http

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravityproject"
)

func TestAntigravityProjectConsentOwnerStopsWithoutPublishingPermission(t *testing.T) {
	fixture := projectCloudStandIn(t)
	s := fixture.server
	project, ok := s.workV2Project(context.Background(), fixture.place)
	if !ok {
		t.Fatal("synthetic Project unavailable")
	}
	if _, err := s.antigravityProjectRoot(context.Background(), project.Path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := s.StartAntigravityProjectConsent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	owner := s.antigravityProjectOwner.Load()
	if owner == nil {
		t.Fatal("owner not published")
	}
	if s.antigravityPolicyInputs.Load() != nil {
		t.Fatal("consent startup enabled native permission inputs")
	}
	if _, err := owner.Preview(ctx, project.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartAntigravityProjectConsent(ctx); err != antigravityproject.Changed {
		t.Fatal("duplicate owner", err)
	}
	if _, err := s.antigravityProjectRoot(ctx, filepath.Dir(project.Path)); err != antigravityproject.Unverified {
		t.Fatal("unknown path borrowed Project", err)
	}
	readers := s.projectReaders()
	originalPlaces := readers.places.Fixture
	readers.places.Fixture = []string{t.TempDir()}
	if _, err := owner.Preview(ctx, project.Path); err != antigravityproject.Unverified {
		t.Fatal("removed catalog membership remained authorized", err)
	}
	readers.places.Fixture = originalPlaces
	cancel()
	if _, err := owner.Preview(context.Background(), project.Path); err != antigravityproject.Unverified {
		t.Fatal("cancelled owner still resolves Project", err)
	}
	stop()
	stop()
	if s.antigravityProjectOwner.Load() != nil {
		t.Fatal("stopped owner retained")
	}
	stopAgain, err := s.StartAntigravityProjectConsent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stopAgain()
	if _, err := owner.Preview(context.Background(), project.Path); err != antigravityproject.Unverified {
		t.Fatal("old owner borrowed restart", err)
	}
	if s.antigravityPolicyInputs.Load() != nil {
		t.Fatal("restart enabled native permission inputs")
	}
}
func TestAntigravityProjectConsentOwnerCancelledBeforeStartup(t *testing.T) {
	fixture := projectCloudStandIn(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if stop, err := fixture.server.StartAntigravityProjectConsent(ctx); stop != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("stop registered=%t error=%v", stop != nil, err)
	}
	if fixture.server.antigravityProjectOwner.Load() != nil {
		t.Fatal("cancelled startup published owner")
	}
}
