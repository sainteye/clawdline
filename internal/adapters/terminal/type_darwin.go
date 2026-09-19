//go:build darwin

package terminal

import (
	"context"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// itermTypeScript writes text into one iTerm2 session without a newline. The
// id and the text are arguments, never part of the script, so no quoting rule
// has to be right about them.
const itermTypeScript = itermEach + `
function run(argv) {
  const id = String(argv[0] || ""), text = String(argv[1] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  let done = false;
  itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    s.write({ text: text, newline: false });
    done = true;
    return true;
  });
  return JSON.stringify(done ? { ok: true } : { ok: false, error: "That session is gone" });
}
`

// Type puts text in an iTerm2 session's input line without submitting it.
func (i *ITerm) Type(ctx context.Context, s session.Session, text string) error {
	if text == "" {
		return nil
	}
	return itermCall(ctx, itermTypeScript, 6*time.Second, s.ID, text)
}
