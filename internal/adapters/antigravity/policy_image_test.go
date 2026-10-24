//go:build darwin || linux

package antigravity

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
)

type executionPlanFixture struct {
	processFixture
	plan         NativeExecutionPlan
	beforeReturn func()
}

func (r *executionPlanFixture) ReadAntigravityExecutionPlan(context.Context, int) (NativeExecutionPlan, error) {
	if r.beforeReturn != nil {
		r.beforeReturn()
	}
	return r.plan, nil
}
func TestBridgeImageFingerprintAndExecutionPlanStayPinned(t *testing.T) {
	for _, kind := range []string{"stable", "replaced-image", "rewritten-image", "wrong-process", "unsupported-reader", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := f.procs[41]
			tokens, err := parseNativeCommandLine([]byte(f.exe+"\x00--mode\x00plan\x00"), f.exe)
			if err != nil {
				t.Fatal(err)
			}
			reader := &executionPlanFixture{processFixture: f.procs, plan: NativeExecutionPlan{Process: p, Mode: tokens.Mode, Digest: tokens.Digest, WorkspaceCWD: f.home}}
			f.b.reader = reader
			original, err := f.b.ImageFingerprint(ctx)
			if err != nil || original.Executable != f.exe || original.Digest == "" {
				t.Fatal(err)
			}
			switch kind {
			case "replaced-image":
				reader.beforeReturn = func() {
					if err := os.Rename(f.exe, f.exe+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(f.exe, []byte("synthetic replacement"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "rewritten-image":
				reader.beforeReturn = func() {
					if err := os.WriteFile(f.exe, []byte("synthetic rewritten image"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "wrong-process":
				reader.plan.Process.KernelGeneration = "new-generation"
			case "unsupported-reader":
				f.b.reader = f.procs
			case "cancelled":
				reader.beforeReturn = cancel
			}
			got, err := f.b.ExecutionPlan(ctx, p)
			if kind == "stable" {
				if err != nil || got != reader.plan {
					t.Fatal("pinned plan refused", err)
				}
				again, err := f.b.ImageFingerprint(ctx)
				if err != nil || again != original {
					t.Fatal("unchanged image changed fingerprint", err)
				}
			} else {
				if err == nil || got != (NativeExecutionPlan{}) {
					t.Fatal("unverified plan escaped", err)
				}
				if kind == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			}
		})
	}
}

var _ ports.AntigravityProcessReader = (*executionPlanFixture)(nil)

func TestBridgePolicyReadsRefuseContentionWithoutQueuing(t *testing.T) {
	f := fixture(t)
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	if _, err := f.b.ImageFingerprint(context.Background()); err == nil {
		t.Fatal("busy fingerprint queued or succeeded")
	} else {
		wantCode(t, err, "antigravity_bridge_busy")
	}
	if _, err := f.b.ExecutionPlan(context.Background(), f.procs[41]); err == nil {
		t.Fatal("busy plan queued or succeeded")
	} else {
		wantCode(t, err, "antigravity_bridge_busy")
	}
}
