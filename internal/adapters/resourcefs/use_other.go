//go:build !darwin && !linux

package resourcefs

import "context"

// A missing native reader is not proof that no process uses the directory.
func UsePresence(context.Context, string) string { return "unknown" }
