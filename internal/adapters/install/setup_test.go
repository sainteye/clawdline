package install

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTmuxInstallCommandNamesThePackageManagerPresent(t *testing.T) {
	cases := []struct {
		goos, has, want string
	}{
		{"linux", "apt-get", "sudo apt-get install -y tmux"},
		{"linux", "dnf", "sudo dnf install -y tmux"},
		{"linux", "pacman", "sudo pacman -S --needed tmux"},
		{"linux", "zypper", "sudo zypper install -y tmux"},
		{"linux", "apk", "sudo apk add tmux"},
		{"linux", "brew", "brew install tmux"},
		{"darwin", "brew", "brew install tmux"},
		{"darwin", "", "install Homebrew (https://brew.sh), then run: brew install tmux"},
		{"linux", "", "install tmux with this machine's package manager"},
	}
	for _, c := range cases {
		pm := DetectPackageManager(c.goos, func(n string) bool { return n == c.has })
		if got := TmuxInstallCommand(c.goos, pm); got != c.want {
			t.Errorf("%s with %q: %q, want %q", c.goos, c.has, got, c.want)
		}
	}
	// A Linux machine with both asks its own manager first.
	pm := DetectPackageManager("linux", func(n string) bool { return n == "brew" || n == "dnf" })
	if pm != "dnf" {
		t.Errorf("linux with brew and dnf chose %q", pm)
	}
	// macOS never names apt even when something called apt-get is on PATH.
	if pm := DetectPackageManager("darwin", func(n string) bool { return n == "apt-get" }); pm != "" {
		t.Errorf("darwin chose %q", pm)
	}
}

func TestPrereqsStopOnlyWithoutTmux(t *testing.T) {
	r := Prereqs{Found: map[string]string{"claude": "/x/claude"}, PackageManager: "apt-get"}.Check("linux")
	if !r.Stop {
		t.Fatal("no tmux did not stop setup")
	}
	text := strings.Join(r.Lines, "\n")
	want := "Clawdline needs tmux. Install it with:  sudo apt-get install -y tmux   then run the same install command again. Nothing was installed."
	if r.StopLine != want {
		t.Errorf("the tmux sentence is\n%s\nnot\n%s", r.StopLine, want)
	}
	if got := TmuxMissing("linux", ""); got != "Clawdline needs tmux. Install tmux with this machine's package manager, then run the same install command again. Nothing was installed." {
		t.Errorf("with no package manager: %s", got)
	}
	if !strings.Contains(text, "npm install -g @openai/codex") || strings.Contains(text, "claude.ai/install.sh") {
		t.Errorf("the missing assistant, and only it, is named:\n%s", text)
	}

	r = Prereqs{Found: map[string]string{"tmux": "/usr/bin/tmux"}}.Check("linux")
	if r.Stop {
		t.Fatal("missing assistants stopped setup")
	}
	text = strings.Join(r.Lines, "\n")
	for _, want := range []string{"warning: neither Claude Code nor Codex", "curl -fsSL https://claude.ai/install.sh | bash", "npm install -g @openai/codex"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
}

// A release that carries tmux installs on a machine without one, and on one
// whose tmux is too old; the machine's own, new enough, is still preferred.
func TestACarriedTmuxMeansNoStop(t *testing.T) {
	carried := "/r/libexec/tmux"
	r := Prereqs{Found: map[string]string{"claude": "/x/claude"}, Carried: carried}.Check("linux")
	if r.Stop || !r.TmuxCarried || !strings.Contains(strings.Join(r.Found, "\n"), carried) {
		t.Fatalf("no tmux with one carried: %+v", r)
	}
	r = Prereqs{Found: map[string]string{"tmux": "/usr/bin/tmux"}, TmuxTooOld: "tmux 2.9 is older than tmux 3.0", Carried: carried}.Check("linux")
	if r.Stop || !r.TmuxCarried {
		t.Fatalf("a tmux too old with one carried: %+v", r)
	}
	r = Prereqs{Found: map[string]string{"tmux": "/usr/bin/tmux"}, Carried: carried}.Check("linux")
	if r.Stop || r.TmuxCarried {
		t.Fatalf("the machine's tmux was passed over: %+v", r)
	}
	r = Prereqs{Found: map[string]string{"tmux": "/usr/bin/tmux"}, TmuxTooOld: "too old"}.Check("linux")
	if r.Stop || r.TmuxCarried {
		t.Fatalf("a tmux too old with nothing carried: %+v", r)
	}
}

func TestServicePathPutsFoundToolsFirstOnce(t *testing.T) {
	got := ServicePath(map[string]string{
		"tmux":   "/opt/homebrew/bin/tmux",
		"claude": "/home/user/.local/bin/claude",
		"codex":  "/home/user/.nvm/versions/node/v22/bin/codex",
	}, ExtraToolDirs("/home/user"))
	want := "/opt/homebrew/bin:/home/user/.local/bin:/home/user/.nvm/versions/node/v22/bin:/usr/local/bin:" +
		"/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
	if got != want {
		t.Errorf("PATH\n got %s\nwant %s", got, want)
	}
	if !OnPath("/a:/home/user/.local/bin/:/b", "/home/user/.local/bin") || OnPath("/a:/b", "/home/user/.local/bin") {
		t.Error("OnPath")
	}
}

func TestServiceNamesTakeASuffixAwayFromTheRealInstall(t *testing.T) {
	if s := ServiceSuffix(7727, "/home/user/.local/share/clawdline-next", false); s != "" || UnitName(s) != "clawdline-next.service" || LaunchdLabel(s) != "com.sainteye.clawdline-next" {
		t.Errorf("the real install is suffixed %q", s)
	}
	s := ServiceSuffix(17811, "/tmp/x", false)
	if s != "-17811" || UnitName(s) != "clawdline-next-17811.service" || LaunchdLabel(s) != "com.sainteye.clawdline-next.17811" {
		t.Errorf("port suffix %q", s)
	}
	a, b := ServiceSuffix(7727, "/tmp/a", true), ServiceSuffix(7727, "/tmp/b", true)
	if a == "" || a == b {
		t.Errorf("named roots %q %q", a, b)
	}
}

func spec() ServiceSpec {
	return ServiceSpec{Exec: "/home/user/.local/share/clawdline-next/current/clawdline", Port: 7727,
		Path: "/home/user/.local/bin:/usr/bin", Shell: "/bin/bash", Home: "/home/user", Autostart: true}
}

func TestSystemdUnitKeepsTheExistingUnitsShape(t *testing.T) {
	u := SystemdUnit(spec())
	for _, want := range []string{
		"ExecStart=/home/user/.local/share/clawdline-next/current/clawdline serve\n",
		"KillMode=process\n", "Restart=on-failure\n", "UMask=0077\n", "WantedBy=default.target\n",
		`Environment=PATH="/home/user/.local/bin:/usr/bin"` + "\n",
		`Environment=SHELL="/bin/bash"` + "\n",
		`Environment=CLAWDLINE_NEXT_PORT="7727"` + "\n",
		`Environment=CLAWDLINE_NEXT_STANDALONE="1"` + "\n",
		`Environment=CLAWDLINE_NEXT_OWN_SESSIONS="1"` + "\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
	// Only what setup resolved: never the console path the caller's
	// environment carried, and no state dir or root for the real install.
	for _, never := range []string{"CLAWDLINE_NEXT_WEB", "CLAWDLINE_NEXT_DIR", "CLAWDLINE_NEXT_INSTALL_ROOT"} {
		if strings.Contains(u, never) {
			t.Errorf("unit names %s:\n%s", never, u)
		}
	}
	s := spec()
	s.StateDir, s.InstallRoot = "/tmp/state 1", "/tmp/root%"
	u = SystemdUnit(s)
	if !strings.Contains(u, `Environment=CLAWDLINE_NEXT_DIR="/tmp/state 1"`) || !strings.Contains(u, `Environment=CLAWDLINE_NEXT_INSTALL_ROOT="/tmp/root%%"`) {
		t.Errorf("isolated unit:\n%s", u)
	}
}

func TestLaunchdPlistAutostartAndNot(t *testing.T) {
	s := spec()
	s.LogPath = "/tmp/s/service.log"
	p := LaunchdPlist("com.sainteye.clawdline-next.17811", s)
	for _, want := range []string{
		"<key>Label</key>\n\t<string>com.sainteye.clawdline-next.17811</string>",
		"<string>/home/user/.local/share/clawdline-next/current/clawdline</string>\n\t\t<string>serve</string>",
		"<key>RunAtLoad</key>\n\t<true/>\n\t<key>KeepAlive</key>\n\t<true/>",
		"<key>ThrottleInterval</key>\n\t<integer>30</integer>",
		"<key>AbandonProcessGroup</key>\n\t<true/>",
		"<key>CLAWDLINE_NEXT_STANDALONE</key>\n\t\t<string>1</string>",
		"<key>CLAWDLINE_NEXT_OWN_SESSIONS</key>\n\t\t<string>1</string>",
		"<key>CLAWDLINE_NEXT_PORT</key>\n\t\t<string>7727</string>",
		"<key>PATH</key>\n\t\t<string>/home/user/.local/bin:/usr/bin</string>",
		"<key>StandardErrorPath</key>\n\t<string>/tmp/s/service.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q:\n%s", want, p)
		}
	}
	s.Autostart = false
	p = LaunchdPlist("x", s)
	if !strings.Contains(p, "<key>RunAtLoad</key>\n\t<false/>\n\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>Crashed</key>\n\t\t<true/>") {
		t.Errorf("no-autostart plist:\n%s", p)
	}
	s.Path = "/a&b<c"
	if p := LaunchdPlist("x", s); !strings.Contains(p, "/a&amp;b&lt;c") {
		t.Error("plist values are not escaped")
	}
}

func TestExistingInstallRefusals(t *testing.T) {
	name := "clawdline-next.service"
	cases := []struct {
		label string
		e     Existing
		adopt bool
		code  string
	}{
		{"fresh machine", Existing{Port: 7727, ServiceName: name}, false, ""},
		{"a dev daemon or the app holds the port", Existing{Port: 7727, PortAnswers: true, ServiceName: name}, false, CodePortHeld},
		{"adopt does not take over an unknown daemon", Existing{Port: 7727, PortAnswers: true, ServiceName: name}, true, CodePortHeld},
		{"a unit file alone is not this layout's service without --adopt", Existing{Port: 7727, PortAnswers: true,
			ServiceName: name, ServiceInstalled: true}, false, CodePortHeld},
		{"our own service answering is a repair", Existing{Port: 7727, PortAnswers: true, ServiceName: name,
			Service: &ServiceFile{Supervisor: "systemd", Name: name, Port: 7727}}, false, ""},
		{"a service on another port is not ours", Existing{Port: 7727, PortAnswers: true, ServiceName: name,
			Service: &ServiceFile{Supervisor: "systemd", Name: name, Port: 7800}}, false, CodePortHeld},
		{"a source deploy without --adopt", Existing{Port: 7727, PortAnswers: true, ServiceName: name, ServiceInstalled: true,
			Current: strings.Repeat("a", 40), CurrentKind: KindSourceDeploy}, false, CodeSourceDeploy},
		{"a source deploy with --adopt", Existing{Port: 7727, PortAnswers: true, ServiceName: name, ServiceInstalled: true,
			Current: strings.Repeat("a", 40), CurrentKind: KindSourceDeploy}, true, ""},
		{"an older release upgrades", Existing{Port: 7727, ServiceName: name, Current: "v0.9.0", CurrentKind: KindRelease}, false, ""},
	}
	for _, c := range cases {
		err := c.e.Decide(c.adopt)
		var r *Refusal
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%s: refused: %v", c.label, err)
		case c.code != "" && (!errors.As(err, &r) || r.Code != c.code):
			t.Errorf("%s: %v, want %s", c.label, err, c.code)
		}
	}
	err := Existing{Port: 7727, PortAnswers: true}.Decide(false)
	if !strings.Contains(err.Error(), "--port") || !strings.Contains(err.Error(), "Quit it first") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
}

func writeArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if target, ok := strings.CutPrefix(body, "->"); ok {
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777})
			continue
		}
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func TestVerifyTreeRefusesAFileThatIsNotTheSignedOne(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeArchive(t, archive, map[string]string{"./clawdline": "binary", "./dist/index.html": "<html>"})
	tree := filepath.Join(dir, "tree")
	if err := Extract(archive, tree); err != nil {
		t.Fatal(err)
	}
	if err := VerifyTree(archive, tree); err != nil {
		t.Fatalf("the unpacked tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tree, "clawdline"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	var r *Refusal
	if err := VerifyTree(archive, tree); !errors.As(err, &r) || r.Code != CodeTreeMismatch {
		t.Fatalf("a changed binary: %v", err)
	}
	os.Remove(filepath.Join(tree, "clawdline"))
	if err := VerifyTree(archive, tree); !errors.As(err, &r) || r.Code != CodeTreeMismatch {
		t.Fatalf("a missing binary: %v", err)
	}
}

func TestExtractRefusesEntriesThatLeave(t *testing.T) {
	dir := t.TempDir()
	for label, files := range map[string]map[string]string{
		"climbing path": {"../evil": "x"},
		"absolute link": {"link": "->/etc/passwd"},
		"climbing link": {"link": "->../../outside"},
	} {
		archive := filepath.Join(dir, strings.ReplaceAll(label, " ", "-")+".tar.gz")
		writeArchive(t, archive, files)
		if err := Extract(archive, filepath.Join(dir, "out-"+filepath.Base(archive))); err == nil {
			t.Errorf("%s was unpacked", label)
		}
	}
	archive := filepath.Join(dir, "ok.tar.gz")
	writeArchive(t, archive, map[string]string{"App.app/Contents/MacOS/x": "bin", "App.app/Current": "->Contents"})
	if err := Extract(archive, filepath.Join(dir, "ok")); err != nil {
		t.Fatalf("an inside link: %v", err)
	}
}
