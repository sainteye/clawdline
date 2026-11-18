//go:build !unix

package main

// Directory fsync is a Unix facility. The database itself is synced before
// publication on every platform, as in the taskdir adapter's publication path.
func syncBotArchiveDirectory(string) error { return nil }
