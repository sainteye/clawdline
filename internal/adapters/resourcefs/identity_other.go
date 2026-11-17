//go:build !darwin && !linux && !windows

package resourcefs

import "os"

func privateDirectory(os.FileInfo) bool                { return false }
func privateRegular(os.FileInfo) bool                  { return false }
func fileIdentity(string, os.FileInfo) (string, error) { return "", ErrUnsafe }
func allocatedBytes(os.FileInfo) (int64, bool)         { return 0, false }
func sameFilesystem(os.FileInfo, os.FileInfo) bool     { return false }
