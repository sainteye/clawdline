//go:build unix

package cloudkeys

import (
	"errors"
	"syscall"
)

// noFollow makes an open fail rather than follow a symlink at the last
// component, so a link put in place after the Lstat is refused too.
const noFollow = syscall.O_NOFOLLOW

// isLinkRefusal is the error an O_NOFOLLOW open gives for a symlink: ELOOP on
// macOS and Linux, EMLINK on FreeBSD.
func isLinkRefusal(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK)
}
