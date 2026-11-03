package orchestrator

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// The answering workspace is an operator installation, not a member checkout.
// Its Codex home is separate so ambient MCP servers, plugins, hooks and API-key
// configuration cannot grant capabilities outside this profile. The runtime may
// use the operator's ChatGPT login; model-generated commands cannot read it.
func supportPermissionArgs(r Record, cwd, executable string) []string {
	if executable == "" {
		executable, _ = os.Executable()
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	read := []string{":minimal", filepath.Join(cwd, "docs"), filepath.Join(cwd, "support-worker.py"), filepath.Join(cwd, "private", "worker-token"), executable}
	entries := make([]string, 0, len(read)+2)
	for _, path := range read {
		if path != "" {
			entries = append(entries, strconv.Quote(path)+`="read"`)
		}
	}
	// A run may write only its own receipt directory and the scoped client's
	// durable state. Neither the project nor the machine-wide task root is writable.
	for _, path := range []string{r.Dir, filepath.Join(cwd, "state")} {
		entries = append(entries, strconv.Quote(path)+`="write"`)
	}
	config := []string{
		`forced_login_method="chatgpt"`,
		`default_permissions="clawdline-support"`,
		`permissions.clawdline-support.filesystem={` + strings.Join(entries, ",") + `}`,
		`features.network_proxy=true`,
		`permissions.clawdline-support.network.enabled=true`,
		`permissions.clawdline-support.network.domains={"api.clawdline.com"="allow","127.0.0.1"="allow","localhost"="allow"}`,
		`features.apps=false`, `features.plugins=false`, `features.multi_agent=false`,
		`web_search="disabled"`,
		`shell_environment_policy.inherit="none"`,
		`shell_environment_policy.set={PATH="/usr/bin:/bin:/usr/sbin:/sbin",LANG="en_US.UTF-8",TMPDIR=` + strconv.Quote(r.Dir) + `,PYTHONDONTWRITEBYTECODE="1"}`,
	}
	args := []string{"--strict-config", "--no-daemon"}
	for _, value := range config {
		args = append(args, "--config", projects.ShellQuoted(value))
	}
	return append(args, "--ask-for-approval", "never")
}
