//go:build linux

package main

import (
	"os"
	"syscall"
)

// lowerPriority is taken by the thread that starts the command just before it
// does, so the command and everything it starts inherit it: ten steps nicer on
// the CPU (a Linux nice value is the calling thread's, which is why the caller
// holds its thread),
// and an out-of-memory score that makes the kernel take a build before the
// sessions a person is typing into. Raising one's own score needs no
// privilege; each failure leaves the command running as it would have.
func lowerPriority() {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
	_ = os.WriteFile("/proc/self/oom_score_adj", []byte("500"), 0)
}
