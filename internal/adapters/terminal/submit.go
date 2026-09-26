package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
	"unicode"
)

// How a line is put into the program in a terminal and submitted — on tmux
// here, on iTerm2 in itermSendScript — and how either knows it went.
//
// **Typed text and its Enter are one byte stream, and the program does not
// read it as one.** The pty hands a reader at most 1022 bytes per read on this
// Mac. Claude Code takes a read longer than 800 bytes that is not inside a
// bracketed paste for a paste of its own, and that text never reached the
// message: a 1045-byte completion notice typed with `send-keys -l` and then
// Enter was submitted as its last 23 bytes, `ck"}</clawdline-notice>`, eight
// times in ten minutes, and the root it was for could never acknowledge it.
//
// So every send is the same four steps, on every backend:
//
//  1. The text goes in as one bracketed paste. A program that asked for
//     bracketed paste reads the whole of it as one paste however the pty cuts
//     it, and knows where it ends.
//  2. **The program's own screen is read until it shows the text arriving**:
//     the end of the text in its input line, or a paste placeholder that was
//     not there before ("[Pasted text #3]", "[Pasted Content 1045 chars]").
//     That is the program having read the paste, which is the only evidence a
//     terminal can give that it did — tmux and iTerm2 answering "written" say
//     only that the bytes were queued.
//  3. **Only then Enter.** A screen that never shows the text gets no Enter
//     at all, and the send answers Unsubmitted: a Return into a program that
//     is not reading its input, or that put a dialog up meanwhile, answers
//     whatever that program shows next.
//  4. After the Enter, the input line is looked at again, and another Enter
//     is sent only while it is a framed composer still showing exactly what
//     step 2 confirmed.

// needleRunes is how much of the end of the text is looked for on screen,
// with whitespace removed so a wrapped line still matches. A briefing ends in
// a 64-character secret and a notice in its ack path, so it is specific.
const needleRunes = 24

// screenLines is how far up from the bottom the input line is looked for.
const screenLines = 12

// inputCarets are the glyphs each CLI starts its input line with: ">" and
// Codex's "›", and Claude Code's "❯" (composerCarets in the orchestrator).
var inputCarets = []string{">", "›", "❯"}

// framedCarets start an input line only inside a frame, a rule directly above
// and one somewhere below: Claude Code's shell mode draws its composer's caret
// as "!", and a "!" anywhere else is a shell's history or that mode's own
// footer. Without it the last caret on a shell-mode screen is a message above
// the composer, and a line pasted with a leading "!" is never seen arriving.
var framedCarets = []string{"!"}

// pastePlaceholders are what a CLI draws in its input line after consuming a
// paste instead of showing its bytes: Claude Code's "[Pasted text #3]",
// Codex's "[Pasted Content 1045 chars]", and Codex's "[Image #1]" after it
// recognises a pasted local image path. All three were read off the programs
// installed on this machine.
var pastePlaceholders = []string{"[Pasted text #", "[Pasted Content ", "[Image #"}

// submitPauses are the waits between looks at the screen for step 2: short
// first, because a program that is reading shows a paste in a few
// milliseconds, then no faster than a person would notice.
var submitPauses = []time.Duration{
	20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 160 * time.Millisecond,
	250 * time.Millisecond, 400 * time.Millisecond,
}

// nudgePauses are the waits before each look after the Enter (step 4). They
// are iTerm2's, from before this file existed.
var nudgePauses = []time.Duration{250 * time.Millisecond, 400 * time.Millisecond, 400 * time.Millisecond}

// looksWithin is how many looks submitPauses fits into window, counting the
// first, which does not wait. The iTerm2 script is given this count as well
// as the window: in the model the tests run it in, time does not pass.
func looksWithin(window time.Duration) int {
	looks, spent := 1, time.Duration(0)
	for {
		pause := submitPauses[min(looks-1, len(submitPauses)-1)]
		if spent+pause > window {
			return looks
		}
		spent += pause
		looks++
	}
}

// seconds is a list of pauses as the iTerm2 script's delay() takes them.
func seconds(pauses []time.Duration) string {
	parts := make([]string, len(pauses))
	for i, p := range pauses {
		parts[i] = fmt.Sprintf("%g", p.Seconds())
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// squeeze removes every whitespace character.
func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// needle is the end of the text as it is looked for on screen.
func needle(text string) string {
	r := []rune(squeeze(text))
	if len(r) > needleRunes {
		r = r[len(r)-needleRunes:]
	}
	return string(r)
}

// shownInput is what a screen shows in its input line.
type shownInput struct {
	// text is what the input line holds after its caret, whitespace removed.
	text string
	// found is whether a line starting with a caret was found at all.
	found bool
	// framed is whether a rule is drawn under it: a composer, as Claude Code
	// draws one, rather than a shell prompt or a chooser's highlighted row.
	framed bool
}

// inputLine is what a screen shows in its input line.
//
// The input line starts at the last line within screenLines of the bottom
// that begins with a caret — past any box verticals — and runs down to the
// first rule under it, which is the frame Claude Code draws, or to the bottom.
// A screen with no caret on it, a shell's, answers its bottom screenLines
// lines, not found.
func inputLine(screen string) shownInput {
	lines := strings.Split(strings.TrimRightFunc(screen, unicode.IsSpace), "\n")
	top := len(lines) - screenLines
	if top < 0 {
		top = 0
	}
	for i := len(lines) - 1; i >= top; i-- {
		head := strings.TrimLeftFunc(lines[i], func(r rune) bool {
			return unicode.IsSpace(r) || r == '│' || r == '┃' || r == '|'
		})
		for _, caret := range append(inputCarets, framedCarets...) {
			if !strings.HasPrefix(head, caret) {
				continue
			}
			end := i + 1
			for end < len(lines) && !isFrame(lines[end]) {
				end++
			}
			framed := end < len(lines)
			if slices.Contains(framedCarets, caret) && !(framed && i > 0 && isFrame(lines[i-1])) {
				continue
			}
			typed := strings.TrimPrefix(head, caret) + strings.Join(lines[i+1:end], "")
			return shownInput{text: squeeze(typed), found: true, framed: framed}
		}
	}
	return shownInput{text: squeeze(strings.Join(lines[top:], ""))}
}

// isFrame is a line of nothing but box-drawing, four or more: the rule a
// composer is framed by. The orchestrator's isRule, which cannot be imported
// from here.
func isFrame(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	n := 0
	for _, r := range trimmed {
		if !strings.ContainsRune("─━═╌┄┈╭╮╰╯│┌┐└┘", r) {
			return false
		}
		n++
	}
	return n >= 4
}

// showsText is whether the screen after the paste shows it arriving that the
// screen before did not: the end of the text, or a paste placeholder, more
// times in the input line than it was there before. Counting rather than
// finding is what keeps a copy already on screen — the same notice typed
// once before, the same command in a shell's history — from confirming a
// paste that has not been read.
func showsText(before, after, text string) bool {
	was, now := inputLine(before).text, inputLine(after).text
	if n := needle(text); n != "" && strings.Count(now, n) > strings.Count(was, n) {
		return true
	}
	for _, p := range pastePlaceholders {
		p = squeeze(p)
		if strings.Count(now, p) > strings.Count(was, p) {
			return true
		}
	}
	return false
}

// stillHolds is whether a composer still shows exactly what was confirmed in
// it before the Enter: the Enter did nothing yet.
//
// Only a framed composer is ever nudged. A shell's line — even one whose
// prompt is a caret — stays on screen after Enter as the command it ran, and
// an Enter sent at it goes to whatever that command is: to an assistant that
// is just starting, the answer to its first dialog. A screen that changed at
// all is not nudged either; it is showing something else now.
func stillHolds(confirmed, now string) bool {
	was, is := inputLine(confirmed), inputLine(now)
	return was.framed && is.framed && was.text != "" && was.text == is.text
}

// inputRuleJS is the same rule for the iTerm2 scripts, which run in
// osascript's JavaScript and cannot call the Go above. It is built from the
// same constants, and TestTheInputRuleIsTheSameInGoAndJavaScript runs both on
// the same screens.
var inputRuleJS = func() string {
	carets, _ := json.Marshal(inputCarets)
	framed, _ := json.Marshal(framedCarets)
	holders, _ := json.Marshal(pastePlaceholders)
	return fmt.Sprintf(`
const FRAMED_CARETS = %s, INPUT_CARETS = %s.concat(FRAMED_CARETS), PASTE_PLACEHOLDERS = %s;
function squeeze(s) { return String(s).replace(/\s+/g, ""); }
function needle(text) { const r = Array.from(squeeze(text)); return r.slice(Math.max(0, r.length - %d)).join(""); }
function isFrame(line) {
  const t = String(line).trim();
  if (!t) return false;
  const r = Array.from(t);
  for (let i = 0; i < r.length; i++) if ("─━═╌┄┈╭╮╰╯│┌┐└┘".indexOf(r[i]) < 0) return false;
  return r.length >= 4;
}
function inputLine(screen) {
  const lines = String(screen || "").replace(/\s+$/, "").split("\n");
  const top = Math.max(0, lines.length - %d);
  for (let i = lines.length - 1; i >= top; i--) {
    const head = lines[i].replace(/^[\s│┃|]+/, "");
    for (let c = 0; c < INPUT_CARETS.length; c++) {
      if (head.indexOf(INPUT_CARETS[c]) !== 0) continue;
      let end = i + 1;
      while (end < lines.length && !isFrame(lines[end])) end++;
      const framed = end < lines.length;
      if (FRAMED_CARETS.indexOf(INPUT_CARETS[c]) >= 0 && !(framed && i > 0 && isFrame(lines[i - 1]))) continue;
      const typed = head.slice(INPUT_CARETS[c].length) + lines.slice(i + 1, end).join("");
      return { text: squeeze(typed), found: true, framed: framed };
    }
  }
  return { text: squeeze(lines.slice(top).join("")), found: false, framed: false };
}
function count(hay, pin) { return pin ? hay.split(pin).length - 1 : 0; }
function showsText(before, after, text) {
  const was = inputLine(before).text, now = inputLine(after).text, n = needle(text);
  if (n && count(now, n) > count(was, n)) return true;
  for (let i = 0; i < PASTE_PLACEHOLDERS.length; i++) {
    const p = squeeze(PASTE_PLACEHOLDERS[i]);
    if (count(now, p) > count(was, p)) return true;
  }
  return false;
}
function stillHolds(confirmed, now) {
  const was = inputLine(confirmed), is = inputLine(now);
  return was.framed && is.framed && was.text !== "" && was.text === is.text;
}
`, framed, carets, holders, needleRunes, screenLines)
}()

// input is one terminal session as a send drives it.
type input interface {
	// paste puts the text in as one bracketed paste. A failure before any
	// byte was written is Unsent.
	paste(ctx context.Context, text string) error
	enter(ctx context.Context) error
	// screen is what the session shows now, and false when it could not be
	// read.
	screen(ctx context.Context) (string, bool)
}

// submit is the four steps above against one session. window bounds step 2.
func submit(ctx context.Context, in input, text string, window time.Duration) error {
	before, _ := in.screen(ctx)
	if err := in.paste(ctx, text); err != nil {
		return err
	}
	confirmed, err := awaitShown(ctx, in, before, text, window)
	if err != nil {
		return err
	}
	if err := in.enter(ctx); err != nil {
		return err
	}
	for _, pause := range nudgePauses {
		if !rest(ctx, pause) {
			return nil
		}
		now, ok := in.screen(ctx)
		if !ok || !stillHolds(confirmed, now) {
			return nil
		}
		// The first Enter went; a nudge that fails is only logged, because
		// reporting the send as failed would have it typed a second time.
		if err := in.enter(ctx); err != nil {
			log.Printf("terminal: a second Enter was not sent: %v", err)
			return nil
		}
		confirmed = now
	}
	return nil
}

// awaitShown reads the screen until it shows the paste arriving, and answers
// the screen that did. A screen that never does, within window, is
// Unsubmitted: the text went in and nothing has pressed Enter on it.
func awaitShown(ctx context.Context, in input, before, text string, window time.Duration) (string, error) {
	deadline := time.Now().Add(window)
	for look := 0; ; look++ {
		if screen, ok := in.screen(ctx); ok && showsText(before, screen, text) {
			return screen, nil
		}
		pause := submitPauses[min(look, len(submitPauses)-1)]
		if time.Now().Add(pause).After(deadline) || !rest(ctx, pause) {
			return "", Unsubmitted{Why: fmt.Sprintf("the text was typed, but the terminal did not show it "+
				"arriving in its input line within %s, so Enter was not pressed", window)}
		}
	}
}

// rest waits d, and answers false if ctx ended first.
func rest(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// tmuxInput is one tmux pane as a send drives it. call runs one tmux command
// with stdin, the way its owner runs every other one.
type tmuxInput struct {
	call   func(ctx context.Context, stdin string, args ...string) (string, error)
	target string
}

// paste loads the text into a buffer of its own and pastes it with `-p`,
// which brackets it exactly when the program in the pane asked for bracketed
// paste — tmux knows, where iTerm2 does not — and `-r`, which leaves a
// newline a newline, as `send-keys -l` did. The text goes to tmux on stdin,
// never on a command line, so neither its length nor its characters are
// tmux's command syntax. `-d` deletes the buffer once pasted; a paste tmux
// refused deletes it here, so a notice is not left in `list-buffers`.
func (p tmuxInput) paste(ctx context.Context, text string) error {
	name := bufferName()
	if _, err := p.call(ctx, text, "load-buffer", "-b", name, "-"); err != nil {
		return Unsent{Why: err.Error()}
	}
	if _, err := p.call(ctx, "", "paste-buffer", "-b", name, "-d", "-p", "-r", "-t", p.target); err != nil {
		p.call(context.WithoutCancel(ctx), "", "delete-buffer", "-b", name)
		if ctx.Err() != nil {
			// Killed while tmux was answering: it may have pasted.
			return err
		}
		// tmux finds the pane before it pastes, and says so when it cannot.
		return Unsent{Why: err.Error()}
	}
	return nil
}

func (p tmuxInput) enter(ctx context.Context) error {
	_, err := p.call(ctx, "", "send-keys", "-t", p.target, "Enter")
	return err
}

func (p tmuxInput) screen(ctx context.Context) (string, bool) {
	out, err := p.call(ctx, "", "capture-pane", "-p", "-J", "-t", p.target)
	return out, err == nil
}

// bufferName is a tmux buffer name nobody else is using.
func bufferName() string {
	var b [8]byte
	rand.Read(b[:])
	return "clawdline-send-" + hex.EncodeToString(b[:])
}
