//go:build !unix

package codegraph

import "os"

func openProbeFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
