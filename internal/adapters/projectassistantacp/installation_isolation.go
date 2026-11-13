package projectassistantacp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func quoted(path string) string { return strconv.Quote(path) }

// The profile is enforced by the OS, not by a native tool's claimed name.
// Claude may read its normal login locations. No process receives a copied
// credential, inherited API key, personal transcript or machine credential.
func claudeProfile(home, owned, node, cli, modules string) string {
	return fmt.Sprintf(`(version 1)
(allow default)
(deny mach-lookup)
(allow mach-lookup (global-name "com.apple.SecurityServer"))
(deny appleevent-send)
(deny file-write*)
(allow file-write* (subpath %s))
(deny file-read-data (subpath %s))
(allow file-read* (subpath %s) (subpath %s) (literal %s) (literal %s))
(deny file-read-data (with errno ENOENT) (regex #"(^|/)\.claude(/|$)") (subpath "/Library/Application Support/ClaudeCode") (subpath "/Library/Managed Preferences"))
(deny file-write* (regex #"(^|/)\.claude(/|$)"))
(allow file-read-data (literal %s) (literal %s))
(deny process-exec)
(allow process-exec (literal %s) (literal %s) (literal "/usr/bin/security"))
`, quoted(owned), quoted(home), quoted(owned), quoted(modules), quoted(node), quoted(cli), quoted(filepath.Join(home, ".claude.json")), quoted(filepath.Join(home, ".claude", ".credentials.json")), quoted(node), quoted(cli))
}

// This program has no native adapter import, auth read or network operation.
// It proves denial, including a memory-path control inside the writable root,
// so a false capability cannot be created by merely setting SDK options.
const claudeIsolationProbe = `
const fs=require('node:fs'),cp=require('node:child_process'),path=require('node:path');
const [owned,foreign]=process.argv.slice(1);const r={};
try{fs.writeFileSync(foreign,'forbidden');r.foreign_write=false}catch(e){r.foreign_write=e.code==='EPERM'}
const x=cp.spawnSync('/usr/bin/true',[]);r.foreign_exec=x.error?.code==='EPERM';
const memory=path.join(owned,'.claude','memory','control');
try{fs.readFileSync(memory);r.memory_read=false}catch(e){r.memory_read=e.code==='EPERM'||e.code==='ENOENT'}
try{fs.writeFileSync(memory,'forbidden');r.memory_write=false}catch(e){r.memory_write=e.code==='EPERM'}
fs.writeFileSync(path.join(owned,'control'),'owned');r.owned_write=fs.readFileSync(path.join(owned,'control'),'utf8')==='owned';
process.stdout.write(JSON.stringify(r));if(!Object.values(r).every(Boolean))process.exitCode=1;
`

func (i ClaudeInstallation) isolated(ctx context.Context, state string, b pa.Bot, l Limits) (Configuration, func(), error) {
	none := func() {}
	home, e := os.UserHomeDir()
	if e != nil || !filepath.IsAbs(home) {
		return Configuration{}, none, refusal(pa.Invalid, "native_home")
	}
	root := filepath.Join(state, "project-assistants", "runtime")
	if e = os.MkdirAll(root, 0700); e != nil {
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	scope, _ := json.Marshal(struct {
		Key     pa.BotKey
		Project pa.ProjectScope
	}{b.Key, b.Project})
	sum := sha256.Sum256(scope)
	owned, e := os.MkdirTemp(root, hex.EncodeToString(sum[:])+"-")
	if e != nil {
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	created := owned
	cleanup := func() { _ = os.RemoveAll(created) }
	owned, e = filepath.EvalSymlinks(owned)
	if e != nil {
		cleanup()
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	modules, e := i.moduleRoot()
	if e != nil {
		cleanup()
		return Configuration{}, none, e
	}
	// Real control bytes are written by the host before entering the sandbox.
	// ENOENT alone cannot pass a test against an actually absent memory file.
	memory := filepath.Join(owned, ".claude", "memory")
	if e = os.MkdirAll(memory, 0700); e == nil {
		e = os.WriteFile(filepath.Join(memory, "control"), []byte("owned control"), 0600)
	}
	if e != nil {
		cleanup()
		return Configuration{}, none, refusal(pa.Invalid, "native_control")
	}
	tmp := filepath.Join(owned, "tmp")
	if e = os.Mkdir(tmp, 0700); e != nil {
		cleanup()
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	config := Configuration{Executable: "/usr/bin/sandbox-exec", Arguments: []string{"-p", claudeProfile(home, owned, i.Node, i.CLI, modules), i.Node, i.Adapter}, Environment: []string{
		"HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TMPDIR=" + tmp,
		"CLAUDE_CODE_EXECUTABLE=" + i.CLI, "CLAUDE_AGENT_ACP_PROTOCOL=v1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	}}
	foreign := filepath.Join(root, filepath.Base(owned)+"-denied")
	if e = verifyClaudeIsolation(ctx, config, owned, foreign, l); e != nil {
		cleanup()
		return Configuration{}, none, e
	}
	return config, cleanup, nil
}

type isolatedNativeSession struct {
	app.ProviderSession
	cleanup func()
	once    sync.Once
}

func (s *isolatedNativeSession) Close(ctx context.Context) error {
	if e := s.ProviderSession.Close(ctx); e != nil {
		return e
	}
	s.once.Do(s.cleanup)
	return nil
}

func verifyClaudeIsolation(ctx context.Context, config Configuration, owned, foreign string, l Limits) error {
	probeCtx, cancel := context.WithTimeout(ctx, l.CleanupTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, config.Executable, "-p", config.Arguments[1], config.Arguments[2], "-e", claudeIsolationProbe, owned, foreign)
	cmd.Env = config.Environment
	capture := &censusCapture{budget: l.FrameBytes}
	cmd.Stdout = capture
	cmd.Stderr = io.Discard
	e := cmd.Run()
	var result map[string]bool
	if json.Unmarshal(capture.buf.Bytes(), &result) != nil || len(result) != 5 {
		return refusal(pa.ReadOnlyUnverified, "native_os_controls")
	}
	for _, key := range []string{"foreign_write", "foreign_exec", "memory_read", "memory_write", "owned_write"} {
		if !result[key] {
			return refusal(pa.ReadOnlyUnverified, "native_os_"+key)
		}
	}
	if e != nil {
		return refusal(pa.ReadOnlyUnverified, "native_os_control_exit")
	}
	if _, e = os.Stat(foreign); !os.IsNotExist(e) {
		return refusal(pa.ReadOnlyUnverified, "native_os_controls")
	}
	return nil
}
