package projects

import (
	"os"
	"path/filepath"
	"strings"
)

// MachinePlaceID is a closed start target, never listed as a Project. It
// reuses the receipted start route so local and Cloud starts have one path.
const MachinePlaceID = "@machine"

// MachineWorkspace is the daemon-owned working directory for a machine role.
// It is a real cwd for an assistant, but never a Project or a start place.
func MachineWorkspace(stateDir string) string {
	return filepath.Join(stateDir, "machine-workspace")
}

// IsMachineWorkspace includes descendants and symlink aliases of that cwd.
// A transcript or a live session below it must not create a Project either.
func IsMachineWorkspace(stateDir, path string) bool {
	if stateDir == "" || path == "" {
		return false
	}
	root := canonicalFilesystemPath(MachineWorkspace(stateDir))
	if filepath.Clean(path) == root {
		return true
	}
	if !MayInspectInBackground(path) {
		// Preserve a direct alias of our own workspace without following an
		// arbitrary foreign cwd. Readlink reads the link, not its target; its
		// parent and target must independently pass the no-follow policy.
		if NeedsExplicitChoice(path) || !MayInspectInBackground(filepath.Dir(path)) {
			return false
		}
		target, err := os.Readlink(path)
		if err != nil {
			return false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		if !MayInspectInBackground(target) {
			return false
		}
		path = target
	}
	path = canonicalFilesystemPath(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
