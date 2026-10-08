package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestGoCacheOwnershipAndCleanupSurviveStoreRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "tasks", "b7310000-0000-4000-8000-000000000001", "work", "cache")
	at := time.Unix(1_790_000_000, 0)
	registered := GoCache{Path: path, OwnerKind: "task", OwnerID: "b7310000-0000-4000-8000-000000000001",
		Purpose: "verification_gate_build_cache", Rebuildable: true, CreatedAt: at}
	s, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterGoCache(ctx, registered); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkGoCacheCleanup(ctx, path, "task", registered.OwnerID, "kept", "owner_unknown", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.GoCachesByOwner(ctx, "task", registered.OwnerID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("after restart: %d registrations, %v", len(rows), err)
	}
	got := rows[0]
	if got.Path != path || got.OwnerKind != "task" || got.OwnerID != registered.OwnerID ||
		got.Purpose != registered.Purpose || !got.Rebuildable || !got.CreatedAt.Equal(at) ||
		got.LastOutcome != "kept" || got.LastReason != "owner_unknown" || !got.LastAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("registration changed across restart: %+v", got)
	}
	if err := s.RegisterGoCache(ctx, registered); err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	other := registered
	other.OwnerID = "b7310000-0000-4000-8000-000000000002"
	if err := s.RegisterGoCache(ctx, other); err == nil {
		t.Fatal("a second owner took an already registered path")
	}
}
