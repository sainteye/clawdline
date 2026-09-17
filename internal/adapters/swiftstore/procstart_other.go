//go:build !darwin

package swiftstore

import "time"

// ProcessStart has no reading off macOS, where the Swift app does not run
// either. The zero time matches no recorded identity.
func ProcessStart(pid int) time.Time { return time.Time{} }
