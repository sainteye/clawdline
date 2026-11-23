package gocache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

// fakeCache writes n entries of size bytes, entry i used i hours before now
// (so the last ones are the oldest), plus one unrecognized file.
func fakeCache(t *testing.T, n int, size int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("This directory holds cached build artifacts from the Go build system.\nRun \"go clean -cache\" if the directory is getting too large.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%064x-d", i)
		sub := filepath.Join(dir, name[:2])
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(sub, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	odd := filepath.Join(dir, "00", "notes.txt")
	_ = os.MkdirAll(filepath.Dir(odd), 0o755)
	if err := os.WriteFile(odd, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-1000 * time.Hour)
	_ = os.Chtimes(odd, old, old)
	return dir
}

func opts(dir string) Options {
	return Options{
		Dir: dir, BudgetBytes: 10 * 1000, ReserveBytes: 0, Protect: 2 * time.Hour,
		MaxFiles: 1000, Now: func() time.Time { return now },
		DiskFree: func(string) (int64, error) { return 1 << 40, nil },
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func entryPath(dir string, i int) string {
	name := fmt.Sprintf("%064x-d", i)
	return filepath.Join(dir, name[:2], name)
}

func TestTrimRemovesOldestRecognizedEntriesDownToTheBudget(t *testing.T) {
	dir := fakeCache(t, 30, 1000) // 30 entries + 1 unknown = 31,000 bytes
	rep, err := Trim(context.Background(), opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before=%d after=%d removed=%d freed=%d unknown=%d", rep.BeforeBytes, rep.AfterBytes, rep.Removed, rep.FreedBytes, rep.Unknown)
	if rep.BeforeBytes != 31000 || rep.Triggered != "budget" || rep.AfterBytes > 10000 {
		t.Fatalf("report %+v", rep)
	}
	if exists(entryPath(dir, 29)) || !exists(entryPath(dir, 0)) || !exists(entryPath(dir, 2)) {
		t.Fatal("did not remove oldest-first")
	}
	if !exists(filepath.Join(dir, "00", "notes.txt")) || !exists(filepath.Join(dir, "README")) {
		t.Fatal("removed a file Go did not write")
	}
}

func TestTrimNeverRemovesRecentlyUsedEntries(t *testing.T) {
	dir := fakeCache(t, 30, 1000)
	o := opts(dir)
	o.BudgetBytes = 0
	rep, err := Trim(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	// Entries 0 and 1 are under two hours old; entry 2 is exactly at the cutoff.
	for i := 0; i <= 2; i++ {
		if !exists(entryPath(dir, i)) {
			t.Fatalf("entry %d used within the protection window was removed", i)
		}
	}
	if rep.Protected == 0 {
		t.Fatalf("report %+v", rep)
	}
}

func TestTrimUnderBudgetWithRoomRemovesNothing(t *testing.T) {
	dir := fakeCache(t, 5, 1000)
	rep, err := Trim(context.Background(), opts(dir))
	if err != nil || rep.Removed != 0 || rep.Triggered != "" {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestTrimLowReserveTriggersUnderBudget(t *testing.T) {
	dir := fakeCache(t, 10, 1000)
	o := opts(dir)
	o.BudgetBytes = 1 << 30
	o.ReserveBytes = 5000
	o.DiskFree = func(string) (int64, error) { return 1000, nil }
	rep, err := Trim(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Triggered != "reserve" || rep.FreedBytes < 4000 {
		t.Fatalf("report %+v", rep)
	}
}

func TestUnknownMeasurementsAuthorizeNoDeletion(t *testing.T) {
	boom := errors.New("injected")
	cases := []struct {
		name string
		edit func(*Options)
		code Code
	}{
		{"disk free fails", func(o *Options) { o.DiskFree = func(string) (int64, error) { return 0, boom } }, CodeDiskUnknown},
		{"a subdirectory cannot be listed", func(o *Options) {
			o.ReadDir = func(d string) ([]fs.DirEntry, error) {
				if d != o.Dir {
					return nil, boom
				}
				return os.ReadDir(d)
			}
		}, CodeUnmeasured},
		{"too many files", func(o *Options) { o.MaxFiles = 5 }, CodeScanBound},
		{"not a Go cache", func(o *Options) { _ = os.Remove(filepath.Join(o.Dir, "README")) }, CodeNotGoCache},
		{"cancelled", func(o *Options) {}, CodeInterrupted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fakeCache(t, 30, 1000)
			o := opts(dir)
			removed := 0
			o.Remove = func(p string) error { removed++; return os.Remove(p) }
			c.edit(&o)
			ctx := context.Background()
			if c.code == CodeInterrupted {
				cctx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cctx
			}
			_, err := Trim(ctx, o)
			if CodeOf(err) != c.code {
				t.Fatalf("code %q (%v), want %q", CodeOf(err), err, c.code)
			}
			if removed != 0 {
				t.Fatalf("%d entries removed on an unknown measurement", removed)
			}
		})
	}
}

func TestARemoveFailureStopsAndIsTyped(t *testing.T) {
	dir := fakeCache(t, 30, 1000)
	o := opts(dir)
	calls := 0
	o.Remove = func(string) error { calls++; return errors.New("injected") }
	_, err := Trim(context.Background(), o)
	if CodeOf(err) != CodeRemoveFailed || calls != 1 {
		t.Fatalf("%v after %d calls", err, calls)
	}
}

func TestAnEntryReusedAfterListingIsKept(t *testing.T) {
	dir := fakeCache(t, 30, 1000)
	o := opts(dir)
	oldest := entryPath(dir, 29)
	o.Lstat = func(p string) (fs.FileInfo, error) {
		if p == oldest {
			_ = os.Chtimes(p, now, now) // a build reused it mid-trim
		}
		return os.Lstat(p)
	}
	if _, err := Trim(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !exists(oldest) {
		t.Fatal("an entry refreshed after listing was removed")
	}
}

func TestTreeBytesIsBoundedAndTyped(t *testing.T) {
	dir := fakeCache(t, 4, 100)
	total, complete, err := TreeBytes(context.Background(), dir, 100)
	if err != nil || !complete || total < 500 {
		t.Fatalf("%d %v %v", total, complete, err)
	}
	if _, _, err := TreeBytes(context.Background(), dir, 2); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("bound not enforced: %v", err)
	}
	if _, _, err := TreeBytes(context.Background(), filepath.Join(dir, "missing"), 2); err == nil {
		t.Fatal("missing directory measured")
	}
}
