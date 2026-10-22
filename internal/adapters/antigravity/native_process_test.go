package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func requireNativeCode(t *testing.T, err error, want string) {
	t.Helper()
	var typed *NativeIdentityError
	if !errors.As(err, &typed) || typed.Code != want {
		t.Fatalf("error = %v, want typed %s", err, want)
	}
}

func nativeSampleFixture(t *testing.T) nativeProcessSample {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standalone")
	if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
		t.Fatal(err)
	}
	image, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return nativeProcessSample{NativeProcessObservation: NativeProcessObservation{
		Process:      ports.AntigravityProcess{PID: 42, ParentPID: 7, Birth: "2026-10-22T00:00:00.123456Z", Executable: path, TTY: "/dev/synthetic"},
		WorkspaceCWD: "/synthetic/workspace", BootID: "synthetic-boot-a",
	}, generation: "generation-a", image: image}
}

func TestNativeProcessConsistency(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("kernel image stamps unsupported on this platform")
	}
	mutations := map[string]func(*nativeProcessSample){
		"reuse":      func(p *nativeProcessSample) { p.Process.Birth = "2026-10-22T00:00:00.123457Z" },
		"exec":       func(p *nativeProcessSample) { p.Process.Executable = "/synthetic/other" },
		"parent":     func(p *nativeProcessSample) { p.Process.ParentPID++ },
		"tty":        func(p *nativeProcessSample) { p.Process.TTY = "/dev/other" },
		"workspace":  func(p *nativeProcessSample) { p.WorkspaceCWD = "/synthetic/other" },
		"boot":       func(p *nativeProcessSample) { p.BootID = "synthetic-boot-b" },
		"generation": func(p *nativeProcessSample) { p.generation = "generation-b" },
		"image":      func(p *nativeProcessSample) { p.image = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			sample := nativeSampleFixture(t)
			reads := 0
			r := NewNativeProcessReader(nil)
			r.read = func(context.Context, int) (nativeProcessSample, error) {
				reads++
				if reads == 2 {
					mutate(&sample)
				}
				return sample, nil
			}
			got, err := r.ReadAntigravityProcessWithWorkspace(context.Background(), 42)
			requireNativeCode(t, err, "antigravity_native_process_changed")
			if got != (NativeProcessObservation{}) {
				t.Fatal("partial identity escaped")
			}
		})
	}
}

func TestNativeProcessTerminalAuthority(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("unsupported image stamp")
	}
	sample := nativeSampleFixture(t)
	r := NewNativeProcessReader(func(ctx context.Context, pid int, tty string) (string, error) {
		if pid != 42 || tty != "/dev/synthetic" {
			t.Fatal("resolver did not receive kernel facts")
		}
		return "%verified-pane", nil
	})
	r.read = func(context.Context, int) (nativeProcessSample, error) { return sample, nil }
	got, err := r.ReadAntigravityProcessWithWorkspace(context.Background(), 42)
	if err != nil || got.Process.TerminalID != "%verified-pane" || got.WorkspaceCWD != sample.WorkspaceCWD || got.KernelGeneration != sample.generation {
		t.Fatalf("verified mapping = %+v, %v", got, err)
	}
	r.resolve = nil
	got, err = r.ReadAntigravityProcessWithWorkspace(context.Background(), 42)
	if err != nil || got.Process.TerminalID != "" {
		t.Fatalf("unmapped terminal became authority: %+v %v", got, err)
	}
	r.resolve = func(context.Context, int, string) (string, error) { return "", errors.New("private backend failure") }
	_, err = r.ReadAntigravityProcess(context.Background(), 42)
	requireNativeCode(t, err, "antigravity_native_terminal_unreadable")
	sample.Process.TTY = ""
	r.resolve = func(context.Context, int, string) (string, error) {
		t.Fatal("no TTY must not invoke resolver")
		return "", nil
	}
	got, err = r.ReadAntigravityProcessWithWorkspace(context.Background(), 42)
	if err != nil || got.Process.TerminalID != "" {
		t.Fatalf("TTY-less process: %+v %v", got, err)
	}
}

func TestNativeProcessRejectsIncompleteIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*nativeProcessSample){
		"missing birth":       func(s *nativeProcessSample) { s.Process.Birth = "" },
		"relative executable": func(s *nativeProcessSample) { s.Process.Executable = "standalone" },
		"foreign pid":         func(s *nativeProcessSample) { s.Process.PID = 43 },
		"local timezone":      func(s *nativeProcessSample) { s.Process.Birth = "2026-10-22T08:00:00.123456+08:00" },
	} {
		t.Run(name, func(t *testing.T) {
			sample := nativeSampleFixture(t)
			mutate(&sample)
			r := NewNativeProcessReader(nil)
			r.read = func(context.Context, int) (nativeProcessSample, error) { return sample, nil }
			got, err := r.ReadAntigravityProcessWithWorkspace(context.Background(), 42)
			requireNativeCode(t, err, "antigravity_native_identity_invalid")
			if got != (NativeProcessObservation{}) {
				t.Fatal("incomplete identity escaped")
			}
		})
	}
}

func TestNativeProcessUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skip("native reader supported")
	}
	_, err := NewNativeProcessReader(nil).ReadAntigravityProcess(context.Background(), os.Getpid())
	requireNativeCode(t, err, "antigravity_native_process_unsupported")
}

func TestNativeProcessCancellationAndExit(t *testing.T) {
	sample := nativeSampleFixture(t)
	r := NewNativeProcessReader(nil)
	r.read = func(context.Context, int) (nativeProcessSample, error) {
		t.Fatal("cancelled/invalid read executed")
		return sample, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.ReadAntigravityProcess(ctx, 42)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	for _, pid := range []int{0, -1} {
		_, err = r.ReadAntigravityProcess(context.Background(), pid)
		requireNativeCode(t, err, "antigravity_native_pid_invalid")
	}
	ctx, cancel = context.WithCancel(context.Background())
	r.read = func(context.Context, int) (nativeProcessSample, error) { return sample, nil }
	r.resolve = func(context.Context, int, string) (string, error) { cancel(); return "%pane", nil }
	_, err = r.ReadAntigravityProcess(ctx, 42)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel after resolver = %v", err)
	}
	reads := 0
	r.resolve = nil
	r.read = func(context.Context, int) (nativeProcessSample, error) {
		reads++
		if reads == 2 {
			return nativeProcessSample{}, nativeIdentityError("antigravity_native_process_missing", nil)
		}
		return sample, nil
	}
	_, err = r.ReadAntigravityProcess(context.Background(), 42)
	requireNativeCode(t, err, "antigravity_native_process_missing")
}

func checkNativeOwnProcess(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := NewNativeProcessReader(nil).ReadAntigravityProcessWithWorkspace(ctx, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Process.PID != os.Getpid() || got.Process.ParentPID != os.Getppid() || got.Process.Executable != exe || got.Process.TerminalID != "" {
		t.Fatalf("own process facts disagree: %+v", got.Process)
	}
	birth, err := time.Parse(time.RFC3339Nano, got.Process.Birth)
	if err != nil || birth.After(time.Now()) {
		t.Fatalf("birth %q: %v", got.Process.Birth, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil || got.WorkspaceCWD != cwd {
		t.Fatalf("kernel CWD differs, known=%t error=%v", got.WorkspaceCWD != "", err)
	}
}

func TestNativeProcessPortRetainsBootAndExecGeneration(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("unsupported image stamp")
	}
	sample := nativeSampleFixture(t)
	r := NewNativeProcessReader(nil)
	r.read = func(context.Context, int) (nativeProcessSample, error) { return sample, nil }
	p, err := r.ReadAntigravityProcess(context.Background(), 42)
	if err != nil || p.BootID != sample.BootID || p.KernelGeneration != sample.generation {
		t.Fatalf("lost durable process identity: %+v %v", p, err)
	}
}
