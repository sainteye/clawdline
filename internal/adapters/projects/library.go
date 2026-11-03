package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// NeedsExplicitChoice is a lexical check: asking the filesystem to resolve a
// path here could itself request access. Provider history, icon rows and a
// process cwd are observations, not consent to inspect personal data.
func NeedsExplicitChoice(path string) bool {
	return needsExplicitChoice(path, home())
}

func needsExplicitChoice(path, home string) bool {
	if home == "" || !filepath.IsAbs(path) {
		return true
	}
	path, home = filepath.Clean(path), filepath.Clean(home)
	if runtime.GOOS == "darwin" {
		// The usual macOS volume is case insensitive. A differently cased
		// spelling must not turn the same personal folder into a safe path.
		path, home = strings.ToLower(path), strings.ToLower(home)
		if path == "/volumes" || strings.HasPrefix(path, "/volumes/") {
			return true
		}
	}
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Library"} {
		root := filepath.Join(home, name)
		if runtime.GOOS == "darwin" {
			root = strings.ToLower(root)
		}
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// MayInspectInBackground rejects personal roots before any filesystem call.
// On macOS it also refuses symlink components without following them. An
// automatically found alias must be registered explicitly before inspection;
// otherwise its target could be another app's data or a personal library.
// This is prompt avoidance, not a sandbox against concurrent filesystem edits.
func MayInspectInBackground(path string) bool {
	if NeedsExplicitChoice(path) {
		return false
	}
	if runtime.GOOS != "darwin" {
		return true
	}
	return backgroundPath(path, home(), os.Lstat)
}

func backgroundPath(path, home string, lstat func(string) (os.FileInfo, error)) bool {
	// These OS aliases are outside the person's home. Spell them physically
	// rather than following arbitrary symlinks while checking path components.
	physical := func(path string) string {
		path = filepath.Clean(path)
		if runtime.GOOS != "darwin" {
			return path
		}
		for _, name := range []string{"var", "tmp", "etc"} {
			root := string(filepath.Separator) + name
			if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
				return filepath.Join(string(filepath.Separator), "private", strings.TrimPrefix(path, string(filepath.Separator)))
			}
		}
		return path
	}
	path, home = physical(path), physical(home)
	if needsExplicitChoice(path, home) {
		return false
	}
	volume := filepath.VolumeName(path)
	cursor := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, cursor), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cursor = filepath.Join(cursor, part)
		info, err := lstat(cursor)
		if os.IsNotExist(err) {
			return true
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// InUserLibrary is whether path is under the person's ~/Library.
//
// Cloud-storage folders live there — Dropbox, Google Drive and OneDrive under
// ~/Library/CloudStorage, iCloud Drive under ~/Library/Mobile Documents — and
// macOS asks the person before a process reads one. A Clawdline process that
// stats or reads such a folder on its own makes that dialog name Clawdline:
// on 2026-10-15 a reinstall of the app asked for Dropbox access because a
// folder in Dropbox, where Claude Code had once been run, was listed as a
// Project and read in the background. So nothing reads there unless the
// person chose it: a folder they registered, or one a running session is in.
func InUserLibrary(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	library := filepath.Join(home, "Library") + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), library)
}
