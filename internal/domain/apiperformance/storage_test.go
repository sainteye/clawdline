package apiperformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func closeRuntime(t *testing.T, r *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStorageRestartDedupAndConcurrentWriterFence(t *testing.T) {
	dir := t.TempDir()
	db, h, revision, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute)
	h.source = Provenance{BootID: "old-boot", Build: "old-build"}
	h.observe(event{at: at, route: "GET /v1/health", status: 200, duration: time.Millisecond}, MaxRoutes)
	payload, _ := json.Marshal(h)
	if err = writeSnapshot(db, revision, payload); err != nil {
		t.Fatal(err)
	}
	if err = writeSnapshot(db, revision, payload); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale writer overwrote snapshot: %v", err)
	}
	_ = db.Close()
	db, h, revision, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if revision != 1 || h.Minutes[floor(at.UnixMilli(), 60000)].Total.Completed != 1 {
		t.Fatalf("recovery duplicated/lost count: revision=%d %+v", revision, h)
	}
	other, _, otherRevision, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = writeSnapshot(db, revision, payload); err != nil {
		t.Fatal(err)
	}
	if err = writeSnapshot(other, otherRevision, payload); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second boot was not fenced: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "api-performance", "history.sqlite"))
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("owner file permissions: %v %v", info, err)
	}
}

func TestDiskFullAndOversizedSnapshotDoNotReplaceLastGoodRecord(t *testing.T) {
	limits := HardLimits()
	limits["store"] = 64 << 10
	db, h, rev, err := openStorage(t.TempDir(), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload, _ := json.Marshal(h)
	if err = writeSnapshot(db, rev, payload); err != nil {
		t.Fatal(err)
	}
	if err = writeSnapshot(db, rev+1, bytes.Repeat([]byte("x"), 128<<10)); err == nil {
		t.Fatal("page cap did not reject growth")
	}
	if err = writeSnapshot(db, rev+1, payload, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("snapshot cap: %v", err)
	}
	var got []byte
	if err = db.QueryRow("SELECT payload FROM snapshot WHERE id=1").Scan(&got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("failed commit changed durable bytes: %v", err)
	}
}

func TestUnavailableStoreStillServesHonestMemoryAndDoesNotRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	r := New(Options{Dir: dir})
	defer closeRuntime(t, r)
	r.Begin("GET /v1/health").Done(200, false)
	var v Reading
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		var err error
		v, err = r.Query(context.Background(), 1, time.Time{})
		if err == nil && v.StorageState == "unavailable" && len(r.events) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if v.StorageState != "unavailable" || v.StoreFailures != 1 || v.PersistedAtMS != 0 {
		t.Fatalf("disk failure was hidden: %+v", v)
	}
	for range 3 {
		_, _ = r.Query(context.Background(), 1, time.Time{})
	}
	if r.storeFailures.Load() != 1 {
		t.Fatal("reader retried failed disk writes")
	}
}

func TestGracefulStopFlushesQueuedRequestsOnceAndReloads(t *testing.T) {
	dir := t.TempDir()
	r := New(Options{Dir: dir})
	for range 20 {
		r.Begin("GET /v1/health").Done(200, false)
	}
	closeRuntime(t, r)
	db, h, _, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	var completed uint64
	for _, b := range h.Minutes {
		completed += b.Total.Completed
	}
	if completed != 20 {
		t.Fatalf("shutdown drain: %d", completed)
	}
	r = New(Options{Dir: dir})
	closeRuntime(t, r)
	db, h, _, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	completed = 0
	for _, b := range h.Minutes {
		completed += b.Total.Completed
	}
	if completed != 20 {
		t.Fatalf("restart replayed completions: %d", completed)
	}
}

func TestCorruptOrFutureSchemaIsUnavailableAndPreserved(t *testing.T) {
	dir := t.TempDir()
	db, _, rev, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"schema":999}`)
	if err = writeSnapshot(db, rev, payload); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, _, _, err = openStorage(dir); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown schema was accepted: %v", err)
	}
	r := New(Options{Dir: dir})
	closeRuntime(t, r)
	// A rejected schema stays rejected after a daemon boot; no empty history
	// is published over it as if it had been a fresh installation.
	if _, _, _, err = openStorage(dir); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed recovery overwrote evidence: %v", err)
	}
}

func TestWriterFailureIsCountedAndPendingSnapshotsCoalesce(t *testing.T) {
	r := unitRuntime()
	r.limits["store"] = 16 << 10
	w := r.startStorage(t.TempDir())
	loaded := <-w.loaded
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	h := emptyHistory()
	h.limits = r.limits
	for i := 0; i < MaxRoutes; i++ {
		h.observe(event{at: time.Now(), route: fmt.Sprintf("GET /%03d", i), status: 200}, MaxRoutes)
	}
	r.persist(w, &h, time.Now())
	close(w.pending)
	<-w.done
	if r.storeFailures.Load() != 1 || *r.storeState.Load() != "unavailable" {
		t.Fatalf("write failure was hidden: count=%d state=%s", r.storeFailures.Load(), *r.storeState.Load())
	}
	r = unitRuntime()
	pending := &storageWorker{pending: make(chan storedSnapshot, 1)}
	h = emptyHistory()
	at := time.Now()
	h.observe(event{at: at, route: "GET /v1/health", status: 200}, MaxRoutes)
	r.persist(pending, &h, at)
	h.observe(event{at: at, route: "GET /v1/health", status: 200}, MaxRoutes)
	r.persist(pending, &h, at)
	var got history
	if err := json.Unmarshal((<-pending.pending).bytes, &got); err != nil {
		t.Fatal(err)
	}
	if got.Minutes[floor(at.UnixMilli(), 60000)].Total.Completed != 2 {
		t.Fatal("coalescing lost or duplicated a completion")
	}
}

func BenchmarkBoundedStorageWrite(b *testing.B) {
	db, h, revision, err := openStorage(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	for hour := 0; hour < MaxHours; hour++ {
		for route := 0; route < MaxRoutes; route++ {
			h.observe(event{at: now.Add(time.Duration(hour-MaxHours+1) * time.Hour), route: fmt.Sprintf("GET /v1/%03d", route), status: 200, duration: time.Millisecond}, MaxRoutes)
		}
	}
	payload, _ := json.Marshal(h)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = writeSnapshot(db, revision, payload); err != nil {
			b.Fatal(err)
		}
		revision++
	}
}
