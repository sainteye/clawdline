//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// itermKeyScript is iterm.js's `key` and `capture` commands. The id and the
// codes are arguments, never part of the script text.
const itermKeyScript = itermEach + `
function run(argv) {
  const cmd = String(argv[0] || ""), id = String(argv[1] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  let found = null;
  const walk = itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    found = s;
    return true;
  });
  if (!found) return JSON.stringify({ ok: false, error: itermMissing(walk) });
  if (cmd === "key") {
    const codes = argv.slice(2).map(function (raw) { return parseInt(String(raw || "0"), 10); });
    found.write({ text: String.fromCharCode.apply(String, codes), newline: false });
    return JSON.stringify({ ok: true });
  }
  if (cmd === "capture") {
    let text = "";
    try { text = String(found.text() || ""); } catch (e) { text = ""; }
    return JSON.stringify({ ok: true, text: text });
  }
  return JSON.stringify({ ok: false, error: "unknown command" });
}
`

func (i *ITerm) keyScript(ctx context.Context, args ...string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	// A screen capture observes the terminal and is already bounded by the
	// held-screen reader. Keeping it in the effects lane let two background
	// captures time out ahead of a person's send while iTerm2 was stalled.
	if len(args) == 0 || args[0] != "capture" {
		release, err := effect(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", append([]string{"-l", "JavaScript", "-"}, args...)...)
	cmd.Stdin = strings.NewReader(itermKeyScript)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	kind, session := "keys", ""
	if len(args) > 0 {
		kind = args[0]
	}
	if len(args) > 1 {
		session = args[1]
	}
	run := startOsascript(ctx, kind, session)
	out, err := cmd.Output()
	run.done(err)
	if err != nil {
		return nil, osascriptFailure(ctx, run, stderr.String(), err, "iTerm2 did not do what it was asked.")
	}
	var answer map[string]any
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return nil, Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if ok, _ := answer["ok"].(bool); !ok {
		said, _ := answer["error"].(string)
		if said == "" {
			said = "iTerm2 refused."
		}
		return nil, Failure{Message: said}
	}
	return answer, nil
}

// Keystroke writes raw key bytes into an iTerm2 session without a newline
// (ITerm.keystroke).
func (i *ITerm) Keystroke(ctx context.Context, s session.Session, codes []byte) error {
	if len(codes) == 0 {
		return errors.New("there is no key to send")
	}
	if !ITermScan() {
		return ITermScanOff{Op: "press a key in that iTerm2 session"}
	}
	args := []string{"key", s.ID}
	for _, b := range codes {
		args = append(args, strconv.Itoa(int(b)))
	}
	_, err := i.keyScript(ctx, args...)
	return err
}

// Capture is what an iTerm2 session shows now. iTerm2 exposes the visible
// screen and no scrollback.
func (i *ITerm) Capture(ctx context.Context, s session.Session) (string, bool) {
	text, ok, _ := i.CaptureWithFailure(ctx, s)
	return text, ok
}

// CaptureWithFailure reports whether iTerm2's Apple Event bridge failed, as
// opposed to a particular tab having gone away. The held-screen reader uses
// that distinction to back off the whole source only during an outage.
func (i *ITerm) CaptureWithFailure(ctx context.Context, s session.Session) (string, bool, bool) {
	// Scanning turned off reads no screen. It is not a source failure: the
	// held-screen reader must not back off and retry a bridge nobody asked.
	if s.Backend != session.BackendITerm || s.ID == "" || !ITermScan() {
		return "", false, false
	}
	answer, err := i.keyScript(ctx, "capture", s.ID)
	if err != nil {
		return "", false, captureSourceFailed(err)
	}
	text, ok := answer["text"].(string)
	return text, ok, false
}

func captureSourceFailed(err error) bool {
	var failure Failure
	return errors.As(err, &failure) && failure.Attention
}

var (
	_ ports.KeyHost    = (*ITerm)(nil)
	_ ports.ScreenHost = (*ITerm)(nil)
)
