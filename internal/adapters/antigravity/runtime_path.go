package antigravity

import (
	"errors"
	"os"
	"path/filepath"
)

// ProviderStateDir validates a read path without creating provider state.
func ProviderStateDir(configRoot string) (string, error) {
	if !safePath(configRoot) || filepath.Clean(configRoot) != configRoot {
		return "", refuse("antigravity_hook_directory_invalid")
	}
	canonical, err := filepath.EvalSymlinks(configRoot)
	if err != nil || canonical != configRoot {
		return "", refuse("antigravity_hook_directory_invalid")
	}
	// Reuse the listener's ownership and unsafe-ancestor checks. The root
	// itself is private configuration, not a public installation directory.
	if err = privateHookDirectory(configRoot, false); err != nil {
		return "", err
	}
	path := filepath.Join(configRoot, "antigravity")
	if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", refuse("antigravity_hook_directory_invalid")
	}
	if err = privateHookDirectory(path, false); err != nil {
		return "", err
	}
	return path, nil
}
