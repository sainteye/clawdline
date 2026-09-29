package owned

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// One read of a terminal is one tmux invocation: `display -p` of the cursor
// and the modes, then `capture-pane -p -e` of the visible screen, joined with
// `;` so tmux answers both from the same moment. Two invocations could put a
// cursor from one screen over the rows of the next.
//
// `capture-pane` without `-J`: a line longer than the terminal is the rows it
// wrapped onto, because that is what is on the screen and what the cursor's
// row counts.

// frameFields are the modes and the cursor, in the order parseFrame reads
// them. `|` separates them because every value is a number or a word, and
// tmux 3.4 rewrites a control-character separator as its octal escape
// (internal/adapters/terminal/tmux.go splitPaneFields).
var frameFields = []string{
	"cursor_x", "cursor_y", "cursor_flag", "cursor_shape", "cursor_blinking",
	"keypad_cursor_flag", "keypad_flag",
	"mouse_standard_flag", "mouse_button_flag", "mouse_all_flag", "mouse_sgr_flag",
	"alternate_on", "window_width", "window_height", "pane_dead",
}

const frameMark = "CLT1"

func frameFormat() string {
	parts := []string{frameMark}
	for _, f := range frameFields {
		parts = append(parts, "#{"+f+"}")
	}
	return strings.Join(parts, "|")
}

// parseFrame reads the joined answer: the fields line, then the screen.
func parseFrame(out string, at time.Time) (terminal.Frame, error) {
	head, screen, _ := strings.Cut(out, "\n")
	fields := strings.Split(head, "|")
	if len(fields) != len(frameFields)+1 || fields[0] != frameMark {
		return terminal.Frame{}, fmt.Errorf("tmux answered a frame this cannot read: %q", head)
	}
	v := map[string]string{}
	for i, name := range frameFields {
		v[name] = fields[i+1]
	}
	num := func(name string) (int, error) {
		n, err := strconv.Atoi(v[name])
		if err != nil {
			return 0, fmt.Errorf("tmux answered %s=%q", name, v[name])
		}
		return n, nil
	}
	flag := func(name string) bool { return v[name] == "1" }

	f := terminal.Frame{At: at}
	var err error
	if f.Cols, err = num("window_width"); err != nil {
		return terminal.Frame{}, err
	}
	if f.Rows, err = num("window_height"); err != nil {
		return terminal.Frame{}, err
	}
	if f.Cursor.X, err = num("cursor_x"); err != nil {
		return terminal.Frame{}, err
	}
	if f.Cursor.Y, err = num("cursor_y"); err != nil {
		return terminal.Frame{}, err
	}
	f.Cursor.Visible = flag("cursor_flag")
	f.Cursor.Shape = terminal.CursorShape(v["cursor_shape"])
	f.Cursor.Blinking = flag("cursor_blinking")
	f.Modes = terminal.Modes{
		AppCursor: flag("keypad_cursor_flag"),
		AppKeypad: flag("keypad_flag"),
		MouseSGR:  flag("mouse_sgr_flag"),
		Alt:       flag("alternate_on"),
		Mouse:     terminal.MouseNone,
	}
	// The most a program asked for wins: 1003 reports everything 1002 does.
	switch {
	case flag("mouse_all_flag"):
		f.Modes.Mouse = terminal.MouseAny
	case flag("mouse_button_flag"):
		f.Modes.Mouse = terminal.MouseButton
	case flag("mouse_standard_flag"):
		f.Modes.Mouse = terminal.MouseStandard
	}
	f.Dead = flag("pane_dead")

	lines := strings.Split(strings.TrimSuffix(screen, "\n"), "\n")
	if screen == "" {
		lines = nil
	}
	// The screen is exactly Rows rows. A capture that raced a resize may be
	// one short or one long, and the frame still says how big it is.
	for len(lines) < f.Rows {
		lines = append(lines, "")
	}
	f.Lines = lines[:f.Rows]
	f.Rev = revision(f)
	return f, nil
}

// revision digests everything a viewer draws, and not the time it was read.
func revision(f terminal.Frame) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d %d %+v %+v %t\n", f.Cols, f.Rows, f.Cursor, f.Modes, f.Dead)
	for _, line := range f.Lines {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// listFields are one terminal in `list-sessions`, the path last because it is
// the one value that may itself contain `|`.
var listFields = []string{
	"session_name", "@clawdline_terminal", "@clawdline_project", "@clawdline_created",
	"window_width", "window_height", "pane_dead", "pane_current_path",
}

func listFormat() string {
	parts := []string{frameMark}
	for _, f := range listFields {
		parts = append(parts, "#{"+f+"}")
	}
	return strings.Join(parts, "|")
}

// parseList reads `list-sessions`. A session that is not one of ours — no
// `clt-` name, or no id matching it — is left out: somebody may have made one
// by hand on this server, and it is not a terminal this daemon opened.
func parseList(out string) []terminal.Terminal {
	var terms []terminal.Terminal
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, "|", len(listFields)+1)
		if len(parts) != len(listFields)+1 || parts[0] != frameMark {
			continue
		}
		name, id := parts[1], terminal.ID(parts[2])
		if !id.Valid() || id.SessionName() != name {
			continue
		}
		t := terminal.Terminal{ID: id, ProjectID: parts[3], Status: terminal.Running, Dir: parts[8]}
		t.Created, _ = time.Parse(time.RFC3339, parts[4])
		t.Cols, _ = strconv.Atoi(parts[5])
		t.Rows, _ = strconv.Atoi(parts[6])
		if parts[7] == "1" {
			t.Status = terminal.Exited
		}
		terms = append(terms, t)
	}
	return terms
}
