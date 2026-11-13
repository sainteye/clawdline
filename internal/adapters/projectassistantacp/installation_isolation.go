package projectassistantacp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// nativePath lets native tools find the ordinary developer programs, as a
// person's own terminal would. Nothing else is taken from the daemon's
// environment: no API key, token, cloud credential or NODE_OPTIONS.
const nativePath = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

func (i ClaudeInstallation) isolated(ctx context.Context, state string, b pa.Bot) (Configuration, func(), error) {
	resolved, e := filepath.EvalSymlinks(state)
	if e != nil {
		return Configuration{}, func() {}, refusal(pa.Invalid, "native_state")
	}
	return i.isolatedWithHistory(ctx, resolved, b, "")
}

// isolatedWithHistory prepares one execution, following CodyNC: the native
// CLI runs as the person, with its own tools, in the Bot's Project, and asks
// before acting. What stays owned here is its configuration: history, when
// given, is a resolved config directory below state that persists across
// executions, so personal Claude history, settings, hooks and memory are not
// read and another Bot's transcripts are elsewhere. The scratch directory is
// new and removed by the returned cleanup. Both config variables reach the ACP
// parent, because ACP and the SDK resolve history from process.env; the empty
// secure-storage entry keeps the login lookup on its normal keychain service.
func (i ClaudeInstallation) isolatedWithHistory(ctx context.Context, state string, b pa.Bot, history string) (Configuration, func(), error) {
	none := func() {}
	if e := ctx.Err(); e != nil {
		return Configuration{}, none, e
	}
	home, e := os.UserHomeDir()
	if e != nil || !filepath.IsAbs(home) {
		return Configuration{}, none, refusal(pa.Invalid, "native_home")
	}
	// The native CLI names its keychain account from USER.
	current, e := user.Current()
	if e != nil || current.Username == "" || strings.ContainsAny(current.Username, "=\x00\n") {
		return Configuration{}, none, refusal(pa.Invalid, "native_account")
	}
	if history != "" {
		native := filepath.Join(state, "project-assistants", "native")
		rel, e := filepath.Rel(native, history)
		if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
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
	cleanup := func() { _ = os.RemoveAll(owned) }
	tmp := filepath.Join(owned, "tmp")
	if e = os.Mkdir(tmp, 0700); e != nil {
		cleanup()
		return Configuration{}, none, refusal(pa.Invalid, "native_state")
	}
	environment := []string{
		"HOME=" + home, "USER=" + current.Username, "PATH=" + nativePath, "LANG=C", "LC_ALL=C", "TMPDIR=" + tmp,
		"CLAUDE_CODE_EXECUTABLE=" + i.CLI, "CLAUDE_AGENT_ACP_PROTOCOL=v1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	}
	if history != "" {
		environment = append(environment, "CLAUDE_CONFIG_DIR="+history, "CLAUDE_SECURESTORAGE_CONFIG_DIR=")
	}
	return Configuration{Executable: i.Node, Arguments: []string{i.Adapter}, Environment: environment}, cleanup, nil
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
