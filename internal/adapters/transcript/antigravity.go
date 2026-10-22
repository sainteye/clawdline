package transcript

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// AntigravityPath is the only composer for the standalone CLI's native log.
// Missing components are allowed before its first turn. Existing components
// below home must not be symlinks, including links that currently stay inside
// the brain directory. An exact conversation ID is required; cwd is no key.
func AntigravityPath(home, conversationID string) (string, bool) {
	if !filepath.IsAbs(home) || !isUUID(conversationID) {
		return "", false
	}
	base, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", false
	}
	path := base
	for _, part := range []string{".gemini", "antigravity-cli", "brain", conversationID, ".system_generated", "logs", "transcript_full.jsonl"} {
		path = filepath.Join(path, part)
		st, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
	}
	return path, true
}

// AntigravityRecordError has a stable, path-free reason. The complete snapshot
// is rejected on malformed or ambiguous records, never rendered as an empty
// successful conversation. Partial writes can be retried on the next poll.
type AntigravityRecordError struct{ Code string }

func (e *AntigravityRecordError) Error() string { return "Antigravity record: " + e.Code }

func antigravityError(code string) error {
	return &UnreadableError{Cause: &AntigravityRecordError{Code: code}}
}

type antigravityTool struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// Only fields evidenced in native logs are decoded. In particular, stored
// thinking and injected SYSTEM/SYSTEM_SDK content have no projection.
type antigravityRecord struct {
	Index     *int64            `json:"step_index"`
	Source    string            `json:"source"`
	Type      string            `json:"type"`
	Status    string            `json:"status"`
	CreatedAt string            `json:"created_at"`
	Content   string            `json:"content"`
	Error     string            `json:"error"`
	Tools     []antigravityTool `json:"tool_calls"`
	Input     *int64            `json:"input_tokens"`
	CacheRead *int64            `json:"cache_read_tokens"`
	Output    *int64            `json:"output_tokens"`
	before    int64
	at        time.Time
}

type antigravitySnapshot struct {
	data       []byte
	rows       []antigravityRecord
	revision   string
	generation string
}

// AntigravityInfo is native record evidence only. No model, context window,
// workspace or monetary cost is inferred from a prompt, filename or tool args.
type AntigravityInfo struct {
	FirstUser    string
	LastActivity time.Time
	Usage        *Summary
	Revision     string
	Bytes        int64
}

// AntigravityEntry preserves the actual native identity and role fields
// beside the existing display projection. Several tool calls from one row
// share its step index; a later GENERIC result has its own independent index.
type AntigravityEntry struct {
	Entry     Entry
	StepIndex int64
	Source    string
	Type      string
	Status    string
}

type AntigravityPage struct {
	Page   Page
	Native []AntigravityEntry
}

func (s antigravitySnapshot) nativePage(page Page) AntigravityPage {
	out := AntigravityPage{Page: page, Native: []AntigravityEntry{}}
	row := 0
	for _, entry := range page.Entries {
		for row < len(s.rows) && s.rows[row].before < entry.Before {
			row++
		}
		if row < len(s.rows) && s.rows[row].before == entry.Before {
			r := s.rows[row]
			out.Native = append(out.Native, AntigravityEntry{Entry: entry, StepIndex: *r.Index, Source: r.Source, Type: r.Type, Status: r.Status})
		}
	}
	return out
}

func ReadAntigravityInfo(path string) (AntigravityInfo, error) {
	snapshot, err := readAntigravitySnapshot(path)
	if err != nil {
		return AntigravityInfo{}, err
	}
	return snapshot.info(), nil
}

func AntigravityFirstUser(path string) (string, error) {
	info, err := ReadAntigravityInfo(path)
	if err != nil {
		return "", err
	}
	if info.FirstUser == "" {
		return "", ErrNotFound
	}
	return info.FirstUser, nil
}

func (s antigravitySnapshot) info() AntigravityInfo {
	out := AntigravityInfo{Revision: s.revision, Bytes: int64(len(s.data))}
	var total Summary
	saw, known := false, true
	for _, row := range s.rows {
		if row.Source == "USER_EXPLICIT" && row.Type == "USER_INPUT" && out.FirstUser == "" {
			out.FirstUser, _ = antigravityUserRequest(row.Content)
		}
		if (row.Source == "USER_EXPLICIT" && row.Type == "USER_INPUT") ||
			(row.Source == "MODEL" && (row.Type == "PLANNER_RESPONSE" || row.Type == "GENERIC")) {
			out.LastActivity = row.at
		}
		if row.Source != "MODEL" || row.Type != "PLANNER_RESPONSE" {
			continue
		}
		if row.Input == nil || row.Output == nil || row.CacheRead == nil {
			known = false
			continue
		}
		saw = true
		for _, pair := range []struct {
			dst   *int64
			value int64
		}{
			{&total.Input, *row.Input}, {&total.Output, *row.Output}, {&total.CacheRead, *row.CacheRead},
		} {
			if *pair.dst > math.MaxInt64-pair.value {
				known = false
			} else {
				*pair.dst += pair.value
			}
		}
	}
	if saw && known && total.Input <= math.MaxInt64-total.Output && total.Input+total.Output <= math.MaxInt64-total.CacheRead {
		total.Total = total.Input + total.Output + total.CacheRead
		out.Usage = &total
	}
	return out
}

// antigravityUserRequest treats the wrapper as a native envelope, rather than
// XML: a person's source code may contain arbitrary angle brackets. Only a
// complete, unambiguous leading USER_REQUEST is accepted. Settings/metadata
// outside it never become user prose, even when the closing marker is absent.
func antigravityUserRequest(content string) (string, bool) {
	const open, close = "<USER_REQUEST>", "</USER_REQUEST>"
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, open) {
		return "", false
	}
	rest := content[len(open):]
	end := strings.Index(rest, close)
	if end < 0 || strings.Contains(rest[:end], open) {
		return "", false
	}
	trailing := strings.TrimSpace(rest[end+len(close):])
	// The observed suffixes are native metadata/settings blocks. Reject a
	// second request or free prose instead of choosing the wrong envelope.
	for trailing != "" {
		matched := false
		for _, tag := range []string{"ADDITIONAL_METADATA", "USER_SETTINGS_CHANGE"} {
			head, tail := "<"+tag+">", "</"+tag+">"
			if strings.HasPrefix(trailing, head) {
				at := strings.Index(trailing[len(head):], tail)
				if at < 0 {
					return "", false
				}
				trailing = strings.TrimSpace(trailing[len(head)+at+len(tail):])
				matched = true
				break
			}
		}
		if !matched {
			return "", false
		}
	}
	return strings.TrimSpace(rest[:end]), true
}

func (r antigravityRecord) entries() []Entry {
	at := r.at.Unix()
	makeEntry := func(kind, text string) Entry { return Entry{Kind: kind, Text: text, At: at, Before: r.before} }
	switch {
	case r.Source == "USER_EXPLICIT" && r.Type == "USER_INPUT":
		text, ok := antigravityUserRequest(r.Content)
		if !ok || text == "" {
			return nil
		}
		if n, ok := decodeNotice(text); ok {
			e := makeEntry(KindNotice, n.Body)
			e.Notice = n
			return []Entry{e}
		}
		if e, ok := sessionMessage(text, at); ok {
			e.Before = r.before
			return []Entry{e}
		}
		return []Entry{makeEntry(KindUser, text)}
	case r.Source == "MODEL" && r.Type == "PLANNER_RESPONSE":
		var out []Entry
		if strings.TrimSpace(r.Content) != "" {
			if e, ok := assistantEntry(r.Content, at, 0); ok {
				e.Before = r.before
				out = append(out, e)
			}
		}
		for _, tool := range r.Tools {
			// A call records intent only. No file change or success is
			// inferred, and no association with a later result is fabricated.
			e := makeEntry(KindTool, string(tool.Args))
			e.Tool = tool.Name
			e.Activity = &Activity{Kind: "called", Status: stringPointer("pending")}
			out = append(out, e)
		}
		if r.Error != "" || r.Status == "INVALID" {
			out = append(out, r.resultEntry())
		}
		return out
	case r.Source == "MODEL" && r.Type == "GENERIC":
		if r.Content == "" && r.Error == "" && r.Status != "INVALID" {
			return nil
		}
		return []Entry{r.resultEntry()}
	}
	return nil
}

func stringPointer(s string) *string { return &s }

func (r antigravityRecord) resultEntry() Entry {
	text := r.Content
	if r.Error != "" && !strings.Contains(text, r.Error) {
		if text != "" {
			text += "\n"
		}
		text += r.Error
	}
	if r.Status == "INVALID" && r.Error == "" {
		text = "INVALID\n" + text
	}
	status := r.Status
	return Entry{Kind: KindToolResult, Text: text, At: r.at.Unix(), Before: r.before,
		Activity: &Activity{Kind: "called", Status: &status}}
}

// readAntigravitySnapshot reuses the existing transcript read budget. Until
// a durable generation index is available, a larger native file is explicitly
// unreadable: a tail-only validation could miss duplicate/revised steps.
func readAntigravitySnapshot(path string) (antigravitySnapshot, error) {
	var out antigravitySnapshot
	if !antigravityNativePath(path) {
		return out, antigravityError("path_invalid")
	}
	f, err := openAntigravityRecord(path)
	if err != nil {
		return out, recordError(err)
	}
	defer f.Close()
	start, err := f.Stat()
	if err != nil {
		return out, recordError(err)
	}
	if !start.Mode().IsRegular() {
		return out, antigravityError("not_regular")
	}
	if start.Size() > ReadBudget {
		return out, antigravityError("read_budget_exceeded")
	}
	data, err := io.ReadAll(io.LimitReader(f, ReadBudget+1))
	if err != nil {
		return out, recordError(err)
	}
	if len(data) > ReadBudget {
		return out, antigravityError("read_budget_exceeded")
	}
	end, err := f.Stat()
	current, currentErr := os.Stat(path)
	if err != nil || currentErr != nil || !antigravityNativePath(path) || !os.SameFile(start, current) ||
		start.Size() != end.Size() || start.ModTime() != end.ModTime() || start.Size() != int64(len(data)) ||
		end.Size() != current.Size() || end.ModTime() != current.ModTime() || antigravityChangeIdentity(start) != antigravityChangeIdentity(end) {
		return out, ErrCursorStale
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return out, antigravityError("partial_record")
	}
	var previous int64 = -1
	var pos int64
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			pos += int64(len(line)) + 1
			continue
		}
		var row antigravityRecord
		if json.Unmarshal(line, &row) != nil || row.Index == nil || *row.Index < 0 || row.Source == "" || row.Type == "" || row.Status == "" {
			return out, antigravityError("malformed_record")
		}
		if *row.Index <= previous {
			return out, antigravityError("step_order_invalid")
		}
		previous = *row.Index
		row.at, err = time.Parse(time.RFC3339Nano, row.CreatedAt)
		if err != nil {
			return out, antigravityError("timestamp_invalid")
		}
		if row.Source == "USER_EXPLICIT" && row.Type == "USER_INPUT" {
			if _, ok := antigravityUserRequest(row.Content); !ok {
				return out, antigravityError("user_request_invalid")
			}
		}
		for _, count := range []*int64{row.Input, row.Output, row.CacheRead} {
			if count != nil && *count < 0 {
				return out, antigravityError("usage_invalid")
			}
		}
		for _, tool := range row.Tools {
			if tool.Name == "" || len(tool.Args) == 0 || !json.Valid(tool.Args) {
				return out, antigravityError("tool_invalid")
			}
		}
		row.before = pos
		out.rows = append(out.rows, row)
		pos += int64(len(line)) + 1
	}
	out.data = data
	out.generation = antigravityFileIdentity(start)
	out.revision = antigravityRevision(out.generation, data)
	return out, nil
}

// Open each directory through a confined handle and check its inode before
// opening any child. A symlink swapped after Lstat cannot turn a native read
// into a read of another conversation or an account store.
func openAntigravityRecord(path string) (*os.File, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 8 {
		return nil, antigravityError("path_invalid")
	}
	home := filepath.FromSlash(strings.Join(parts[:len(parts)-7], "/"))
	current, err := openAntigravityDirectory(home, parts[len(parts)-7:len(parts)-1])
	if err != nil {
		return nil, err
	}
	defer current.Close()
	name := parts[len(parts)-1]
	before, err := current.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, antigravityError("not_regular")
	}
	f, err := current.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		f.Close()
		return nil, antigravityError("path_invalid")
	}
	return f, nil
}

// OpenAntigravityBrain opens only the known native history parent. The
// directory handle, like record handles, is confined before it is enumerated.
func OpenAntigravityBrain(home string) (*os.File, error) {
	path, ok := AntigravityPath(home, "00000000-0000-4000-8000-000000000000")
	if !ok {
		return nil, antigravityError("path_invalid")
	}
	brain := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	base, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, recordError(err)
	}
	rel, err := filepath.Rel(base, brain)
	if err != nil {
		return nil, antigravityError("path_invalid")
	}
	root, err := openAntigravityDirectory(base, strings.Split(rel, string(filepath.Separator)))
	if err != nil {
		return nil, recordError(err)
	}
	defer root.Close()
	f, err := root.Open(".")
	return f, recordError(err)
}

func openAntigravityDirectory(home string, parts []string) (*os.Root, error) {
	current, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	for _, name := range parts {
		if name == "" || name == "." || name == ".." {
			current.Close()
			return nil, antigravityError("path_invalid")
		}
		before, err := current.Lstat(name)
		if err != nil {
			current.Close()
			return nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			current.Close()
			return nil, antigravityError("path_invalid")
		}
		next, err := current.OpenRoot(name)
		if err != nil {
			current.Close()
			return nil, err
		}
		after, err := next.Stat(".")
		current.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, antigravityError("path_invalid")
		}
		current = next
	}
	return current, nil
}

func antigravityNativePath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "transcript_full.jsonl" {
		return false
	}
	logs := filepath.Dir(path)
	system := filepath.Dir(logs)
	conversation := filepath.Dir(system)
	brain := filepath.Dir(conversation)
	cli := filepath.Dir(brain)
	gemini := filepath.Dir(cli)
	if filepath.Base(logs) != "logs" || filepath.Base(system) != ".system_generated" || filepath.Base(brain) != "brain" ||
		filepath.Base(cli) != "antigravity-cli" || filepath.Base(gemini) != ".gemini" {
		return false
	}
	want, ok := AntigravityPath(filepath.Dir(gemini), filepath.Base(conversation))
	return ok && want == path
}

// Dev/Ino are read without a platform-specific syscall dependency. Platforms
// whose FileInfo does not expose a stable file identity still support full
// reads, but cannot authorize a byte cursor across requests.
func antigravityFileIdentity(info os.FileInfo) string {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return "unknown"
	}
	dev, ino := v.FieldByName("Dev"), v.FieldByName("Ino")
	if !dev.IsValid() || !ino.IsValid() {
		return "unknown"
	}
	return fmt.Sprint(dev) + "." + fmt.Sprint(ino)
}

// Change time also invalidates polling caches when a writer preserves mtime
// and length. On platforms without that evidence, bypass the cache entirely.
func antigravityChangeIdentity(info os.FileInfo) string {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return ""
	}
	for _, name := range []string{"Ctim", "Ctimespec"} {
		if change := v.FieldByName(name); change.IsValid() {
			return antigravityFileIdentity(info) + ":" + fmt.Sprint(change)
		}
	}
	return ""
}

func antigravityRevision(generation string, data []byte) string {
	sum := sha256.Sum256(data)
	return "agy1:" + generation + ":" + strconv.Itoa(len(data)) + ":" + hex.EncodeToString(sum[:])
}

func (s antigravitySnapshot) matchesRevision(revision string, prefix int64) bool {
	if s.generation == "unknown" || prefix < 0 || prefix > int64(len(s.data)) {
		return false
	}
	return revision == antigravityRevision(s.generation, s.data[:prefix])
}

// ReadAntigravityBefore reads complete native rows, preserving every entry
// from a planner row together. An older-page cursor needs the full revision
// it was obtained from. Omitting it is safe for the initial newest page only.
func ReadAntigravityBefore(path string, limit int, before int64, revision ...string) (Page, error) {
	native, err := ReadAntigravityNativeBefore(path, limit, before, revision...)
	return native.Page, err
}

func ReadAntigravityNativeBefore(path string, limit int, before int64, revision ...string) (AntigravityPage, error) {
	snapshot, err := readAntigravitySnapshot(path)
	if err != nil {
		return AntigravityPage{}, err
	}
	if before < 0 || before > int64(len(snapshot.data)) {
		return AntigravityPage{}, ErrCursorStale
	}
	end := int64(len(snapshot.data))
	if before > 0 {
		if len(revision) != 1 || !snapshot.matchesRevision(revision[0], int64(len(snapshot.data))) || snapshot.data[before-1] != '\n' {
			return AntigravityPage{}, ErrCursorStale
		}
		end = before
	}
	page := Page{Entries: []Entry{}, Signature: snapshot.revision, NextAfter: int64(len(snapshot.data))}
	for i := len(snapshot.rows) - 1; i >= 0; i-- {
		row := snapshot.rows[i]
		if row.before >= end {
			continue
		}
		entries := row.entries()
		for j := len(entries) - 1; j >= 0; j-- {
			page.Entries = append(page.Entries, entries[j])
		}
		if len(page.Entries) >= max(limit, 1) {
			page.NextBefore = row.before
			break
		}
	}
	page.Entries = oldestFirst(page.Entries, len(page.Entries))
	return snapshot.nativePage(page), nil
}

// ReadAntigravityAfter verifies the entire old prefix and file generation.
// A bare byte offset cannot distinguish an append from an in-place rewrite,
// so old clients without a revision receive the existing typed stale result.
func ReadAntigravityAfter(path string, limit int, after int64, revision ...string) (Page, error) {
	native, err := ReadAntigravityNativeAfter(path, limit, after, revision...)
	return native.Page, err
}

func ReadAntigravityNativeAfter(path string, limit int, after int64, revision ...string) (AntigravityPage, error) {
	snapshot, err := readAntigravitySnapshot(path)
	if err != nil {
		return AntigravityPage{}, err
	}
	if len(revision) != 1 || !snapshot.matchesRevision(revision[0], after) || (after > 0 && snapshot.data[after-1] != '\n') {
		return AntigravityPage{}, ErrCursorStale
	}
	page := Page{Entries: []Entry{}, Signature: snapshot.revision, NextAfter: int64(len(snapshot.data))}
	for _, row := range snapshot.rows {
		if row.before >= after {
			page.Entries = append(page.Entries, row.entries()...)
		}
		if len(page.Entries) > max(limit, 1) {
			return AntigravityPage{}, ErrCursorStale
		}
	}
	return snapshot.nativePage(page), nil
}
