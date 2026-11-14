package projectassistantacp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Following CodyNC, a Bot uses the Claude Code and Node the person already
// installed; Claude Code updates itself, so only a supported floor is checked.
// The ACP adapter is the one part Clawdline supplies: its lockfile ships in
// the binary, npm installs exactly that tree, and the tree digest seals it.
const (
	MinimumClaudeCLIVersion = "2.1.323"
	MinimumNodeVersion      = "22.0.0"
	// claudeAdapterTreeSHA is adapterTreeDigest of the tree `npm ci
	// --omit=dev --omit=optional --ignore-scripts` builds from the bundled
	// lockfile. The optional platform package would carry a second Claude
	// binary; the person's own CLI is used instead.
	claudeAdapterTreeSHA = "fcb005f36bd2c73bc6ed58a8b8657191ae10cc36e371c55718d253bfae209d86"
)

//go:embed assets/claude-agent-acp/package.json assets/claude-agent-acp/package-lock.json
var claudeAdapterAssets embed.FS

// nativeTools are the person's executables found for one execution. Paths
// are resolved through links, so a launcher that Claude Code repoints after an
// update is followed on the next start.
type nativeTools struct {
	Node, NPM, CLI          string
	NodeVersion, CLIVersion string
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func parseVersion(text string) ([3]int, bool) {
	var v [3]int
	m := versionPattern.FindStringSubmatch(text)
	if m == nil {
		return v, false
	}
	for i := range v {
		n, e := strconv.Atoi(m[i+1])
		if e != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func versionAtLeast(have, floor string) bool {
	h, ok := parseVersion(have)
	f, okFloor := parseVersion(floor)
	if !ok || !okFloor {
		return false
	}
	for i := range h {
		if h[i] != f[i] {
			return h[i] > f[i]
		}
	}
	return true
}

// probeVersion runs `<path> --version` with a fixed environment, a bounded
// time and bounded output. It is a local process observation, not a login or
// model request.
func probeVersion(ctx context.Context, path string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotToolProbeMilliseconds)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env, cmd.Stdin = env, nil
	var out bytes.Buffer
	limited := &limitedBuffer{b: &out, left: capacity.MaximumBotToolProbeBytes}
	cmd.Stdout = limited
	if e := cmd.Run(); e != nil {
		return "", e
	}
	if limited.over {
		return "", errors.New("version output over its bound")
	}
	v, ok := parseVersion(out.String())
	if !ok {
		return "", errors.New("no version")
	}
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2]), nil
}

type limitedBuffer struct {
	b    *bytes.Buffer
	left int
	over bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > w.left {
		p, w.over = p[:w.left], true
	}
	w.left -= len(p)
	w.b.Write(p)
	return n, nil
}

func executable(path string) (string, bool) {
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || !filepath.IsAbs(resolved) {
		return "", false
	}
	info, e := os.Stat(resolved)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", false
	}
	return resolved, true
}

// SearchDirectories is where the person's tools are looked for. Hermetic test
// binaries replace it before any test runs so that no test can find, probe or
// run npm from a developer's real installation.
var SearchDirectories = toolDirectories

// toolDirectories lists where installers put these programs. The daemon may
// start from launchd with a minimal PATH, so the common per-user locations are
// named explicitly; the person's PATH, when present, comes first.
func toolDirectories(home string) []string {
	var out []string
	out = append(out, filepath.SplitList(os.Getenv("PATH"))...)
	out = append(out, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local"),
		"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".volta", "bin"))
	// Version managers keep one directory per version; prefer the newest.
	for _, pattern := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin"),
		filepath.Join(home, ".local", "share", "mise", "installs", "node", "*", "bin"),
		filepath.Join(home, ".asdf", "installs", "nodejs", "*", "bin"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin"),
	} {
		found, _ := filepath.Glob(pattern)
		sort.Slice(found, func(i, j int) bool {
			a, _ := parseVersion(found[i])
			b, _ := parseVersion(found[j])
			for k := range a {
				if a[k] != b[k] {
					return a[k] > b[k]
				}
			}
			return found[i] > found[j]
		})
		out = append(out, found...)
	}
	out = append(out, "/usr/bin", "/bin")
	seen := map[string]bool{}
	var unique []string
	for _, d := range out {
		if d == "" || !filepath.IsAbs(d) || seen[d] {
			continue
		}
		seen[d] = true
		unique = append(unique, d)
	}
	return unique
}

// findTools chooses the first Node with its npm and the first Claude Code
// that meet the supported floors. A program below its floor is reported as
// unsupported rather than silently skipped when nothing better exists.
func findTools(ctx context.Context, home, user string, dirs []string) (nativeTools, error) {
	var t nativeTools
	env := func(dir string) []string {
		return []string{"HOME=" + home, "USER=" + user, "PATH=" + dir + string(os.PathListSeparator) + nativePath, "LANG=C", "LC_ALL=C"}
	}
	nodeOld, cliOld := false, false
	for _, d := range dirs {
		if t.Node != "" {
			break
		}
		node, ok := executable(filepath.Join(d, "node"))
		if !ok {
			continue
		}
		if _, ok := executable(filepath.Join(d, "npm")); !ok {
			continue
		}
		v, e := probeVersion(ctx, node, env(d))
		if e != nil {
			continue
		}
		if !versionAtLeast(v, MinimumNodeVersion) {
			nodeOld = true
			continue
		}
		t.Node, t.NPM, t.NodeVersion = node, filepath.Join(d, "npm"), v
	}
	for _, d := range dirs {
		if t.CLI != "" {
			break
		}
		cli, ok := executable(filepath.Join(d, "claude"))
		if !ok {
			continue
		}
		v, e := probeVersion(ctx, cli, env(filepath.Dir(cli)))
		if e != nil {
			continue
		}
		if !versionAtLeast(v, MinimumClaudeCLIVersion) {
			cliOld = true
			continue
		}
		t.CLI, t.CLIVersion = cli, v
	}
	if e := ctx.Err(); e != nil {
		return t, e
	}
	switch {
	case t.Node == "" && nodeOld:
		return t, refusal(pa.ProviderVersionUnsupported, "native_node_version")
	case t.Node == "":
		return t, refusal(pa.CLINotInstalled, "native_node")
	case t.CLI == "" && cliOld:
		return t, refusal(pa.ProviderVersionUnsupported, "native_cli_version")
	case t.CLI == "":
		return t, refusal(pa.CLINotInstalled, "native_cli")
	}
	return t, nil
}

// adapterTreeDigest is one digest over every file below a node_modules root,
// by relative path and content. npm's own `.package-lock.json` cache record
// is excluded; it is regenerated and is not imported by the adapter.
func adapterTreeDigest(ctx context.Context, root string) (string, map[string]string, error) {
	files := map[string]string{}
	var rows []string
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", nil, refusal(pa.AdapterNotFound, "native_dependencies")
	}
	e = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if walkErr != nil {
			return refusal(pa.AdapterNotFound, "native_dependencies")
		}
		if d.IsDir() {
			return nil
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil {
			return refusal(pa.Invalid, "native_dependency_link")
		}
		rel, e := filepath.Rel(root, resolved)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return refusal(pa.ScopeDenied, "native_dependency_link")
		}
		name, _ := filepath.Rel(root, path)
		if name == ".package-lock.json" {
			return nil
		}
		digest, e := fileDigest(path)
		if e != nil {
			return e
		}
		files[path] = digest
		rows = append(rows, filepath.ToSlash(name)+"\x00"+digest)
		return nil
	})
	if e != nil {
		return "", nil, e
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:]), files, nil
}

func adapterDirectory(state string) string {
	return filepath.Join(state, "project-assistants", "adapters", "claude-agent-acp-"+ClaudeAdapterVersion)
}

func adapterEntry(dir string) string {
	return filepath.Join(dir, "node_modules", "@agentclientprotocol", "claude-agent-acp", "dist", "index.js")
}

var provisionLock sync.Mutex

// installAdapter returns the sealed adapter directory, building it with the
// person's npm from the bundled lockfile when absent or changed. Scripts and
// optional packages are not installed; the npm cache is private to the build
// and removed with it. A build that does not reproduce the digest is removed.
func installAdapter(ctx context.Context, state string, t nativeTools, home, user string) (string, error) {
	provisionLock.Lock()
	defer provisionLock.Unlock()
	dir := adapterDirectory(state)
	if digest, _, e := adapterTreeDigest(ctx, filepath.Join(dir, "node_modules")); e == nil && digest == claudeAdapterTreeSHA {
		return dir, nil
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	parent := filepath.Dir(dir)
	if e := os.MkdirAll(parent, 0700); e != nil {
		return "", refusal(pa.Invalid, "native_state")
	}
	build, e := os.MkdirTemp(parent, ".build-")
	if e != nil {
		return "", refusal(pa.Invalid, "native_state")
	}
	defer os.RemoveAll(build)
	for _, name := range []string{"package.json", "package-lock.json"} {
		data, e := claudeAdapterAssets.ReadFile("assets/claude-agent-acp/" + name)
		if e != nil || os.WriteFile(filepath.Join(build, name), data, 0600) != nil {
			return "", refusal(pa.Invalid, "native_state")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotAdapterInstallSeconds)*time.Second)
	defer cancel()
	cache := filepath.Join(build, ".npm-cache")
	cmd := exec.CommandContext(ctx, t.NPM, "ci", "--ignore-scripts", "--omit=dev", "--omit=optional",
		"--no-audit", "--no-fund", "--loglevel=error", "--cache", cache)
	cmd.Dir = build
	cmd.Env = []string{"HOME=" + home, "USER=" + user, "PATH=" + filepath.Dir(t.Node) + string(os.PathListSeparator) + nativePath,
		"LANG=C", "LC_ALL=C", "TMPDIR=" + build, "npm_config_update_notifier=false"}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = nil, &limitedBuffer{b: &stderr, left: capacity.MaximumBotStderrBytes}
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return "", refusal(pa.AdapterNotFound, "native_adapter_install_timeout")
		}
		return "", refusal(pa.AdapterNotFound, "native_adapter_install")
	}
	_ = os.RemoveAll(cache)
	digest, _, e := adapterTreeDigest(ctx, filepath.Join(build, "node_modules"))
	if e != nil {
		return "", e
	}
	if digest != claudeAdapterTreeSHA {
		return "", refusal(pa.ProviderVersionUnsupported, "native_adapter_digest")
	}
	_ = os.RemoveAll(dir)
	if e := os.Rename(build, dir); e != nil {
		return "", refusal(pa.Invalid, "native_state")
	}
	return dir, nil
}
