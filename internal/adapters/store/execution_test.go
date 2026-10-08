package store

import (
	"context"
	"testing"
)

func TestExecutionGenerationSurvivesRestartAndRejectsReuse(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	row := []ExecutionSeen{{ID: "%19", Source: "tmux", Fingerprint: "claude:31:100"}}
	proof := func(string, string) bool { return false }
	first, err := s.ObserveExecutions(ctx, "mac_a", row, proof)
	if err != nil || first["%19"] == "" {
		t.Fatalf("first: %v %v", first, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.ObserveExecutions(ctx, "mac_a", row, proof)
	if err != nil || again["%19"] != first["%19"] {
		t.Fatalf("restart changed generation: %v %v", again, err)
	}
	if count, err := s.ExecutionCount(ctx, "mac_a"); err != nil || count != 1 {
		t.Fatalf("durable execution count: %d %v", count, err)
	}
	if count, err := s.ExecutionCount(ctx, ""); err != nil || count != 0 {
		t.Fatalf("no Cloud identity claimed executions: %d %v", count, err)
	}
	other, err := s.ObserveExecutions(ctx, "mac_b", row, proof)
	if err != nil || other["%19"] == first["%19"] {
		t.Fatalf("other machine shared generation: %v %v", other, err)
	}
	if err := s.AdmitExecution(ctx, "mac_b", "%19", first["%19"]); err == nil || err.Error() != "execution_generation_changed" {
		t.Fatalf("cross-machine target: %v", err)
	}
	if _, err := s.ObserveExecutions(ctx, "mac_a", nil, proof); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitExecution(ctx, "mac_a", "%19", first["%19"]); err != nil {
		t.Fatalf("partial source lost authority: %v", err)
	}
	if _, err := s.ObserveExecutions(ctx, "mac_a", nil, func(_, source string) bool { return source == "tmux" }); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitExecution(ctx, "mac_a", "%19", first["%19"]); err == nil || err.Error() != "execution_target_missing" {
		t.Fatalf("proven absence: %v", err)
	}
	if count, err := s.ExecutionCount(ctx, "mac_a"); err != nil || count != 0 {
		t.Fatalf("proven absence was still counted: %d %v", count, err)
	}
	returned, err := s.ObserveExecutions(ctx, "mac_a", row, proof)
	if err != nil || returned["%19"] == first["%19"] {
		t.Fatalf("reappearance reused generation: %v %v", returned, err)
	}
	row[0].Fingerprint = "claude:31:200"
	reused, err := s.ObserveExecutions(ctx, "mac_a", row, proof)
	if err != nil || reused["%19"] == returned["%19"] {
		t.Fatalf("PID reuse: %v %v", reused, err)
	}
	if err := s.AdmitExecution(ctx, "mac_a", "%19", returned["%19"]); err == nil || err.Error() != "execution_generation_changed" {
		t.Fatalf("stale target admitted: %v", err)
	}
}

func TestExecutionWriteFailureDoesNotInventAStoredGeneration(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	row := []ExecutionSeen{{ID: "%1", Source: "tmux", Fingerprint: "codex:2:300"}}
	got, err := s.ObserveExecutions(context.Background(), "mac_a", row, func(string, string) bool { return false })
	if err == nil || got != nil {
		t.Fatalf("closed database invented generation: %v %v", got, err)
	}
	if err := s.AdmitExecution(context.Background(), "mac_a", "%1", "anything"); err == nil {
		t.Fatal("closed database admitted a target")
	}
}
