package transcript

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Shells finds the commands a Claude session left running in the background —
// the Swift app's `Shells`, reproduced rule for rule.
//
// Every `Bash` call gets an output file of its own:
//
//	/tmp/claude-<uid>/<project>/<session>/tasks/<task-id>.output
//
// **The file alone does not settle it.** A background command that finished
// has `[exited with code 0]` (or `[killed]`) written under its last line, and a
// foreground one has its file deleted when it returns — but a foreground one
// that was interrupted leaves its file behind with no marker, looking exactly
// like a build still going. So the transcript is the second fact: Claude Code
// answers a backgrounded call with "Command running in background with ID: …",
// and only an id announced that way, whose file has no ending under it, is a
// running command.
//
// Nothing here is promised by anybody. A session whose files say nothing this
// recognises has no shells, never an error.
type Shells struct {
	// Root is where Claude Code writes the output files: `/tmp` literally, not
	// the per-process temporary directory, which is not where the writer put
	// them.
	Root string
	UID  int
	Now  func() time.Time

	mu     sync.Mutex
	reads  map[string]shellRead
	starts map[string]announcements
}

type shellRead struct {
	signature string
	ended     bool
	last      string
}

type announcements struct {
	offset int64
	found  map[string]asked
}

type asked struct {
	command string
	what    string
}

const (
	// shellLiveWindow is how long a silent command stays believable. Past it,
	// it either ended without a marker or is the rarest thing on the list, and
	// a line that never goes away is worse than one that is late.
	shellLiveWindow = 12 * time.Hour
	// shellsShown is how many are reported per session, newest first.
	shellsShown = 6
	// shellTail is how much of an output file's end is read for its last line.
	shellTail = 8 << 10
	// shellRoom is how much of one line is carried: the width of a phone.
	shellRoom = 160

	backgroundAnnouncement = "Command running in background with ID: "
	backgroundFlag         = "run_in_background"
)

func NewShells() *Shells {
	return &Shells{
		Root:   "/tmp",
		UID:    os.Getuid(),
		Now:    time.Now,
		reads:  map[string]shellRead{},
		starts: map[string]announcements{},
	}
}

// Folder is where the session whose transcript is at transcriptPath keeps its
// command output. Built from the same two names the transcript path is.
func (s *Shells) Folder(transcriptPath string) string {
	name := strings.TrimSuffix(filepath.Base(transcriptPath), filepath.Ext(transcriptPath))
	project := filepath.Base(filepath.Dir(transcriptPath))
	if name == "" || project == "" || project == "." || project == string(filepath.Separator) {
		return ""
	}
	return filepath.Join(s.Root, fmt.Sprintf("claude-%d", s.UID), project, name, "tasks")
}

// Running is every background command still going for this transcript's
// session, newest first.
//
// The ordinary case is a session with nothing running, and it costs one
// directory listing: only a session with an unfinished file opens its
// transcript.
func (s *Shells) Running(transcriptPath string) []session.Shell {
	folder := s.Folder(transcriptPath)
	if folder == "" {
		return nil
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	now := s.Now()
	var unfinished []session.Shell
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".output") {
			continue
		}
		id := strings.TrimSuffix(name, ".output")
		if id == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		at := info.ModTime()
		if now.Sub(at) >= shellLiveWindow {
			continue
		}
		file := filepath.Join(folder, name)
		read := s.read(file, fmt.Sprintf("%d-%d", at.UnixNano(), info.Size()))
		if read.ended {
			continue
		}
		unfinished = append(unfinished, session.Shell{ID: id, At: at, Doing: read.last})
	}
	if len(unfinished) == 0 {
		return nil
	}
	if _, err := os.Stat(transcriptPath); err != nil {
		return nil
	}
	announced := s.announced(transcriptPath)

	out := make([]session.Shell, 0, len(unfinished))
	for _, sh := range unfinished {
		was, ok := announced[sh.ID]
		if !ok {
			continue
		}
		sh.Command, sh.What = was.command, was.what
		out = append(out, sh)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > shellsShown {
		out = out[:shellsShown]
	}
	return out
}

// read is what one output file's tail says, once per version of the file.
func (s *Shells) read(path, signature string) shellRead {
	s.mu.Lock()
	if hit, ok := s.reads[path]; ok && hit.signature == signature {
		s.mu.Unlock()
		return hit
	}
	s.mu.Unlock()

	out := shellRead{signature: signature}
	if data, _, err := tailData(path, shellTail); err == nil {
		last := ""
		for _, line := range strings.Split(string(data), "\n") {
			if t := trimSpaces(line); t != "" {
				last = t
			}
		}
		switch {
		case last == "":
		case isEnding(last):
			out.ended = true
		default:
			out.last = clipped(last)
		}
	}

	s.mu.Lock()
	s.reads[path] = out
	s.mu.Unlock()
	return out
}

// isEnding is one of the two markers Claude Code writes under a background
// command once it is over — matched by its words, not by its brackets, because
// a program's own log line can be bracketed too.
func isEnding(line string) bool {
	return (strings.HasPrefix(line, "[exited") && strings.HasSuffix(line, "]")) || line == "[killed]"
}

// announced is every background command the transcript has announced, by id.
//
// Read forward from where the last read stopped, not from the end: the command
// still running after ninety minutes is the one whose announcement is furthest
// back. A file shorter than the offset was replaced, and is read again.
//
// Two records are joined by the tool call between them: the assistant's
// `tool_use`, marked `run_in_background`, carries the command, and the
// `tool_result` answering it carries the id Claude Code minted. Every line is
// tested for its substring before anything is decoded.
func (s *Shells) announced(path string) map[string]asked {
	s.mu.Lock()
	seen, had := s.starts[path]
	s.mu.Unlock()

	found := map[string]asked{}
	offset := int64(0)
	if had {
		offset = seen.offset
		for k, v := range seen.found {
			found[k] = v
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return found
	}
	defer f.Close()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return found
	}
	if size < offset {
		offset = 0
		found = map[string]asked{}
	}
	if size <= offset {
		return found
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return found
	}

	// Only within this pass: a command whose two records straddle two reads
	// keeps its id and loses its command line, which is the right way round
	// to fail — the id is what decides whether anything is running.
	calls := map[string]asked{}
	r := bufio.NewReaderSize(io.LimitReader(f, size-offset), 256<<10)
	advanced := offset
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// A line with no newline yet is still being written; it is read
			// next time, whole.
			break
		}
		advanced += int64(len(line))
		if bytes.Contains(line, []byte(backgroundFlag)) {
			if rec, ok := decodeObject(line); ok {
				for _, block := range contentBlocks(rec) {
					if t, _ := block.str("type"); t != "tool_use" {
						continue
					}
					id, ok := block.str("id")
					if !ok {
						continue
					}
					input, ok := block.object("input")
					if !ok {
						continue
					}
					if bg, ok := input.boolean(backgroundFlag); !ok || !bg {
						continue
					}
					command, ok := input.str("command")
					if !ok {
						continue
					}
					was := asked{command: clipped(oneLine(command))}
					if what, ok := input.str("description"); ok {
						was.what = clipped(oneLine(what))
					}
					calls[id] = was
				}
			}
		}
		if !bytes.Contains(line, []byte(backgroundAnnouncement)) {
			continue
		}
		rec, ok := decodeObject(line)
		if !ok {
			continue
		}
		for _, block := range contentBlocks(rec) {
			if t, _ := block.str("type"); t != "tool_result" {
				continue
			}
			text, _ := block.str("content")
			i := strings.Index(text, backgroundAnnouncement)
			if i < 0 {
				continue
			}
			rest := text[i+len(backgroundAnnouncement):]
			n := 0
			for n < len(rest) && isASCIIAlnum(rest[n]) {
				n++
			}
			if n == 0 {
				continue
			}
			call, _ := block.str("tool_use_id")
			found[rest[:n]] = calls[call]
		}
	}

	s.mu.Lock()
	s.starts[path] = announcements{offset: advanced, found: found}
	s.mu.Unlock()
	return found
}

func contentBlocks(rec object) []object {
	message, ok := rec.object("message")
	if !ok {
		return nil
	}
	blocks, _ := message.objects("content")
	return blocks
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// oneLine is a command as one line: its newlines become the spaces they read
// as, since it goes into a row one line tall.
func oneLine(text string) string {
	parts := strings.FieldsFunc(text, isNewline)
	kept := parts[:0]
	for _, p := range parts {
		if t := trimSpaces(p); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, " ")
}

func isNewline(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// trimSpaces trims what Foundation calls whitespace without newlines: tabs
// and the space separators.
func trimSpaces(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '\t' || unicode.In(r, unicode.Zs) })
}

// clipped keeps a line to what a phone-width row can show. `curl` of a web
// page prints a hundred kilobytes without a newline.
func clipped(line string) string {
	if utf8.RuneCountInString(line) <= shellRoom {
		return line
	}
	runes := []rune(line)
	return string(runes[:shellRoom-1]) + "…"
}

const (
	// MaxShellOutput is the most of one command's output a reader is sent:
	// its tail, never more. The Cloud word pins the same window
	// (`internal/app/cloudops`, the `shell` op).
	MaxShellOutput = 1 << 20
	// ShellOutputFloor is the least; a smaller ask is raised to it.
	ShellOutputFloor = 1 << 10
	// ShellOutputDefault is what a reader that names no window gets.
	ShellOutputDefault = 64 << 10
	// maxShellID is the longest id taken from a route. Claude Code's are nine
	// characters; this only keeps a hostile one from being a long string.
	maxShellID = 64
)

// ShellOutput is one background command's output as the Shell panel reads it
// — the Swift app's `Shells.output(of:id:bytes:)`, and its answer's four
// fields.
type ShellOutput struct {
	// Shell is the command's row: what started it and when it last printed.
	// Doing is empty once it has ended, as it is on the session row.
	Shell session.Shell
	// Text is the tail of the file, starting at a line boundary when it was
	// cut.
	Text string
	// Truncated is whether the file holds more than Text, before it.
	Truncated bool
	// Ended is whether the last line written is Claude Code's ending marker.
	Ended bool
	// Signature changes whenever the file does, so a reader repaints only when
	// bytes moved.
	Signature string
}

// ValidShellID is whether id can name an output file in the folder and
// nothing else: ASCII letters and digits only, which is every id Claude Code
// has minted on this machine (`b0aau3e6s`) and the only characters the
// announcement parser above accepts.
func ValidShellID(id string) bool {
	if id == "" || len(id) > maxShellID {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !isASCIIAlnum(id[i]) {
			return false
		}
	}
	return true
}

// Output is the tail of one background command's output, at most window
// bytes of it.
//
// Only an id the transcript announced as a background command is read, the
// same rule Running keeps — but whether or not it has ended, because a reader
// watching a command must still see how it finished. Anything else, and an
// announced id whose file is gone, is not found.
func (s *Shells) Output(transcriptPath, id string, window int64) (ShellOutput, bool) {
	if !ValidShellID(id) {
		return ShellOutput{}, false
	}
	folder := s.Folder(transcriptPath)
	if folder == "" {
		return ShellOutput{}, false
	}
	if _, err := os.Stat(transcriptPath); err != nil {
		return ShellOutput{}, false
	}
	was, ok := s.announced(transcriptPath)[id]
	if !ok {
		return ShellOutput{}, false
	}
	file := filepath.Join(folder, id+".output")
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return ShellOutput{}, false
	}
	window = min(max(window, ShellOutputFloor), MaxShellOutput)
	data, whole, err := tailData(file, window)
	if err != nil {
		return ShellOutput{}, false
	}
	out := ShellOutput{
		Shell:     session.Shell{ID: id, At: info.ModTime(), Command: was.command, What: was.what},
		Text:      string(data),
		Truncated: !whole,
		Signature: fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size()),
	}
	last := ""
	for _, line := range strings.Split(out.Text, "\n") {
		if t := trimSpaces(line); t != "" {
			last = t
		}
	}
	switch {
	case last == "":
	case isEnding(last):
		out.Ended = true
	default:
		out.Shell.Doing = clipped(last)
	}
	return out, true
}
