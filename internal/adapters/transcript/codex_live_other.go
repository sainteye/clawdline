//go:build !darwin

package transcript

// iTerm2 exists only on Darwin, so another platform has no terminal title to
// validate against this provider-specific lock. False keeps the source
// explicitly unread rather than treating it as an empty active-thread set.
func heldCodexThreadIDs(string) ([]string, bool) { return nil, false }
