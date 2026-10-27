package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// Opt in on the installed host: this exercises the CLI's actual kernel and
// network proxy without a model request, authentication or production data.
func TestSupportAnswerKernelConfinement(t *testing.T) {
	if os.Getenv("CLAWDLINE_TEST_CODEX_SANDBOX") != "1" {
		t.Skip("requires the installed Codex sandbox capability")
	}
	root := t.TempDir()
	cwd, own := filepath.Join(root, "support"), filepath.Join(root, "task")
	for _, path := range []string{filepath.Join(cwd, "docs"), filepath.Join(cwd, "private"), filepath.Join(cwd, "state"), filepath.Join(cwd, ".codex-runtime"), own, filepath.Join(root, "other-task")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(cwd, "docs", "guide.md"), filepath.Join(cwd, "private", "worker-token"), filepath.Join(cwd, ".codex-runtime", "auth.json"), filepath.Join(root, "other-task", "private-session.txt")} {
		if err := os.WriteFile(path, []byte("synthetic canary"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe := `import os, pathlib, urllib.request, urllib.error
root = pathlib.Path(__file__).parent.parent
cwd = root / 'support'
assert (cwd / 'docs/guide.md').read_text() == 'synthetic canary'
assert (cwd / 'private/worker-token').read_text() == 'synthetic canary'
(cwd / 'state/result.json').write_text('{}')
(root / 'task/result.json').write_text('{}')
for path in [cwd / '.codex-runtime/auth.json', root / 'other-task/private-session.txt']:
    try:
        path.read_bytes()
    except PermissionError:
        pass
    else:
        raise AssertionError('private read was allowed')
try:
    (cwd / 'docs/guide.md').write_text('changed')
except PermissionError:
    pass
else:
    raise AssertionError('documentation write was allowed')
request = urllib.request.Request('https://api.clawdline.com/readyz', headers={'User-Agent':'ClawdlineSupportWorker/1'})
with urllib.request.urlopen(request, timeout=15) as response:
    assert response.status == 200
try:
    urllib.request.urlopen('https://example.com', timeout=15)
except urllib.error.HTTPError:
    raise AssertionError('unrelated HTTP server was reached')
except urllib.error.URLError as error:
    assert '403' in str(error.reason), str(error.reason)
else:
    raise AssertionError('unrelated network destination was allowed')
print('support sandbox: scoped reads, state and receipts work; private reads, docs writes and unrelated network denied')
`
	path := filepath.Join(own, "probe.py")
	if err := os.WriteFile(path, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	r := Record{Kind: TaskKindSupportAnswer, Assistant: "codex", Dir: own}
	args := supportPermissionArgs(r, cwd, "/usr/bin/true")
	// The sandbox subcommand does not accept the interactive host's strict
	// configuration or daemon flags; it still loads the same permission table.
	args = args[2:]
	command := "TMPDIR=" + projects.ShellQuoted(own) + " CODEX_HOME=" + projects.ShellQuoted(filepath.Join(cwd, ".codex-runtime")) + " codex " + strings.Join(args, " ") + " sandbox -P clawdline-support -- /usr/bin/python3 " + projects.ShellQuoted(path)
	output, err := exec.Command("/bin/sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("real support sandbox: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestSupportAnswerAdmissionCannotBecomeAnOrdinaryDeveloperTask(t *testing.T) {
	b, _ := newTestBroker(t)
	id := "da730001-0000-4000-8000-000000000001"
	for _, c := range []struct {
		name, assistant, isolation string
		claims                     []string
		scheduled, valid           bool
	}{
		{"dedicated schedule", "codex", "none", []string{}, true, true},
		{"ordinary dispatch", "codex", "none", []string{}, false, false},
		{"other provider", "claude", "none", []string{}, true, false},
		{"writable checkout", "codex", "worktree", []string{}, true, false},
		{"repository claim", "codex", "none", []string{"docs"}, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeBrief(t, b, id, b.Dir, map[string]any{"kind": TaskKindSupportAnswer, "assistant": c.assistant, "isolation": c.isolation, "claims": c.claims})
			_, err := b.readDraftAs(id, c.scheduled, false)
			if (err == nil) != c.valid {
				t.Fatalf("admission valid=%v: %v", c.valid, err)
			}
		})
	}
}

func TestSupportAnswerLaunchIgnoresTheGeneralPermissionCeiling(t *testing.T) {
	root := t.TempDir()
	cwd, taskDir, executable := filepath.Join(root, "support"), filepath.Join(root, "tasks", "own"), filepath.Join(root, "bin", "clawdline")
	for _, mode := range []string{"ask", "edits", "full"} {
		r := Record{Kind: TaskKindSupportAnswer, Assistant: "codex", PermissionMode: mode, Dir: taskDir}
		launch, err := projects.Admit(projects.LaunchRequest{ProjectRoot: cwd, Assistant: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		line := shellCommandWithExecutable(launch, r, filepath.Join(root, "tasks"), cwd, executable)
		for _, expected := range []string{
			"CODEX_HOME=", ".codex-runtime", "--strict-config --no-daemon", `forced_login_method="chatgpt"`,
			`default_permissions="clawdline-support"`, `features.network_proxy=true`,
			`"api.clawdline.com"="allow"`, `features.apps=false`, `features.plugins=false`, `features.multi_agent=false`,
			`web_search="disabled"`, `shell_environment_policy.inherit="none"`,
			`"` + filepath.Join(cwd, "docs") + `"="read"`,
			`"` + filepath.Join(cwd, "private", "worker-token") + `"="read"`,
			`"` + filepath.Join(cwd, "state") + `"="write"`, `"` + taskDir + `"="write"`,
			"--ask-for-approval never",
		} {
			if !strings.Contains(line, expected) {
				t.Errorf("%s launch lacks %q: %s", mode, expected, line)
			}
		}
		for _, forbidden := range []string{"--add-dir", "--sandbox", `":root"`, `"*"="allow"`, `"` + cwd + `"="write"`} {
			if strings.Contains(line, forbidden) {
				t.Errorf("%s launch expands support authority with %q", mode, forbidden)
			}
		}
	}
}
