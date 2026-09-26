//go:build !darwin && !linux

package bootid

import "context"

// read has no source here: Windows keeps no boot id this daemon reads, and a
// boot time rounded from an uptime would be a guess that drifts.
func read(context.Context) (string, error) { return "", ErrUnsupported }
