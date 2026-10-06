package install

import (
	"path/filepath"
	"strings"
)

// The prerequisites `setup` checks before it activates anything. tmux is the
// one the daemon cannot run sessions without, so its absence stops setup with
// the exact command for the package manager this machine actually has; a
// missing assistant is a warning, because either one is enough and the person
// may add the other later. Nothing here installs anything.

// PackageManagers is every manager setup knows how to name, in the order they
// are looked for. A Linux machine with Homebrew on it is still asked for its
// own package manager first: that is where a system tmux comes from.
var PackageManagers = []string{"apt-get", "dnf", "pacman", "zypper", "apk", "brew"}

// DetectPackageManager is the first of PackageManagers that look finds, or ""
// when none is there. On macOS only brew is asked for.
func DetectPackageManager(goos string, look func(string) bool) string {
	if goos == "darwin" {
		if look("brew") {
			return "brew"
		}
		return ""
	}
	for _, pm := range PackageManagers {
		if look(pm) {
			return pm
		}
	}
	return ""
}

// TmuxInstallCommand is the one command that installs tmux with pm. With no
// package manager found it says what to do instead.
func TmuxInstallCommand(goos, pm string) string {
	switch pm {
	case "brew":
		return "brew install tmux"
	case "apt-get":
		return "sudo apt-get install -y tmux"
	case "dnf":
		return "sudo dnf install -y tmux"
	case "pacman":
		return "sudo pacman -S --needed tmux"
	case "zypper":
		return "sudo zypper install -y tmux"
	case "apk":
		return "sudo apk add tmux"
	}
	if goos == "darwin" {
		return `install Homebrew (https://brew.sh), then run: brew install tmux`
	}
	return "install tmux with this machine's package manager"
}

// Assistant is one assistant Clawdline runs, and the official way to install
// it.
type Assistant struct {
	Name    string
	Command string
	Install string
}

// Assistants are the assistants setup looks for.
var Assistants = []Assistant{
	{Name: "Claude Code", Command: "claude", Install: "curl -fsSL https://claude.ai/install.sh | bash"},
	{Name: "Codex", Command: "codex", Install: "npm install -g @openai/codex"},
}

// Prereqs is what setup found.
type Prereqs struct {
	// Found maps a command (tmux, claude, codex) to the path it was found at.
	Found map[string]string
	// PackageManager is the one DetectPackageManager chose, or "".
	PackageManager string
}

// Report is the prerequisite verdict: Stop is set when setup must not go on,
// and Lines are what is said either way.
type Report struct {
	Stop  bool
	Lines []string
}

// Check turns what was found into what setup says.
func (p Prereqs) Check(goos string) Report {
	var r Report
	if path, ok := p.Found["tmux"]; ok {
		r.Lines = append(r.Lines, "tmux: "+path)
	} else {
		r.Stop = true
		r.Lines = append(r.Lines, "tmux is not installed, and Clawdline runs every session inside it. Install it with:",
			"    "+TmuxInstallCommand(goos, p.PackageManager),
			"then run the installer again.")
	}
	var missing []Assistant
	for _, a := range Assistants {
		if path, ok := p.Found[a.Command]; ok {
			r.Lines = append(r.Lines, a.Name+": "+path)
		} else {
			missing = append(missing, a)
		}
	}
	if len(missing) == len(Assistants) {
		r.Lines = append(r.Lines, "warning: neither Claude Code nor Codex is installed; Clawdline needs one of them to start an assistant. Install either:")
	}
	for _, a := range missing {
		if len(missing) < len(Assistants) {
			r.Lines = append(r.Lines, "note: "+a.Name+" is not installed (optional while "+other(a).Name+" is). To add it:")
		}
		r.Lines = append(r.Lines, "    "+a.Install+"    # "+a.Name)
	}
	return r
}

func other(a Assistant) Assistant {
	for _, b := range Assistants {
		if b.Command != a.Command {
			return b
		}
	}
	return a
}

// SystemPath is the PATH every generated service ends with.
const SystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// ExtraToolDirs are the places tools are commonly installed outside a
// service's minimal PATH: the per-user bin Claude Code installs into, and
// Homebrew's two prefixes.
func ExtraToolDirs(home string) []string {
	return []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"}
}

// ServicePath is the PATH written into the unit or plist: the directory of
// each tool that was found, then the extra directories, then the system PATH,
// each once and in that order. A service started by systemd or launchd gets
// none of the person's shell PATH, so a tmux from Homebrew or a claude in
// ~/.local/bin would otherwise be invisible to the daemon.
func ServicePath(found map[string]string, extra []string) string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		d = filepath.Clean(d)
		if d == "" || d == "." || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	for _, cmd := range []string{"tmux", "claude", "codex"} {
		if p, ok := found[cmd]; ok {
			add(filepath.Dir(p))
		}
	}
	for _, d := range extra {
		add(d)
	}
	for _, d := range strings.Split(SystemPath, ":") {
		add(d)
	}
	return strings.Join(dirs, ":")
}

// OnPath is whether dir is one of the entries of path.
func OnPath(path, dir string) bool {
	dir = filepath.Clean(dir)
	for _, d := range filepath.SplitList(path) {
		if d != "" && filepath.Clean(d) == dir {
			return true
		}
	}
	return false
}
