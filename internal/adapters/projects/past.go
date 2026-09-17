package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Past is StartPoints.Past: one conversation an assistant has already written
// down in a place. `ID` is the only part a client sends back.
type Past struct {
	ID    string
	Title string
	At    time.Time
	// Live is whether something is writing to this conversation right now.
	// Resuming one would put a second process on the same record.
	Live bool
}

// BriefingOpening is Orchestrator.briefingOpening: how every dispatched
// child's first message begins. Those are plumbing, not conversations.
const BriefingOpening = "You are a Clawdline CHILD agent for task"

// PastTitles is the name ladder's sources that live outside this package: the
// Swift store's typed and automatic names, and the transcript's own titles.
type PastTitles struct {
	// Recorded is the transcript's title and its current `/rename`.
	Recorded func(path string) (title, custom string)
	// Manual is a name somebody typed for this conversation, given the
	// transcript's current `/rename` (a rename since retires it).
	Manual func(conversationID, custom string) string
	// Automatic is the model-chosen fallback name.
	Automatic func(conversationID string) string
}

// front is StartPoints.Front.
type front struct {
	opening string
	// said is the whole of that first turn's text, which the weak-title
	// rule judges.
	said         string
	entrypoint   string
	promptSource string
	dispatched   bool
}

func (f front) isConversation() bool {
	return !f.dispatched && f.entrypoint != "sdk-cli" && f.promptSource != "sdk"
}

var (
	frontsMu sync.Mutex
	fronts   = map[string]front{}
)

// ClaudePast is StartPoints.past(in:limit:scan:): the top level of the
// project's transcript folder, newest first, conversations somebody had only.
// `open` is every transcript something is writing to, by resolved path.
func ClaudePast(place Place, open map[string]bool, titles PastTitles, limit, scan int) []Past {
	dir := filepath.Join(home(), ".claude", "projects", Slug(place.Path))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type file struct {
		id   string
		path string
		at   time.Time
	}
	var files []file
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id, ok := SessionName(strings.TrimSuffix(name, ".jsonl"))
		if !ok {
			continue
		}
		full := filepath.Join(dir, name)
		st, err := os.Stat(full)
		if err != nil || st.IsDir() {
			continue
		}
		files = append(files, file{id, full, st.ModTime()})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	if len(files) > scan {
		files = files[:scan]
	}
	out := []Past{}
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		fr := cachedFront(f.path)
		if !fr.isConversation() {
			continue
		}
		title := displayedTitle(f.path, f.id, fr, titles)
		if title == "" {
			continue
		}
		resolved := f.path
		if r, err := filepath.EvalSymlinks(f.path); err == nil {
			resolved = r
		}
		out = append(out, Past{ID: f.id, Title: title, At: f.at, Live: open[resolved]})
	}
	return out
}

// displayedTitle is StartPoints.displayedTitle(ofTranscript:…): a typed name,
// the transcript's own, the automatic fallback, then the opening line.
//
// A weak recorded title — `Image review` over an opening that was only pasted
// images — steps aside for the fallback, as TargetSession
// .displayedConversationTitle has it.
func displayedTitle(path, id string, fr front, titles PastTitles) string {
	recorded, custom := "", ""
	if titles.Recorded != nil {
		recorded, custom = titles.Recorded(path)
	}
	manual, fallback := "", ""
	if titles.Manual != nil {
		manual = titles.Manual(id, custom)
	}
	if titles.Automatic != nil {
		fallback = titles.Automatic(id)
	}
	conversation := recorded
	if strings.TrimSpace(fallback) != "" && titleIsWeak(recorded, custom, fr.said, fr.opening != "") {
		conversation = ""
	}
	for _, candidate := range []string{manual, conversation, fallback, fr.opening} {
		if c := strings.TrimSpace(candidate); c != "" {
			return c
		}
	}
	return ""
}

// cachedFront is StartPoints.cachedFront(of:): remembered by path alone, since
// the first turn is written once, and never remembered while it is empty.
func cachedFront(path string) front {
	frontsMu.Lock()
	hit, ok := fronts[path]
	frontsMu.Unlock()
	if ok {
		return hit
	}
	found := frontOfFile(path, 60, 8<<20)
	if found.opening == "" {
		return found
	}
	frontsMu.Lock()
	fronts[path] = found
	frontsMu.Unlock()
	return found
}

// frontOfFile is StartPoints.front(ofFile:records:bytes:): a record at a time
// until the first turn somebody addressed to the session turns up.
func frontOfFile(path string, records, limit int) front {
	f, err := os.Open(path)
	if err != nil {
		return front{}
	}
	defer f.Close()
	r := bufio.NewReaderSize(io.LimitReader(f, int64(limit)), 64<<10)
	for seen := 0; seen < records; seen++ {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if found, ok := frontInRecord(line, 80); ok {
				return found
			}
		}
		if err != nil {
			break
		}
	}
	return front{}
}

// frontInRecord is StartPoints.front(inRecord:limit:). The type is read after
// parsing: a file-history snapshot can carry the literal text of a user turn.
func frontInRecord(line []byte, limit int) (front, bool) {
	var row map[string]json.RawMessage
	if json.Unmarshal(line, &row) != nil {
		return front{}, false
	}
	var kind string
	if json.Unmarshal(row["type"], &kind) != nil || kind != "user" {
		return front{}, false
	}
	var sidechain bool
	if raw, ok := row["isSidechain"]; ok && json.Unmarshal(raw, &sidechain) == nil && sidechain {
		return front{}, false
	}
	if raw, ok := row["toolUseResult"]; ok && string(raw) != "null" {
		return front{}, false
	}
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if raw, ok := row["message"]; !ok || json.Unmarshal(raw, &message) != nil {
		return front{}, false
	}
	said := ""
	var text string
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(message.Content, &text) == nil {
		said = text
	} else if json.Unmarshal(message.Content, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				said = p.Text
				break
			}
		}
	}
	one := said
	if i := strings.IndexAny(said, "\n\r"); i >= 0 {
		one = said[:i]
	}
	tidy := strings.Trim(one, " \t")
	if tidy == "" {
		return front{}, false
	}
	if utf8.RuneCountInString(tidy) > limit {
		tidy = string([]rune(tidy)[:limit]) + "…"
	}
	out := front{opening: tidy, said: said, dispatched: strings.HasPrefix(said, BriefingOpening)}
	_ = json.Unmarshal(row["entrypoint"], &out.entrypoint)
	_ = json.Unmarshal(row["promptSource"], &out.promptSource)
	return out, true
}

// restatementLimit is ConversationTitle.restatementLimit.
const restatementLimit = 40

var attachmentMarkers = regexp.MustCompile(`(?i)\[[A-Za-z][A-Za-z0-9]*(?:[ \t]+[A-Za-z0-9]+)*[ \t]*#[ \t]*[0-9]+\]` +
	`|\[(?:Images?|Screenshots?|Attachments?|Pasted (?:text|content))\]`)

// titleIsWeak is ConversationTitle.isWeak over the first turn: a title is weak
// when the opening said nothing once its attachment markers came off, or so
// little that the title only hands it back. A typed `/rename` is never weak,
// and an opening that could not be read is no evidence.
//
// One difference from the Swift reduction: strings are not NFKC-folded (this
// module carries no normalisation table), so a full-width restatement of a
// short opening is not recognised.
func titleIsWeak(title, custom, opening string, read bool) bool {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(custom) != "" || !read {
		return false
	}
	prose := strings.TrimSpace(attachmentMarkers.ReplaceAllString(opening, ""))
	if prose == "" {
		return true
	}
	if utf8.RuneCountInString(prose) > restatementLimit {
		return false
	}
	named, said := comparable(title), comparable(prose)
	return named != "" && said != "" && strings.Contains(said, named)
}

func comparable(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ErrCodexUnavailable is the answer when no Codex executable could be found or
// its app-server would not answer. The listing is then empty, as the Swift
// app's is: there is nothing to pick back up that this machine can prove.
var ErrCodexUnavailable = errors.New("codex_unavailable")

// codexServers bounds how many `codex app-server` processes this reading may
// have running at once.
//
// **This route is a GET that starts a program.** `GET /v1/places/{id}/sessions/codex`
// is read-level — it draws the list of conversations to pick back up — and it
// has no cache in front of it, so without a bound N simultaneous reads are N
// Node processes, each for up to twenty seconds. Opening a session already
// queues for one slot (`admitOpening`); this is the same idea for the reading
// beside it. Two at a time, and a third waits rather than forking: the answer
// is worth a wait and is not worth a machine.
var codexServers = make(chan struct{}, 2)

// CodexPast is StartPoints.past(in:assistant: .codex): Codex's own thread
// index for this directory, through `codex app-server`, which owns it.
// `open` is every Codex conversation id something is writing to now.
func CodexPast(ctx context.Context, place Place, open map[string]bool, limit int) ([]Past, error) {
	exe := codexExecutable()
	if exe == "" {
		return nil, ErrCodexUnavailable
	}
	select {
	case codexServers <- struct{}{}:
		defer func() { <-codexServers }()
	case <-ctx.Done():
		// The caller went away while waiting, so nothing was started and
		// nothing is owed. The empty listing is what the app being replicated
		// answers when Codex cannot be read.
		return nil, errors.Join(ErrCodexUnavailable, ctx.Err())
	}
	scan := 400
	response, err := codexThreadList(ctx, exe, place.Path, scan)
	if err != nil {
		return nil, err
	}
	rows := codexListed(response, place.Path, open)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// codexListed is CodexNaming.listedThreads(in:cwd:open:).
func codexListed(response []byte, cwd string, open map[string]bool) []Past {
	var answer struct {
		Result struct {
			Data []struct {
				ID        string          `json:"id"`
				Cwd       string          `json:"cwd"`
				UpdatedAt json.RawMessage `json:"updatedAt"`
				Preview   string          `json:"preview"`
				Name      *string         `json:"name"`
			} `json:"data"`
		} `json:"result"`
	}
	if json.Unmarshal(response, &answer) != nil {
		return nil
	}
	out := []Past{}
	for _, row := range answer.Result.Data {
		if row.Cwd != cwd {
			continue
		}
		id, ok := SessionName(row.ID)
		if !ok {
			continue
		}
		var seconds float64
		if json.Unmarshal(row.UpdatedAt, &seconds) != nil {
			continue
		}
		if strings.HasPrefix(row.Preview, BriefingOpening) {
			continue
		}
		named := ""
		if row.Name != nil {
			named = strings.TrimSpace(*row.Name)
		}
		opening := row.Preview
		if i := strings.IndexAny(opening, "\n\r"); i >= 0 {
			opening = opening[:i]
		}
		opening = strings.TrimSpace(opening)
		title := named
		if title == "" {
			title = opening
		}
		if title == "" {
			continue
		}
		whole := int64(seconds)
		at := time.Unix(whole, int64((seconds-float64(whole))*1e9))
		out = append(out, Past{ID: id, Title: title, At: at, Live: open[id]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// codexExecutable is CodexNaming.executable(for:) without the configured path
// and the running-process probe: PATH, then the places installers put it.
func codexExecutable() string {
	executable := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
	}
	h := home()
	candidates := []string{"/opt/homebrew/bin/codex", "/usr/local/bin/codex",
		filepath.Join(h, ".local/bin/codex"), filepath.Join(h, ".volta/bin/codex")}
	nvm := filepath.Join(h, ".nvm/versions/node")
	if entries, err := os.ReadDir(nvm); err == nil {
		var versions []string
		for _, e := range entries {
			versions = append(versions, e.Name())
		}
		sort.Sort(sort.Reverse(sort.StringSlice(versions)))
		for _, v := range versions {
			candidates = append(candidates, filepath.Join(nvm, v, "bin/codex"))
		}
	}
	if found, err := exec.LookPath("codex"); err == nil {
		candidates = append([]string{found}, candidates...)
	}
	for _, c := range candidates {
		if executable(c) {
			return c
		}
	}
	return ""
}

// codexThreadList is CodexNameServer's `initialize`, `initialized` and
// `thread/list`, each request bounded, and the server stopped on every path.
func codexThreadList(ctx context.Context, exe, cwd string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "app-server", "--stdio")
	env := os.Environ()
	// A Finder-launched daemon has no login PATH; the npm shim needs its node.
	env = append(env, "PATH="+filepath.Dir(exe)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if os.Getenv("CODEX_HOME") == "" {
		env = append(env, "CODEX_HOME="+filepath.Join(home(), ".codex"))
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()

	lines := make(chan []byte, 16)
	go func() {
		defer close(lines)
		r := bufio.NewReaderSize(stdout, 256<<10)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	send := func(v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = stdin.Write(append(body, '\n'))
		return err
	}
	await := func(id int) ([]byte, error) {
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					return nil, ErrCodexUnavailable
				}
				var head struct {
					ID *int `json:"id"`
				}
				if json.Unmarshal(line, &head) == nil && head.ID != nil && *head.ID == id {
					return line, nil
				}
			case <-deadline.C:
				return nil, errors.Join(ErrCodexUnavailable, errors.New("codex app-server did not answer"))
			case <-ctx.Done():
				return nil, errors.Join(ErrCodexUnavailable, ctx.Err())
			}
		}
	}
	initialize := map[string]any{"method": "initialize", "id": 0, "params": map[string]any{
		"clientInfo": map[string]string{"name": "clawdline", "title": "Clawdline", "version": "0.0.0-next"}}}
	if err := send(initialize); err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	if _, err := await(0); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	list := map[string]any{"method": "thread/list", "id": 1, "params": map[string]any{
		"archived": false, "cwd": cwd, "limit": max(1, limit),
		"sortDirection": "desc", "sortKey": "updated_at", "sourceKinds": []string{"cli"}}}
	if err := send(list); err != nil {
		return nil, errors.Join(ErrCodexUnavailable, err)
	}
	return await(1)
}
