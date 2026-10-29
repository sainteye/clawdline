//go:build !unix

package graphify

import "os"

func openProbeFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
