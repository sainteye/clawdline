package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// ExactTerminalResolver maps a kernel PID and controlling TTY to a backend
// terminal. It must verify backend ownership, never inspect hook JSON or argv.
// An absent mapping returns an empty string. The caller owns its timeout.
type ExactTerminalResolver func(context.Context, int, string) (string, error)

// NativeProcessObservation keeps kernel workspace/boot evidence separate from
// hook claims. An empty WorkspaceCWD or BootID is unknown, never a guessed path.
// The process port also retains these generations so durable bridge bindings
// and every subsequent live read compare the same native identity.
type NativeProcessObservation struct {
	Process      ports.AntigravityProcess
	WorkspaceCWD string
	BootID       string
	// KernelGeneration additionally fences Darwin exec via pidversion and
	// Linux observed exec via start-code/end-code/stack addresses. Persist it
	// beside the binding if it must survive a later process exec/revalidation.
	KernelGeneration string
}

// NativeProcessReader has no cache, retries, background goroutines or shell/C
// dependency. Its resolver must be safe for concurrent calls if shared.
type NativeProcessReader struct {
	resolve ExactTerminalResolver
	read    func(context.Context, int) (nativeProcessSample, error)
}

var _ ports.AntigravityProcessReader = (*NativeProcessReader)(nil)

func NewNativeProcessReader(resolve ExactTerminalResolver) *NativeProcessReader {
	return &NativeProcessReader{resolve: resolve, read: readNativeProcessSample}
}

// NativeIdentityError exposes only stable codes, never private paths/argv.
// Cancellation remains discoverable with errors.Is.
type NativeIdentityError struct {
	Code  string
	cause error
}

func (e *NativeIdentityError) Error() string { return e.Code }
func (e *NativeIdentityError) Unwrap() error { return e.cause }
func nativeIdentityError(code string, cause error) error {
	return &NativeIdentityError{Code: code, cause: cause}
}

func nativeContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nativeIdentityError("antigravity_native_cancelled", err)
	}
	return nil
}

type nativeProcessSample struct {
	NativeProcessObservation
	// Kernel-specific generation (including boot and start ticks on Linux).
	generation string
	image      os.FileInfo
}

func nativeSamplesEqual(a, b nativeProcessSample) bool {
	return a.Process == b.Process && a.WorkspaceCWD == b.WorkspaceCWD &&
		a.BootID == b.BootID && a.generation == b.generation &&
		a.image != nil && b.image != nil && sameExecutable(a.image, b.image)
}

func (r *NativeProcessReader) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	o, err := r.ReadAntigravityProcessWithWorkspace(ctx, pid)
	return o.Process, err
}

// ReadAntigravityProcessWithWorkspace brackets terminal resolution with two
// complete kernel reads. Exit, reuse, executable/parent/TTY/workspace changes
// refuse the entire observation. No partially validated identity is returned.
// The backend resolver must honor ctx; synchronous kernel calls are checked
// before/after, rather than abandoned in leaking timeout goroutines.
func (r *NativeProcessReader) ReadAntigravityProcessWithWorkspace(ctx context.Context, pid int) (NativeProcessObservation, error) {
	if err := nativeContext(ctx); err != nil {
		return NativeProcessObservation{}, err
	}
	if pid <= 0 || int64(pid) > 1<<31-1 {
		return NativeProcessObservation{}, nativeIdentityError("antigravity_native_pid_invalid", nil)
	}
	before, err := r.read(ctx, pid)
	if err != nil {
		return NativeProcessObservation{}, err
	}
	p := before.Process
	birth, birthErr := time.Parse(time.RFC3339Nano, p.Birth)
	if p.PID != pid || p.ParentPID < 0 || birthErr != nil || birth.IsZero() ||
		p.Birth != birth.UTC().Format(time.RFC3339Nano) || !filepath.IsAbs(p.Executable) || before.image == nil {
		return NativeProcessObservation{}, nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	if err = nativeContext(ctx); err != nil {
		return NativeProcessObservation{}, err
	}
	terminal := ""
	if r.resolve != nil && p.TTY != "" {
		terminal, err = r.resolve(ctx, pid, p.TTY)
		if err != nil {
			if cancelled := nativeContext(ctx); cancelled != nil {
				return NativeProcessObservation{}, cancelled
			}
			return NativeProcessObservation{}, nativeIdentityError("antigravity_native_terminal_unreadable", nil)
		}
	}
	if err = nativeContext(ctx); err != nil {
		return NativeProcessObservation{}, err
	}
	after, err := r.read(ctx, pid)
	if err != nil {
		return NativeProcessObservation{}, err
	}
	if err = nativeContext(ctx); err != nil {
		return NativeProcessObservation{}, err
	}
	if !nativeSamplesEqual(before, after) {
		return NativeProcessObservation{}, nativeIdentityError("antigravity_native_process_changed", nil)
	}
	after.Process.TerminalID = terminal
	after.Process.BootID = after.BootID
	after.Process.KernelGeneration = after.generation
	after.KernelGeneration = after.generation
	return after.NativeProcessObservation, nil
}

func nativeProcessReadError(err error) error {
	code := "antigravity_native_process_unreadable"
	if errors.Is(err, os.ErrNotExist) {
		code = "antigravity_native_process_missing"
	}
	return nativeIdentityError(code, nil)
}
