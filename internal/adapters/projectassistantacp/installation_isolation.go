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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func quoted(path string) string { return strconv.Quote(path) }

// claudeBoundary names every path the OS profile is built from. History is
// empty for metadata admission; Open passes the owned native history config.
type claudeBoundary struct{ Home, State, Owned, Node, CLI, Modules, History string }

// sandboxRegex quotes a resolved path for an SBPL #"..." regular expression.
func sandboxRegex(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`\.^$|?*+()[]{}`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sandboxPath refuses a path that cannot be quoted into either literal form.
func sandboxPath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\"\\\n\r\x00")
}

// The profile is enforced by the OS, not by a native tool's claimed name.
// Claude may read its normal login locations. No process receives a copied
// credential, inherited API key, personal transcript or machine credential.
// Every feature-owned directory under the state is unreadable except this
// execution's scratch and, when present, this binding's exact history config.
// Inside that config, native memory and credential-shaped files stay denied.
// The native CLI reads its login with `security`, once through a shell
// string (/bin/sh hands it to /bin/bash on macOS) and once directly. Both need USER, because os.userInfo() cannot reach directory
// services here; the shell may exec only the same literal programs.
// The login keychain files are readable so the native CLI can use the
// person's existing login; every write there stays denied, so the native
// process cannot add, change or delete a keychain item through these files.
// An allow names file-read-data explicitly: Seatbelt does not let a broader
// file-read* allowance override an earlier, more specific file-read-data deny.
func claudeProfile(b claudeBoundary) string {
	history := ""
	if b.History != "" {
		h := sandboxRegex(b.History)
		history = fmt.Sprintf(`(allow file-read* file-read-data file-write* (subpath %s))
(deny file-read-data file-write* (regex #"^%s/memory(/|$)") (regex #"^%s/projects/[^/]+/memory(/|$)") (regex #"^%s/(.+/)?MEMORY\.md$"))
(deny file-write* (regex #"^%s/(.+/)?\.(credentials|device-keys|config)\.json") (regex #"^%s/(.+/)?\.claude[^/]*\.json"))
`, quoted(b.History), h, h, h, h, h)
	}
	return fmt.Sprintf(`(version 1)
(allow default)
(deny mach-lookup)
(allow mach-lookup (global-name "com.apple.SecurityServer"))
(deny appleevent-send)
(deny file-write*)
(allow file-write* (subpath %s))
(deny file-read-data (subpath %s))
(deny file-read-data (subpath %s))
(allow file-read* file-read-data (subpath %s) (subpath %s) (literal %s) (literal %s))
(allow file-read* file-read-data (subpath %s))
%s(deny file-read-data (with errno ENOENT) (regex #"(^|/)\.claude(/|$)") (subpath "/Library/Application Support/ClaudeCode") (subpath "/Library/Managed Preferences"))
(deny file-write* (regex #"(^|/)\.claude(/|$)"))
(allow file-read-data (literal %s) (literal %s))
(deny process-exec)
(allow process-exec (literal %s) (literal %s) (literal "/usr/bin/security") (literal "/bin/sh") (literal "/bin/bash"))
`, quoted(b.Owned), quoted(b.Home), quoted(filepath.Join(b.State, "project-assistants")), quoted(b.Owned), quoted(b.Modules), quoted(b.Node), quoted(b.CLI), quoted(filepath.Join(b.Home, "Library", "Keychains")), history, quoted(filepath.Join(b.Home, ".claude.json")), quoted(filepath.Join(b.Home, ".claude", ".credentials.json")), quoted(b.Node), quoted(b.CLI))
}

// probeProject is a reserved project key no native working directory maps to.
const probeProject = "clawdline-isolation-probe"

// This program has no native adapter import, auth read or network operation.
// It proves denial, including memory-path controls inside the writable roots,
// so a false capability cannot be created by merely setting SDK options. With
// a history config it also proves the exact transcript allowance and denials.
const claudeIsolationProbe = `
const fs=require('node:fs'),cp=require('node:child_process'),path=require('node:path');
const [owned,foreign,history,foreignTranscript]=process.argv.slice(1);const r={};
try{fs.writeFileSync(foreign,'forbidden');r.foreign_write=false}catch(e){r.foreign_write=e.code==='EPERM'}
const x=cp.spawnSync('/usr/bin/true',[]);r.foreign_exec=x.error?.code==='EPERM';
const memory=path.join(owned,'.claude','memory','control');
try{fs.readFileSync(memory);r.memory_read=false}catch(e){r.memory_read=e.code==='EPERM'||e.code==='ENOENT'}
try{fs.writeFileSync(memory,'forbidden');r.memory_write=false}catch(e){r.memory_write=e.code==='EPERM'}
try{fs.readdirSync(path.join(process.env.HOME,'Library','Keychains'));r.keychain_read=true}catch(e){r.keychain_read=false}
try{cp.execSync('true',{stdio:['ignore','pipe','pipe']});r.login_lookup=!!process.env.USER}catch(e){r.login_lookup=false}
fs.writeFileSync(path.join(owned,'control'),'owned');r.owned_write=fs.readFileSync(path.join(owned,'control'),'utf8')==='owned';
const denied=p=>{let a,b;try{fs.readFileSync(p);a=false}catch(e){a=e.code==='EPERM'}try{fs.writeFileSync(p,'forbidden');b=false}catch(e){b=e.code==='EPERM'}return a&&b};
const refused=p=>{try{fs.writeFileSync(p,'forbidden');return false}catch(e){return e.code==='EPERM'}};
if(history){
const project=path.join(history,'projects','` + probeProject + `');
r.personal_memory=denied(path.join(history,'memory','personal','control'));
r.project_memory=denied(path.join(project,'memory','control'));
try{const t=path.join(project,'control.jsonl');fs.writeFileSync(t,'owned');r.transcript=fs.readFileSync(t,'utf8')==='owned'}catch(e){r.transcript=false}
try{fs.readFileSync(foreignTranscript);r.foreign_transcript=false}catch(e){r.foreign_transcript=e.code==='EPERM'}
r.credential_write=refused(path.join(history,'.credentials.json'))&&refused(path.join(project,'.credentials.json.tmp'))&&refused(path.join(history,'.claude.json'));
}
process.stdout.write(JSON.stringify(r));if(!Object.values(r).every(Boolean))process.exitCode=1;
`

func (i ClaudeInstallation) isolated(ctx context.Context, state string, b pa.Bot, l Limits) (Configuration, func(), error) {
	resolved, e := filepath.EvalSymlinks(state)
	if e != nil {
		return Configuration{}, func() {}, refusal(pa.Invalid, "native_state")
	}
	return i.isolatedWithHistory(ctx, resolved, b, l, "")
}

// isolatedWithHistory prepares one execution. The scratch directory is new and
// removed by the returned cleanup; history, when given, is a resolved config
// directory below state that persists across executions and is never removed
// here. Both config variables reach the ACP parent process, because ACP and the
// SDK resolve history from process.env; the empty secure-storage entry keeps
// the native login lookup on its normal service instead of a config hash.
func (i ClaudeInstallation) isolatedWithHistory(ctx context.Context, state string, b pa.Bot, l Limits, history string) (Configuration, func(), error) {
	none := func() {}
	home, e := os.UserHomeDir()
	if e != nil || !filepath.IsAbs(home) {
		return Configuration{}, none, refusal(pa.Invalid, "native_home")
	}
	// The OS account names the existing login item; nothing else is taken
	// from the person's environment.
	current, e := user.Current()
	if e != nil || current.Username == "" || strings.ContainsAny(current.Username, "=\x00\n") {
		return Configuration{}, none, refusal(pa.Invalid, "native_account")
	}
	account := current.Username
	if !sandboxPath(state) || !sandboxPath(home) {
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	native := filepath.Join(state, "project-assistants", "native")
	if history != "" {
		rel, e := filepath.Rel(native, history)
		if e != nil || !sandboxPath(history) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Configuration{}, none, refusal(pa.ScopeDenied, "native_history")
		}
		info, e := os.Lstat(history)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Configuration{}, none, refusal(pa.ScopeDenied, "native_history")
		}
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
	if e != nil || !sandboxPath(owned) {
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
	boundary := claudeBoundary{Home: home, State: state, Owned: owned, Node: i.Node, CLI: i.CLI, Modules: modules, History: history}
	environment := []string{
		"HOME=" + home, "USER=" + account, "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TMPDIR=" + tmp,
		"CLAUDE_CODE_EXECUTABLE=" + i.CLI, "CLAUDE_AGENT_ACP_PROTOCOL=v1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	}
	if history != "" {
		environment = append(environment, "CLAUDE_CONFIG_DIR="+history, "CLAUDE_SECURESTORAGE_CONFIG_DIR=")
	}
	config := Configuration{Executable: "/usr/bin/sandbox-exec", Arguments: []string{"-p", claudeProfile(boundary), i.Node, i.Adapter}, Environment: environment}
	foreign := filepath.Join(root, filepath.Base(owned)+"-denied")
	if e = verifyClaudeIsolation(ctx, config, owned, foreign, history, l); e != nil {
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

// seedHistoryControls writes real host-owned control bytes into the actual
// history config and a sibling foreign transcript, so a denial cannot pass on
// ENOENT. The returned function removes only what it created.
func seedHistoryControls(history string) (string, func(), error) {
	project := filepath.Join(history, "projects", probeProject)
	foreignRoot, e := os.MkdirTemp(filepath.Dir(filepath.Dir(history)), "probe-")
	remove := func() {
		_ = os.RemoveAll(project)
		_ = os.RemoveAll(filepath.Join(history, "memory"))
		if foreignRoot != "" {
			_ = os.RemoveAll(foreignRoot)
		}
	}
	if e != nil {
		foreignRoot = ""
		return "", remove, refusal(pa.Invalid, "native_control")
	}
	foreign := filepath.Join(foreignRoot, "config", "projects", probeProject, "control.jsonl")
	for _, control := range []string{filepath.Join(history, "memory", "personal", "control"), filepath.Join(project, "memory", "control"), foreign} {
		if e = os.MkdirAll(filepath.Dir(control), 0700); e == nil {
			e = os.WriteFile(control, []byte("owned control"), 0600)
		}
		if e != nil {
			remove()
			return "", func() {}, refusal(pa.Invalid, "native_control")
		}
	}
	return foreign, remove, nil
}

func verifyClaudeIsolation(ctx context.Context, config Configuration, owned, foreign, history string, l Limits) error {
	keys := []string{"foreign_write", "foreign_exec", "memory_read", "memory_write", "keychain_read", "login_lookup", "owned_write"}
	args := []string{"-p", config.Arguments[1], config.Arguments[2], "-e", claudeIsolationProbe, owned, foreign}
	credentials := []string{}
	if history != "" {
		transcript, remove, e := seedHistoryControls(history)
		if e != nil {
			return e
		}
		defer remove()
		keys = append(keys, "personal_memory", "project_memory", "transcript", "foreign_transcript", "credential_write")
		args = append(args, history, transcript)
		credentials = []string{filepath.Join(history, ".credentials.json"), filepath.Join(history, ".claude.json")}
	}
	probeCtx, cancel := context.WithTimeout(ctx, l.CleanupTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, config.Executable, args...)
	cmd.Env = config.Environment
	capture := &censusCapture{budget: l.FrameBytes}
	cmd.Stdout = capture
	cmd.Stderr = io.Discard
	e := cmd.Run()
	// A probe that escaped wrote only its marker; remove it before refusing.
	leaked := false
	for _, path := range credentials {
		if data, err := os.ReadFile(path); err == nil {
			leaked = true
			if string(data) == "forbidden" {
				_ = os.Remove(path)
			}
		}
	}
	var result map[string]bool
	if json.Unmarshal(capture.buf.Bytes(), &result) != nil || len(result) != len(keys) {
		return refusal(pa.ReadOnlyUnverified, "native_os_controls")
	}
	for _, key := range keys {
		if !result[key] {
			return refusal(pa.ReadOnlyUnverified, "native_os_"+key)
		}
	}
	if e != nil || leaked {
		return refusal(pa.ReadOnlyUnverified, "native_os_control_exit")
	}
	if _, e = os.Stat(foreign); !os.IsNotExist(e) {
		return refusal(pa.ReadOnlyUnverified, "native_os_controls")
	}
	return nil
}
