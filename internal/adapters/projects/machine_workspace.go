package projects

import (
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
	path = canonicalFilesystemPath(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
