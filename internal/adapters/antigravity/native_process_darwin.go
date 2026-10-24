//go:build darwin

package antigravity

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/sainteye/clawdline/internal/app/ports"
	"golang.org/x/sys/unix"
)

// These are Darwin ABI layouts, not configurable capacity limits. Reviewed
// against SDK sys/proc_info.h (proc_vnodepathinfo/vnode_info_path/vinfo_stat),
// sys/param.h and XNU bsd/kern/proc_info.c. This identity sampler reads no
// argv/environment; the separate execution-plan reader owns its bounded ABI.
type darwinVnodePath struct {
	Info [152]byte
	Path [1024]byte
}
type darwinVnodePaths struct{ CWD, Root darwinVnodePath }

// MaxNativeTTYEntries bounds the direct /dev directory scan. No recursion or
// process-name-derived path is used. Overflow refuses identity.
const MaxNativeTTYEntries = 4096

// XNU bsd/sys/proc_info_private.h declares this 56-byte structure as API;
// PROC_PIDUNIQIDENTIFIERINFO=17 returns executable UUID/unique ID/pidversion.
// A denied or unsupported generation query fails closed.
func darwinGeneration(pid int) (string, error) {
	var data [56]byte
	n, err := darwinProcInfo(pid, 17, data[:])
	if err != nil || n != len(data) {
		return "", nativeIdentityError("antigravity_native_generation_unreadable", nil)
	}
	return hex.EncodeToString(data[:40]), nil // reserved tail is not identity.
}

// proc_info(callnum=2, pid, flavor, arg, buffer, buffersize) is the kernel
// interface underneath proc_pidinfo/proc_pidpath. SYS_PROC_INFO comes from
// x/sys; syscall.Syscall6 performs the Go scheduler entersyscall/exitsyscall.
// Unlike linking private runtime trampolines, it needs neither cgo nor assembly.
func darwinProcInfo(pid, flavor int, buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, unix.EINVAL
	}
	n, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 2, uintptr(pid), uintptr(flavor), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return 0, errno
	}
	if n > uintptr(len(buffer)) {
		return 0, unix.EINVAL
	}
	return int(n), nil
}

func darwinProcessInfo(pid int) (*unix.KinfoProc, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	// x/sys returns EIO when sysctl reports zero bytes for an absent PID.
	// Distinguish that from an unreadable/malformed kernel structure.
	if errors.Is(err, unix.EIO) {
		if raw, readErr := unix.SysctlRaw("kern.proc.pid", pid); readErr == nil && len(raw) == 0 {
			return nil, nativeIdentityError("antigravity_native_process_missing", nil)
		}
	}
	if errors.Is(err, unix.ESRCH) || err == nil && (p == nil || p.Proc.P_pid != int32(pid) || p.Proc.P_stat == 5) {
		return nil, nativeIdentityError("antigravity_native_process_missing", nil)
	}
	if err != nil {
		return nil, nativeProcessReadError(err)
	}
	if p.Proc.P_starttime.Sec <= 0 || p.Proc.P_starttime.Usec < 0 || p.Proc.P_starttime.Usec >= 1000000 {
		return nil, nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	return p, nil
}

func darwinKernelEqual(a, b *unix.KinfoProc) bool {
	return a.Proc.P_pid == b.Proc.P_pid && a.Eproc.Ppid == b.Eproc.Ppid &&
		a.Proc.P_starttime == b.Proc.P_starttime && a.Eproc.Tdev == b.Eproc.Tdev
}

func darwinTTY(ctx context.Context, dev int32) (string, error) {
	if dev == -1 {
		return "", nil // NODEV: no controlling terminal, not fd 0's target.
	}
	dir, err := os.Open("/dev")
	if err != nil {
		return "", nativeIdentityError("antigravity_native_tty_unreadable", nil)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(MaxNativeTTYEntries + 1)
	if err != nil && err != io.EOF {
		return "", nativeIdentityError("antigravity_native_tty_unreadable", nil)
	}
	if len(entries) > MaxNativeTTYEntries {
		return "", nativeIdentityError("antigravity_native_tty_entries_full", nil)
	}
	found := ""
	for _, entry := range entries {
		if err := nativeContext(ctx); err != nil {
			return "", err
		}
		if entry.Type()&os.ModeCharDevice == 0 {
			continue
		}
		path := filepath.Join("/dev", entry.Name())
		var st unix.Stat_t
		if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFCHR || st.Rdev != dev {
			continue
		}
		if found != "" {
			return "", nativeIdentityError("antigravity_native_tty_ambiguous", nil)
		}
		found = path
	}
	if found == "" {
		return "", nativeIdentityError("antigravity_native_tty_unreadable", nil)
	}
	return found, nil
}

func readNativeProcessSample(ctx context.Context, pid int) (nativeProcessSample, error) {
	if err := nativeContext(ctx); err != nil {
		return nativeProcessSample{}, err
	}
	before, err := darwinProcessInfo(pid)
	if err != nil {
		return nativeProcessSample{}, err
	}
	generation, err := darwinGeneration(pid)
	if err != nil {
		return nativeProcessSample{}, err
	}
	// PROC_PIDPATHINFO_MAXSIZE = 4 * MAXPATHLEN, flavor PROC_PIDPATHINFO=11.
	var path [4 * 1024]byte
	// XNU proc_pidpathinfo leaves retval at zero; libc's proc_pidpath wrapper
	// computes strlen itself. Only syscall status and a terminated path count.
	_, err = darwinProcInfo(pid, 11, path[:])
	end := bytes.IndexByte(path[:], 0)
	if err != nil || end <= 0 || !filepath.IsAbs(string(path[:end])) {
		return nativeProcessSample{}, nativeProcessReadError(err)
	}
	executable := string(path[:end])
	image, err := os.Stat(executable)
	if err != nil || !image.Mode().IsRegular() {
		return nativeProcessSample{}, nativeProcessReadError(err)
	}
	tty, err := darwinTTY(ctx, before.Eproc.Tdev)
	if err != nil {
		return nativeProcessSample{}, err
	}
	// PROC_PIDVNODEPATHINFO=9. A denied/unsupported CWD is optional unknown.
	var paths darwinVnodePaths
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&paths)), int(unsafe.Sizeof(paths)))
	workspace := ""
	if count, e := darwinProcInfo(pid, 9, buf); e == nil && count == len(buf) {
		if end := bytes.IndexByte(paths.CWD.Path[:], 0); end > 0 {
			candidate := string(paths.CWD.Path[:end])
			if filepath.IsAbs(candidate) {
				workspace = candidate
			}
		}
	}
	after, err := darwinProcessInfo(pid)
	if err != nil {
		return nativeProcessSample{}, err
	}
	afterGeneration, err := darwinGeneration(pid)
	if err != nil {
		return nativeProcessSample{}, err
	}
	if !darwinKernelEqual(before, after) || generation != afterGeneration {
		return nativeProcessSample{}, nativeIdentityError("antigravity_native_process_changed", nil)
	}
	if err := nativeContext(ctx); err != nil {
		return nativeProcessSample{}, err
	}
	birth := time.Unix(after.Proc.P_starttime.Sec, int64(after.Proc.P_starttime.Usec)*1000).UTC().Format(time.RFC3339Nano)
	return nativeProcessSample{NativeProcessObservation: NativeProcessObservation{
		Process:      ports.AntigravityProcess{PID: pid, ParentPID: int(after.Eproc.Ppid), Birth: birth, Executable: executable, TTY: tty},
		WorkspaceCWD: workspace,
	}, generation: generation, image: image}, nil
}
