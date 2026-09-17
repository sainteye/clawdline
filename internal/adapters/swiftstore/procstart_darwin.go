//go:build darwin

package swiftstore

import (
	"time"

	"golang.org/x/sys/unix"
)

// ProcessStart is when a process started, to the second, as the kernel keeps
// it. The Swift app records the same fact (ITerm.processStart(ofPID:)) and
// compares within five seconds, so whole seconds are enough on both sides.
//
// A process that is gone, or one this user may not inspect, has no start time:
// the zero time, which matches no recorded identity.
func ProcessStart(pid int) time.Time {
	if pid <= 0 {
		return time.Time{}
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil || info.Proc.P_pid != int32(pid) {
		return time.Time{}
	}
	tv := info.Proc.P_starttime
	if tv.Sec <= 0 {
		return time.Time{}
	}
	return time.Unix(tv.Sec, 0)
}
