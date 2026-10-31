//go:build darwin

package terminal

import (
	"bytes"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func nativeTerminalExecutable(pid int) (string, error) {
	// PROC_PIDPATHINFO=11 and its ABI maximum (4 * MAXPATHLEN).
	// XNU returns zero on success; the terminated path is the result.
	var path [4 * 1024]byte
	_, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 2, uintptr(pid), 11, 0, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)))
	runtime.KeepAlive(path)
	end := bytes.IndexByte(path[:], 0)
	if errno != 0 {
		return "", errno
	}
	if end <= 0 {
		return "", unix.EIO
	}
	return string(path[:end]), nil
}
