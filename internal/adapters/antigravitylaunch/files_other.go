//go:build !darwin && !linux

package antigravitylaunch

import (
	"errors"
	"os"
)

var errMissing = errors.New("private_file_missing")

type privateDir struct{ path string }

func (*privateDir) checkLock(*os.File) error { return fail("platform_unsupported") }

func openPrivate(string, bool) (*privateDir, error)         { return nil, fail("platform_unsupported") }
func (*privateDir) close()                                  {}
func (*privateDir) current() error                          { return fail("platform_unsupported") }
func (*privateDir) child(string, bool) (*privateDir, error) { return nil, fail("platform_unsupported") }
func (*privateDir) read(string, int) ([]byte, error)        { return nil, fail("platform_unsupported") }
func (*privateDir) replace(string, []byte) error            { return fail("platform_unsupported") }
func (*privateDir) exclusive(string, []byte) error          { return fail("platform_unsupported") }
func (*privateDir) lock() (*os.File, error)                 { return nil, fail("platform_unsupported") }
func (*privateDir) removePending(string) error              { return fail("platform_unsupported") }
