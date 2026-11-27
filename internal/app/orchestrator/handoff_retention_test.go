package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// seedPackage stores a handoff record and writes its package directory with
// one file of the given size.
func seedPackage(t *testing.T, b *Broker, h Handoff, size int) string {
	t.Helper()
	dir := filepath.Join(b.HandoffRoot(), h.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.md"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Dir = dir
	if h.CreatedAt == 0 {
		h.CreatedAt = b.now().Unix()
	}
	if err := b.createOpened(t.Context(), store.TableHandoffs, h.ID, h.State, h, "handoff.opening", b.now()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

const (
	idEnded     = "11111111-1111-4111-8111-111111111111"
	idRecent    = "22222222-2222-4222-8222-222222222222"
	idOpening   = "33333333-3333-4333-8333-333333333333"
	idBoards    = "44444444-4444-4444-8444-444444444444"
	idMilestone = "55555555-5555-4555-8555-555555555555"
	idFailed    = "66666666-6666-4666-8666-666666666666"
	idOrphan    = "77777777-7777-4777-8777-777777777777"
)

func TestAHandoffPackageGoesADayAfterItsHandoffEnded(t *testing.T) {
	b, ctx, clock, _ := stuckBroker(t)
	ended := seedPackage(t, b, Handoff{ID: idEnded, State: HandoffDelivered}, 1000)
	failed := seedPackage(t, b, Handoff{ID: idFailed, State: HandoffSpawnFailed}, 500)
	opening := seedPackage(t, b, Handoff{ID: idOpening, State: HandoffOpening}, 10)
	boards := seedPackage(t, b, Handoff{ID: idBoards, State: HandoffDelivered, BoardItems: []string{"item"}}, 10)
	milestone := seedPackage(t, b, Handoff{ID: idMilestone, State: HandoffDelivered, Milestone: true}, 10)

	clock.at = clock.at.Add(24*time.Hour + time.Second)
	recent := seedPackage(t, b, Handoff{ID: idRecent, State: HandoffDelivered}, 10)
	clock.at = clock.at.Add(23 * time.Hour)

	rep, err := b.SweepHandoffPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exists(ended) || exists(failed) {
		t.Fatalf("ended packages a day old were kept: delivered %v, spawn_failed %v", exists(ended), exists(failed))
	}
	for name, dir := range map[string]string{"recent": recent, "opening": opening, "pending board transfer": boards, "milestone without receiver": milestone} {
		if !exists(dir) {
			t.Errorf("%s package was removed", name)
		}
	}
	if rep.Removed != 2 || rep.RemovedBytes != 1500 || rep.InProgress != 3 || rep.Recent != 1 {
		t.Fatalf("report %+v, want 2 removed (1500 bytes), 3 in progress, 1 recent", rep)
	}
	if h := handoffState(t, b, ctx, idEnded); h.PackageRemovedAt != clock.at.Unix() {
		t.Fatalf("package_removed_at %d, want %d", h.PackageRemovedAt, clock.at.Unix())
	}
	if h := handoffState(t, b, ctx, idOpening); h.PackageRemovedAt != 0 {
		t.Fatalf("a kept package says it was removed: %+v", h)
	}
}

func TestAPackageNothingProvesEndedIsKept(t *testing.T) {
	b, ctx, clock, _ := stuckBroker(t)
	root := b.HandoffRoot()
	orphan := filepath.Join(root, idOrphan)
	notAnID := filepath.Join(root, "notes")
	for _, dir := range []string{orphan, notAnID} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A record whose directory is elsewhere, and a symbolic link named like
	// a handoff pointing out of the root at a directory that must survive.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Handoff{ID: idEnded, State: HandoffDelivered, Dir: outside, CreatedAt: b.now().Unix()}
	if err := b.createOpened(ctx, store.TableHandoffs, idEnded, h.State, h, "handoff.opening", b.now()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, idEnded)); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(48 * time.Hour)
	rep, err := b.SweepHandoffPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(orphan) || !exists(notAnID) || !exists(filepath.Join(outside, "keep.txt")) || !exists(filepath.Join(root, idEnded)) {
		t.Fatal("a package nothing proves ended, or a link out of the root, was removed")
	}
	if rep.Removed != 0 || rep.Unknown != 3 {
		t.Fatalf("report %+v, want nothing removed and 3 unknown", rep)
	}
}

func TestTheSweepRemovesAtMostItsBatch(t *testing.T) {
	b, ctx, clock, _ := stuckBroker(t)
	for i := 0; i < HandoffPackageSweepLimit+2; i++ {
		id := NewUUID()
		seedPackage(t, b, Handoff{ID: id, State: HandoffSpawnFailed}, 1)
	}
	clock.at = clock.at.Add(25 * time.Hour)
	rep, err := b.SweepHandoffPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Removed != HandoffPackageSweepLimit || rep.Deferred != 2 {
		t.Fatalf("report %+v, want %d removed and 2 deferred", rep, HandoffPackageSweepLimit)
	}
	if rep, _ = b.SweepHandoffPackages(ctx); rep.Removed != 2 {
		t.Fatalf("second sweep %+v, want the 2 left", rep)
	}
}
