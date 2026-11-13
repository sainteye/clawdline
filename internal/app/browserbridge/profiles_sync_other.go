//go:build !darwin && !linux

package browserbridge

func syncProfilesDir(string) error { return nil }
