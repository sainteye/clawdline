// Package projects reads what the Projects page shows: the places an assistant
// has been run in, the Swift app's Board catalog of Projects, and one Project's
// worktree lifecycle.
//
// Every rule here is the Swift app's, restated. Each function names the one it
// follows (StartPoints.swift, ProjectBoardIntegration.swift,
// ProjectWorktreeLifecycle.swift, UsageLedger.swift); where this port cannot
// follow it, the comment says what differs and why.
//
// Nothing in this package writes. The lifecycle observation runs git with
// optional locks off and never fetches, the Board catalog is read as a file and
// never locked, and nothing under ~/.config/clawdline is opened here — task
// records arrive through a port, from internal/adapters/swiftstore, which is
// the one reader of that store.
package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// PlaceID is StartPoints.id(for:): sixteen hex characters of the path's digest.
func PlaceID(path string) string { return sha(path)[:16] }

// ProjectID is ProjectBoardIntegration.projectID: the canonical repository
// path's digest, twelve bytes of it.
func ProjectID(canonical string) string { return "project-" + sha(canonical)[:24] }

// RepositoryID is ProjectWorktreeLifecycleService.repositoryID.
func RepositoryID(canonical string) string { return "repository-" + sha(canonical)[:24] }

// IsProjectID is ProjectWorktreeLifecycleService.isProjectID.
func IsProjectID(value string) bool {
	digest, ok := strings.CutPrefix(value, "project-")
	return ok && len(digest) == 24 && lowerHex(digest)
}

func lowerHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// digest is ProjectWorktreeLifecycleService.digest: parts joined by U+001F.
func digest(parts ...string) string { return sha(strings.Join(parts, "\x1f")) }

// WorktreeID is ProjectWorktreeLifecycleService.worktreeID.
func WorktreeID(commonDirectory, path string) string {
	return "wt-" + digest(comparablePath(commonDirectory), comparablePath(path))[:24]
}

// IsWorktreeID is ProjectWorktreeHTTP.isWorktreeID.
func IsWorktreeID(value string) bool {
	digest, ok := strings.CutPrefix(value, "wt-")
	return ok && len(digest) == 24 && lowerHex(digest)
}

// standardized is URL.standardizedFileURL.path: lexical, no symlinks.
func standardized(path string) string {
	if path == "" {
		return path
	}
	return filepath.Clean(path)
}

// canonicalFilesystemPath is OrchestratorDraft.canonicalFilesystemPath:
// standardise, then follow symlinks once.
func canonicalFilesystemPath(path string) string {
	clean := standardized(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}

// comparablePath is ProjectWorktreeLifecycleService.comparablePath.
func comparablePath(path string) string {
	canonical := canonicalFilesystemPath(path)
	for _, prefix := range []string{"/private/var/", "/private/tmp/", "/private/etc/"} {
		if strings.HasPrefix(canonical, prefix) {
			canonical = strings.TrimPrefix(canonical, "/private")
			break
		}
	}
	return canonical
}

// CanonicalProjectKey is UsageLedger.canonicalProjectKey(projectDir:) without
// the repositoryCommonDir argument, which only task records carry: the nearest
// `.git` marker names the repository, and a linked worktree's marker is
// followed to its common directory.
func CanonicalProjectKey(projectDir string) (string, bool) {
	raw := strings.TrimSpace(projectDir)
	if !strings.HasPrefix(raw, "/") {
		return "", false
	}
	cursor := standardized(raw)
	if st, err := os.Stat(cursor); err == nil && !st.IsDir() {
		cursor = filepath.Dir(cursor)
	}
	for cursor != "/" {
		marker := filepath.Join(cursor, ".git")
		if st, err := os.Stat(marker); err == nil {
			if st.IsDir() {
				return cursor, true
			}
			if data, err := os.ReadFile(marker); err == nil {
				line, _, _ := strings.Cut(string(data), "\n")
				line = strings.TrimSuffix(line, "\r")
				if rawGit, ok := strings.CutPrefix(line, "gitdir:"); ok {
					rawGit = strings.TrimSpace(rawGit)
					gitDir := rawGit
					if !strings.HasPrefix(rawGit, "/") {
						gitDir = filepath.Join(cursor, rawGit)
					}
					gitDir = standardized(gitDir)
					if rel, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
						if relative := strings.TrimSpace(string(rel)); relative != "" {
							common := relative
							if !strings.HasPrefix(relative, "/") {
								common = filepath.Join(gitDir, relative)
							}
							common = standardized(common)
							if filepath.Base(common) == ".git" {
								return filepath.Dir(common), true
							}
						}
					}
				}
			}
			return cursor, true
		}
		cursor = filepath.Dir(cursor)
	}
	return standardized(raw), true
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// ManagedWorktreeRoot is OrchestratorDraft.worktreeRoot.
func ManagedWorktreeRoot() string {
	return filepath.Join(home(), "Library", "Application Support", "Clawdline", "worktrees")
}

// isTaskID is OrchestratorDraft.isTaskID.
func isTaskID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'f') && !(c >= '0' && c <= '9') && c != '-' {
			return false
		}
	}
	return true
}

// worktreeBranch is OrchestratorDraft.worktreeBranch(for:).
func worktreeBranch(taskID string) string {
	if !isTaskID(taskID) {
		return ""
	}
	return "clawdline/task/" + taskID
}

// relativePath is OrchestratorDraft.relativePath(from:to:): the path below
// root, both canonicalised, or false when it is not below it.
func relativePath(root, path string) (string, bool) {
	root, path = canonicalFilesystemPath(root), canonicalFilesystemPath(path)
	if path == root {
		return "", true
	}
	if root == "/" {
		return strings.TrimPrefix(path, "/"), strings.HasPrefix(path, "/")
	}
	rest, ok := strings.CutPrefix(path, root+"/")
	return rest, ok
}
