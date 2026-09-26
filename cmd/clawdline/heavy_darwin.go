//go:build darwin

package main

import "syscall"

// lowerPriority makes the command ten steps nicer on the CPU, inherited by
// everything it starts. A Mac has no out-of-memory score a process may raise
// for itself; under pressure it compresses and swaps, which the memory wait in
// front of the command is for.
func lowerPriority() {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
}
