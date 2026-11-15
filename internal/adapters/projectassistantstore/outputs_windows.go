package projectassistantstore

import "os"

func openOutput(root *os.Root, path string) (*os.File, error) { return root.Open(path) }

// Windows FlushFileBuffers covers the snapshot file; directory handles do not
// support the Unix fsync operation.
func syncOutputDirectory(string) error { return nil }
