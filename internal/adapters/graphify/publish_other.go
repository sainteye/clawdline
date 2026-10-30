//go:build !darwin && !linux && !windows

package graphify

import "os"

func publishDirectory(root *os.Root, from, to string) error {
	return BuildError("platform_unavailable")
}
