//go:build darwin

package terminal

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The iTerm2 scripts are run here, as they are, against a model of iTerm2 in
// node rather than against the real application: a test must not open, type
// into or read the person's tabs, and the fault these guard against — a window
// whose `tabs()` answers null — is a shape of the model, not of any one Mac.
// docs/switch-blockers.md has the measurement.

// itermModel is one iTerm2 with a window that will not list its tabs, ahead
// of a window holding the session every test looks for.
const itermModel = `
const vm = require("vm");
const writes = [];
function session(id, tty, screen) {
  return {
    id: () => id, tty: () => tty, name: () => "shell", text: () => screen,
    write: (o) => { writes.push({ id: id, text: o.text, newline: o.newline }); },
    select: () => {}, variable: () => "",
  };
}
const unlisted = { tabs: () => null, select: () => {} };
const listed = { select: () => {}, tabs: () => [
  { select: () => {}, sessions: () => null },
  { select: () => {}, sessions: () => [session("GUID-A", "/dev/ttys031", "> ")] },
] };
const app = { running: () => true, windows: () => [unlisted, listed], activate: () => {} };
const context = vm.createContext({ Application: () => app, delay: () => {}, JSON: JSON });
const script = process.env.SCRIPT, argv = JSON.parse(process.env.ARGV || "null");
const answer = argv === null
  ? vm.runInContext(script, context)
  : vm.runInContext(script + "\n;run(" + JSON.stringify(argv) + ")", context);
process.stdout.write(JSON.stringify({ answer: JSON.parse(answer), writes: writes }));
`

type modelRun struct {
	Answer map[string]any `json:"answer"`
	Writes []struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		Newline bool   `json:"newline"`
	} `json:"writes"`
}

func runInModel(t *testing.T, script string, argv []string) modelRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the iTerm2 scripts are run in node's vm")
	}
	harness := filepath.Join(t.TempDir(), "model.js")
	if err := os.WriteFile(harness, []byte(itermModel), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, harness)
	cmd.Env = append(os.Environ(), "SCRIPT="+script)
	if argv != nil {
		raw, _ := json.Marshal(argv)
		cmd.Env = append(cmd.Env, "ARGV="+string(raw))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the script threw in the model: %v\n%s", err, out)
	}
	var run modelRun
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatalf("unreadable answer %q: %v", out, err)
	}
	return run
}

// The listing publishes the session iTerm2 does name, under its session id,
// and counts what it could not read instead of throwing on it.
func TestTheITermListingSkipsAWindowThatWillNotListItsTabs(t *testing.T) {
	run := runInModel(t, itermList, nil)
	sessions, _ := run.Answer["sessions"].([]any)
	if len(sessions) != 1 || sessions[0].(map[string]any)["id"] != "GUID-A" {
		t.Fatalf("sessions %v, want the one listed session by its id", run.Answer["sessions"])
	}
	if run.Answer["unreadable"] != float64(2) {
		t.Fatalf("unreadable %v, want the window and the tab that would not list", run.Answer["unreadable"])
	}
}

// Every script that looks for one session finds it past such a window.
func TestEveryITermScriptFindsASessionPastAWindowThatWillNotList(t *testing.T) {
	if run := runInModel(t, itermTypeScript, []string{"GUID-A", "hello"}); run.Answer["ok"] != true ||
		len(run.Writes) != 1 || run.Writes[0].Text != "hello" || run.Writes[0].Newline {
		t.Fatalf("type: %+v", run)
	}
	if run := runInModel(t, itermKeyScript, []string{"key", "GUID-A", "27"}); run.Answer["ok"] != true ||
		len(run.Writes) != 1 || run.Writes[0].Text != "\x1b" {
		t.Fatalf("key: %+v", run)
	}
	if run := runInModel(t, itermKeyScript, []string{"capture", "GUID-A"}); run.Answer["ok"] != true ||
		run.Answer["text"] != "> " {
		t.Fatalf("capture: %+v", run)
	}
	if run := runInModel(t, itermRevealScript, []string{"reveal", "GUID-A", "0"}); run.Answer["ok"] != true {
		t.Fatalf("reveal: %+v", run)
	}
	if run := runInModel(t, itermRevealScript, []string{"list"}); run.Answer["ok"] != true {
		t.Fatalf("reveal list: %+v", run)
	}
	for _, script := range []string{itermTypeScript, itermSendScript} {
		if run := runInModel(t, script, []string{"GUID-GONE", "hello"}); run.Answer["ok"] != false ||
			len(run.Writes) != 0 {
			t.Fatalf("a session that is not there answered %+v", run)
		}
	}
}

// Send is the Swift app's: one bracketed paste, then a Return of its own. It
// used to call a command iTerm2 does not have and send no Return at all.
func TestTheITermSendPastesThenSubmits(t *testing.T) {
	run := runInModel(t, itermSendScript, []string{"GUID-A", "line one\nline two"})
	if run.Answer["ok"] != true || len(run.Writes) != 2 {
		t.Fatalf("send: %+v", run)
	}
	if run.Writes[0].Text != "\x1b[200~line one\nline two\x1b[201~" || run.Writes[0].Newline {
		t.Fatalf("the paste was %q", run.Writes[0].Text)
	}
	if run.Writes[1].Text != "\r" || run.Writes[1].Newline {
		t.Fatalf("the submit was %q", run.Writes[1].Text)
	}
}

// What a script answered is read: "not done" is a failure, never a delivery.
func TestAnITermAnswerOfNotDoneIsAFailure(t *testing.T) {
	if err := itermAnswer([]byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]string{
		`{"ok":false,"error":"That session is gone"}`: "That session is gone",
		`{"sent":false}`: "iTerm2 refused.",
		`not json`:       "not JSON",
	} {
		if err := itermAnswer([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s answered %v, want %q", body, err, want)
		}
	}
}
