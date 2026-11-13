//go:build darwin || linux

package browserbridge

import "os"

func syncProfilesDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
