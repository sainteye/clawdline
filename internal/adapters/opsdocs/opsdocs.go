// Package opsdocs is the Ops documents list's reach outside the store: the
// clawdline-ops folder, the repository's info/exclude, and an optional
// question to GitHub about whether the repository is public
// (docs/ops-documents.md).
package opsdocs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
	"github.com/sainteye/clawdline/internal/contract"
	domain "github.com/sainteye/clawdline/internal/domain/opsdocs"
)

// ExcludeLine is the one line this package adds to and removes from a
// repository's info/exclude.
const ExcludeLine = "/" + domain.Folder + "/"

// The GitHub question's bounds; registered in internal/domain/capacity.
const (
	GitHubProbeMillisecondsLimit = 3000
	GitHubCacheEntriesLimit      = 64
)

const githubCacheTTL = 10 * time.Minute

// gitTimeout bounds one local git question.
const gitTimeout = 5 * time.Second

func git(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// ExcludeFile is the info/exclude git reads for root, which for a linked
// worktree is its common directory's. ok is false when root is not in a
// repository.
func ExcludeFile(ctx context.Context, root string) (string, bool) {
	p, err := git(ctx, root, "rev-parse", "--git-path", "info/exclude")
	if err != nil || p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p), true
}

// InRepository says whether root is inside a git work tree.
func InRepository(ctx context.Context, root string) bool {
	_, ok := ExcludeFile(ctx, root)
	return ok
}

func hasLine(data []byte) bool {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if strings.TrimRight(sc.Text(), "\r") == ExcludeLine {
			return true
		}
	}
	return false
}

// Setup reads the folder and exclude state. Visibility is unchecked; the
// caller asks a Prober when the person asked.
func Setup(ctx context.Context, root string) contract.OpsSetup {
	out := contract.OpsSetup{Folder: domain.Folder, Git: contract.OpsGitStateNone,
		Visibility: contract.OpsVisibilityUnchecked}
	if st, err := os.Lstat(filepath.Join(root, domain.Folder)); err == nil && st.IsDir() {
		out.FolderExists = true
	}
	if file, ok := ExcludeFile(ctx, root); ok {
		out.Git = contract.OpsGitStateRepo
		if data, err := os.ReadFile(file); err == nil {
			out.Excluded = hasLine(data)
		}
	}
	return out
}

var excludeLocks sync.Map

// ErrNoRepository is an exclude change asked of a root outside git.
var ErrNoRepository = errors.New("the Project is not in a git repository")

// SetExcluded adds the exclude line when exclude is true and removes exactly
// that line when it is false. Tracked files are never touched; the file is
// replaced through a temporary sibling.
func SetExcluded(ctx context.Context, root string, exclude bool) error {
	file, ok := ExcludeFile(ctx, root)
	if !ok {
		return ErrNoRepository
	}
	v, _ := excludeLocks.LoadOrStore(file, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if hasLine(data) == exclude {
		return nil
	}
	var next []byte
	if exclude {
		next = append([]byte{}, data...)
		if len(next) > 0 && next[len(next)-1] != '\n' {
			next = append(next, '\n')
		}
		next = append(next, ExcludeLine+"\n"...)
	} else {
		lines := strings.SplitAfter(string(data), "\n")
		var kept strings.Builder
		for _, line := range lines {
			if strings.TrimRight(line, "\r\n") == ExcludeLine {
				continue
			}
			kept.WriteString(line)
		}
		next = []byte(kept.String())
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(file); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".exclude-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(next); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

// Prober asks GitHub whether a repository is public, at most once per
// repository every ten minutes.
type Prober struct {
	Client *http.Client
	// Base is GitHub's API root; a test points it at its own server.
	Base string
	// Token is the connected GitHub integration's token, or "" to ask
	// anonymously.
	Token func(context.Context) string
	Now   func() time.Time

	mu    sync.Mutex
	cache map[string]probeAnswer
}

type probeAnswer struct {
	visibility contract.OpsVisibility
	detail     string
	at         time.Time
}

// Probe fills setup's visibility for root's origin remote.
func (p *Prober) Probe(ctx context.Context, root string, setup *contract.OpsSetup) {
	remote, err := git(ctx, root, "remote", "get-url", "origin")
	repo := ""
	if err == nil {
		repo = githubissues.GitHubRepo(remote)
	}
	if repo == "" {
		setup.Visibility, setup.VisibilityDetail = contract.OpsVisibilityUnknown, "not a GitHub remote"
		return
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	p.mu.Lock()
	if a, ok := p.cache[repo]; ok && now().Sub(a.at) < githubCacheTTL {
		p.mu.Unlock()
		setup.Visibility, setup.VisibilityDetail = a.visibility, a.detail
		return
	}
	p.mu.Unlock()
	a := p.ask(ctx, repo)
	a.at = now()
	p.mu.Lock()
	if p.cache == nil {
		p.cache = map[string]probeAnswer{}
	}
	if len(p.cache) >= GitHubCacheEntriesLimit {
		oldest := ""
		for k, v := range p.cache {
			if oldest == "" || v.at.Before(p.cache[oldest].at) {
				oldest = k
			}
		}
		delete(p.cache, oldest)
	}
	p.cache[repo] = a
	p.mu.Unlock()
	setup.Visibility, setup.VisibilityDetail = a.visibility, a.detail
}

func (p *Prober) ask(ctx context.Context, repo string) probeAnswer {
	ctx, cancel := context.WithTimeout(ctx, GitHubProbeMillisecondsLimit*time.Millisecond)
	defer cancel()
	base := p.Base
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repo, nil)
	if err != nil {
		return probeAnswer{visibility: contract.OpsVisibilityUnknown, detail: "GitHub did not answer"}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clawdline")
	if p.Token != nil {
		if token := p.Token(ctx); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeAnswer{visibility: contract.OpsVisibilityUnknown, detail: "GitHub did not answer"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return probeAnswer{visibility: contract.OpsVisibilityUnknown, detail: "GitHub did not say (it may be private or need sign-in)"}
	}
	var body struct {
		Private *bool `json:"private"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&body); err != nil || body.Private == nil {
		return probeAnswer{visibility: contract.OpsVisibilityUnknown, detail: "GitHub's answer was not readable"}
	}
	if *body.Private {
		return probeAnswer{visibility: contract.OpsVisibilityPrivate}
	}
	return probeAnswer{visibility: contract.OpsVisibilityPublic}
}

// Cached is how many repositories' answers are kept now.
func (p *Prober) Cached() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cache)
}
