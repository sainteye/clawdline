package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// WorkspaceMetadataBytesLimit bounds each Git membership metadata file. This
// verifier runs no Git command, hook, repository config or external service.
const WorkspaceMetadataBytesLimit = 4 << 10

type WorkspaceError string

func (e WorkspaceError) Error() string { return string(e) }

const (
	WorkspaceUnverified         WorkspaceError = "project_workspace_unverified"
	WorkspaceMetadataUnreadable WorkspaceError = "project_workspace_metadata_unreadable"
	WorkspaceMetadataTooLarge   WorkspaceError = "project_workspace_metadata_too_large"
)

func workspaceMetadata(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", WorkspaceMetadataUnreadable
	}
	if before.Size() > WorkspaceMetadataBytesLimit {
		return "", WorkspaceMetadataTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return "", WorkspaceMetadataUnreadable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", WorkspaceMetadataUnreadable
	}
	data, err := io.ReadAll(io.LimitReader(f, WorkspaceMetadataBytesLimit+1))
	if err != nil {
		return "", WorkspaceMetadataUnreadable
	}
	if len(data) > WorkspaceMetadataBytesLimit {
		return "", WorkspaceMetadataTooLarge
	}
	after, err := os.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", WorkspaceMetadataUnreadable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	text := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if text == "" || !utf8.ValidString(text) || strings.ContainsAny(text, "\x00\r\n") {
		return "", WorkspaceUnverified
	}
	return text, nil
}
func workspaceCanonical(path string) (string, bool) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", false
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return "", false
	}
	info, err := os.Stat(path)
	return canonical, err == nil && info.IsDir()
}
func relativeMetadataPath(base, raw string) string {
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Clean(filepath.Join(base, raw))
}

// VerifyProjectWorkspace observes membership only. Its caller must separately
// prove that root is a known, explicitly consented Project. Canonical nested
// directories are admitted only under their nearest verified Git workspace;
// another repository inside a directory does not inherit its parent's grant.
func VerifyProjectWorkspace(ctx context.Context, root, workspace string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := workspaceCanonical(root); !ok {
		return WorkspaceUnverified
	}
	if _, ok := workspaceCanonical(workspace); !ok {
		return WorkspaceUnverified
	}
	if root == workspace {
		return ctx.Err()
	}
	owner := ""
	for cursor := workspace; ; cursor = filepath.Dir(cursor) {
		marker := filepath.Join(cursor, ".git")
		if _, err := os.Lstat(marker); err == nil {
			owner = cursor
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return WorkspaceMetadataUnreadable
		}
		if filepath.Dir(cursor) == cursor {
			break
		}
	}
	if owner == "" {
		return WorkspaceUnverified
	}
	// A normal checkout's nested directory shares that checkout, but a nested
	// repository with a different .git marker never falls through to the parent.
	if owner == root {
		info, err := os.Lstat(filepath.Join(root, ".git"))
		if err != nil || !info.IsDir() {
			return WorkspaceUnverified
		}
		return ctx.Err()
	}
	raw, err := workspaceMetadata(ctx, filepath.Join(owner, ".git"))
	if err != nil {
		return err
	}
	raw, ok := strings.CutPrefix(raw, "gitdir: ")
	if !ok {
		return WorkspaceUnverified
	}
	gitdir := relativeMetadataPath(owner, raw)
	common := filepath.Join(root, ".git")
	registry := filepath.Join(common, "worktrees")
	for _, dir := range []string{common, registry, gitdir} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return WorkspaceUnverified
		}
		actual, err := filepath.EvalSymlinks(dir)
		if err != nil || actual != dir {
			return WorkspaceUnverified
		}
	}
	if filepath.Dir(gitdir) != registry {
		return WorkspaceUnverified
	}
	backlink, err := workspaceMetadata(ctx, filepath.Join(gitdir, "gitdir"))
	if err != nil {
		return err
	}
	if relativeMetadataPath(gitdir, backlink) != filepath.Join(owner, ".git") {
		return WorkspaceUnverified
	}
	commonLink, err := workspaceMetadata(ctx, filepath.Join(gitdir, "commondir"))
	if err != nil {
		return err
	}
	if relativeMetadataPath(gitdir, commonLink) != common {
		return WorkspaceUnverified
	}
	if _, err := workspaceMetadata(ctx, filepath.Join(gitdir, "HEAD")); err != nil {
		return err
	}
	return ctx.Err()
}
