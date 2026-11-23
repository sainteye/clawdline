//go:build darwin

package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/sainteye/clawdline/internal/domain/capacity"
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
	return darwinProcessIdentities(ctx, budget, readDarwinProcesses)
}

// darwinProcessReader is one sysctl(KERN_PROC_ALL) call; tests inject faults.
type darwinProcessReader func(out *unix.KinfoProc, size *uintptr) error

func darwinProcessIdentities(ctx context.Context, budget int, read darwinProcessReader) ([]processIdentity, error) {
	procs, err := darwinProcessSnapshot(ctx, budget, read)
	if err != nil {
		return nil, err
	}
	rows := make([]processIdentity, 0, len(procs))
	for _, p := range procs {
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

// darwinProcessSnapshot copies the kernel process table. The size query's
// estimate leaves room for only five processes started before the copy; when
// more start, the copy answers ENOMEM and is incomplete. Each attempt asks for
// a fresh estimate plus headroom, within the census budget, and a bounded
// number of attempts never turns a persistently incomplete copy into proof.
func darwinProcessSnapshot(ctx context.Context, budget int, read darwinProcessReader) ([]unix.KinfoProc, error) {
	var err error
	for attempt := 0; attempt < capacity.MaximumBotProcessCensusAttempts; attempt++ {
		var procs []unix.KinfoProc
		procs, err = darwinProcessCopy(ctx, budget, read)
		if !errors.Is(err, syscall.ENOMEM) {
			return procs, err
		}
	}
	return nil, err
}

func darwinProcessCopy(ctx context.Context, budget int, read darwinProcessReader) ([]unix.KinfoProc, error) {
	var size uintptr
	if err := read(nil, &size); err != nil {
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
	size = min(size+size/4, uintptr(budget)/unix.SizeofKinfoProc*unix.SizeofKinfoProc)
	procs := make([]unix.KinfoProc, size/unix.SizeofKinfoProc)
	if err := read(&procs[0], &size); err != nil {
		return nil, err
	}
	if size == 0 || size%unix.SizeofKinfoProc != 0 || size/unix.SizeofKinfoProc > uintptr(len(procs)) {
		return nil, syscall.EIO
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return procs[:size/unix.SizeofKinfoProc], nil
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
