package antigravity

import (
	"context"
	"os"
	"runtime"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// ImageFingerprint is private local policy input. The digest was computed
// once at bridge construction; each read verifies the same kernel change stamp.
// It is not a compatibility, account or effective-permission receipt.
type ImageFingerprint struct {
	Executable, Digest, Platform, Architecture string
}

func (b *Bridge) imageFingerprint(ctx context.Context) (ImageFingerprint, error) {
	if err := nativeContext(ctx); err != nil {
		return ImageFingerprint{}, err
	}
	info, err := os.Lstat(b.executable)
	if err != nil || !info.Mode().IsRegular() || !sameExecutable(info, b.executableInfo) {
		return ImageFingerprint{}, refuse("antigravity_executable_changed")
	}
	if err := nativeContext(ctx); err != nil {
		return ImageFingerprint{}, err
	}
	return ImageFingerprint{Executable: b.executable, Digest: b.executableDigest, Platform: runtime.GOOS, Architecture: runtime.GOARCH}, nil
}
func (b *Bridge) ImageFingerprint(ctx context.Context) (ImageFingerprint, error) {
	if err := nativeContext(ctx); err != nil {
		return ImageFingerprint{}, err
	}
	if !b.mu.TryLock() {
		return ImageFingerprint{}, refuse("antigravity_bridge_busy")
	}
	defer b.mu.Unlock()
	return b.imageFingerprint(ctx)
}

// ExecutionPlan requires this bridge's exact pinned process image, plus the
// reader's fresh generation/workspace/terminal and argument observations. It
// never promotes observed argv to an effective native permission ceiling.
func (b *Bridge) ExecutionPlan(ctx context.Context, expected ports.AntigravityProcess) (NativeExecutionPlan, error) {
	if err := nativeContext(ctx); err != nil {
		return NativeExecutionPlan{}, err
	}
	if !b.mu.TryLock() {
		return NativeExecutionPlan{}, refuse("antigravity_bridge_busy")
	}
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, MaxProcessReadTime)
	defer cancel()
	if _, err := b.imageFingerprint(ctx); err != nil {
		return NativeExecutionPlan{}, err
	}
	if !b.supported(expected) {
		return NativeExecutionPlan{}, refuse("antigravity_process_unverified")
	}
	reader, ok := b.reader.(interface {
		ReadAntigravityExecutionPlan(context.Context, int) (NativeExecutionPlan, error)
	})
	if !ok {
		return NativeExecutionPlan{}, nativeIdentityError("antigravity_native_plan_unsupported", nil)
	}
	plan, err := reader.ReadAntigravityExecutionPlan(ctx, expected.PID)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	if plan.Process != expected {
		return NativeExecutionPlan{}, refuse("antigravity_process_unverified")
	}
	if _, err := b.imageFingerprint(ctx); err != nil {
		return NativeExecutionPlan{}, err
	}
	return plan, nil
}
