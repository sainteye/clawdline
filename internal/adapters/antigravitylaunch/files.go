package antigravitylaunch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// Error contains only a stable code. Neither paths nor private content are
// included, even when the underlying filesystem error contains them.
type Error struct {
	Code  string
	cause error
}

func (e *Error) Error() string { return e.Code }
func (e *Error) Unwrap() error { return e.cause }
func fail(code string) error   { return &Error{Code: "antigravity_context_" + code} }
func cancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "antigravity_context_cancelled", cause: err}
	}
	return nil
}
func hash(b []byte) string   { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func safeText(s string) bool { return s != "" && !strings.ContainsAny(s, "\x00\r\n") }
func absolute(s string) bool { return safeText(s) && filepath.IsAbs(s) && filepath.Clean(s) == s }
func component(s string) bool {
	return safeText(s) && s != "." && s != ".." && filepath.Base(s) == s && !strings.ContainsAny(s, `/\`)
}
func missing(err error) bool { return errors.Is(err, errMissing) }

var squadLaunchComponent = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
var personaPromptComponent = regexp.MustCompile(`^[a-z][a-z0-9-]*\.md$`)

// Every filesystem operation is relative to an opened, owned directory. The
// platform implementation rejects symlinks at each component, validates inode
// identity after reads, and never deletes unknown files or follows their links.
func readPrompt(root *privateDir, path string, ceiling int) ([]byte, error) {
	if !absolute(path) {
		return nil, fail("prompt_invalid")
	}
	rel, err := filepath.Rel(root.path, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return nil, fail("prompt_invalid")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	// Only the two prompt layouts produced by squadfiles.PublishFor qualify.
	// The trusted root also holds capabilities/snapshots; those are not prompts.
	if len(parts) < 2 || !squadLaunchComponent.MatchString(parts[0]) ||
		!((len(parts) == 2 && parts[1] == "prompt.md") ||
			(len(parts) == 3 && parts[1] == "personas" && personaPromptComponent.MatchString(parts[2]))) {
		return nil, fail("prompt_invalid")
	}
	dir := root
	for _, name := range parts[:len(parts)-1] {
		next, err := dir.child(name, false)
		if dir != root {
			dir.close()
		}
		if err != nil {
			return nil, err
		}
		dir = next
	}
	if dir != root {
		defer dir.close()
	}
	b, err := dir.read(parts[len(parts)-1], ceiling)
	if err != nil {
		return nil, fail("prompt_unavailable")
	}
	if err := root.current(); err != nil {
		return nil, err
	}
	return b, nil
}
