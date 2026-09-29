package owned

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// What a person's shell is started with (plan v3 D1).
//
// **Nothing of this daemon's own environment reaches the shell except by
// name.** A daemon started by an assistant carries `CODEX_THREAD_ID`,
// `LC_ALL=C`, `NO_COLOR`, `ITERM_SESSION_ID` and more; a shell that inherited
// them would think it was inside that assistant or that terminal. So the pane
// runs `env -i <whitelist> $SHELL -l`, and whatever the tmux server itself
// inherited — from this daemon or from whoever started it first — stops at
// `env -i`. That also clears the `TMUX` and `TMUX_PANE` tmux sets in a pane:
// a `tmux kill-server` typed in a terminal then reaches the person's own
// server, not this one.
//
// The login shell rebuilds the rest from the person's own profile.

// passedThrough are taken from this daemon as they are, when set.
var passedThrough = []string{"HOME", "USER", "LOGNAME", "SHELL", "PATH", "TMPDIR", "SSH_AUTH_SOCK"}

// fallbackLang is the locale when neither this daemon nor the machine names a
// UTF-8 one. A Linux machine may not have it — a minimal Debian has only
// C.UTF-8, and a LANG naming a missing locale leaves the shell in C while it
// says UTF-8 — so there the C library's own UTF-8 locale comes next.
const (
	fallbackLang  = "en_US.UTF-8"
	fallbackLangC = "C.UTF-8"
)

// Machine is what the environment is read from. Every field has a working
// zero value; they exist so a test can be a machine it is not.
type Machine struct {
	GOOS   string
	Getenv func(string) string
	// AppleLocale is macOS's `AppleLocale` (`zh_TW`, `en_US@rg=twzzzz`), or
	// empty. Read the way internal/adapters/whisper/locale.go reads it.
	AppleLocale func(ctx context.Context) string
	// LocaleExists is whether a locale name is installed.
	LocaleExists func(name string) bool
	// Account is the daemon's own user, for a daemon started with no USER or
	// HOME in its environment.
	Account func() (*user.User, error)
	// LoginShell is the account's shell from the user database.
	LoginShell func(ctx context.Context, name string) string
}

func (m Machine) goos() string {
	if m.GOOS != "" {
		return m.GOOS
	}
	return runtime.GOOS
}

func (m Machine) getenv(key string) string {
	if m.Getenv != nil {
		return m.Getenv(key)
	}
	return os.Getenv(key)
}

// paneEnv is the whole environment a new shell starts with, as KEY=VALUE, and
// the shell to run.
func (m Machine) paneEnv(ctx context.Context) (env []string, shell string) {
	values := map[string]string{}
	for _, key := range passedThrough {
		if v := m.getenv(key); v != "" {
			values[key] = v
		}
	}
	if values["HOME"] == "" || values["USER"] == "" || values["LOGNAME"] == "" {
		account := m.Account
		if account == nil {
			account = user.Current
		}
		if u, err := account(); err == nil {
			fill(values, "HOME", u.HomeDir)
			fill(values, "USER", u.Username)
			fill(values, "LOGNAME", u.Username)
		}
	}
	if values["SHELL"] == "" {
		values["SHELL"] = m.loginShell(ctx, values["USER"])
	}
	fill(values, "PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	values["LANG"] = m.lang(ctx)
	values["TERM"] = "tmux-256color"
	values["COLORTERM"] = "truecolor"

	for _, key := range append(append([]string{}, passedThrough...), "LANG", "TERM", "COLORTERM") {
		if v, ok := values[key]; ok {
			env = append(env, key+"="+v)
		}
	}
	return env, values["SHELL"]
}

func fill(values map[string]string, key, value string) {
	if values[key] == "" && value != "" {
		values[key] = value
	}
}

// lang is the shell's LANG: this daemon's own when it is UTF-8; else on macOS
// the machine's region (`AppleLocale`) as `<locale>.UTF-8` when that locale is
// installed; else en_US.UTF-8, or C.UTF-8 where en_US.UTF-8 is not installed.
// A daemon started from the desktop has no LANG at all, and a shell with none
// reads `中` as three unknown bytes.
//
// LC_ALL is never passed: it overrides everything the person's profile says.
func (m Machine) lang(ctx context.Context) string {
	if v := m.getenv("LANG"); isUTF8(v) {
		return v
	}
	exists := m.LocaleExists
	if exists == nil {
		exists = localeInstalled
	}
	if m.goos() == "darwin" {
		read := m.AppleLocale
		if read == nil {
			read = readAppleLocale
		}
		if name := appleLocaleName(read(ctx)); name != "" && exists(name) {
			return name
		}
		return fallbackLang
	}
	if !exists(fallbackLang) && exists(fallbackLangC) {
		return fallbackLangC
	}
	return fallbackLang
}

func isUTF8(v string) bool {
	lower := strings.ToLower(v)
	return strings.HasSuffix(lower, ".utf-8") || strings.HasSuffix(lower, ".utf8")
}

// appleLocaleName turns `en_US@rg=twzzzz` or `zh-Hant_TW` into a POSIX name
// with UTF-8: `en_US.UTF-8`, `zh_TW.UTF-8`.
func appleLocaleName(raw string) string {
	v := strings.TrimSpace(raw)
	if i := strings.IndexAny(v, "@."); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return ""
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) < 2 {
		return ""
	}
	// A script subtag (`Hant`) is not part of a POSIX locale name.
	return parts[0] + "_" + parts[len(parts)-1] + ".UTF-8"
}

func readAppleLocale(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// localeInstalled is whether the C library has the locale. macOS keeps each
// one as a directory; glibc answers `locale -a`, spelling `UTF-8` as `utf8`.
// A machine that cannot be asked is taken as having it: the answer only
// chooses between two UTF-8 names.
func localeInstalled(name string) bool {
	if runtime.GOOS == "darwin" {
		info, err := os.Stat(filepath.Join("/usr/share/locale", name))
		return err == nil && info.IsDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "locale", "-a").Output()
	if err != nil {
		return true
	}
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "") }
	for _, have := range strings.Split(string(out), "\n") {
		if norm(have) == norm(name) {
			return true
		}
	}
	return false
}

// loginShell is the account's shell from the user database, for a daemon
// started without SHELL.
func (m Machine) loginShell(ctx context.Context, name string) string {
	read := m.LoginShell
	if read == nil {
		read = userShell
	}
	if shell := read(ctx, name); shell != "" {
		return shell
	}
	if m.goos() == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func userShell(ctx context.Context, name string) string {
	if name == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/usr/bin/dscl", ".", "-read", "/Users/"+name, "UserShell").Output()
		if err != nil {
			return ""
		}
		// `UserShell: /bin/zsh`
		if _, shell, ok := strings.Cut(strings.TrimSpace(string(out)), ":"); ok {
			return strings.TrimSpace(shell)
		}
		return ""
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Split(scan.Text(), ":")
		if len(fields) == 7 && fields[0] == name {
			return fields[6]
		}
	}
	return ""
}

// clientEnv is the environment the tmux client runs with. The server a client
// starts takes its locale from it, and a server whose locale is `C` measures
// `中` as two unknown bytes rather than one wide character; so LC_ALL and
// LC_CTYPE go and LANG is the shell's. What else the server inherits never
// reaches a shell (paneEnv).
func clientEnv(base []string, lang string) []string {
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "LC_ALL", "LC_CTYPE", "LANG", "TMUX", "TMUX_PANE":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "LANG="+lang)
}
