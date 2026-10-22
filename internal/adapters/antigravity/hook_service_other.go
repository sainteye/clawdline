//go:build !darwin && !linux

package antigravity

import "os"

func hookPlatform() error                     { return refuse("antigravity_hook_transport_unsupported") }
func hookOwned(os.FileInfo) bool              { return false }
func privateHookDirectory(string, bool) error { return hookPlatform() }
func validateHookSocket(string) error         { return hookPlatform() }
