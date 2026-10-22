package http

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"crypto/sha256"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
)

func TestAntigravityLaunchCapacityTracksItsActualOwner(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("native private ownership is unsupported")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: root}}
	rows := []string{"antigravity.saved_launches", "antigravity.context_history", "antigravity.context_receipts", "antigravity.context_index_bytes", "antigravity.launch_context_bytes", "antigravity.first_input_slots"}
	for _, row := range rows {
		if s.antigravityCapacityMeasures()[row]().Known {
			t.Fatal("absent owner became zero:", row)
		}
	}
	stop, err := s.StartAntigravityLaunchStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := s.StartAntigravityLaunchStore(context.Background()); err == nil {
		t.Fatal("duplicate owner admitted")
	}
	for _, row := range rows {
		if r := s.antigravityCapacityMeasures()[row](); !r.Known || r.Used != 0 {
			t.Fatalf("allocated empty owner %s: %+v", row, r)
		}
	}
	owner := s.antigravityLaunchStore.Load()
	prepared, err := owner.Prepare(context.Background(), projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: root, Executable: filepath.Join(root, "agy"), PermissionMode: "plan", Memory: "Synthetic original memory"})
	if err != nil {
		t.Fatal(err)
	}
	readings := s.antigravityCapacityMeasures()
	if r := readings[rows[0]](); !r.Known || r.Used != 1 {
		t.Fatalf("real launch not measured: %+v", r)
	}
	if r := readings[rows[4]](); !r.Known || r.Used <= 0 {
		t.Fatalf("real context not measured: %+v", r)
	}
	p := ports.AntigravityProcess{PID: 1234, ParentPID: 123, Birth: "2026-10-22T10:00:00.123456Z", Executable: prepared.Request.Executable, TTY: "ttysynthetic", TerminalID: "%42", BootID: "synthetic-boot", KernelGeneration: "synthetic-exec"}
	if err := owner.AttachTerminal(context.Background(), prepared.Record.ID, "tmux", p.TerminalID); err != nil {
		t.Fatal(err)
	}
	if err := owner.PinProcess(context.Background(), prepared.Record.ID, p); err != nil {
		t.Fatal(err)
	}
	if _, grant, err := owner.ReserveFirstInput(context.Background(), prepared.Record.ID, "74000000-0000-4000-8000-000000000098", fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic first input"))), p); err != nil || grant == nil {
		t.Fatal("reservation failed", err)
	}
	if r := s.antigravityCapacityMeasures()[rows[5]](); !r.Known || r.Used != 1 {
		t.Fatalf("actual first-input reservation not measured: %+v", r)
	}
	if err := os.WriteFile(prepared.Record.ContextPath, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := s.antigravityCapacityMeasures()[rows[4]](); r.Known {
		t.Fatal("tampered context became a known reading")
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); stop() }()
	}
	wg.Wait()
	for _, row := range rows {
		if s.antigravityCapacityMeasures()[row]().Known {
			t.Fatal("closed owner became zero:", row)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StartAntigravityLaunchStore(ctx); err == nil {
		t.Fatal("cancelled owner started")
	}
}
