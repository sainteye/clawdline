package owned

import (
	"context"
	"errors"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func envMachine(goos string, vars map[string]string) Machine {
	return Machine{
		GOOS:         goos,
		Getenv:       func(k string) string { return vars[k] },
		AppleLocale:  func(context.Context) string { return "" },
		LocaleExists: func(string) bool { return true },
		Account:      func() (*user.User, error) { return nil, errors.New("no account in a test") },
		LoginShell:   func(context.Context, string) string { return "" },
	}
}

func asMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// A daemon started by an assistant carries that assistant's variables and
// LC_ALL=C. None of it reaches the shell but the whitelist.
func TestTheShellGetsOnlyTheWhitelist(t *testing.T) {
	daemon := map[string]string{
		"HOME": "/home/alice", "USER": "p", "LOGNAME": "p", "SHELL": "/bin/zsh", "PATH": "/usr/bin:/bin",
		"TMPDIR": "/tmp/p", "SSH_AUTH_SOCK": "/tmp/agent.sock", "LANG": "zh_TW.UTF-8",
		"LC_ALL": "C", "CODEX_THREAD_ID": "x", "CODEX_CI": "1", "NO_COLOR": "1",
		"ITERM_SESSION_ID": "w0", "TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/tmux-1/default,1,0", "TMUX_PANE": "%3",
	}
	env, shell := envMachine("linux", daemon).paneEnv(context.Background())
	got := asMap(env)
	want := map[string]string{
		"HOME": "/home/alice", "USER": "p", "LOGNAME": "p", "SHELL": "/bin/zsh", "PATH": "/usr/bin:/bin",
		"TMPDIR": "/tmp/p", "SSH_AUTH_SOCK": "/tmp/agent.sock", "LANG": "zh_TW.UTF-8",
		"TERM": "tmux-256color", "COLORTERM": "truecolor",
	}
	if len(got) != len(want) {
		t.Errorf("the shell gets %d variables, not %d: %v", len(got), len(want), env)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s=%q, want %q", k, got[k], v)
		}
	}
	if shell != "/bin/zsh" {
		t.Errorf("shell %q", shell)
	}
}

// A daemon started from the desktop has HOME and a short PATH and nothing
// else. Its shell still gets a user, a shell and a UTF-8 locale.
func TestADesktopLaunchedDaemonStillGivesAUTF8Shell(t *testing.T) {
	m := envMachine("darwin", map[string]string{"HOME": "/Users/alice", "PATH": "/usr/bin:/bin"})
	m.Account = func() (*user.User, error) { return &user.User{Username: "p", HomeDir: "/Users/alice"}, nil }
	m.LoginShell = func(_ context.Context, name string) string {
		if name == "p" {
			return "/bin/zsh"
		}
		return ""
	}
	m.AppleLocale = func(context.Context) string { return "zh-Hant_TW\n" }
	env, shell := m.paneEnv(context.Background())
	got := asMap(env)
	if got["USER"] != "p" || got["LOGNAME"] != "p" || shell != "/bin/zsh" || got["SHELL"] != "/bin/zsh" {
		t.Errorf("%v %q", env, shell)
	}
	if got["LANG"] != "zh_TW.UTF-8" {
		t.Errorf("LANG=%q", got["LANG"])
	}
	if _, ok := got["LC_ALL"]; ok {
		t.Error("LC_ALL reached the shell")
	}
	if _, ok := got["SSH_AUTH_SOCK"]; ok {
		t.Error("an unset SSH_AUTH_SOCK was passed as empty")
	}
}

func TestLang(t *testing.T) {
	cases := []struct {
		name, goos, lang, apple string
		installed               bool
		want                    string
	}{
		{"the daemon's UTF-8 LANG", "darwin", "en_GB.UTF-8", "zh_TW", true, "en_GB.UTF-8"},
		{"utf8 spelled short", "linux", "de_DE.utf8", "", true, "de_DE.utf8"},
		{"a LANG that is not UTF-8 is passed over", "darwin", "C", "zh_TW", true, "zh_TW.UTF-8"},
		{"a region override is dropped", "darwin", "", "en_US@rg=twzzzz", true, "en_US.UTF-8"},
		{"a locale this machine does not have", "darwin", "", "en_TW", false, fallbackLang},
		{"no AppleLocale", "darwin", "", "", true, fallbackLang},
		{"AppleLocale is macOS's only", "linux", "", "zh_TW", true, fallbackLang},
		{"a bare language names no locale", "darwin", "", "en", true, fallbackLang},
		{"a Linux with only C.UTF-8", "linux", "", "", false, fallbackLangC},
	}
	for _, c := range cases {
		m := envMachine(c.goos, map[string]string{"LANG": c.lang})
		m.AppleLocale = func(context.Context) string { return c.apple }
		m.LocaleExists = func(name string) bool { return c.installed || name == fallbackLangC }
		if got := m.lang(context.Background()); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTheShellFallsBackPerPlatform(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "/bin/zsh", "linux": "/bin/sh"} {
		if got := envMachine(goos, nil).loginShell(context.Background(), "p"); got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}
}

// The tmux client drops what would give the server a C locale or tie it to
// another tmux.
func TestTheClientEnvironment(t *testing.T) {
	got := asMap(clientEnv([]string{"PATH=/bin", "LC_ALL=C", "LC_CTYPE=C", "LANG=C", "TMUX=x", "TMUX_PANE=%1", "CODEX_X=1"}, "en_US.UTF-8"))
	if got["LANG"] != "en_US.UTF-8" || got["PATH"] != "/bin" {
		t.Errorf("%v", got)
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "TMUX", "TMUX_PANE"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s reached the client", k)
		}
	}
}

func TestParseFrame(t *testing.T) {
	at := time.Unix(100, 0)
	head := "CLT1|3|1|1|bar|1|1|0|1|1|0|1|0|10|3|0"
	f, err := parseFrame(head+"\n$ ls\n\x1b[31mred\x1b[39m\n", at)
	if err != nil {
		t.Fatal(err)
	}
	if f.Cols != 10 || f.Rows != 3 || len(f.Lines) != 3 || f.Lines[1] != "\x1b[31mred\x1b[39m" || f.Lines[2] != "" {
		t.Errorf("%+v", f)
	}
	want := terminal.Cursor{X: 3, Y: 1, Visible: true, Shape: terminal.CursorBar, Blinking: true}
	if f.Cursor != want {
		t.Errorf("cursor %+v", f.Cursor)
	}
	if !f.Modes.AppCursor || f.Modes.AppKeypad || f.Modes.Mouse != terminal.MouseButton || !f.Modes.MouseSGR || f.Modes.Alt {
		t.Errorf("modes %+v", f.Modes)
	}
	if !f.At.Equal(at) || len(f.Rev) != 16 {
		t.Errorf("at %v rev %q", f.At, f.Rev)
	}

	// The revision is the screen, not the moment.
	again, _ := parseFrame(head+"\n$ ls\n\x1b[31mred\x1b[39m\n", at.Add(time.Second))
	moved, _ := parseFrame("CLT1|4|1|1|bar|1|1|0|1|1|0|1|0|10|3|0\n$ ls\n\x1b[31mred\x1b[39m\n", at)
	if again.Rev != f.Rev || moved.Rev == f.Rev {
		t.Errorf("rev %s %s %s", f.Rev, again.Rev, moved.Rev)
	}

	// Mouse: the most reporting asked for wins; none is none.
	for fields, want := range map[string]terminal.MouseMode{
		"0|0|0": terminal.MouseNone, "1|0|0": terminal.MouseStandard, "1|1|0": terminal.MouseButton, "1|1|1": terminal.MouseAny,
	} {
		f, err := parseFrame("CLT1|0|0|1||0|0|0|"+fields+"|0|0|80|1|0\n\n", at)
		if err != nil || f.Modes.Mouse != want || f.Cursor.Shape != "" {
			t.Errorf("%s: %v %+v", fields, err, f.Modes)
		}
	}

	for _, bad := range []string{"", "CLT1|1|2\n", "XXXX|0|0|1||0|0|0|0|0|0|0|0|80|1|0\n", "CLT1|a|0|1||0|0|0|0|0|0|0|0|80|1|0\n"} {
		if _, err := parseFrame(bad, at); err == nil {
			t.Errorf("%q was read as a frame", bad)
		}
	}
}

func TestParseList(t *testing.T) {
	id := terminal.NewID()
	other := terminal.NewID()
	out := strings.Join([]string{
		"CLT1|" + id.SessionName() + "|" + string(id) + "|prj_1|2026-09-29T10:00:00Z|120|40|0|/work/a|b",
		// Made by hand on this server: not ours.
		"CLT1|scratch||||80|24|0|/tmp",
		// A name and an id that do not belong together.
		"CLT1|" + id.SessionName() + "|" + string(other) + "|prj_1||80|24|0|/tmp",
		"CLT1|" + other.SessionName() + "|" + string(other) + "|prj_2||80|24|1|/tmp",
	}, "\n") + "\n"
	got := parseList(out)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].ID != id || got[0].ProjectID != "prj_1" || got[0].Cols != 120 || got[0].Rows != 40 ||
		got[0].Dir != "/work/a|b" || got[0].Status != terminal.Running || got[0].Created.IsZero() {
		t.Errorf("%+v", got[0])
	}
	if got[1].Status != terminal.Exited {
		t.Errorf("%+v", got[1])
	}
}
