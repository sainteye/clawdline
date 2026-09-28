package orchestrator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type GateScratch struct {
	Build string
	Cache string
	Temp  string
}

func (b *Broker) gateScratch(taskID string) GateScratch {
	root := filepath.Join(b.Tasks.Path(taskID), "work")
	return GateScratch{
		Build: filepath.Join(root, "build"),
		Cache: filepath.Join(root, "cache"),
		Temp:  filepath.Join(root, "tmp"),
	}
}

func (b *Broker) prepareGateReadonly(r Record) error {
	if r.Worktree == nil || !r.Worktree.Detached {
		return fmt.Errorf("the checker checkout is not detached")
	}
	scratch := b.gateScratch(r.ID)
	for _, path := range []string{scratch.Build, scratch.Cache, scratch.Temp} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	if err := removeTreeWrite(r.Worktree.Path, nil); err != nil {
		return fmt.Errorf("candidate checkout: %w", err)
	}
	scratchPaths := map[string]bool{
		filepath.Clean(scratch.Build): true,
		filepath.Clean(scratch.Cache): true,
		filepath.Clean(scratch.Temp):  true,
	}
	if err := removeTreeWrite(r.Dir, scratchPaths); err != nil {
		return fmt.Errorf("task root: %w", err)
	}
	return nil
}

// removeTreeWrite removes write bits without changing execute bits on tracked
// programs. Symlinks are skipped so a checkout can never chmod a target
// outside itself. Paths explicitly in writable keep 0700; their descendants
// are not walked by the read-only pass.
func removeTreeWrite(root string, writable map[string]bool) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		clean := filepath.Clean(path)
		if writable[clean] {
			if err := os.Chmod(path, 0o700); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() &^ 0o222
		if entry.IsDir() {
			mode |= 0o500
		} else {
			mode |= 0o400
		}
		return os.Chmod(path, mode)
	})
}

func makeTreeOwnerWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()|0o200)
	})
}
