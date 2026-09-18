//go:build !darwin && !linux && !windows

package store

import "errors"

// diskFree has no reading on this platform yet. The row says so; it does not
// say zero.
func diskFree(string) (int64, error) {
	return 0, errors.New("not measured on this platform")
}
