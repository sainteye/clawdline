//go:build !darwin && !linux && !windows

package gocache

import "errors"

// DiskFree has no reading on this platform yet. The row says so; it does not
// say zero.
func DiskFree(string) (int64, error) {
	return 0, errors.New("not measured on this platform")
}
