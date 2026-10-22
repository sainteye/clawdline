//go:build darwin || linux

package antigravity

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func hookPlatform() error { return nil }

func hookOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func privateHookDirectory(path string, create bool) error {
	if !filepath.IsAbs(path) || path != filepath.Clean(path) {
		return refuse("antigravity_hook_directory_invalid")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() {
			return refuse("antigravity_hook_directory_invalid")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
			return refuse("antigravity_hook_directory_invalid")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	if create {
		// Validate the exact parent before creating anything through it.
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil || parent != filepath.Dir(path) {
			return refuse("antigravity_hook_directory_invalid")
		}
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return refuse("antigravity_hook_directory_invalid")
		}
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return refuse("antigravity_hook_directory_invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !hookOwned(info) {
		return refuse("antigravity_hook_directory_invalid")
	}

	return nil
}

func validateHookSocket(dir string) error {
	if err := privateHookDirectory(dir, false); err != nil {
		return err
	}
	info, err := os.Lstat(HookSocketPath(dir))
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !hookOwned(info) {
		return refuse("antigravity_hook_socket_invalid")
	}
	return nil
}
