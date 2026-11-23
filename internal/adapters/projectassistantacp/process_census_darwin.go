//go:build darwin

package projectassistantacp

import (
	"context"
	"io"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Darwin sys/sysctl.h and sys/proc.h ABI values absent from x/sys.
const (
	kernProc    = 14
	kernProcAll = 0
	procZombie  = 5
	procExiting = 0x00002000 // P_WEXIT
)

// processIdentities reads the kernel snapshot without starting a child process
// under the close deadline. Use the existing census byte budget before any
// allocation; an incomplete or changing snapshot cannot prove cleanup.
func processIdentities(ctx context.Context, budget int) ([]processIdentity, error) {
	var size uintptr
	if err := readDarwinProcesses(nil, &size); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size > uintptr(budget) {
		return nil, io.ErrShortBuffer
	}
	if size == 0 || size%unix.SizeofKinfoProc != 0 {
		return nil, syscall.EIO
	}
	procs := make([]unix.KinfoProc, size/unix.SizeofKinfoProc)
	if err := readDarwinProcesses(&procs[0], &size); err != nil {
		return nil, err
	}
	if size == 0 || size%unix.SizeofKinfoProc != 0 || size/unix.SizeofKinfoProc > uintptr(len(procs)) {
		return nil, syscall.EIO
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows := make([]processIdentity, 0, size/unix.SizeofKinfoProc)
	for _, p := range procs[:size/unix.SizeofKinfoProc] {
		pid := int(p.Proc.P_pid)
		if pid <= 0 {
			continue
		}
		state := ""
		if p.Proc.P_stat == procZombie {
			state = "Z"
		} else if p.Proc.P_flag&procExiting != 0 {
			state = "?E"
		}
		birth := p.Proc.P_starttime
		if birth.Sec <= 0 || birth.Usec < 0 || birth.Usec >= 1_000_000 {
			return nil, syscall.EIO
		}
		rows = append(rows, processIdentity{
			pid: pid, ppid: int(p.Eproc.Ppid), pgid: int(p.Eproc.Pgid), state: state,
			stamp: time.Unix(birth.Sec, int64(birth.Usec)*1000).UTC().Format(time.RFC3339Nano),
		})
	}
	return rows, nil
}

func readDarwinProcesses(out *unix.KinfoProc, size *uintptr) error {
	mib := [3]int32{unix.CTL_KERN, kernProc, kernProcAll}
	_, _, errno := syscall.Syscall6(unix.SYS_SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), uintptr(unsafe.Pointer(out)), uintptr(unsafe.Pointer(size)), 0, 0)
	runtime.KeepAlive(out)
	if errno != 0 {
		return errno
	}
	return nil
}
