//go:build darwin

package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
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
const ESC = String.fromCharCode(27);
// Each session draws its screen from what was written to it: the text pasted
// so far, brackets removed, and how many Returns arrived.
function session(id, tty, draw) {
  const state = { typed: "", returns: 0 };
  return {
    id: () => id, tty: () => tty, name: () => "shell", text: () => draw(state),
    write: (o) => {
      writes.push({ id: id, text: o.text, newline: o.newline });
      if (o.text === "\r") { state.returns++; return; }
      state.typed += o.text.split(ESC + "[200~").join("").split(ESC + "[201~").join("");
    },
    select: () => {}, variable: () => "",
    close: () => { writes.push({ id: id, text: "<close>", newline: false }); },
  };
}
const rule = "────────────────────────────────────────";
function composer(line) { return [rule, "❯ " + line, rule, "  ? for shortcuts"].join("\n"); }
// GUID-A is a shell: it echoes what is typed, and a Return starts a new line.
const shell = (st) => "> " + (st.returns > 0 ? "" : st.typed);
// GUID-C is Claude Code, the way it draws its composer — the caret is ❯, not
// > — and it swallows the first Return after a paste.
const claude = (st) => composer(st.returns >= 2 ? "" : st.typed);
// GUID-D is a program that is not reading: its composer never shows anything.
const deaf = (st) => composer("");
const unlisted = { tabs: () => null, select: () => {} };
const complete = process.env.COMPLETE === "1";
const listed = { select: () => {}, tabs: () => [
  complete ? { select: () => {}, sessions: () => [] } : { select: () => {}, sessions: () => null },
  { select: () => {}, sessions: () => [session("GUID-A", "/dev/ttys031", shell), session("GUID-C", "/dev/ttys032", claude),
    session("GUID-D", "/dev/ttys033", deaf)] },
] };
const app = { running: () => true, windows: () => complete ? [listed] : [unlisted, listed], activate: () => {} };
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
	return runInModelWith(t, script, argv, false)
}

// runInModelWith runs the script against the model with every window
// readable when complete is true.
func runInModelWith(t *testing.T, script string, argv []string, complete bool) modelRun {
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
	if complete {
		cmd.Env = append(cmd.Env, "COMPLETE=1")
	}
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
	if len(sessions) != 3 || sessions[0].(map[string]any)["id"] != "GUID-A" || sessions[1].(map[string]any)["id"] != "GUID-C" ||
		sessions[2].(map[string]any)["id"] != "GUID-D" {
		t.Fatalf("sessions %v, want the listed sessions by their ids", run.Answer["sessions"])
	}
	if run.Answer["unreadable"] != float64(2) {
		t.Fatalf("unreadable %v, want the window and the tab that would not list", run.Answer["unreadable"])
	}
}

// Every script that looks for one session finds it past such a window.
func TestEveryITermScriptFindsASessionPastAWindowThatWillNotList(t *testing.T) {
	if run := runInModel(t, itermTypeScript, []string{"GUID-A", "hello"}); run.Answer["ok"] != true ||
		len(run.Writes) != 1 || run.Writes[0].Text != "\x1b[200~hello\x1b[201~" || run.Writes[0].Newline {
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

// Send is one bracketed paste, then — once the session shows it — a Return of
// its own. It used to call a command iTerm2 does not have and send no Return
// at all.
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

// F4: the Return that follows the paste can be swallowed, and the script looks
// again and sends another only while the paste is still in the composer. It
// used to look for ">" and "›" only, and Claude Code draws its composer with
// "❯" — so for Claude the look never found a composer and never helped. Once
// the composer lets go, no more Returns are sent.
func TestTheITermSendNudgesAClaudeComposerThatKeptThePaste(t *testing.T) {
	run := runInModel(t, itermSendScript, []string{"GUID-C", "please read CHILD.md 0123456789abcdef0123456789abcdef"})
	if run.Answer["ok"] != true || len(run.Writes) != 3 {
		t.Fatalf("a Claude composer that kept the paste once got %+v, want the paste and two Returns", run)
	}
	for _, w := range run.Writes[1:] {
		if w.Text != "\r" {
			t.Fatalf("after the paste only Returns are sent, got %q", w.Text)
		}
	}
}

// The iTerm2 half of submit.go's step 3: a session that never shows the paste
// arriving gets no Return at all, and the send answers Unsubmitted — the text
// is in the session, so it is not Unsent either.
func TestTheITermSendPressesNoReturnUntilTheSessionShowsThePaste(t *testing.T) {
	run := runInModel(t, itermSendScript, []string{"GUID-D", "please read CHILD.md 0123456789abcdef0123456789abcdef"})
	if run.Answer["ok"] != false || run.Answer["typed"] != true {
		t.Fatalf("a session that never showed the paste answered %+v", run.Answer)
	}
	if len(run.Writes) != 1 || !strings.HasPrefix(run.Writes[0].Text, "\x1b[200~") {
		t.Fatalf("want the paste and nothing else, got %+v", run.Writes)
	}
	raw, _ := json.Marshal(run.Answer)
	var unsubmitted Unsubmitted
	if err := itermAnswer(raw); !errors.As(err, &unsubmitted) {
		t.Fatalf("typed-and-not-submitted read as %T %v, want Unsubmitted", err, err)
	}
}

// F7: a session not found by a walk that could not read every window was not
// seen, which is not the same as gone. Each script that looks for one session
// says which; a complete walk still says gone.
func TestAnITermScriptThatCouldNotReadEveryWindowDoesNotSayGone(t *testing.T) {
	for name, argv := range map[string][]string{
		"type":    nil,
		"send":    nil,
		"key":     {"key", "GUID-GONE", "27"},
		"capture": {"capture", "GUID-GONE"},
		"reveal":  {"reveal", "GUID-GONE", "0"},
		"close":   {"GUID-GONE"},
	} {
		script := map[string]string{"type": itermTypeScript, "send": itermSendScript, "key": itermKeyScript,
			"capture": itermKeyScript, "reveal": itermRevealScript, "close": itermCloseScript}[name]
		if argv == nil {
			argv = []string{"GUID-GONE", "hello"}
		}
		partial := runInModelWith(t, script, argv, false)
		said, _ := partial.Answer["error"].(string)
		if partial.Answer["ok"] != false || strings.Contains(said, "gone") || !strings.Contains(said, "could not be read") {
			t.Errorf("%s past an unreadable window answered %+v", name, partial.Answer)
		}
		whole := runInModelWith(t, script, argv, true)
		if said, _ := whole.Answer["error"].(string); whole.Answer["ok"] != false || !strings.Contains(said, "gone") {
			t.Errorf("%s on a complete walk answered %+v", name, whole.Answer)
		}
		if len(partial.Writes)+len(whole.Writes) != 0 {
			t.Errorf("%s wrote to a session it did not find", name)
		}
	}
}

// F5: the close finds its session by id past a window that will not list, and
// closes that session only — not its tab, whose other panes are not ours.
func TestTheITermCloseClosesOnlyTheSessionItNames(t *testing.T) {
	run := runInModel(t, itermCloseScript, []string{"GUID-A"})
	if run.Answer["ok"] != true || len(run.Writes) != 1 || run.Writes[0].ID != "GUID-A" || run.Writes[0].Text != "<close>" {
		t.Fatalf("close: %+v", run)
	}
}

// F2: an effect script that answered "not done" said so before its effect —
// every one of them looks the session up first — so the failure is Unsent,
// and a caller may type again. An answer that is not JSON came from a script
// that ran, and is not.
func TestAnITermNotDoneIsUnsentAndAnUnreadableAnswerIsNot(t *testing.T) {
	var unsent Unsent
	if err := itermAnswer([]byte(`{"ok":false,"error":"That session is gone"}`)); !errors.As(err, &unsent) {
		t.Fatalf("not done answered %T %v, want Unsent", err, err)
	}
	if err := itermAnswer([]byte(`not json`)); errors.As(err, &unsent) {
		t.Fatalf("an unreadable answer is not known to have sent nothing: %v", err)
	}
}

// F8: osascript's own words — the script's position, the Apple Event error
// number — stay in this machine's log. What travels on, into a refusal a phone
// shows and a record's spawn_error, is a sentence. And a script that failed
// part way is not Unsent: it may have written before it failed.
func TestAnITermScriptFailureCarriesASentenceNotOsascriptsWords(t *testing.T) {
	if _, err := exec.LookPath("/usr/bin/osascript"); err != nil {
		t.Skip("no osascript")
	}
	// Runs no Apple Event: it throws before it could reach any application.
	err := itermCall(context.Background(), `function run(argv) { throw new Error("internal-detail-7731"); }`, 10*time.Second)
	if err == nil {
		t.Fatal("a script that threw answered success")
	}
	if strings.Contains(err.Error(), "internal-detail-7731") || strings.Contains(err.Error(), "execution error") {
		t.Fatalf("osascript's own words went out: %q", err.Error())
	}
	var unsent Unsent
	if errors.As(err, &unsent) {
		t.Fatalf("a script that failed part way is not known to have sent nothing: %v", err)
	}
}

// ITerm.Close closes one session, through the same script the broker's close
// uses. It used to answer Unsupported, so no iTerm2 tab was ever closed from
// the list — five finished tasks' tabs stayed open on this Mac until a person
// closed them. A session with no id is refused before any Apple Event, as a
// write that sent nothing.
func TestTheITermCloseIsImplemented(t *testing.T) {
	err := NewITerm().Close(context.Background(), session.Session{Backend: session.BackendITerm})
	var unsupported Unsupported
	if errors.As(err, &unsupported) {
		t.Fatalf("ITerm.Close still answers %v", err)
	}
	var unsent Unsent
	if !errors.As(err, &unsent) {
		t.Fatalf("a session with no id answered %T %v, want Unsent", err, err)
	}
}
