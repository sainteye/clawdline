package store

import (
	"context"
	"sync"
	"testing"
)

func TestCodegraphGuidanceOnceSurvivesRestartAndUnknownAttempts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.ConfigureCodegraphGuidance(ctx, "codex:one", 0, true, false, "1", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := s.ClaimCodegraphGuidance(ctx, "codex:one")
			if err != nil {
				t.Error(err)
			}
			if claimed {
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claims != 1 {
		t.Fatalf("concurrent automatic attempts=%d", claims)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, found, err := s.CodegraphGuidance(ctx, "codex:one")
	if err != nil || !found || got.State != "unknown" {
		t.Fatalf("crash state=%+v found=%v err=%v", got, found, err)
	}
	if _, again, _ := s.ClaimCodegraphGuidance(ctx, "codex:one"); again {
		t.Fatal("unknown attempt was repeated")
	}
	if _, found, _ = s.CodegraphGuidance(ctx, "codex:two"); found {
		t.Fatal("conversation leaked")
	}
	if _, err = s.ConfigureCodegraphGuidance(ctx, "codex:one", 0, false, false, "1", "en", false); err != ErrCodegraphGuidanceChanged {
		t.Fatalf("stale browser overwrote preference: %v", err)
	}
	newer, err := s.ConfigureCodegraphGuidance(ctx, "codex:one", g.Revision, true, true, "1", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishCodegraphGuidance(ctx, "codex:one", g.Revision, "provided"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.CodegraphGuidance(ctx, "codex:one")
	if got.State != "pending" || got.Revision != newer.Revision {
		t.Fatal("old receipt overwrote explicit re-provision")
	}
	_, claimed, err := s.ClaimCodegraphGuidance(ctx, "codex:one")
	if err != nil || !claimed {
		t.Fatal("explicit re-provision not claimed", err)
	}
	if err = s.FinishCodegraphGuidance(ctx, "codex:one", newer.Revision, "pending"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err = s.ClaimCodegraphGuidance(ctx, "codex:one"); err != nil || !claimed {
		t.Fatal("proved unsent did not restore pending", err)
	}
	if err = s.FinishCodegraphGuidance(ctx, "codex:one", newer.Revision, "provided"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, _ = s.ClaimCodegraphGuidance(ctx, "codex:one"); claimed {
		t.Fatal("provided instructions repeated")
	}
}

func TestCodegraphGuidanceLaunchAndDisableAreSeparatePreferences(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, err := s.ConfigureCodegraphGuidance(ctx, "claude:launch", 0, true, false, "1", "en", true)
	if err != nil || g.State != "launch" {
		t.Fatal(g, err)
	}
	if _, claimed, _ := s.ClaimCodegraphGuidance(ctx, "claude:launch"); claimed {
		t.Fatal("launch context repeated")
	}
	g, err = s.ConfigureCodegraphGuidance(ctx, "claude:launch", g.Revision, false, false, "1", "en", false)
	if err != nil || g.State != "pending" || g.Enabled {
		t.Fatal(g, err)
	}
	if _, claimed, _ := s.ClaimCodegraphGuidance(ctx, "claude:launch"); !claimed {
		t.Fatal("disable update missing")
	}
	if _, err = s.ConfigureCodegraphGuidance(ctx, "claude:launch", 0, true, false, "1", "en", true); err != nil {
		t.Fatal(err)
	}
	off, err := s.ConfigureCodegraphGuidance(ctx, "claude:not-yet-sent", 0, false, true, "1", "en", false)
	if err != nil || off.State != "pending" {
		t.Fatal("an initial launch opt-out lost its stop update", off, err)
	}

	got, _, _ := s.CodegraphGuidance(ctx, "claude:launch")
	if got.Enabled || got.State != "unknown" {
		t.Fatal("launch reset manual preference", got)
	}
}
