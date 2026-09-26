//go:build !linux && !darwin

package main

// lowerPriority has nothing it can change portably here; the slot and the
// memory wait are the whole of the command on this platform.
func lowerPriority() {}
