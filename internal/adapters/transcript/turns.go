package transcript

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/artifacts"
	"github.com/sainteye/clawdline-go/internal/config"
)

// The kinds of entry a conversation is read into. They are the Swift app's
// `Transcript.Entry.Kind`, because the console that draws them is a copy of
// the one that reads that app's wire: a kind this reader invented would be a
// kind that page has no way to draw.
const (
	KindUser       = "user"
	KindAssistant  = "assistant"
	KindPeer       = "peer"    // another Claude Code session addressing this one
	KindMessage    = "message" // another session addressing this one through Clawdline
	KindNotice     = "notice"  // a versioned Clawdline message, not a person's words
	KindTool       = "tool"    // a tool being called
	KindToolResult = "toolResult"
)

// ReadBudget is how much of a record's end one transcript read looks at: the
// Swift app's `Transcript.tail(of:bytes: 8 << 20)`. Only the newest entries
// are ever wanted, and a Codex rollout on this Mac is 900 MB.
const ReadBudget = 8 << 20

// AskTool is the tool Claude Code stops on when it wants a decision rather
// than a file, and AskMarker is what that call's text starts with. A question
// is the one call whose arguments are the content, so it carries them — as
// JSON after a marker no transcript contains and no keyboard produces — and a
// renderer that has not been taught about it can tell data from a summary.
const (
	AskTool   = "AskUserQuestion"
	AskMarker = "\x01ask\x01"
)

// Entry is one thing in a conversation, in the Swift app's shape.
//
// Text is what a reader sees. On a tool call it is the one line describing
// what the tool was asked to do — a command, a path, a query — never the
// payload, except for AskTool, whose questions are the point.
type Entry struct {
	Kind string
	Text string
	// Tool names the tool on a call. A result carries none.
	Tool string
	// At is Unix seconds, or 0 when the record carried no timestamp.
	At         int64
	ImageCount int
	// Source and SourceMode say who spoke on a peer or message entry;
	// SourceAssistant says which assistant sent a Clawdline message.
	Source          string
	SourceMode      string
	SourceAssistant string
	Notice          *Notice
	// ArtifactIDs are the pictures an assistant turn showed with
	// `<clawdline-image id="…">`, in order. Ids only: what each one is — a
	// picture, an expired one, an unknown one — is asked of the image stores
	// when the entry is served, because that answer changes while the
	// transcript does not.
	ArtifactIDs []string
	// Artifacts are the pictures a Clawdline session message carried, as its
	// envelope described them.
	Artifacts   []ImageRef
	FileChanges []FileChange
	Plan        []PlanStep
	Activity    *Activity

	// Claude records a message from another session twice: once when it is
	// queued and once when it is delivered. These two tell the copies apart,
	// and never leave this package.
	peerMessageID string
	peerDelivery  bool
}

// FileChange is one file a tool changed. Codex carries the exact patch and a
// Claude `Write` carries the whole new file; either is kept as it was
// recorded rather than being turned into prose.
type FileChange struct {
	Path        string
	Kind        string
	UnifiedDiff *string
	Content     *string
	MovePath    *string
}

// PlanStep is one checklist row. The status is Codex app-server's spelling
// (`inProgress`), not the rollout's (`in_progress`).
type PlanStep struct {
	Step   string
	Status string
}

// Activity is what a Codex tool item recorded about itself beyond its one
// line: `called` for an MCP call with a title, `explored` for a shell
// command Codex already classified as reads and searches.
type Activity struct {
	Kind   string
	Title  *string
	Status *string
	// DurationMs is nil when the record did not say how long it took.
	DurationMs *int64
	Result     *string
	Actions    []Action
}

// Action is one read or search inside an explored command. A field is nil
// when the record did not carry it, which is different from carrying "".
type Action struct {
	Kind    string
	Command *string
	Name    *string
	Path    *string
	Query   *string
}

// Notice is a decoded Clawdline notice. Only the fields its kind carries are
// set; Body is the prose the entry shows.
type Notice struct {
	Kind               string
	Audience           string
	Task               *NoticeTask
	State              string
	ResultPath         string
	Outstanding        int64
	ClaimsReleased     bool
	ChildMayStillWrite bool
	NoticeID           string
	AckPath            string
	Overlaps           []NoticeOverlap
	WaitID             string
	Repository         string
	Paths              []string
	WaiterSessionID    string
	Reason             string
	ReleaseCondition   string
	Commit             string
	Note               string
	HandoffID          string
	Assistant          string
	ProjectDir         string
	Title              string
	Body               string
}

type NoticeTask struct {
	ID    string
	Title string
}

type NoticeOverlap struct {
	Task NoticeTask
	Path string
}

// Page is one read of a record: its newest entries, oldest first, and the
// signature of the bytes they were read from.
type Page struct {
	Entries   []Entry
	Signature string
	// Unread is how many bytes before the read window were never looked at,
	// when the window ran out before `limit` entries were found: the
	// conversation goes back further than Entries shows. Zero when the read
	// reached the record's start, or found all it was asked for first. The
	// Swift app cuts the same window and does not say so (limits N17).
	Unread int64
}

// ReadClaude returns the newest `limit` entries of a Claude conversation.
func ReadClaude(path string, limit int) (Page, error) {
	return readPage(path, func(r io.ReaderAt, size int64) ([]Entry, int64) {
		return parseClaude(r, size, limit)
	})
}

// ReadCodex returns the newest `limit` entries of a Codex thread.
func ReadCodex(path string, limit int) (Page, error) {
	return readPage(path, func(r io.ReaderAt, size int64) ([]Entry, int64) {
		return parseCodex(r, size, limit)
	})
}

// readPage reads one stable snapshot: if the file moved while its tail was
// being read, it is read once more, so the entries and the signature describe
// the same bytes. A record that is not there is ErrNoRecord, and one that
// could not be opened is an UnreadableError.
func readPage(path string, parse func(io.ReaderAt, int64) ([]Entry, int64)) (Page, error) {
	var page Page
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.Open(path)
		if err != nil {
			return Page{}, recordError(err)
		}
		before, err := f.Stat()
		if err != nil {
			f.Close()
			return Page{}, recordError(err)
		}
		entries, unread := parse(f, before.Size())
		page = Page{
			Entries:   entries,
			Signature: signature(before),
			Unread:    unread,
		}
		f.Close()
		after, err := os.Stat(path)
		if err != nil || signature(after) == page.Signature {
			break
		}
	}
	return page, nil
}

func signature(info os.FileInfo) string {
	return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix())
}

// eachLineFromEnd hands every non-empty line of the last ReadBudget bytes to
// body, newest first, until body returns false.
//
// The window is read backwards a chunk at a time, so the cost follows how far
// back the answer is rather than how large the file is. The first line of a
// window that does not start at byte zero was almost certainly cut by the
// seek, and is dropped — as the Swift app drops it.
//
// It answers how many bytes it never handed over because the window ran out
// before body had what it wanted: everything before the window, and the line
// the seek cut. Zero when body said it was done, or the window reached the
// start of the record. That number is what the Swift app's reader throws away
// without a word (limits N17), and a page that shows it can say so.
func eachLineFromEnd(r io.ReaderAt, size int64, body func([]byte) bool) (unread int64) {
	floor := size - ReadBudget
	if floor < 0 {
		floor = 0
	}
	pos := size
	var carry []byte
	for pos > floor {
		step := int64(chunk)
		if pos-floor < step {
			step = pos - floor
		}
		pos -= step
		buf := make([]byte, step, step+int64(len(carry)))
		if _, err := r.ReadAt(buf, pos); err != nil && err != io.EOF {
			// Everything from here back is unread, and nothing says whether
			// the answer was in it.
			return pos + step
		}
		data := append(buf, carry...)
		// Everything after the first newline is whole; what comes before it
		// waits for the next chunk to bring its beginning.
		first := bytes.IndexByte(data, '\n')
		if first < 0 {
			carry = data
			continue
		}
		rest := data[first+1:]
		for len(rest) > 0 {
			cut := bytes.LastIndexByte(rest, '\n')
			line := rest[cut+1:]
			if len(line) > 0 && !body(line) {
				return 0
			}
			if cut < 0 {
				break
			}
			rest = rest[:cut]
		}
		carry = data[:first]
	}
	if floor == 0 {
		if len(carry) > 0 {
			body(carry)
		}
		return 0
	}
	return floor + int64(len(carry))
}

// ---------- Claude ----------

func parseClaude(r io.ReaderAt, size int64, limit int) ([]Entry, int64) {
	var newestFirst []Entry
	// While a turn is running, Claude records a queued cross-session message
	// and later its delivered peer turn, sometimes a whole busy turn apart. The
	// delivery wins for its (source, text); distinct delivery ids stay
	// distinct, because somebody can really send the same words twice.
	deliveredIDs := map[string]bool{}
	deliveredKeys := map[string]bool{}
	queuedKeys := map[string]bool{}

	unread := eachLineFromEnd(r, size, func(line []byte) bool {
		rowEntries := claudeEntries(line)
		for i := len(rowEntries) - 1; i >= 0; i-- {
			e := rowEntries[i]
			if e.Kind == KindPeer {
				key := e.Source + "\x00" + e.Text
				if e.peerDelivery {
					if e.peerMessageID != "" {
						receipt := e.Source + "\x00" + e.peerMessageID
						if deliveredIDs[receipt] {
							continue
						}
						deliveredIDs[receipt] = true
					} else if deliveredKeys[key] {
						continue
					}
					deliveredKeys[key] = true
					kept := newestFirst[:0]
					for _, q := range newestFirst {
						drop := q.Kind == KindPeer && !q.peerDelivery &&
							q.Source+"\x00"+q.Text == key &&
							(q.At == 0 || e.At == 0 || q.At <= e.At)
						if !drop {
							kept = append(kept, q)
						}
					}
					newestFirst = kept
					delete(queuedKeys, key)
				} else {
					if deliveredKeys[key] || queuedKeys[key] {
						continue
					}
					queuedKeys[key] = true
				}
			}
			newestFirst = append(newestFirst, e)
		}
		return len(newestFirst) < limit
	})
	return oldestFirst(newestFirst, limit), unread
}

func oldestFirst(newestFirst []Entry, limit int) []Entry {
	out := make([]Entry, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		out = append(out, newestFirst[i])
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// claudeEntries is the entries one Claude row yields, in the order they were
// written: the Swift app's `Transcript.entries(inRow:)`. Anything it does not
// recognise yields nothing rather than a guess.
func claudeEntries(line []byte) []Entry {
	row, ok := decodeObject(line)
	if !ok {
		return nil
	}
	typ, _ := row.str("type")
	if typ != "user" && typ != "assistant" && typ != "queue-operation" && typ != "system" {
		return nil
	}
	// Sidechains are subagents talking among themselves.
	if v, ok := row.boolean("isSidechain"); ok && v {
		return nil
	}
	at := row.time("timestamp")

	// A delivered peer message is meta on purpose, so it is recognised by its
	// structured origin before meta rows are dropped.
	if origin, ok := row.object("origin"); ok {
		if kind, _ := origin.str("kind"); kind == "peer" {
			if body, ok := origin.str("body"); ok {
				text := strings.TrimSpace(body)
				if text == "" {
					return nil
				}
				return []Entry{{
					Kind: KindPeer, Text: text, At: at,
					Source:        origin.clean("name"),
					SourceMode:    origin.clean("fromMode"),
					peerMessageID: origin.clean("msg_id"),
					peerDelivery:  true,
				}}
			}
		}
	}
	if v, ok := row.boolean("isMeta"); ok && v {
		return nil
	}

	// A slash command submitted from the page is a system row; every other
	// system row is bookkeeping.
	if typ == "system" {
		raw, ok := row.str("content")
		if !ok || !strings.Contains(raw, "<command-name>") || !strings.Contains(raw, "</command-name>") ||
			(strings.Contains(raw, "<command-args>") && !strings.Contains(raw, "</command-args>")) {
			return nil
		}
		typed, ok := slashCommand(raw)
		if !ok {
			return nil
		}
		return []Entry{{Kind: KindUser, Text: typed, At: at}}
	}

	// Input typed during a turn is queued rather than written as a user row,
	// and is still something somebody said.
	if typ == "queue-operation" {
		op, _ := row.str("operation")
		raw, ok := row.str("content")
		if op != "enqueue" || !ok {
			return nil
		}
		if e, ok := sessionMessage(raw, at); ok {
			return []Entry{e}
		}
		if e, ok := crossSessionMessage(raw, at); ok {
			return []Entry{e}
		}
		if n, ok := decodeNotice(raw); ok {
			return []Entry{{Kind: KindNotice, Text: n.Body, At: at, Notice: n}}
		}
		text := strings.TrimSpace(withoutMachineBlocks(raw))
		text, images := canonicalImageContent(text, 0, true)
		if text == "" {
			return nil
		}
		return []Entry{{Kind: KindUser, Text: text, At: at, ImageCount: images}}
	}

	message, ok := row.object("message")
	if !ok {
		return nil
	}
	var blocks []object
	if list, ok := message.objects("content"); ok {
		blocks = list
	} else if text, ok := message.str("content"); ok {
		blocks = []object{textBlock(text)}
	}

	// The envelope owns the whole turn, so it is only recognised when it is
	// the turn's one text block.
	if typ == "user" && len(blocks) == 1 {
		if kind, _ := blocks[0].str("type"); kind == "text" {
			if raw, ok := blocks[0].str("text"); ok {
				if e, ok := sessionMessage(raw, at); ok {
					return []Entry{e}
				}
				if n, ok := decodeNotice(raw); ok {
					return []Entry{{Kind: KindNotice, Text: n.Body, At: at, Notice: n}}
				}
			}
		}
	}

	// Pasted images: the blocks own the count, and the `[Image #N]` markers in
	// the prose are presentation, not authored text.
	if typ == "user" {
		images := 0
		for _, b := range blocks {
			if kind, _ := b.str("type"); kind == "image" {
				images++
			}
		}
		if images > 0 {
			parts := []string{}
			for _, b := range blocks {
				if kind, _ := b.str("type"); kind != "text" {
					continue
				}
				raw, _ := b.str("text")
				if t := strings.TrimSpace(withoutMachineBlocks(raw)); t != "" {
					parts = append(parts, t)
				}
			}
			text, count := canonicalImageContent(strings.Join(parts, "\n"), images, false)
			return []Entry{{Kind: KindUser, Text: text, At: at, ImageCount: count}}
		}
		// Images handed over as drop-cache paths instead.
		parts := []string{}
		for _, b := range blocks {
			if kind, _ := b.str("type"); kind != "text" {
				continue
			}
			raw, _ := b.str("text")
			if t := withoutMachineBlocks(raw); t != "" {
				parts = append(parts, t)
			}
		}
		if text, count := canonicalImageContent(strings.Join(parts, "\n"), 0, false); count > 0 {
			return []Entry{{Kind: KindUser, Text: text, At: at, ImageCount: count}}
		}
	}

	var out []Entry
	// One budget of pictures for the whole turn, which arrives here a block at
	// a time (`Transcript.assistantEntry`'s `limit`).
	pictures := artifacts.ProductionPolicy.MaxImagesPerMessage
	prose := func(raw string) {
		if e, ok := assistantEntry(raw, at, pictures); ok {
			pictures -= len(e.ArtifactIDs)
			out = append(out, e)
		}
	}
	for _, b := range blocks {
		kind, _ := b.str("type")
		switch kind {
		case "text":
			raw, _ := b.str("text")
			if typ != "user" {
				prose(raw)
				continue
			}
			// A slash command goes back in as the line that was typed, and
			// what it printed is filed as that call's result.
			text := strings.TrimSpace(withoutMachineBlocks(raw))
			if typed, ok := slashCommand(raw); ok {
				if text == "" {
					text = typed
				} else {
					text = typed + "\n" + text
				}
			}
			if text != "" {
				out = append(out, Entry{Kind: KindUser, Text: text, At: at})
			}
			if printed, ok := commandOutput(raw); ok {
				out = append(out, Entry{Kind: KindToolResult, Text: printed, At: at})
			}
		case "tool_use":
			name, ok := b.str("name")
			if !ok {
				name = "tool"
			}
			input, _ := b.object("input")
			if name == "Write" {
				path, _ := input.str("file_path")
				content, hasContent := input.str("content")
				if path != "" && hasContent {
					out = append(out, Entry{Kind: KindTool, Text: path, Tool: name, At: at,
						FileChanges: []FileChange{{Path: path, Kind: "write", Content: &content}}})
					continue
				}
			}
			text := ""
			asked := false
			if name == AskTool {
				text, asked = askPayload(input)
			}
			if !asked {
				text = summarise(input)
			}
			out = append(out, Entry{Kind: KindTool, Text: text, Tool: name, At: at})
		case "tool_result":
			// A successful file creation is repeated as a receipt after the
			// `Write` that already carries the whole file.
			if result, ok := row.object("toolUseResult"); ok {
				rt, _ := result.str("type")
				path, _ := result.str("filePath")
				_, hasContent := result.str("content")
				if rt == "create" && path != "" && hasContent {
					continue
				}
			}
			text := firstLineOfContent(b["content"])
			if text == "" {
				continue
			}
			out = append(out, Entry{Kind: KindToolResult, Text: text, At: at})
		case "thinking":
			// Claude Code writes its short prose between tool calls as a
			// `thinking` block. Its signature says whether it is narration or
			// reasoning; reasoning is never shown.
			if typ == "user" {
				continue
			}
			raw, _ := b.str("thinking")
			if strings.TrimSpace(raw) == "" || thinkingKind(b) == "thinking" {
				continue
			}
			prose(raw)
		}
	}
	return out
}

func textBlock(text string) object {
	raw, _ := json.Marshal(text)
	return object{"type": json.RawMessage(`"text"`), "text": raw}
}

// thinkingKind reads the kind token out of the first 64 base64 characters of
// a thinking block's signature: "narration", "thinking", or "" when there is
// none to read.
func thinkingKind(b object) string {
	sig, ok := b.str("signature")
	if !ok {
		return ""
	}
	if len(sig) > 64 {
		sig = sig[:64]
	}
	sig = sig[:len(sig)-len(sig)%4]
	decoded, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return ""
	}
	if bytes.Contains(decoded, append([]byte{0x42, 0x09}, "narration"...)) {
		return "narration"
	}
	if bytes.Contains(decoded, append([]byte{0x42, 0x08}, "thinking"...)) {
		return "thinking"
	}
	return ""
}

// summarise is one line describing what a tool was asked to do. The fields
// are tried in the order a person reads them, so a Bash call shows its
// command rather than its description.
func summarise(input object) string {
	for _, key := range []string{"command", "file_path", "path", "pattern", "url", "query", "prompt", "description"} {
		if v, ok := input.str(key); ok && v != "" {
			return firstLineOf(v)
		}
	}
	return ""
}

// askPayload is an AskTool call's questions, marked, in the short keys the
// console reads: q (question), h (header), m (several answers), o (options,
// each l and d). False when the call is not recognisably a question.
func askPayload(input object) (string, bool) {
	asked, ok := input.objects("questions")
	if !ok {
		return "", false
	}
	type option struct {
		D string `json:"d,omitempty"`
		L string `json:"l"`
	}
	type question struct {
		H string   `json:"h,omitempty"`
		M bool     `json:"m,omitempty"`
		O []option `json:"o"`
		Q string   `json:"q"`
	}
	items := []question{}
	for _, q := range asked {
		row := question{Q: q.clean("question"), H: q.clean("header"), O: []option{}}
		if m, ok := q.boolean("multiSelect"); ok && m {
			row.M = true
		}
		options, _ := q.objects("options")
		for _, o := range options {
			label := o.clean("label")
			if label == "" {
				continue
			}
			row.O = append(row.O, option{L: label, D: o.clean("description")})
		}
		if row.Q == "" && len(row.O) == 0 {
			continue
		}
		items = append(items, row)
	}
	if len(items) == 0 {
		return "", false
	}
	encoded, ok := encodeJSON(items)
	if !ok {
		return "", false
	}
	return AskMarker + encoded, true
}

// firstLineOfContent is the first line of a tool result, whose content is a
// string on some rows and a list of text blocks on others.
func firstLineOfContent(raw json.RawMessage) string {
	var text string
	if s, ok := rawString(raw); ok {
		text = s
	} else if list, ok := rawObjects(raw); ok {
		parts := []string{}
		for _, b := range list {
			if t, ok := b.str("text"); ok {
				parts = append(parts, t)
			}
		}
		text = strings.Join(parts, " ")
	}
	return firstLineOf(text)
}

// firstLineOf is the first line of something a tool printed, without the
// escape sequences it printed for a terminal.
func firstLineOf(text string) string {
	text = strings.TrimSpace(plain(text))
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

var machineTags = []string{"task-notification", "system-reminder", "local-command-stdout",
	"local-command-stderr", "command-name", "command-message", "command-args"}

// withoutMachineBlocks takes out what Claude Code injects into the user's side
// of the conversation — reminders, notifications, a command's expansion —
// which nobody typed. Unknown tags are left alone.
func withoutMachineBlocks(text string) string {
	for _, tag := range machineTags {
		text = removingTag(tag, text)
	}
	return text
}

func removingTag(tag, text string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	if !strings.Contains(text, open) {
		return text
	}
	var out strings.Builder
	rest := text
	for {
		start := strings.Index(rest, open)
		if start < 0 {
			break
		}
		out.WriteString(rest[:start])
		end := strings.Index(rest[start+len(open):], close)
		if end < 0 {
			// Opened and never closed: a truncated record, and everything
			// after the opening is the block.
			return out.String()
		}
		rest = rest[start+len(open)+end+len(close):]
	}
	out.WriteString(rest)
	return out.String()
}

// slashCommand is the command a turn is, as the line somebody typed.
func slashCommand(text string) (string, bool) {
	name, ok := innerTag("command-name", text)
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return "", false
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	args, ok := innerTag("command-args", text)
	args = strings.TrimSpace(args)
	if !ok || args == "" {
		return name, true
	}
	return name + " " + args, true
}

// commandOutput is what a slash command printed, without its colours.
func commandOutput(text string) (string, bool) {
	parts := []string{}
	for _, tag := range []string{"local-command-stdout", "local-command-stderr"} {
		body, ok := innerTag(tag, text)
		if !ok {
			continue
		}
		if p := strings.TrimSpace(plain(body)); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n"), len(parts) > 0
}

func innerTag(tag, text string) (string, bool) {
	open, close := "<"+tag+">", "</"+tag+">"
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	body := text[start+len(open):]
	if end := strings.Index(body, close); end >= 0 {
		return body[:end], true
	}
	return body, true
}

// crossSessionMessage is Claude Code's queued form of a message another
// session sent. Only the one observed envelope, occupying the whole string,
// is recognised; somebody quoting the format stays ordinary prose.
func crossSessionMessage(raw string, at int64) (Entry, bool) {
	const tag = "cross-session-message"
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "<"+tag) {
		return Entry{}, false
	}
	openEnd := strings.IndexByte(text, '>')
	after := len(tag) + 1
	if openEnd < 0 || after > openEnd {
		return Entry{}, false
	}
	if c := text[after]; c != ' ' && c != '\t' && c != '>' {
		return Entry{}, false
	}
	close := "</" + tag + ">"
	closeStart := strings.LastIndex(text, close)
	if !strings.HasSuffix(text, close) || closeStart <= openEnd {
		return Entry{}, false
	}
	header := text[:openEnd+1]
	body := strings.TrimSpace(text[openEnd+1 : closeStart])
	if body == "" {
		return Entry{}, false
	}
	return Entry{Kind: KindPeer, Text: body, At: at,
		Source: tagAttribute("from-name", header), SourceMode: tagAttribute("from-mode", header)}, true
}

func tagAttribute(name, tag string) string {
	prefix := " " + name + `="`
	start := strings.Index(tag, prefix)
	if start < 0 {
		return ""
	}
	value := tag[start+len(prefix):]
	end := strings.IndexByte(value, '"')
	if end < 0 {
		return ""
	}
	// In the Swift app's order, one entity after another, so `&amp;lt;`
	// comes out as `&lt;` there and here alike.
	v := value[:end]
	for _, pair := range [][2]string{{"&quot;", `"`}, {"&apos;", "'"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&amp;", "&"}} {
		v = strings.ReplaceAll(v, pair[0], pair[1])
	}
	return v
}

// ---------- Codex ----------

func parseCodex(r io.ReaderAt, size int64, limit int) ([]Entry, int64) {
	var newestFirst []Entry
	unread := eachLineFromEnd(r, size, func(line []byte) bool {
		// Only two row shapes can yield anything, and both spell their marker
		// literally. Skipping the rest unparsed is most of what a rollout is.
		if !bytes.Contains(line, []byte("item_completed")) && !bytes.Contains(line, []byte("tools.update_plan(")) {
			return true
		}
		rowEntries := codexEntries(line)
		for i := len(rowEntries) - 1; i >= 0; i-- {
			newestFirst = append(newestFirst, rowEntries[i])
		}
		return len(newestFirst) < limit
	})
	return oldestFirst(newestFirst, limit), unread
}

// codexEntries is the Swift app's `Codex.entries(inRow:)`: finished
// `item_completed` records are the conversation, and one exec-wrapped
// `update_plan` call is admitted because Codex does not repeat the checklist
// as a finished item. Its input is read as inert literals and never run.
func codexEntries(line []byte) []Entry {
	row, ok := decodeObject(line)
	if !ok {
		return nil
	}
	payload, ok := row.object("payload")
	if !ok {
		return nil
	}
	at := row.time("timestamp")
	rowType, _ := row.str("type")
	payloadType, _ := payload.str("type")
	if rowType == "event_msg" && payloadType == "item_completed" {
		if item, ok := payload.object("item"); ok {
			return codexItemEntries(item, at)
		}
	}
	if rowType != "response_item" || payloadType != "custom_tool_call" {
		return nil
	}
	name, _ := payload.str("name")
	input, ok := payload.str("input")
	if name != "exec" || !ok {
		return nil
	}
	steps := literalPlan(input)
	if len(steps) == 0 {
		return nil
	}
	return []Entry{{Kind: KindTool, Text: "Updated Plan", Tool: "plan", At: at, Plan: steps}}
}

func codexItemEntries(item object, at int64) []Entry {
	entry := func(kind, text, tool string) (Entry, bool) {
		text = strings.TrimSpace(text)
		return Entry{Kind: kind, Text: text, Tool: tool, At: at}, text != ""
	}
	some := func(e Entry, ok bool) []Entry {
		if ok {
			return []Entry{e}
		}
		return nil
	}
	itemType, _ := item.str("type")
	switch itemType {
	case "UserMessage":
		if raw, ok := exactText(item["content"]); ok {
			if e, ok := sessionMessage(raw, at); ok {
				return []Entry{e}
			}
			if n, ok := decodeNotice(raw); ok {
				return []Entry{{Kind: KindNotice, Text: n.Body, At: at, Notice: n}}
			}
		}
		text, images := canonicalImageContent(codexText(item["content"]), 0, false)
		if images > 0 {
			return []Entry{{Kind: KindUser, Text: text, At: at, ImageCount: images}}
		}
		return some(entry(KindUser, text, ""))

	case "AgentMessage":
		if e, ok := assistantEntry(codexText(item["content"]), at, artifacts.ProductionPolicy.MaxImagesPerMessage); ok {
			return []Entry{e}
		}
		return nil

	case "CommandExecution":
		if actions := parsedActions(item["parsed_cmd"]); len(actions) > 0 {
			e, ok := entry(KindTool, exploredTitle(actions), "shell")
			if !ok {
				return nil
			}
			e.Activity = &Activity{
				Kind: "explored", Status: normalizedStatus(item.strPtr("status")),
				DurationMs: durationMs(item["duration"]), Actions: actions,
			}
			return []Entry{e}
		}
		out := some(entry(KindTool, codexCommand(item["command"]), "shell"))
		return append(out, some(entry(KindToolResult, commandOutcome(item), ""))...)

	case "McpToolCall":
		names := []string{}
		for _, key := range []string{"server", "tool"} {
			if v, ok := item.str(key); ok {
				names = append(names, v)
			}
		}
		name := strings.Join(names, ".")
		if name == "" {
			name = "mcp"
		}
		result, _ := item.object("result")
		if title := mcpTitle(item["arguments"]); title != "" {
			e, ok := entry(KindTool, title, name)
			if !ok {
				return nil
			}
			status := normalizedStatus(item.strPtr("status"))
			if isError, ok := result.boolean("isError"); ok && isError {
				failed := "failed"
				status = &failed
			}
			activity := &Activity{Kind: "called", Title: &title, Status: status,
				DurationMs: durationMs(item["duration"]), Actions: []Action{}}
			if detail := mcpResultDetail(result); detail != "" {
				activity.Result = &detail
			}
			e.Activity = activity
			return []Entry{e}
		}
		out := some(entry(KindTool, mcpArguments(item["arguments"]), name))
		return append(out, some(entry(KindToolResult, codexFirstLine(codexText(result["content"])), ""))...)

	case "FileChange":
		changes, _ := item.object("changes")
		e, ok := entry(KindTool, changedFiles(changes), "edit")
		if !ok {
			return nil
		}
		e.FileChanges = fileChanges(changes)
		return []Entry{e}

	// Codex's plugins arrive under one name with the kind beside it; the
	// kind is the tool as far as a reader is concerned.
	case "Extension":
		kind, ok := item.str("kind")
		if !ok {
			kind = "extension"
		}
		what, ok := item.str("query")
		if !ok {
			what = kind
		}
		return some(entry(KindTool, what, kind))
	}
	// Reasoning is encrypted by the time it reaches a rollout, and nothing
	// to show beats a row that says so.
	return nil
}

// codexText is the text out of a content array. Codex spells the block type
// `text` on the way in and `Text` on the way out, so the presence of text is
// what is matched.
func codexText(raw json.RawMessage) string {
	if s, ok := rawString(raw); ok {
		return s
	}
	list, ok := rawObjects(raw)
	if !ok {
		return ""
	}
	parts := []string{}
	for _, b := range list {
		if t, ok := b.str("text"); ok && t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// exactText is a user item's one textual body, so that a structured envelope
// is only recognised when it is the whole message.
func exactText(raw json.RawMessage) (string, bool) {
	if s, ok := rawString(raw); ok {
		return s, true
	}
	list, ok := rawObjects(raw)
	if !ok || len(list) != 1 {
		return "", false
	}
	return list[0].str("text")
}

// codexCommand is a command as somebody would read it: `/bin/zsh -lc "…"`
// unwrapped to the "…", and anything else joined as it is.
func codexCommand(raw json.RawMessage) string {
	var parts []string
	if json.Unmarshal(raw, &parts) != nil || !isArray(raw) {
		s, _ := rawString(raw)
		return s
	}
	if len(parts) == 3 && (parts[1] == "-lc" || parts[1] == "-c") {
		shell := ""
		for _, seg := range strings.Split(parts[0], "/") {
			if seg != "" {
				shell = seg
			}
		}
		switch shell {
		case "zsh", "bash", "sh", "fish":
			return parts[2]
		}
	}
	return strings.Join(parts, " ")
}

// commandOutcome is what a command did, in one line: its output, or the code
// it failed with.
func commandOutcome(item object) string {
	output, ok := item.str("aggregated_output")
	if !ok {
		if output, ok = item.str("stdout"); !ok {
			output, _ = item.str("stderr")
		}
	}
	if first := codexFirstLine(output); first != "" {
		return first
	}
	code, ok := item.integer("exit_code")
	if !ok || code == 0 {
		return ""
	}
	return "exit " + strconv.FormatInt(code, 10)
}

// codexFirstLine is the first line with anything on it. It stops there
// rather than splitting the whole of a large output.
func codexFirstLine(text string) string {
	for text != "" {
		end := strings.IndexByte(text, '\n')
		if end < 0 {
			return text
		}
		if end > 0 {
			return text[:end]
		}
		text = text[1:]
	}
	return ""
}

func mcpArguments(raw json.RawMessage) string {
	if s, ok := rawString(raw); ok {
		return codexFirstLine(s)
	}
	args, ok := rawObject(raw)
	if !ok {
		return ""
	}
	for _, key := range []string{"title", "query", "cmd", "command", "path", "code"} {
		if v, ok := args.str(key); ok && v != "" {
			return codexFirstLine(v)
		}
	}
	return ""
}

func mcpTitle(raw json.RawMessage) string {
	args, _ := rawObject(raw)
	title, _ := args.str("title")
	return codexFirstLine(title)
}

// mcpResultDetail is more than one line and still bounded: a result can be a
// whole page, and the transcript must not mirror it.
func mcpResultDetail(result object) string {
	if result == nil {
		return ""
	}
	value := strings.TrimSpace(codexText(result["content"]))
	if utf8.RuneCountInString(value) <= 4000 {
		return value
	}
	runes := []rune(value)
	return string(runes[:4000]) + "…"
}

func normalizedStatus(raw *string) *string {
	if raw == nil {
		return nil
	}
	var out string
	switch *raw {
	case "in_progress", "inProgress", "running":
		out = "inProgress"
	case "pending":
		out = "pending"
	case "completed", "success":
		out = "completed"
	case "failed", "error":
		out = "failed"
	default:
		return nil
	}
	return &out
}

func durationMs(raw json.RawMessage) *int64 {
	value, ok := rawObject(raw)
	if !ok {
		return nil
	}
	secs := value.truncated("secs")
	nanos := value.truncated("nanos")
	if secs < 0 || nanos < 0 || nanos >= 1_000_000_000 || secs > (1<<63-1)/1000 {
		return nil
	}
	ms := secs*1000 + nanos/1_000_000
	return &ms
}

// parsedActions keeps a command Codex already classified, when every part of
// it is a read or a search with something to show. Anything else falls back
// to an ordinary shell row rather than being guessed into this shape.
func parsedActions(raw json.RawMessage) []Action {
	list, ok := rawObjects(raw)
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]Action, 0, len(list))
	for _, v := range list {
		kind, _ := v.str("type")
		if kind != "read" && kind != "search" {
			return nil
		}
		a := Action{Kind: kind, Command: v.strPtr("cmd"), Name: v.strPtr("name"),
			Path: v.strPtr("path"), Query: v.strPtr("query")}
		visible := false
		for _, f := range []*string{a.Name, a.Path, a.Query, a.Command} {
			if f != nil && *f != "" {
				visible = true
			}
		}
		if !visible {
			return nil
		}
		out = append(out, a)
	}
	return out
}

func exploredTitle(actions []Action) string {
	first := func(values ...*string) string {
		for _, v := range values {
			if v != nil {
				return *v
			}
		}
		return ""
	}
	labels := []string{}
	for i, a := range actions {
		if i == 3 {
			break
		}
		if a.Kind == "search" {
			labels = append(labels, "Search "+first(a.Query, a.Path))
		} else {
			labels = append(labels, "Read "+first(a.Name, a.Path))
		}
	}
	title := strings.Join(labels, ", ")
	if len(actions) > 3 {
		title += " +" + strconv.Itoa(len(actions)-3)
	}
	return title
}

// changedFiles is the files a change touched, by name, with how many more
// there were when it is more than three.
func changedFiles(changes object) string {
	if len(changes) == 0 {
		return ""
	}
	names := []string{}
	for _, path := range sortedKeys(changes) {
		names = append(names, lastPathComponent(path))
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:3], ", ") + " +" + strconv.Itoa(len(names)-3)
}

func lastPathComponent(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		if path == "" {
			return ""
		}
		return "/"
	}
	return filepath.Base(trimmed)
}

// fileChanges is the lossless half of the same item: updates carry a unified
// diff, additions and deletions the file's content. Sorted by path so a
// re-read of the same rollout reads the same.
func fileChanges(changes object) []FileChange {
	out := []FileChange{}
	for _, path := range sortedKeys(changes) {
		change, ok := rawObject(changes[path])
		if !ok {
			continue
		}
		kind, _ := change.str("type")
		if kind == "" {
			kind = "change"
		}
		out = append(out, FileChange{Path: path, Kind: kind,
			UnifiedDiff: change.strPtr("unified_diff"), Content: change.strPtr("content"),
			MovePath: change.strPtr("move_path")})
	}
	return out
}

func sortedKeys(o object) []string {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var (
	planKey    = regexp.MustCompile(`\bplan\s*:\s*(\[|[A-Za-z_$][A-Za-z0-9_$]*)`)
	planMarker = "tools.update_plan("
)

// literalPlan extracts only a literal plan array from Codex's generated exec
// wrapper: `plan:[…]`, or `const p=[…]; …plan:p`. It accepts punctuation,
// identifiers and JSON strings, and nothing it would have to evaluate.
func literalPlan(source string) []PlanStep {
	call := strings.Index(source, planMarker)
	if len(source) > 100_000 || call < 0 {
		return nil
	}
	suffix := source[call+len(planMarker):]
	m := planKey.FindStringSubmatchIndex(suffix)
	if m == nil {
		return nil
	}
	token := suffix[m[2]:m[3]]
	var array string
	var ok bool
	if token == "[" {
		array, ok = bracketedArray(suffix, m[2])
	} else {
		prefix := source[:call]
		assignment := regexp.MustCompile(`(?:const|let|var)\s+` + regexp.QuoteMeta(token) + `\s*=\s*\[`)
		found := assignment.FindAllStringIndex(prefix, -1)
		if len(found) == 0 {
			return nil
		}
		array, ok = bracketedArray(prefix, found[len(found)-1][1]-1)
	}
	if !ok {
		return nil
	}
	p := planParser{b: []byte(array)}
	return p.parse()
}

// bracketedArray is the array opening at `open`, found without treating a
// bracket inside a JSON string as structure.
func bracketedArray(text string, open int) (string, bool) {
	depth := 0
	quoted, escaped := false, false
	for i := open; i < len(text); i++ {
		c := text[i]
		switch {
		case quoted:
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		case c == '"':
			quoted = true
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return text[open : i+1], true
			}
			if depth < 0 {
				return "", false
			}
		}
	}
	return "", false
}

type planParser struct {
	b []byte
	i int
}

func (p *planParser) parse() []PlanStep {
	if !p.take('[') {
		return nil
	}
	out := []PlanStep{}
	p.skipSpace()
	if p.take(']') {
		return out
	}
	for len(out) < 100 {
		step, ok := p.object()
		if !ok {
			return nil
		}
		out = append(out, step)
		p.skipSpace()
		if p.take(']') {
			if p.i == len(p.b) {
				return out
			}
			return nil
		}
		if !p.take(',') {
			return nil
		}
	}
	return nil
}

func (p *planParser) object() (PlanStep, bool) {
	if !p.take('{') {
		return PlanStep{}, false
	}
	var step, status *string
	for {
		key, ok := p.identifier()
		if !ok || !p.take(':') {
			return PlanStep{}, false
		}
		value, ok := p.str()
		if !ok {
			return PlanStep{}, false
		}
		switch {
		case key == "step" && step == nil:
			step = &value
		case key == "status" && status == nil:
			status = &value
		default:
			return PlanStep{}, false
		}
		p.skipSpace()
		if p.take('}') {
			break
		}
		if !p.take(',') {
			return PlanStep{}, false
		}
	}
	if step == nil {
		return PlanStep{}, false
	}
	text := strings.TrimSpace(*step)
	normal := normalizedStatus(status)
	if text == "" || normal == nil || *normal == "failed" {
		return PlanStep{}, false
	}
	return PlanStep{Step: text, Status: *normal}, true
}

func (p *planParser) identifier() (string, bool) {
	p.skipSpace()
	start := p.i
	for p.i < len(p.b) {
		c := p.b[p.i]
		letter := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		digit := p.i > start && c >= '0' && c <= '9'
		if !letter && !digit {
			break
		}
		p.i++
	}
	return string(p.b[start:p.i]), p.i > start
}

func (p *planParser) str() (string, bool) {
	p.skipSpace()
	if p.i >= len(p.b) || p.b[p.i] != '"' {
		return "", false
	}
	start := p.i
	p.i++
	escaped := false
	for p.i < len(p.b) {
		c := p.b[p.i]
		p.i++
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			var s string
			if json.Unmarshal(p.b[start:p.i], &s) != nil {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}

func (p *planParser) take(c byte) bool {
	p.skipSpace()
	if p.i >= len(p.b) || p.b[p.i] != c {
		return false
	}
	p.i++
	return true
}

func (p *planParser) skipSpace() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case '\t', '\n', '\r', ' ':
			p.i++
		default:
			return
		}
	}
}

// ---------- Clawdline's own envelopes ----------

// sessionMessage decodes a message one session sent another through
// Clawdline: `<clawdline-message>{…}</clawdline-message>` on one line, with a
// closed key set. Version 2 adds image artifacts, which this daemon does not
// resolve yet; the message itself is still recognised.
func sessionMessage(raw string, at int64) (Entry, bool) {
	const opening, closing = "<clawdline-message>", "</clawdline-message>"
	obj, ok := envelope(raw, opening, closing)
	if !ok {
		return Entry{}, false
	}
	proto, _ := obj.str("protocol")
	kind, _ := obj.str("kind")
	version, hasVersion := obj.integer("version")
	body, hasBody := obj.str("body")
	source, hasSource := obj.object("source")
	if proto != "clawdline.message" || kind != "session_message" || !hasVersion || !hasBody || !hasSource ||
		!sameKeys(source, "id", "label", "assistant") {
		return Entry{}, false
	}
	id, _ := source.str("id")
	label, _ := source.str("label")
	assistant, _ := source.str("assistant")
	if id == "" || label == "" || (assistant != "claude" && assistant != "codex") {
		return Entry{}, false
	}
	var refs []ImageRef
	switch version {
	case 1:
		if !sameKeys(obj, "protocol", "version", "kind", "source", "body") || body == "" {
			return Entry{}, false
		}
	case 2:
		policy := artifacts.ProductionPolicy
		list, ok := obj.objects("artifacts")
		if !sameKeys(obj, "protocol", "version", "kind", "source", "body", "artifacts") ||
			!ok || len(list) == 0 || len(list) > policy.MaxImagesPerMessage {
			return Entry{}, false
		}
		total := 0
		for _, item := range list {
			ref, ok := decodeImageRef(item)
			if !ok {
				return Entry{}, false
			}
			total += ref.ByteCount
			refs = append(refs, ref)
		}
		if total > policy.MaxTotalBytes {
			return Entry{}, false
		}
	default:
		return Entry{}, false
	}
	return Entry{Kind: KindMessage, Text: body, At: at,
		Source: label, SourceMode: "clawdline", SourceAssistant: assistant, Artifacts: refs}, true
}

// ImageRef is one picture a version-2 session message described.
type ImageRef struct {
	ID        string
	MediaType string
	ByteCount int
	Width     int
	Height    int
	ExpiresAt int64
}

// decodeImageRef is `SessionImageArtifact.decode`: exactly the six keys, whole
// numbers that are not booleans, and a reference the store could have made.
func decodeImageRef(o object) (ImageRef, bool) {
	if !sameKeys(o, "id", "media_type", "byte_count", "width", "height", "expires_at") {
		return ImageRef{}, false
	}
	id, ok1 := o.str("id")
	mediaType, ok2 := o.str("media_type")
	bytes, ok3 := o.integer("byte_count")
	width, ok4 := o.integer("width")
	height, ok5 := o.integer("height")
	expires, ok6 := o.integer("expires_at")
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
		return ImageRef{}, false
	}
	const most = int64(1) << 31
	if bytes > most || width > most || height > most {
		return ImageRef{}, false
	}
	a := artifacts.Artifact{ID: id, MediaType: mediaType, ByteCount: int(bytes),
		Width: int(width), Height: int(height), ExpiresAt: expires}
	if !a.Valid(artifacts.ProductionPolicy) {
		return ImageRef{}, false
	}
	return ImageRef{ID: a.ID, MediaType: a.MediaType, ByteCount: a.ByteCount,
		Width: a.Width, Height: a.Height, ExpiresAt: a.ExpiresAt}, true
}

// assistantEntry is `Transcript.assistantEntry`: one assistant turn with the
// image markers it honoured lifted out of its prose. A turn that was nothing
// but a picture is not empty — the marker was the whole message.
func assistantEntry(raw string, at int64, limit int) (Entry, bool) {
	text, ids := readImageMarkers(raw, limit)
	text = strings.TrimSpace(text)
	if text == "" && len(ids) == 0 {
		return Entry{}, false
	}
	return Entry{Kind: KindAssistant, Text: text, At: at, ArtifactIDs: ids}, true
}

// readImageMarkers is `SessionImageMarker.read`.
//
// Recognition is all or nothing: the one spelling around one opaque id, and
// anything else — a malformed tag, one past the limit, one inside a fenced
// code block, which is a reply *about* the format — stays exactly where it was
// written, because a marker that vanished silently looks like one that worked.
func readImageMarkers(raw string, limit int) (string, []string) {
	opening, closing := artifacts.MarkerOpening, artifacts.MarkerClosing
	if limit <= 0 || !strings.Contains(raw, opening) {
		return raw, nil
	}
	fenced := fencedRanges(raw)
	var b strings.Builder
	var ids []string
	cursor := 0
	for {
		rel := strings.Index(raw[cursor:], opening)
		if rel < 0 {
			break
		}
		open := cursor + rel
		after := open + len(opening)
		id, end := "", -1
		if len(ids) < limit && !inRanges(fenced, open) {
			if c := strings.Index(raw[after:], closing); c >= 0 {
				if candidate := raw[after : after+c]; artifacts.IsID(candidate) {
					id, end = candidate, after+c+len(closing)
				}
			}
		}
		if end < 0 {
			// Not honoured. Scanning resumes just past the opening, so a real
			// marker later in the turn is still found.
			b.WriteString(raw[cursor:after])
			cursor = after
			continue
		}
		ids = append(ids, id)
		from, to := markerRemoval(raw, open, end)
		b.WriteString(raw[cursor:max(from, cursor)])
		cursor = to
	}
	b.WriteString(raw[cursor:])
	return b.String(), ids
}

// fencedRanges are the line-anchored ``` and ~~~ blocks of one turn; an
// unclosed fence runs to the end, because that is what a reader sees too.
func fencedRanges(raw string) [][2]int {
	var ranges [][2]int
	openedAt, openChar, openRun := -1, byte(0), 0
	lineStart := 0
	for {
		lineEnd := len(raw)
		if i := strings.IndexByte(raw[lineStart:], '\n'); i >= 0 {
			lineEnd = lineStart + i
		}
		if ch, run, ok := fenceRun(raw[lineStart:lineEnd]); ok {
			if openedAt >= 0 {
				if ch == openChar && run >= openRun {
					ranges = append(ranges, [2]int{openedAt, lineEnd})
					openedAt = -1
				}
			} else {
				openedAt, openChar, openRun = lineStart, ch, run
			}
		}
		if lineEnd == len(raw) {
			break
		}
		lineStart = lineEnd + 1
	}
	if openedAt >= 0 {
		ranges = append(ranges, [2]int{openedAt, len(raw)})
	}
	return ranges
}

func fenceRun(line string) (byte, int, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0, false
	}
	mark, run := line[i], 0
	for i < len(line) && line[i] == mark {
		run++
		i++
	}
	return mark, run, run >= 3
}

func inRanges(ranges [][2]int, at int) bool {
	for _, r := range ranges {
		if at >= r[0] && at < r[1] {
			return true
		}
	}
	return false
}

// markerRemoval is what one honoured marker takes with it. Alone on its line
// it takes the line and its newline — and one blank line when it sat between
// two paragraphs, so a picture leaves one paragraph break rather than two. With
// words beside it, it takes only itself: those are the sentence somebody wrote.
func markerRemoval(raw string, start, end int) (int, int) {
	lineStart := start
	for lineStart > 0 {
		c := raw[lineStart-1]
		if c == ' ' || c == '\t' {
			lineStart--
			continue
		}
		if c == '\n' {
			break
		}
		return start, end
	}
	lineEnd := end
	for lineEnd < len(raw) {
		c := raw[lineEnd]
		if c == ' ' || c == '\t' {
			lineEnd++
			continue
		}
		if c != '\n' {
			return start, end
		}
		cut := lineEnd + 1
		if lineStart > 0 && raw[lineStart-1] == '\n' && cut < len(raw) && raw[cut] == '\n' {
			cut++
		}
		return lineStart, cut
	}
	return lineStart, lineEnd
}

// decodeNotice decodes Clawdline's own notice about a task, a wait or a
// handoff: `<clawdline-notice>{…}</clawdline-notice>`, with each kind's
// closed key set, exactly as the Swift app's `ClawdlineMessage.decode`.
func decodeNotice(raw string) (*Notice, bool) {
	obj, ok := envelope(raw, "<clawdline-notice>", "</clawdline-notice>")
	if !ok {
		return nil, false
	}
	proto, _ := obj.str("protocol")
	version, hasVersion := obj.integer("version")
	kind, hasKind := obj.str("kind")
	body, _ := obj.str("body")
	if proto != "clawdline.notice" || !hasVersion || (version != 1 && version != 2) || !hasKind || body == "" {
		return nil, false
	}
	n := &Notice{Kind: kind, Body: body}
	taskAudience := func() bool {
		a, _ := obj.str("audience")
		n.Audience = a
		return a == "root" || a == "parent"
	}
	nonempty := func(key string) (string, bool) {
		v, ok := obj.str(key)
		return v, ok && v != ""
	}
	optional := func(key string, target *string, mustHaveText bool) bool {
		raw, present := obj[key]
		if !present {
			return true
		}
		v, ok := rawString(raw)
		if !ok || (mustHaveText && v == "") {
			return false
		}
		*target = v
		return true
	}

	switch kind {
	case "task_finished":
		legacy := []string{"protocol", "version", "kind", "audience", "task", "state", "result_path",
			"outstanding", "claims_released", "child_may_still_write", "body"}
		if id, ok := nonempty("notice_id"); version == 2 && ok && sameKeys(obj, append(legacy, "notice_id", "ack_path")...) &&
			isUUID(id) {
			path, ok := nonempty("ack_path")
			if !ok || !strings.HasPrefix(path, "/") {
				return nil, false
			}
			n.NoticeID, n.AckPath = strings.ToLower(id), path
		} else if !sameKeys(obj, legacy...) {
			return nil, false
		}
		task, ok := noticeTask(obj["task"])
		state, _ := obj.str("state")
		resultPath, _ := obj.str("result_path")
		outstanding, hasOutstanding := obj.integer("outstanding")
		released, hasReleased := obj.boolean("claims_released")
		mayWrite, hasMayWrite := obj.boolean("child_may_still_write")
		if !taskAudience() || !ok || (version == 1 && n.NoticeID != "") || !knownTaskState(state) ||
			resultPath == "" || !hasOutstanding || outstanding < 0 || !hasReleased || !hasMayWrite {
			return nil, false
		}
		n.Task, n.State, n.ResultPath = &task, state, resultPath
		n.Outstanding, n.ClaimsReleased, n.ChildMayStillWrite = outstanding, released, mayWrite

	case "workspace_overlap":
		task, ok := noticeTask(obj["task"])
		rows, isList := rawArray(obj["overlaps"])
		if !taskAudience() || !ok || !sameKeys(obj, "protocol", "version", "kind", "audience", "task", "overlaps", "body") ||
			!isList || len(rows) == 0 {
			return nil, false
		}
		for _, raw := range rows {
			row, ok := rawObject(raw)
			if !ok || !sameKeys(row, "task", "path") {
				return nil, false
			}
			other, ok := noticeTask(row["task"])
			path, _ := row.str("path")
			if !ok || path == "" {
				return nil, false
			}
			n.Overlaps = append(n.Overlaps, NoticeOverlap{Task: other, Path: path})
		}
		n.Task = &task

	case "file_wait_request":
		audience, _ := obj.str("audience")
		waitID, ok1 := nonempty("wait_id")
		repo, ok2 := nonempty("repository")
		paths, ok3 := nonemptyStrings(obj["paths"])
		waiter, ok4 := nonempty("waiter_session_id")
		reason, ok5 := nonempty("reason")
		release, ok6 := nonempty("release_condition")
		if version != 2 || audience != "owner" || !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) ||
			!sameKeys(obj, "protocol", "version", "kind", "audience", "wait_id", "repository", "paths",
				"waiter_session_id", "reason", "release_condition", "body") {
			return nil, false
		}
		n.Audience, n.WaitID, n.Repository, n.Paths = audience, waitID, repo, paths
		n.WaiterSessionID, n.Reason, n.ReleaseCondition = waiter, reason, release

	case "file_wait_release":
		audience, _ := obj.str("audience")
		waitID, ok1 := nonempty("wait_id")
		repo, ok2 := nonempty("repository")
		paths, ok3 := nonemptyStrings(obj["paths"])
		if version != 2 || audience != "waiter" || !(ok1 && ok2 && ok3) ||
			!keysBetween(obj, []string{"protocol", "version", "kind", "audience", "wait_id", "repository", "paths", "body"},
				[]string{"commit", "note"}) ||
			!optional("commit", &n.Commit, true) || !optional("note", &n.Note, true) {
			return nil, false
		}
		n.Audience, n.WaitID, n.Repository, n.Paths = audience, waitID, repo, paths

	case "handoff_receipt":
		audience, _ := obj.str("audience")
		handoffID, ok1 := nonempty("handoff_id")
		assistant, _ := obj.str("assistant")
		projectDir, ok2 := nonempty("project_dir")
		state, _ := obj.str("state")
		if version != 2 || audience != "source" || !ok1 || !ok2 ||
			(assistant != "claude" && assistant != "codex") ||
			(state != "picked_up" && state != "first_line_failed") ||
			!keysBetween(obj, []string{"protocol", "version", "kind", "audience", "handoff_id", "assistant",
				"project_dir", "state", "body"}, []string{"title"}) ||
			!optional("title", &n.Title, false) {
			return nil, false
		}
		n.Audience, n.HandoffID, n.Assistant, n.ProjectDir, n.State = audience, handoffID, assistant, projectDir, state

	default:
		return nil, false
	}
	return n, true
}

func knownTaskState(s string) bool {
	switch s {
	case "success", "failure", "timeout", "cancelled", "spawn_failed":
		return true
	}
	return false
}

var uuidShape = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

func isUUID(s string) bool { return uuidShape.MatchString(s) }

func noticeTask(raw json.RawMessage) (NoticeTask, bool) {
	obj, ok := rawObject(raw)
	if !ok || !sameKeys(obj, "id", "title") {
		return NoticeTask{}, false
	}
	id, _ := obj.str("id")
	title, _ := obj.str("title")
	return NoticeTask{ID: id, Title: title}, id != "" && title != ""
}

func nonemptyStrings(raw json.RawMessage) ([]string, bool) {
	rows, ok := rawArray(raw)
	if !ok || len(rows) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		s, ok := rawString(r)
		if !ok || s == "" {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// envelope is the JSON object between a one-line envelope's tags.
func envelope(raw, opening, closing string) (object, bool) {
	if strings.ContainsAny(raw, "\n\r") || !strings.HasPrefix(raw, opening) || !strings.HasSuffix(raw, closing) ||
		len(raw) <= len(opening)+len(closing) {
		return nil, false
	}
	return decodeObject([]byte(raw[len(opening) : len(raw)-len(closing)]))
}

func sameKeys(obj object, keys ...string) bool {
	if len(obj) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	return true
}

func keysBetween(obj object, required, allowed []string) bool {
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	for k := range obj {
		known := false
		for _, a := range append(required, allowed...) {
			if k == a {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

// ---------- Images ----------

var imageMarkers = regexp.MustCompile(`\[Image #\d+\]\s*`)

// dropPattern matches only paths Clawdline's own drop cache created. The
// directory is the left boundary, so a greedy match cannot eat authored text
// such as `Resources/web/app.js` before the path begins.
//
// Two caches: the Swift app's, and this daemon's own (artifacts.DropsDir), which
// holds what a picture sent from this daemon's page became when it was handed
// over as a path.
var dropPattern = func() *regexp.Regexp {
	dirs := regexp.QuoteMeta(dropDirectory()+string(filepath.Separator)) + "|" +
		regexp.QuoteMeta(artifacts.DropsDir(config.Dir())+string(filepath.Separator))
	file := `(?:` + dirs + `)clawdline-[A-Za-z0-9-]+\.(?:png|tiff|jpg|jpeg|heic|gif|webp)`
	return regexp.MustCompile(`(?:'` + file + `'|` + file + `)`)
}()

func dropDirectory() string {
	if dir := os.Getenv("CLAWDLINE_DROPS_DIR"); dir != "" {
		if strings.HasPrefix(dir, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = home + dir[1:]
			}
		}
		return strings.TrimRight(filepath.Clean(dir), "/")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "com.tsunamiworks.clawdline", "drops")
}

// canonicalImageContent is a user turn's text and how many images it carried:
// structured image blocks, `[Image #N]` markers when they are to be counted,
// and paths into the drop cache. A turn that was nothing but pictures keeps a
// visible marker for each.
func canonicalImageContent(raw string, structured int, inferMarkers bool) (string, int) {
	value := raw
	markers := 0
	if structured > 0 || inferMarkers {
		if inferMarkers {
			markers = len(imageMarkers.FindAllStringIndex(value, -1))
		}
		value = imageMarkers.ReplaceAllString(value, "")
	}
	dropped := len(dropPattern.FindAllStringIndex(value, -1))
	value = strings.TrimSpace(dropPattern.ReplaceAllLiteralString(value, ""))
	count := structured + markers + dropped
	if value == "" && count > 0 {
		marks := make([]string, count)
		for i := range marks {
			marks[i] = "[Image #" + strconv.Itoa(i+1) + "]"
		}
		return strings.Join(marks, " "), count
	}
	return value, count
}

// ---------- Terminal text ----------

// plain removes terminal controls: CSI and OSC sequences whole, other escapes
// with the character after them, and C0 controls other than newline and tab.
// An unterminated sequence consumes the rest rather than leaking its payload.
func plain(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	dirty := false
	for i := 0; i < len(text); i++ {
		if c := text[i]; c == 0x1b || (c < 0x20 && c != '\n' && c != '\t') {
			dirty = true
			break
		}
	}
	if !dirty {
		return text
	}
	runes := []rune(text)
	var out strings.Builder
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == 0x1b {
			if i+1 >= len(runes) {
				break
			}
			switch runes[i+1] {
			case '[':
				j := i + 2
				for j < len(runes) && !(runes[j] >= '@' && runes[j] <= '~') {
					j++
				}
				i = min(j+1, len(runes))
			case ']':
				j := i + 2
				for j < len(runes) {
					if runes[j] == 0x07 {
						j++
						break
					}
					if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
			default:
				i += 2
			}
			continue
		}
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			out.WriteRune(r)
		}
		i++
	}
	return out.String()
}

// ---------- JSON, read the way the Swift app reads it ----------

// object is one JSON object with its values left raw. A field is read only
// when it has the type asked for, as `as? String` reads it there: a record
// with an unexpected type in one field is not thrown away whole.
type object map[string]json.RawMessage

func decodeObject(b []byte) (object, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return nil, false
	}
	var o object
	if json.Unmarshal(b, &o) != nil {
		return nil, false
	}
	return o, true
}

func rawObject(raw json.RawMessage) (object, bool) { return decodeObject(raw) }

func rawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func isArray(raw json.RawMessage) bool { return len(raw) > 0 && raw[0] == '[' }

func rawArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if !isArray(raw) {
		return nil, false
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil, false
	}
	return list, true
}

// rawObjects is a list whose every element is an object, as
// `as? [[String: Any]]` requires; one element of another type fails it.
func rawObjects(raw json.RawMessage) ([]object, bool) {
	list, ok := rawArray(raw)
	if !ok {
		return nil, false
	}
	out := make([]object, 0, len(list))
	for _, item := range list {
		o, ok := rawObject(item)
		if !ok {
			return nil, false
		}
		out = append(out, o)
	}
	return out, true
}

func (o object) str(key string) (string, bool) { return rawString(o[key]) }

func (o object) strPtr(key string) *string {
	if s, ok := o.str(key); ok {
		return &s
	}
	return nil
}

// clean is a string field trimmed, or "" when it is not a string.
func (o object) clean(key string) string {
	s, _ := o.str(key)
	return strings.TrimSpace(s)
}

func (o object) object(key string) (object, bool) { return rawObject(o[key]) }

func (o object) objects(key string) ([]object, bool) { return rawObjects(o[key]) }

func (o object) boolean(key string) (bool, bool) {
	switch string(o[key]) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// integer is a JSON number with no fractional part, and never a boolean.
func (o object) integer(key string) (int64, bool) {
	raw := o[key]
	if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return 0, false
	}
	if n, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		return n, true
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// truncated is a number field as `NSNumber.int64Value` reads it: towards
// zero, and 0 when the field is absent or not a number.
func (o object) truncated(key string) int64 {
	raw := o[key]
	if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return 0
	}
	if n, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		return n
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

// time is an ISO 8601 timestamp with fractional seconds, as Unix seconds, or
// 0. The Swift app's formatter requires the fraction, so this does too.
func (o object) time(key string) int64 {
	s, ok := o.str(key)
	if !ok || len(s) < 20 || s[19] != '.' {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// encodeJSON writes a value the way the Swift app's serializer does with
// `.withoutEscapingSlashes`: no HTML escaping, no trailing newline.
func encodeJSON(v any) (string, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}
