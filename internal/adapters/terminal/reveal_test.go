package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// fixtureTmux is a tmux that answers `list-clients` from a file of rows and
// `display-message` with one session name, and says yes to everything else.
//
// A fixture rather than a server because the fact under test is a *client*, and
// attaching one needs a terminal to attach it to: the rows below are what tmux
// 3.6a printed on this Mac for the two shapes — an iTerm2 `tmux -CC` client and
// an ordinary `tmux attach` typed into an iTerm2 tab. Nothing here reaches
// iTerm2: `Reveal`'s tail is a separate function and is not called.
func fixtureTmux(t *testing.T, rows []string, paneSession string, refuse bool) *Tmux {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "clients")
	if err := os.WriteFile(data, []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fail := "0"
	if refuse {
		fail = "1"
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do\n" +
		"  if [ \"$a\" = list-clients ]; then [ " + fail + " = 1 ] && exit 1; cat " + data + "; exit 0; fi\n" +
		"  if [ \"$a\" = display-message ]; then printf '%s\\n' '" + paneSession + "'; exit 0; fi\n" +
		"done\nexit 0\n"
	bin := filepath.Join(dir, "tmux")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return &Tmux{Binary: bin}
}

func row(tty, flags, sess string) string {
	return strings.Join([]string{tty, flags, sess}, paneSeparator)
}

// An ordinary `tmux attach` is a person looking at a screen. Reading only
// control-mode clients as clients is what made the whole reveal tail do nothing
// on the commonest setup there is.
func TestAnOrdinaryAttachIsAClientAndAMirrorIsToldApartFromIt(t *testing.T) {
	tmux := fixtureTmux(t, []string{
		row("/dev/ttys047", "attached,UTF-8", "clawdline"),
		row("/dev/ttys011", "attached,focused,control-mode,wait-exit,pause-after=0,UTF-8", "other"),
		row("", "attached,UTF-8", "clawdline"),
	}, "clawdline", false)

	clients, known := tmux.attachedClients(context.Background())
	if !known || len(clients) != 3 {
		t.Fatalf("clients=%+v known=%v, want the three rows", clients, known)
	}
	if clients[0].control || clients[0].tty != "/dev/ttys047" || clients[0].session != "clawdline" {
		t.Errorf("a plain attach read as %+v", clients[0])
	}
	if !clients[1].control {
		t.Errorf("a control-mode client read as %+v", clients[1])
	}
	watching := clientsWatching("clawdline", clients)
	if len(watching) != 2 {
		t.Fatalf("watching %+v, want both clients on this session and neither on the other", watching)
	}
	if len(controlModeOnly(watching)) != 0 {
		t.Errorf("a plain attach was taken for a mirror: %+v", controlModeOnly(watching))
	}
	// An unknown pane session is nobody, never everybody.
	if got := clientsWatching("", clients); len(got) != 0 {
		t.Errorf("an unnamed session matched %+v", got)
	}
}

// A client list tmux would not give is not an empty client list. Refusing on
// one would call a watched session unwatched the first time tmux was slow.
func TestAClientListThatWasNotGivenIsNotAnEmptyOne(t *testing.T) {
	_, known := fixtureTmux(t, nil, "clawdline", true).attachedClients(context.Background())
	if known {
		t.Fatal("a refused list-clients answered as though tmux had said there are no clients")
	}
}

// The one shape where the selection lands and `ok` would be a lie: a tmux
// session with nothing attached is on no screen, so `Reveal` says so instead of
// answering yes. Runs against a private detached server, which is exactly that.
func TestRevealWillNotSayYesWhenNobodyIsAttached(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-reveal-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_PANE")
	t.Setenv("TMUX_TMPDIR", dir)
	start := exec.Command(bin, "new-session", "-d", "-s", "unwatched", "-c", dir, "sleep", "30")
	if out, err := start.CombinedOutput(); err != nil {
		t.Skipf("could not start a private tmux server: %v %s", err, out)
	}
	defer exec.Command(bin, "kill-server").Run()

	tmux := NewTmux()
	inv, err := tmux.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 {
		t.Fatalf("err=%v sessions=%+v", err, inv.Sessions)
	}
	err = tmux.Reveal(context.Background(), session.Session{ID: inv.Sessions[0].ID}, true)
	if err == nil {
		t.Fatal("a pane nobody is attached to was answered as shown")
	}
	unwatched, ok := err.(Unwatched)
	if !ok || unwatched.Session != "unwatched" {
		t.Fatalf("err %#v, want Unwatched naming the session", err)
	}
	if !strings.Contains(err.Error(), "nobody can see this session") {
		t.Errorf("the sentence was %q", err.Error())
	}
	// The selection still happened: it is the answer that was wrong, not the
	// act. The next client to attach arrives on that pane.
	out, err := exec.Command(bin, "display-message", "-p", "#{pane_id}").Output()
	if err != nil || strings.TrimSpace(string(out)) != inv.Sessions[0].ID {
		t.Errorf("active pane %q (%v), want %q", strings.TrimSpace(string(out)), err, inv.Sessions[0].ID)
	}
}

// A pane id is a pane id, and nothing else reaches the reveal at all.
func TestRevealRefusesWhatIsNotAPane(t *testing.T) {
	err := NewTmux().Reveal(context.Background(), session.Session{ID: "GUID-A"}, true)
	if _, no := err.(Unsupported); !no {
		t.Fatalf("err %#v, want Unsupported", err)
	}
}
