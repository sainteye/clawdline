//go:build !unix

package taskdir

func syncDir(string) error { return nil }
