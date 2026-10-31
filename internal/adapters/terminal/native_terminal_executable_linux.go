//go:build linux

package terminal

import (
	"os"
	"strconv"
)

func nativeTerminalExecutable(pid int) (string, error) {
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
}
