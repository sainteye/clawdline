//go:build darwin

package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestDarwinProcessCensusIncludesCurrentBirthAndRespectsBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows, err := processIdentities(ctx, capacity.MaximumBotProcessCensusBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.pid != os.Getpid() {
			continue
		}
		if row.ppid != os.Getppid() || row.pgid <= 0 || row.stamp == "" || deadProcess(row) {
			t.Fatalf("current process census identity: %+v", row)
		}
		if _, err := processIdentities(ctx, 1); !errors.Is(err, io.ErrShortBuffer) {
			t.Fatalf("oversized census result: %v", err)
		}
		return
	}
	t.Fatal("current process was missing from the census")
}

// The kernel's size estimate leaves room for only five more processes. When
// more start before the copy, sysctl answers ENOMEM; the 2026-11-24 incident
// latched one such answer from the 50 ms survey as a final cleanup failure.
func TestDarwinProcessCensusRetriesASnapshotThatOutgrewItsEstimate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	failures := 2
	read := func(out *unix.KinfoProc, size *uintptr) error {
		if out != nil && failures > 0 {
			failures--
			return syscall.ENOMEM
		}
		return readDarwinProcesses(out, size)
	}
	rows, err := darwinProcessIdentities(ctx, capacity.MaximumBotProcessCensusBytes, read)
	if err != nil {
		t.Fatalf("transient ENOMEM was not retried: %v", err)
	}
	if !containsPID(rows, os.Getpid()) {
		t.Fatal("current process was missing from the retried census")
	}
}

// A snapshot that keeps outgrowing every grown buffer is still an inventory
// failure: it cannot prove cleanup, and the attempts stay bounded.
func TestDarwinProcessCensusGivesUpAfterBoundedAttempts(t *testing.T) {
	calls := 0
	read := func(out *unix.KinfoProc, size *uintptr) error {
		if out == nil {
			*size = 4 * unix.SizeofKinfoProc
			return nil
		}
		calls++
		return syscall.ENOMEM
	}
	_, err := darwinProcessIdentities(context.Background(), capacity.MaximumBotProcessCensusBytes, read)
	if !errors.Is(err, syscall.ENOMEM) {
		t.Fatalf("persistent ENOMEM: %v", err)
	}
	if calls != capacity.MaximumBotProcessCensusAttempts {
		t.Fatalf("copy attempts = %d, want %d", calls, capacity.MaximumBotProcessCensusAttempts)
	}
}

func containsPID(rows []processIdentity, pid int) bool {
	for _, row := range rows {
		if row.pid == pid {
			return true
		}
	}
	return false
}
