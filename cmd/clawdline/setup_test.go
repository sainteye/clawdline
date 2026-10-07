package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
)

// fakeHost is a setupHost whose commands are recorded and answered by fail.
type fakeHost struct {
	setupHost
	ran  []string
	out  bytes.Buffer
	fail map[string]string
}

func newFakeHost(t *testing.T, goos string) *fakeHost {
	f := &fakeHost{fail: map[string]string{}}
	f.setupHost = setupHost{goos: goos, goarch: "arm64", home: t.TempDir(), uid: 501, username: "someone",
		getenv: os.Getenv,
		run: func(name string, args ...string) ([]byte, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			f.ran = append(f.ran, line)
			for prefix, answer := range f.fail {
				if strings.HasPrefix(line, prefix) {
					return []byte(answer), errors.New("exit status 1")
				}
			}
			if strings.HasSuffix(line, "-p Linger --value") {
				return []byte("no\n"), nil
			}
			return nil, nil
		},
		portAnswers: func(int) bool { return false },
		out:         &f.out, errOut: &f.out,
	}
	return f
}

// fakeRelease is a release unpacked under root/releases/<version>, with its
// archive and a manifest signed by a key of the test's own.
type fakeRelease struct {
	layout   install.Layout
	dir      string
	archive  string
	mdir     string
	key      ed25519.PrivateKey
	manifest []byte
}

func makeRelease(t *testing.T, version string) fakeRelease {
	t.Helper()
	root := t.TempDir()
	l := install.NewLayout(root, filepath.Join(root, "bin"))
	r := fakeRelease{layout: l, dir: l.ReleaseDir(version), mdir: t.TempDir()}
	files := map[string]string{"clawdline": "#!/bin/sh\n", "dist/index.html": "<html>", "dist/BUILD.json": "{}"}
	r.archive = filepath.Join(r.mdir, "clawdline_"+version+"_linux_arm64.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "./" + name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
		p := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(r.archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	m := release.Manifest{Version: version, Commit: strings.Repeat("ab", 20), Channel: "beta",
		Artifacts: []release.Artifact{{OS: "linux", Arch: "arm64", Kind: release.KindDaemon,
			Name: filepath.Base(r.archive), URL: "http://example.invalid/" + filepath.Base(r.archive),
			Size: int64(buf.Len()), SHA256: hex.EncodeToString(sum[:])}}}
	r.manifest, _ = json.MarshalIndent(m, "", "  ")
	_, r.key, _ = ed25519.GenerateKey(rand.Reader)
	sigs, _ := json.Marshal([]release.Signature{release.Sign(r.manifest, r.key)})
	_ = os.WriteFile(filepath.Join(r.mdir, "manifest.json"), r.manifest, 0o644)
	_ = os.WriteFile(filepath.Join(r.mdir, "manifest.sig.json"), sigs, 0o644)
	return r
}

func (r fakeRelease) verify(h setupHost) error {
	h.keys = []ed25519.PublicKey{r.key.Public().(ed25519.PublicKey)}
	h.goos = "linux"
	_, _, err := verifyRelease(h, r.layout, filepath.Join(r.dir, "clawdline"),
		setupOptions{archive: r.archive, manifestDir: r.mdir})
	return err
}

func codeOf(err error) string {
	var r *install.Refusal
	var rel *release.Error
	switch {
	case errors.As(err, &r):
		return r.Code
	case errors.As(err, &rel):
		return rel.Code
	}
	return ""
}

func TestSetupVerifiesTheReleaseBeforeAnything(t *testing.T) {
	h := newFakeHost(t, "linux")
	r := makeRelease(t, "v0.10.0-test.9")
	if err := r.verify(h.setupHost); err != nil {
		t.Fatalf("a signed release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "manifest.sig.json")); err != nil {
		t.Errorf("the manifest is not kept beside the release for a later repair: %v", err)
	}

	cases := map[string]func(r fakeRelease){
		release.CodeManifestUnsigned: func(r fakeRelease) { _ = os.Remove(filepath.Join(r.mdir, "manifest.sig.json")) },
		release.CodeSignatureInvalid: func(r fakeRelease) {
			_ = os.WriteFile(filepath.Join(r.mdir, "manifest.json"), bytes.Replace(r.manifest, []byte("beta"), []byte("stable"), 1), 0o644)
		},
		release.CodeArtifactSHA256: func(r fakeRelease) {
			b, _ := os.ReadFile(r.archive)
			b[len(b)-1] ^= 0xff
			_ = os.WriteFile(r.archive, b, 0o644)
		},
		release.CodeArtifactSize: func(r fakeRelease) {
			b, _ := os.ReadFile(r.archive)
			_ = os.WriteFile(r.archive, append(b, 0), 0o644)
		},
		install.CodeTreeMismatch: func(r fakeRelease) {
			_ = os.WriteFile(filepath.Join(r.dir, "clawdline"), []byte("not the signed one"), 0o755)
		},
		install.CodeSignature + " (version)": func(r fakeRelease) {
			_ = os.Rename(r.dir, r.layout.ReleaseDir("v0.10.0-test.8"))
		},
	}
	for want, breakIt := range cases {
		r := makeRelease(t, "v0.10.0-test.9")
		breakIt(r)
		dir := r.dir
		if strings.Contains(want, "(version)") {
			r.dir = r.layout.ReleaseDir("v0.10.0-test.8")
		}
		code := strings.TrimSuffix(want, " (version)")
		if err := r.verify(h.setupHost); codeOf(err) != code {
			t.Errorf("%s: got %v", want, err)
		}
		_ = dir
	}

	// A binary that is not inside the layout's releases is not a release.
	r = makeRelease(t, "v0.10.0-test.9")
	h.keys = []ed25519.PublicKey{r.key.Public().(ed25519.PublicKey)}
	_, _, err := verifyRelease(h.setupHost, r.layout, "/usr/local/bin/clawdline", setupOptions{})
	if codeOf(err) != install.CodeCurrentNotRelease {
		t.Errorf("a binary outside the layout: %v", err)
	}
}

func TestSystemdServiceIsEnabledRestartedAndLingered(t *testing.T) {
	h := newFakeHost(t, "linux")
	unit := filepath.Join(h.home, ".config", "systemd", "user", "clawdline-next.service")
	spec := install.ServiceSpec{Exec: "/x/current/clawdline", Port: 7727, Path: "/usr/bin", Shell: "/bin/bash", Home: h.home, Autostart: true}
	if err := installSystemdUnit(h.setupHost, unit, "clawdline-next.service", spec); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"systemctl --user show-environment",
		"systemctl --user daemon-reload",
		"systemctl --user enable clawdline-next.service",
		"systemctl --user restart clawdline-next.service",
		"loginctl show-user someone -p Linger --value",
		"loginctl enable-linger someone",
	}
	if strings.Join(h.ran, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(h.ran, "\n"), strings.Join(want, "\n"))
	}
	if b, err := os.ReadFile(unit); err != nil || !strings.Contains(string(b), "KillMode=process") {
		t.Errorf("unit: %v %s", err, b)
	}

	// Linger refused: the exact sudo command, and the install goes on.
	h = newFakeHost(t, "linux")
	h.fail["loginctl enable-linger"] = "Access denied"
	if err := installSystemdUnit(h.setupHost, unit, "clawdline-next.service", spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "    sudo loginctl enable-linger someone\n") {
		t.Errorf("no sudo command:\n%s", h.out.String())
	}

	// --no-autostart: started, not enabled, no linger.
	h = newFakeHost(t, "linux")
	spec.Autostart = false
	if err := installSystemdUnit(h.setupHost, unit, "clawdline-next.service", spec); err != nil {
		t.Fatal(err)
	}
	ran := strings.Join(h.ran, "\n")
	if strings.Contains(ran, "enable clawdline") || strings.Contains(ran, "loginctl") || !strings.Contains(ran, "restart clawdline-next.service") {
		t.Errorf("--no-autostart ran:\n%s", ran)
	}

	// No user manager: refused with what to do, nothing written.
	h = newFakeHost(t, "linux")
	h.fail["systemctl --user show-environment"] = "Failed to connect to bus"
	other := filepath.Join(h.home, "unit")
	err := installSystemdUnit(h.setupHost, other, "clawdline-next.service", spec)
	if codeOf(err) != install.CodeNoUserManager || !strings.Contains(err.Error(), "sudo loginctl enable-linger someone") {
		t.Errorf("no bus: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Error("a unit was written without a user manager to run it")
	}
}

func TestLaunchAgentIsBootstrappedIntoTheDomainFound(t *testing.T) {
	h := newFakeHost(t, "darwin")
	plist := filepath.Join(h.home, "Library", "LaunchAgents", "com.sainteye.clawdline-next.17811.plist")
	spec := install.ServiceSpec{Exec: "/x/current/clawdline", Port: 17811, Path: "/usr/bin", Shell: "/bin/zsh", Home: h.home, Autostart: true}
	domain, gui := launchdDomain(h.setupHost)
	if domain != "gui/501" || !gui {
		t.Fatalf("domain %s %v", domain, gui)
	}
	if err := installLaunchAgent(h.setupHost, plist, "com.sainteye.clawdline-next.17811", domain, spec); err != nil {
		t.Fatal(err)
	}
	want := "launchctl print gui/501\nlaunchctl bootout gui/501/com.sainteye.clawdline-next.17811\nlaunchctl bootstrap gui/501 " + plist
	if strings.Join(h.ran, "\n") != want {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(h.ran, "\n"), want)
	}

	// Over SSH with nobody at the desktop: user/<uid>, and started by hand
	// with --no-autostart.
	h = newFakeHost(t, "darwin")
	h.fail["launchctl print gui/"] = "Could not find domain for port identifier"
	domain, gui = launchdDomain(h.setupHost)
	if domain != "user/501" || gui {
		t.Fatalf("headless domain %s %v", domain, gui)
	}
	spec.Autostart = false
	if err := installLaunchAgent(h.setupHost, plist, "l", domain, spec); err != nil {
		t.Fatal(err)
	}
	if last := h.ran[len(h.ran)-1]; last != "launchctl kickstart user/501/l" {
		t.Errorf("--no-autostart did not start it now: %s", last)
	}
}

func TestUninstallRemovesWhatSetupInstalledAndKeepsState(t *testing.T) {
	h := newFakeHost(t, "linux")
	root := filepath.Join(h.home, "root")
	state := filepath.Join(h.home, "state")
	bin := filepath.Join(h.home, "bin")
	t.Setenv("CLAWDLINE_NEXT_INSTALL_ROOT", root)
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_BIN_DIR", bin)
	p, err := resolvePlace(h.setupHost, 17811)
	if err != nil {
		t.Fatal(err)
	}
	unit := serviceFilePath(h.setupHost, p)
	for _, d := range []string{p.layout.ReleaseDir("v0.10.0"), filepath.Dir(unit), state, bin} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(filepath.Join(p.layout.ReleaseDir("v0.10.0"), "clawdline"), []byte("x"), 0o755)
	_ = p.layout.SwitchCurrent("v0.10.0")
	_ = linkBin(p.layout)
	_ = os.WriteFile(unit, []byte("[Unit]\n"), 0o644)
	// What `systemctl --user edit` leaves: drop-ins for this unit alone.
	_ = os.MkdirAll(unit+".d", 0o755)
	_ = os.WriteFile(filepath.Join(unit+".d", "override.conf"), []byte("[Service]\n"), 0o644)
	_ = os.WriteFile(filepath.Join(state, "local-token"), []byte("t"), 0o600)
	_ = install.WriteServiceFile(state, install.ServiceFile{Supervisor: "systemd", Name: p.serviceName("linux"), Port: 17811, Root: root, File: unit})

	if code := runUninstall(h.setupHost, setupOptions{port: 17811}); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.out.String())
	}
	if !strings.Contains(strings.Join(h.ran, "\n"), "systemctl --user disable --now "+p.serviceName("linux")) {
		t.Errorf("the service was not stopped:\n%s", strings.Join(h.ran, "\n"))
	}
	for _, gone := range []string{root, unit, unit + ".d", p.layout.BinLink, filepath.Join(state, install.ServiceFileName)} {
		if _, err := os.Lstat(gone); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(state, "local-token")); err != nil {
		t.Errorf("the state directory went without --purge: %v", err)
	}
	// The binary is gone, so how to delete the rest needs nothing of it.
	if !strings.Contains(h.out.String(), "kept "+tilde(h.setupHost, state)+" (your devices, sessions and settings). To delete it too: rm -rf "+tilde(h.setupHost, state)) {
		t.Errorf("does not say what it kept and how to delete it:\n%s", h.out.String())
	}

	// A second run finds nothing and says so.
	h.out.Reset()
	_ = runUninstall(h.setupHost, setupOptions{port: 17811})
	if !strings.Contains(h.out.String(), "nothing of this install was found") {
		t.Errorf("second uninstall:\n%s", h.out.String())
	}

	// --purge takes the state directory as well.
	_ = runUninstall(h.setupHost, setupOptions{port: 17811, purge: true})
	if _, err := os.Stat(state); err == nil {
		t.Error("--purge kept the state directory")
	}
}

// --purge with terminals still open keeps the directory their tmux server
// listens in, and says so: removing it strands them, and the line that says
// how to reach them would name a socket that is gone.
func TestPurgeKeepsTheSocketOfTerminalsStillOpen(t *testing.T) {
	h := newFakeHost(t, "linux")
	state := filepath.Join(h.home, "state")
	t.Setenv("CLAWDLINE_NEXT_INSTALL_ROOT", filepath.Join(h.home, "root"))
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_BIN_DIR", filepath.Join(h.home, "bin"))
	sock := filepath.Join(state, "tmux", "term.sock")
	_ = os.MkdirAll(filepath.Dir(sock), 0o700)
	_ = os.WriteFile(sock, nil, 0o600)
	_ = os.WriteFile(filepath.Join(state, "local-token"), []byte("t"), 0o600)
	run := h.run
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "tmux" && strings.Join(args, " ") == "-S "+sock+" ls" {
			return []byte("t-1: 1 windows\n"), nil
		}
		return run(name, args...)
	}

	_ = runUninstall(h.setupHost, setupOptions{port: 17811, purge: true})
	if _, err := os.Stat(sock); err != nil {
		t.Errorf("--purge removed the socket of terminals still open: %v\n%s", err, h.out.String())
	}
	if _, err := os.Stat(filepath.Join(state, "local-token")); err == nil {
		t.Errorf("--purge kept the rest of the state directory:\n%s", h.out.String())
	}
	if !strings.Contains(h.out.String(), "kept "+filepath.Dir(sock)) || !strings.Contains(h.out.String(), "tmux -S "+sock+" ls") {
		t.Errorf("does not say it kept the terminals' directory and how to reach them:\n%s", h.out.String())
	}

	// Once they are closed, --purge takes the rest.
	h.run = run
	h.out.Reset()
	_ = runUninstall(h.setupHost, setupOptions{port: 17811, purge: true})
	if _, err := os.Stat(state); err == nil {
		t.Errorf("--purge kept the state directory with no terminal open:\n%s", h.out.String())
	}
}

func TestUninstallLeavesALinkItDidNotMake(t *testing.T) {
	h := newFakeHost(t, "linux")
	bin := filepath.Join(h.home, "bin")
	t.Setenv("CLAWDLINE_NEXT_INSTALL_ROOT", filepath.Join(h.home, "root"))
	t.Setenv("CLAWDLINE_NEXT_DIR", filepath.Join(h.home, "state"))
	t.Setenv("CLAWDLINE_NEXT_BIN_DIR", bin)
	_ = os.MkdirAll(bin, 0o755)
	mine := filepath.Join(bin, "clawdline")
	_ = os.Symlink("/somewhere/else/clawdline", mine)
	_ = runUninstall(h.setupHost, setupOptions{port: 17811})
	if _, err := os.Lstat(mine); err != nil {
		t.Error("uninstall removed a clawdline link that points outside the install")
	}
}

// A finished install says what to do next: sign in (with the address and the
// SSH forward that reaches it when nothing could open it here), start an
// assistant inside tmux, and turn autostart off or remove it, every command
// with this install's port and the path that runs it.
func TestTheInstallEndsWithWhatToDoNext(t *testing.T) {
	h := newFakeHost(t, "linux")
	h.hostname = "myhost"
	printNext(h.setupHost, setupOptions{port: 7800}, nextStep{version: "v0.10.0", command: "~/.local/bin/clawdline",
		address: "http://127.0.0.1:7800/v1/auth/open#t=k", found: map[string]string{"claude": "/x/claude"}})
	out := h.out.String()
	for _, want := range []string{
		"Clawdline v0.10.0 is running at http://127.0.0.1:7800\n",
		"\nNext:\n  1. Sign in: open this address in a browser on this computer (it contains a key; don't share it)\n       http://127.0.0.1:7800/v1/auth/open#t=k\n",
		"No browser here? On your laptop run   ssh -L 7800:127.0.0.1:7800 someone@myhost\n     then open the same address there.",
		"  2. Start an assistant inside tmux:   tmux new -s work   then run   claude\n",
		"Starts at login. Turn off: ~/.local/bin/clawdline setup --no-autostart --port 7800   Remove: ~/.local/bin/clawdline setup --uninstall --port 7800\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	h.out.Reset()
	printNext(h.setupHost, setupOptions{port: 7727, noAutostart: true}, nextStep{version: "v0.10.0", command: "clawdline",
		opened: "Clawdline Next.app", found: map[string]string{"claude": "/x/claude", "codex": "/x/codex"}})
	out = h.out.String()
	for _, want := range []string{
		"  1. Sign in: Clawdline Next.app just opened the console on this computer.\n     Not in front of you? Run   clawdline open\n",
		"then run   claude   (or codex)\n",
		"Started now, not at login. Turn on: clawdline setup   Remove: clawdline setup --uninstall\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--port") || strings.Contains(out, "ssh -L") {
		t.Errorf("the default port or a desktop install got a --port or an SSH line:\n%s", out)
	}
}

// The PATH hint names the profile file of the shell in $SHELL and the full
// path that works until the new PATH takes effect.
func TestThePathHintNamesTheShellsProfile(t *testing.T) {
	h := newFakeHost(t, "linux")
	bin := filepath.Join(h.home, ".local", "bin")
	for shell, want := range map[string]string{
		"/bin/zsh":          `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`,
		"/bin/bash":         `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc`,
		"/usr/bin/fish":     `fish_add_path ~/.local/bin`,
		"/bin/sh":           `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile`,
		"/opt/odd/bin/tcsh": `>> ~/.profile`,
	} {
		h.getenv = func(k string) string {
			if k == "SHELL" {
				return shell
			}
			return ""
		}
		got := pathHint(h.setupHost, bin)
		if !strings.HasPrefix(got, "~/.local/bin is not on your PATH. Run once, then open a new terminal:  ") || !strings.Contains(got, want) {
			t.Errorf("%s: %s", shell, got)
		}
		if !strings.HasSuffix(got, "Until then, run Clawdline as ~/.local/bin/clawdline") {
			t.Errorf("%s: no full path until PATH takes effect: %s", shell, got)
		}
	}
	h.goos = "darwin"
	h.getenv = func(k string) string { return map[string]string{"SHELL": "/bin/bash"}[k] }
	if got := pathHint(h.setupHost, bin); !strings.Contains(got, ">> ~/.bash_profile") {
		t.Errorf("bash on macOS reads ~/.bash_profile: %s", got)
	}
}
