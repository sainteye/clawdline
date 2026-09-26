package projects

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/projectsync"
)

// MaxGitConfigBytes is how much of one checkout's git config OriginRepo reads.
// A config longer than this is not parsed at all and the place answers no
// repository, which is the same answer as a checkout without an origin: the
// console says the project cannot be moved rather than guessing.
const MaxGitConfigBytes = 64 << 10

// OriginRepo is the repository a place's checkout clones, in
// projectsync.Repo's spelling (host/owner/name), or "" when it has none.
//
// It is what lets a schedule move between machines: a place id is a digest of
// a path on one machine (identity.go), so the same project on another machine
// has another id, and the only name the two checkouts share is their origin.
//
// It reads the config file rather than asking git, because GET /v1/places
// answers up to forty places on every open of the Start and Schedule sheets
// and forty `git config` processes is not a cheap read. It follows what git
// itself follows to find that file — a `.git` directory, a `.git` file naming
// the real one (a linked worktree or submodule), and a worktree's `commondir`
// — from the place up to the root, so a place inside a repository answers the
// repository. It does not follow `include` or `insteadOf`; a remote spelled
// through either answers what the file literally says, or nothing.
func OriginRepo(dir string) string {
	gitDir, ok := findGitDir(dir)
	if !ok {
		return ""
	}
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		c := strings.TrimSpace(string(common))
		if c != "" {
			if !filepath.IsAbs(c) {
				c = filepath.Join(gitDir, c)
			}
			gitDir = c
		}
	}
	url := originURL(filepath.Join(gitDir, "config"))
	if url == "" {
		return ""
	}
	repo, err := projectsync.Repo(url)
	if err != nil {
		return ""
	}
	return repo
}

// findGitDir walks from dir to the root for the first `.git`, answering the
// directory it names.
func findGitDir(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	at := filepath.Clean(dir)
	for {
		candidate := filepath.Join(at, ".git")
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return candidate, true
			}
			raw, err := readBounded(candidate, 4<<10)
			if err != nil {
				return "", false
			}
			line := strings.TrimSpace(string(raw))
			target, ok := strings.CutPrefix(line, "gitdir:")
			if !ok {
				return "", false
			}
			target = strings.TrimSpace(target)
			if !filepath.IsAbs(target) {
				target = filepath.Join(at, target)
			}
			return target, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", false
		}
		at = parent
	}
}

// originURL is `remote.origin.url` as the config file spells it.
func originURL(path string) string {
	raw, err := readBounded(path, MaxGitConfigBytes)
	if err != nil {
		return ""
	}
	inOrigin := false
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 4096), MaxGitConfigBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				inOrigin = false
				continue
			}
			section := strings.TrimSpace(line[1:end])
			name, sub, _ := strings.Cut(section, " ")
			inOrigin = strings.EqualFold(name, "remote") && strings.TrimSpace(sub) == `"origin"`
			if rest := strings.TrimSpace(line[end+1:]); inOrigin && rest != "" {
				line = rest
			} else {
				continue
			}
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		return value
	}
	return ""
}

// readBounded reads a whole small file, or refuses one longer than limit.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, io.ErrShortBuffer
	}
	return raw, nil
}
