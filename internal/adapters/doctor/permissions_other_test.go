//go:build !darwin && !linux

package doctor

import (
	"os"
	"testing"
)

func assertHistoryPermissions(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
