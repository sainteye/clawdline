// Package projectsync carries a project's machine-local settings from one
// machine that owns them to others that only read them.
//
// A local path is not a cross-machine identity (docs/project-icons.md): two
// checkouts of one repository can sit at different paths, and two unrelated
// folders can share a name. The identity used here is the repository itself,
// spelled from its `origin` remote (Repo). A project without one is not
// carried, and says so, rather than being matched by its folder name.
//
// One side is the source and every other side is a mirror. A mirror stores
// what it was given and never republishes it, so a settings change can travel
// in exactly one direction and two machines cannot start echoing each other.
package projectsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

// Bounds. Each is registered in internal/domain/capacity.
const (
	// MaxManifestProjects is how many projects one manifest names.
	MaxManifestProjects = 256
	// MaxProjectFiles is how many local files one project carries.
	MaxProjectFiles = 64
	// MaxFileBytes is the largest single carried file.
	MaxFileBytes = 256 << 10
	// MaxEntryBytes is the largest single project, contents included, as a
	// request body. Well under the relay's 16 MiB envelope.
	MaxEntryBytes = 4 << 20
	// MaxMirrorRecords is how many repositories one machine mirrors.
	MaxMirrorRecords = 512
	// MaxPathBytes is the longest carried relative path.
	MaxPathBytes = 512
)

// Version is the manifest wire version.
const Version = 1

// Dirs are the project-local places whose untracked files travel. What git
// already tracks arrives with the repository and is never carried here; what
// is not tracked is exactly what only this machine has.
var Dirs = []string{".claude/skills", ".claude/commands", ".claude/agents"}

// Files are single project-local files that travel when git does not track
// them. `.claude/settings.local.json` is deliberately absent: it holds
// permission grants and absolute paths of the machine that wrote it, and
// copying a grant to another machine is an escalation nobody chose there.
var Files = []string{"CLAUDE.local.md"}

// File is one carried file. Content is present only in a single project's
// entry, never in the manifest listing.
type File struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Content []byte `json:"content,omitempty"`
}

// Entry is one project as its source machine describes it.
type Entry struct {
	Repo     string    `json:"repo"`
	CloneURL string    `json:"clone_url"`
	Label    string    `json:"label"`
	Icon     icon.Grid `json:"icon"`
	Files    []File    `json:"files"`
	// Withheld are carried paths the source has and did not send: over the
	// file or entry budget, or unreadable. A mirror never deletes one of these
	// as if the source had dropped it.
	Withheld []string `json:"withheld,omitempty"`
	Revision string   `json:"revision"`
}

// Skipped is a project the source has and does not carry, with why.
type Skipped struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Manifest is what a source machine offers.
type Manifest struct {
	Version  int       `json:"version"`
	At       int64     `json:"at"`
	Revision string    `json:"revision"`
	Projects []Entry   `json:"projects"`
	Skipped  []Skipped `json:"skipped"`
}

// Skip reasons.
const (
	SkipNoRemote      = "no_remote"
	SkipNotRepository = "not_a_repository"
	SkipLocalRemote   = "remote_not_portable"
	SkipMirrored      = "mirrored_here"
	SkipUnreadable    = "unreadable"
	SkipDuplicate     = "duplicate_repository"
	SkipManifestFull  = "manifest_full"
)

// repoShape is host/owner/…/name. No segment starts with a dot or a tilde:
// the last one names the clone's directory, and `.git` or `.ssh` there is a
// directory nobody meant to create.
var repoShape = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(/[a-z0-9_-][a-z0-9._~-]*)+$`)

// ErrNotPortable is a remote that names a place only this machine can reach:
// a local path or a file URL.
var ErrNotPortable = errors.New("the origin remote is not reachable from another machine")

// Repo spells a remote URL as the repository it names: host and path, lower
// case, without scheme, credentials, port or `.git`.
//
//	git@github.com:Owner/Name.git      → github.com/owner/name
//	https://github.com/owner/name/     → github.com/owner/name
//	ssh://git@host:22/team/name.git    → host/team/name
func Repo(remote string) (string, error) {
	r := strings.TrimSpace(remote)
	if r == "" {
		return "", errors.New("empty remote")
	}
	var host, p string
	switch {
	case strings.Contains(r, "://"):
		u, err := url.Parse(r)
		if err != nil {
			return "", fmt.Errorf("unreadable remote: %w", err)
		}
		if u.Scheme == "file" || u.Host == "" {
			return "", ErrNotPortable
		}
		host, p = u.Hostname(), u.Path
	case strings.HasPrefix(r, "/") || strings.HasPrefix(r, ".") || strings.HasPrefix(r, "~"):
		return "", ErrNotPortable
	default:
		// scp-like: [user@]host:path
		colon := strings.Index(r, ":")
		if colon <= 0 {
			return "", ErrNotPortable
		}
		host, p = r[:colon], r[colon+1:]
		// git reads a slash before the first colon as a local path, not a host.
		if strings.Contains(host, "/") {
			return "", ErrNotPortable
		}
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		// A Windows drive letter is a path, not a host.
		if len(host) == 1 {
			return "", ErrNotPortable
		}
	}
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	out := strings.ToLower(host + "/" + p)
	if !repoShape.MatchString(out) || strings.Contains(out, "..") {
		return "", fmt.Errorf("remote %q does not name a repository", remote)
	}
	return out, nil
}

// ValidRepo is whether a string is already in Repo's spelling.
func ValidRepo(repo string) bool {
	return len(repo) <= MaxPathBytes && repoShape.MatchString(repo) && !strings.Contains(repo, "..")
}

// Allowed is whether a relative path may be carried: one of Files, or a file
// under one of Dirs, spelled canonically and with no part that climbs out.
func Allowed(rel string) bool {
	if rel == "" || len(rel) > MaxPathBytes || strings.ContainsAny(rel, "\\\x00") {
		return false
	}
	if path.Clean(rel) != rel || path.IsAbs(rel) {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "." || part == "" || strings.EqualFold(part, ".git") {
			return false
		}
	}
	for _, f := range Files {
		if rel == f {
			return true
		}
	}
	for _, d := range Dirs {
		if strings.HasPrefix(rel, d+"/") {
			// A dot file or directory inside a carried place is where an
			// ignored secret sits (`.env`), not a skill.
			for _, part := range strings.Split(strings.TrimPrefix(rel, d+"/"), "/") {
				if strings.HasPrefix(part, ".") {
					return false
				}
			}
			return true
		}
	}
	return false
}

// Sum is a file's content hash as carried.
func Sum(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// EntryRevision is what changes when anything a mirror would apply changes.
// The clone URL is not part of it: two spellings of one remote are one
// repository, and a mirror applies nothing from it once cloned.
func EntryRevision(e Entry) string {
	files := make([][2]string, 0, len(e.Files))
	for _, f := range e.Files {
		files = append(files, [2]string{f.Path, f.SHA256})
	}
	sort.Slice(files, func(i, j int) bool { return files[i][0] < files[j][0] })
	withheld := append([]string(nil), e.Withheld...)
	sort.Strings(withheld)
	data, _ := json.Marshal(struct {
		Repo     string      `json:"repo"`
		Label    string      `json:"label"`
		Icon     icon.Grid   `json:"icon"`
		Files    [][2]string `json:"files"`
		Withheld []string    `json:"withheld"`
	}{e.Repo, e.Label, e.Icon, files, withheld})
	return Sum(data)[:32]
}

// ManifestRevision summarizes every entry's revision.
func ManifestRevision(entries []Entry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.Repo+"="+e.Revision)
	}
	sort.Strings(parts)
	return Sum([]byte(strings.Join(parts, "\n")))[:32]
}

// Check refuses an entry a mirror must not apply: an unspelled repository, a
// clone URL naming another repository, an invalid icon, a path outside the
// allowed places, or content that does not match its own hash. Contents are
// required when withContent is set.
func Check(e Entry, withContent bool) error {
	if !ValidRepo(e.Repo) {
		return errors.New("the repository is not spelled as host/owner/name")
	}
	if e.CloneURL != "" {
		repo, err := Repo(e.CloneURL)
		if err != nil || repo != e.Repo {
			return errors.New("the clone URL names a different repository")
		}
	}
	if len(e.Label) > 200 {
		return errors.New("the label is longer than 200 bytes")
	}
	if err := icon.Validate(e.Icon); err != nil {
		return err
	}
	if len(e.Files) > MaxProjectFiles || len(e.Withheld) > MaxProjectFiles {
		return fmt.Errorf("a project carries at most %d files", MaxProjectFiles)
	}
	for _, w := range e.Withheld {
		if !Allowed(w) {
			return fmt.Errorf("%q is not a carried project file", w)
		}
	}
	seen := map[string]bool{}
	for _, f := range e.Files {
		if !Allowed(f.Path) {
			return fmt.Errorf("%q is not a carried project file", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("%q appears twice", f.Path)
		}
		seen[f.Path] = true
		if f.Size < 0 || f.Size > MaxFileBytes {
			return fmt.Errorf("%q is larger than %d bytes", f.Path, MaxFileBytes)
		}
		if withContent {
			if int64(len(f.Content)) != f.Size || Sum(f.Content) != f.SHA256 {
				return fmt.Errorf("%q does not match its own hash", f.Path)
			}
		}
	}
	return nil
}
