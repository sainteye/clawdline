//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// itermTypeScript writes text into one iTerm2 session without a newline. The
// id and the text are arguments, never part of the script, so no quoting rule
// has to be right about them.
const itermTypeScript = `
function run(argv) {
  const id = String(argv[0] || ""), text = String(argv[1] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  const wins = it.windows();
  for (let a = 0; a < wins.length; a++) {
    const tabs = wins[a].tabs();
    for (let b = 0; b < tabs.length; b++) {
      const ss = tabs[b].sessions();
      for (let c = 0; c < ss.length; c++) {
        if (String(ss[c].id()) === id) {
          ss[c].write({ text: text, newline: false });
          return JSON.stringify({ ok: true });
        }
      }
    }
  }
  return JSON.stringify({ ok: false, error: "That session is gone" });
}
`

// Type puts text in an iTerm2 session's input line without submitting it.
func (i *ITerm) Type(ctx context.Context, s session.Session, text string) error {
	if text == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-", s.ID, text)
	cmd.Stdin = strings.NewReader(itermTypeScript)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = "iTerm2 did not answer: " + err.Error()
		}
		return Failure{Attention: strings.Contains(said, "-1712") || strings.Contains(said, "-1743"), Message: said}
	}
	var answer struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if !answer.OK {
		if answer.Error == "" {
			answer.Error = "iTerm2 refused."
		}
		return Failure{Message: answer.Error}
	}
	return nil
}
