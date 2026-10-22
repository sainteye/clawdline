//go:build darwin

package antigravity

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestNativeDarwinOwnProcess(t *testing.T) {
	infoCheck, infoErr := darwinProcessInfo(os.Getpid())
	var raw [4096]byte
	n, pathErr := darwinProcInfo(os.Getpid(), 11, raw[:])
	_, genErr := darwinGeneration(os.Getpid())
	t.Logf("kernel info available=%t error=%v; proc_info path count=%d error=%v; generation error=%v", infoCheck != nil, infoErr, n, pathErr, genErr)
	checkNativeOwnProcess(t)
	got, err := NewNativeProcessReader(nil).ReadAntigravityProcess(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Unix(info.Proc.P_starttime.Sec, int64(info.Proc.P_starttime.Usec)*1000).UTC().Format(time.RFC3339Nano)
	if got.Birth != want {
		t.Fatalf("birth %q != microsecond kernel birth %q", got.Birth, want)
	}
	// These assertions validate the SDK ABI layout consumed by proc_info.
	if unsafe.Sizeof(darwinVnodePaths{}) != 2352 || unsafe.Offsetof(darwinVnodePath{}.Path) != 152 {
		t.Fatal("Darwin vnode layout differs")
	}
}

func TestNativeDarwinSubprocessExit(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	r := NewNativeProcessReader(nil)
	got, err := r.ReadAntigravityProcess(context.Background(), cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentPID != os.Getpid() || got.Executable != "/bin/sleep" {
		t.Fatalf("subprocess facts disagree: %+v", got)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	_, err = r.ReadAntigravityProcess(context.Background(), cmd.Process.Pid)
	requireNativeCode(t, err, "antigravity_native_process_missing")
}

func TestNativeDarwinControllingTTY(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	for _, request := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if err := unix.IoctlSetInt(int(master.Fd()), request, 0); err != nil {
			t.Fatal(err)
		}
	}
	// TIOCPTYGNAME's _IOR ABI returns char[128] (SDK sys/ttycom.h).
	var name [128]byte
	_, _, errno := syscall.Syscall(unix.SYS_IOCTL, master.Fd(), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0])))
	if errno != 0 {
		t.Fatal(errno)
	}
	end := bytes.IndexByte(name[:], 0)
	if end <= 0 {
		t.Fatal("PTY path absent")
	}
	tty := string(name[:end])
	slave, err := os.OpenFile(tty, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	cmd := exec.Command("/bin/sleep", "10")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	r := NewNativeProcessReader(func(_ context.Context, pid int, kernelTTY string) (string, error) {
		if pid != cmd.Process.Pid || kernelTTY != tty {
			t.Fatal("resolver received wrong PID/TTY")
		}
		return "%synthetic-verified", nil
	})
	got, err := r.ReadAntigravityProcess(context.Background(), cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if got.TTY != tty || got.TerminalID != "%synthetic-verified" {
		t.Fatal("controlling TTY or mapping differs")
	}
}
