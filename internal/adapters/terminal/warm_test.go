package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// privateTmux starts a tmux server of its own with one session, "home", for a
// warm-shell test, and answers its launcher pieces.
func warmTmux(t *testing.T) (TmuxChoice, *Tmux, string) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "cl-warm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("SHELL", "/bin/sh")
	choice := TmuxChoice{Path: bin, Socket: sock}
	t.Cleanup(func() { exec.Command(bin, "-S", sock, "kill-server").Run() })
	if _, err := runTmux(context.Background(), choice, "new-session", "-d", "-s", "home", "sleep 600"); err != nil {
		t.Skipf("could not start private tmux: %v", err)
	}
	old := warmReplenishDelay
	warmReplenishDelay = 0
	t.Cleanup(func() { warmReplenishDelay = old })
	return choice, &Tmux{Binary: bin, Socket: sock}, dir
}

func paneFacts(t *testing.T, bin TmuxChoice, pane string) (session, warm, path string) {
	t.Helper()
	out, err := runTmux(context.Background(), bin, "display-message", "-p", "-t", pane,
		"#{?@clawdline_warm,1,-} #{session_name}")
	if err != nil {
		t.Fatalf("%s: %v", pane, err)
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), " ", 2)
	if f[0] == "-" {
		f[0] = ""
	}
	return f[1], f[0], ""
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	var data []byte
	waitFor(t, path, func() bool {
		var err error
		data, err = os.ReadFile(path)
		return err == nil && len(data) > 0
	})
	return string(data)
}

func warmPanes(t *testing.T, bin TmuxChoice) []string {
	t.Helper()
	out, _ := runTmux(context.Background(), bin, "list-panes", "-a", "-F", "#{pane_id} #{?@clawdline_warm,1,0}")
	var panes []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " 1") {
			panes = append(panes, strings.TrimSuffix(line, " 1"))
		}
	}
	return panes
}

// A Session started in a directory with a warm shell runs in that shell —
// already in the directory, moved to where a cold window goes — and the
// directory gets its next warm shell afterwards.
func TestANewWindowTakesTheWarmShellOfItsDirectory(t *testing.T) {
	bin, tmux, dir := warmTmux(t)
	ctx := context.Background()
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	warm := NewWarmShells()
	warm.Replenish(ctx, bin, project)
	waiting := warmPanes(t, bin)
	if len(waiting) != 1 {
		t.Fatalf("warm panes after replenishing: %v", waiting)
	}
	if session, mark, _ := paneFacts(t, bin, waiting[0]); session != WarmSessionName || mark != "1" {
		t.Fatalf("the warm shell is in %q marked %q", session, mark)
	}
	launcher := Launcher{Tmux: tmux, Warm: warm}
	out := filepath.Join(dir, "pwd")
	id, err := launcher.NewTmuxWindow(ctx, project, "pwd > "+out)
	if err != nil {
		t.Fatal(err)
	}
	if id != waiting[0] {
		t.Fatalf("the Session opened %s, not the warm shell %s", id, waiting[0])
	}
	if session, mark, _ := paneFacts(t, bin, id); session != "home" || mark != "" {
		t.Fatalf("the taken shell is in %q marked %q", session, mark)
	}
	got := strings.TrimSpace(readFile(t, out))
	want, _ := filepath.EvalSymlinks(project)
	if resolved, _ := filepath.EvalSymlinks(got); resolved != want {
		t.Fatalf("the launch line ran in %q, not %q", got, want)
	}
	waitFor(t, "the next warm shell", func() bool {
		next := warmPanes(t, bin)
		return len(next) == 1 && next[0] != id
	})
}

// With a warm session on the server, a cold window still goes to the person's
// most recently active session and never into the warm one; a directory with
// no warm shell is opened cold.
func TestAColdWindowNeverLandsInTheWarmSession(t *testing.T) {
	bin, tmux, dir := warmTmux(t)
	ctx := context.Background()
	warm := NewWarmShells()
	warm.Replenish(ctx, bin, dir) // the warm session is now the newest one
	other := filepath.Join(dir, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := Launcher{Tmux: tmux} // no warm record: every window cold
	id, err := launcher.NewTmuxWindow(ctx, other, "true")
	if err != nil {
		t.Fatal(err)
	}
	if session, _, _ := paneFacts(t, bin, id); session != "home" {
		t.Fatalf("a cold window opened in %q", session)
	}
	launcher.Warm = warm
	id, err = launcher.NewTmuxWindow(ctx, other, "true")
	if err != nil {
		t.Fatal(err)
	}
	if session, mark, _ := paneFacts(t, bin, id); session != "home" || mark != "" {
		t.Fatalf("a directory without a warm shell opened in %q marked %q", session, mark)
	}
}

// Turned off, a warm shell is not used and the waiting ones are closed.
func TestTurnedOffWarmShellsAreNotUsedAndAreClosed(t *testing.T) {
	bin, tmux, dir := warmTmux(t)
	ctx := context.Background()
	on := true
	warm := NewWarmShells()
	warm.Enabled = func() bool { return on }
	warm.Replenish(ctx, bin, dir)
	waiting := warmPanes(t, bin)
	if len(waiting) != 1 {
		t.Fatalf("warm panes: %v", waiting)
	}
	on = false
	id, err := Launcher{Tmux: tmux, Warm: warm}.NewTmuxWindow(ctx, dir, "true")
	if err != nil {
		t.Fatal(err)
	}
	if id == waiting[0] {
		t.Fatal("a warm shell was used with the setting off")
	}
	warm.Clear(ctx, bin)
	if left := warmPanes(t, bin); len(left) != 0 {
		t.Fatalf("warm shells left after turning off: %v", left)
	}
}

// A daemon that starts again keeps using the warm shells the previous one
// left, and keeps no more than MaxWarmShells of them.
func TestARestartedDaemonAdoptsTheWarmShellsLeftBehind(t *testing.T) {
	bin, tmux, dir := warmTmux(t)
	ctx := context.Background()
	first := NewWarmShells()
	var dirs []string
	for i := 0; i <= MaxWarmShells; i++ {
		d := filepath.Join(dir, "p"+string(rune('a'+i)))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
		first.Replenish(ctx, bin, d)
	}
	if left := warmPanes(t, bin); len(left) != MaxWarmShells {
		t.Fatalf("%d warm shells kept, want %d", len(left), MaxWarmShells)
	}
	second := NewWarmShells()
	last := dirs[len(dirs)-1]
	waiting := warmPanes(t, bin)
	id, err := Launcher{Tmux: tmux, Warm: second}.NewTmuxWindow(ctx, last, "true")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range waiting {
		found = found || p == id
	}
	if !found {
		t.Fatalf("the restarted daemon opened %s cold instead of a warm shell in %v", id, waiting)
	}
}

// A warm shell past its age is closed instead of taken.
func TestAnOldWarmShellIsNotTaken(t *testing.T) {
	bin, tmux, dir := warmTmux(t)
	ctx := context.Background()
	clock := time.Now()
	warm := NewWarmShells()
	warm.now = func() time.Time { return clock }
	warm.Replenish(ctx, bin, dir)
	waiting := warmPanes(t, bin)
	clock = clock.Add(WarmShellSecondsLimit * time.Second)
	id, err := Launcher{Tmux: tmux, Warm: warm}.NewTmuxWindow(ctx, dir, "true")
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || id == waiting[0] {
		t.Fatalf("an expired warm shell %v was taken as %s", waiting, id)
	}
}
