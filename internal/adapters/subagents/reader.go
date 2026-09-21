// Package subagents reads provider-native background work. Broker children do
// not come through this package: the daemon created those records itself and
// the console joins them with this reading without throwing their stronger
// evidence away.
package subagents

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const (
	// MaximumShown is how many provider threads one session row carries. The
	// omitted count travels beside the rows, so truncation is never an empty
	// claim. This keeps the Swift measurement, not its architecture
	// (retired Subagents.swift:87-90).
	MaximumShown = 6
	// MaximumCache is the most immutable sidecars, changing tails, and parent
	// notification cursors each cache keeps. A miss re-reads the source.
	MaximumCache     = 256
	latestToolWindow = 64 << 10
)

const (
	liveWindow   = 30 * time.Minute
	settleWindow = 3 * time.Minute
)

// Measurement is the cost of one session reading. Stat calls are separated
// from opened files because a stable beat pays only the former for agent
// transcripts whose tails are cached.
type Measurement struct {
	Stats    int
	Opened   int
	Bytes    int64
	Duration time.Duration
}

type meta struct {
	What         string
	Type         string
	Model        string
	Parent       string
	Depth        int
	At           time.Time
	Conversation string
	Thread       string
	Source       string
}

type verdict struct {
	Status  string
	Result  string
	Tokens  int64
	Tools   int64
	Seconds float64
}

type metaCache struct {
	value meta
	used  uint64
}

type doingCache struct {
	size  int64
	mod   time.Time
	value string
	used  uint64
}

type noticeCache struct {
	offset int64
	found  map[string]verdict
	used   uint64
}

// Reader holds only rebuildable caches. The provider files remain the source.
type Reader struct {
	Home string
	Now  func() time.Time

	mu       sync.Mutex
	limit    int
	clock    uint64
	metas    map[string]metaCache
	doings   map[string]doingCache
	notices  map[string]noticeCache
	evicted  int64
	lastCost Measurement
	lastRows int64
}

func New(home string) *Reader {
	return &Reader{
		Home: home, Now: time.Now, limit: MaximumCache,
		metas: map[string]metaCache{}, doings: map[string]doingCache{}, notices: map[string]noticeCache{},
	}
}

func (r *Reader) SetLimit(n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n < 1 {
		n = 1
	}
	r.limit = int(n)
	r.trimLocked()
}

func (r *Reader) Reading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	used := len(r.metas)
	if len(r.doings) > used {
		used = len(r.doings)
	}
	if len(r.notices) > used {
		used = len(r.notices)
	}
	return capacity.Reading{Known: true, Used: int64(used), Counters: capacity.Counters{Evicted: r.evicted}}
}

func (r *Reader) LastMeasurement() Measurement {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCost
}

// Begin starts one inventory reading. The row bound is per session, so the
// diagnostic is the fullest row in that reading rather than their sum.
func (r *Reader) Begin() {
	r.mu.Lock()
	r.lastRows = 0
	r.mu.Unlock()
}

// RowsReading is the fullest session in the latest inventory reading.
func (r *Reader) RowsReading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	return capacity.Reading{Known: true, Used: r.lastRows}
}

// ForSession fills the provider-native half of a session's work tree.
func (r *Reader) ForSession(s session.Session) session.Session {
	if s.Assistant == session.AssistantCodex {
		// The process adapter already asked the kernel which rollouts this pid
		// holds open. Their immutable heads add the nickname and start time that
		// distinguish several otherwise identical `thread_spawn` rows. Cache
		// those heads just like Claude's immutable sidecars: a stable beat opens
		// nothing, and process evidence remains the authority for "running".
		started := time.Now()
		cost := Measurement{}
		s.Agents = r.enrichCodex(s, &cost)
		cost.Duration = time.Since(started)
		r.mu.Lock()
		r.lastCost = cost
		if rows := int64(len(s.Agents) + s.AgentReading.Truncated); rows > r.lastRows {
			r.lastRows = rows
		}
		r.mu.Unlock()
		return s
	}
	if s.Assistant != session.AssistantClaude {
		return s
	}
	started := time.Now()
	cost := Measurement{}
	agents, reading := r.readClaude(s, &cost)
	cost.Duration = time.Since(started)
	r.mu.Lock()
	r.lastCost = cost
	if rows := int64(len(agents) + reading.Truncated); rows > r.lastRows {
		r.lastRows = rows
	}
	r.mu.Unlock()
	s.Agents, s.AgentReading = agents, reading
	return s
}

func (r *Reader) enrichCodex(s session.Session, cost *Measurement) []session.Agent {
	agents := append([]session.Agent(nil), s.Agents...)
	for i := range agents {
		m, identity, ok := r.readCodexMeta(agents[i].ID, cost)
		if !ok || identity.conversation != s.ConversationID || identity.thread != agents[i].ID || identity.source != "subagent" {
			// The kernel already proved this rollout is open and the process
			// adapter already classified it. Failure to read optional naming
			// fields must not erase that stronger running-work evidence.
			continue
		}
		if m.What != "" {
			agents[i].What = m.What
		}
		if m.Type != "" {
			agents[i].Type = m.Type
		}
		if !m.At.IsZero() {
			agents[i].At = m.At
		}
	}
	return agents
}

type candidate struct {
	id       string
	metaPath string
	path     string
	at       time.Time
	root     *verdict
}

func (r *Reader) readClaude(s session.Session, cost *Measurement) ([]session.Agent, session.AgentReading) {
	if s.CWD == "" || s.ConversationID == "" {
		return nil, unknown(session.AgentsNoRecord)
	}
	record := transcript.ClaudePath(r.Home, s.CWD, s.ConversationID)
	folder := strings.TrimSuffix(record, filepath.Ext(record))
	folder = filepath.Join(folder, "subagents")
	cost.Stats++
	names, err := os.ReadDir(folder)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, unknown(session.AgentsNoRecord)
		}
		return nil, unknown(session.AgentsUnreadable)
	}

	now := r.Now()
	var candidates []candidate
	for _, entry := range names {
		name := entry.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, unknown(session.AgentsUnrecognized)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".meta.json")
		if !validID(id) {
			return nil, unknown(session.AgentsUnrecognized)
		}
		path := filepath.Join(folder, "agent-"+id+".jsonl")
		cost.Stats++
		st, err := os.Lstat(path)
		if err != nil {
			return nil, unknown(session.AgentsUnreadable)
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			return nil, unknown(session.AgentsUnrecognized)
		}
		if now.Sub(st.ModTime()) >= liveWindow {
			continue
		}
		candidates = append(candidates, candidate{
			id: id, metaPath: filepath.Join(folder, name), path: path, at: st.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return nil, session.AgentReading{State: session.AgentsComplete}
	}

	root, ok := r.readNotices(record, cost)
	if !ok {
		return nil, unknown(session.AgentsUnreadable)
	}
	for i := range candidates {
		if v, found := root[candidates[i].id]; found {
			copy := v
			candidates[i].root = &copy
		}
	}
	// Running first, then the newest movement. This is the old measured
	// ordering, but the result below is this architecture's domain value
	// (retired Subagents.swift:175-180).
	sort.Slice(candidates, func(i, j int) bool {
		ri, rj := candidates[i].root == nil, candidates[j].root == nil
		if ri != rj {
			return ri
		}
		return candidates[i].at.After(candidates[j].at)
	})

	type resolved struct {
		agent session.Agent
		path  string
	}
	eligible := make([]resolved, 0, len(candidates))
	for _, c := range candidates {
		m, ok := r.readMeta(c.metaPath, cost)
		if !ok {
			return nil, unknown(session.AgentsUnrecognized)
		}
		v := c.root
		if v == nil && m.Parent != "" {
			if !validID(m.Parent) {
				return nil, unknown(session.AgentsUnrecognized)
			}
			parent, readable := r.readNotices(filepath.Join(folder, "agent-"+m.Parent+".jsonl"), cost)
			if !readable {
				return nil, unknown(session.AgentsUnreadable)
			}
			if found, yes := parent[c.id]; yes {
				copy := found
				v = &copy
			}
		}
		state := session.AgentRunning
		if v != nil {
			if v.Status == "completed" {
				state = session.AgentDone
			} else {
				state = session.AgentFailed
			}
			if now.Sub(c.at) > settleWindow {
				continue
			}
		}
		a := session.Agent{ID: c.id, What: m.What, Type: m.Type, Model: m.Model, Depth: m.Depth, State: state, At: c.at}
		if state != session.AgentRunning && v != nil {
			a.Result, a.Tokens, a.Tools, a.Seconds = v.Result, v.Tokens, v.Tools, v.Seconds
		}
		eligible = append(eligible, resolved{agent: a, path: c.path})
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		ri, rj := eligible[i].agent.State == session.AgentRunning, eligible[j].agent.State == session.AgentRunning
		if ri != rj {
			return ri
		}
		return eligible[i].agent.At.After(eligible[j].agent.At)
	})
	omitted := max(0, len(eligible)-MaximumShown)
	if len(eligible) > MaximumShown {
		eligible = eligible[:MaximumShown]
	}
	out := make([]session.Agent, 0, len(eligible))
	for _, item := range eligible {
		if item.agent.State == session.AgentRunning {
			item.agent.Doing, ok = r.readDoing(item.path, cost)
			if !ok {
				return nil, unknown(session.AgentsUnreadable)
			}
		}
		out = append(out, item.agent)
	}
	return out, session.AgentReading{State: session.AgentsComplete, Truncated: omitted}
}

func unknown(reason session.AgentUnknownReason) session.AgentReading {
	return session.AgentReading{State: session.AgentsUnknown, Reason: reason}
}

func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (r *Reader) readMeta(path string, cost *Measurement) (meta, bool) {
	r.mu.Lock()
	if hit, ok := r.metas[path]; ok {
		r.clock++
		hit.used = r.clock
		r.metas[path] = hit
		r.mu.Unlock()
		return hit.value, true
	}
	r.mu.Unlock()
	body, err := os.ReadFile(path)
	cost.Opened++
	cost.Bytes += int64(len(body))
	if err != nil {
		return meta{}, false
	}
	var row struct {
		Description   string `json:"description"`
		AgentType     string `json:"agentType"`
		Model         string `json:"model"`
		ParentAgentID string `json:"parentAgentId"`
		SpawnDepth    int    `json:"spawnDepth"`
	}
	if json.Unmarshal(body, &row) != nil {
		return meta{}, false
	}
	if row.AgentType == "" {
		row.AgentType = "agent"
	}
	if row.SpawnDepth < 1 {
		row.SpawnDepth = 1
	}
	value := meta{What: row.Description, Type: row.AgentType, Model: row.Model, Parent: row.ParentAgentID, Depth: row.SpawnDepth}
	r.mu.Lock()
	r.clock++
	r.metas[path] = metaCache{value: value, used: r.clock}
	r.trimLocked()
	r.mu.Unlock()
	return value, true
}

// readCodexMeta reads only a rollout's immutable session_meta head. It never
// reaches the conversation beneath it. Codex's source name says how the thread
// was spawned; agent_nickname is the separate, useful name that tells sibling
// threads apart.
func (r *Reader) readCodexMeta(id string, cost *Measurement) (meta, codexMeta, bool) {
	if !validID(id) {
		return meta{}, codexMeta{}, false
	}
	cacheKey := "codex:" + id
	r.mu.Lock()
	if hit, ok := r.metas[cacheKey]; ok {
		r.clock++
		hit.used = r.clock
		r.metas[cacheKey] = hit
		r.mu.Unlock()
		identity := codexMeta{conversation: hit.value.Conversation, thread: hit.value.Thread, source: hit.value.Source}
		return hit.value, identity, identity.conversation != "" && identity.thread != ""
	}
	r.mu.Unlock()

	path := transcript.CodexPath(r.Home, id)
	f, err := os.Open(path)
	if err != nil {
		return meta{}, codexMeta{}, false
	}
	line, readErr := bufio.NewReaderSize(io.LimitReader(f, 1<<20), 64<<10).ReadBytes('\n')
	_ = f.Close()
	cost.Opened++
	cost.Bytes += int64(len(line))
	if readErr != nil && len(line) == 0 {
		return meta{}, codexMeta{}, false
	}
	value, identity, ok := decodeCodexHead(line)
	if !ok {
		return meta{}, codexMeta{}, false
	}
	r.mu.Lock()
	r.clock++
	r.metas[cacheKey] = metaCache{value: value, used: r.clock}
	r.trimLocked()
	r.mu.Unlock()
	return value, identity, true
}

func (r *Reader) readDoing(path string, cost *Measurement) (string, bool) {
	cost.Stats++
	st, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	r.mu.Lock()
	if hit, ok := r.doings[path]; ok && hit.size == st.Size() && hit.mod.Equal(st.ModTime()) {
		r.clock++
		hit.used = r.clock
		r.doings[path] = hit
		r.mu.Unlock()
		return hit.value, true
	}
	r.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	start := st.Size() - latestToolWindow
	if start < 0 {
		start = 0
	}
	data := make([]byte, st.Size()-start)
	n, err := f.ReadAt(data, start)
	cost.Opened++
	cost.Bytes += int64(n)
	if err != nil && err != io.EOF {
		return "", false
	}
	data = data[:n]
	if start > 0 {
		if cut := bytes.IndexByte(data, '\n'); cut >= 0 {
			data = data[cut+1:]
		}
	}
	value := latestTool(data)
	r.mu.Lock()
	r.clock++
	r.doings[path] = doingCache{size: st.Size(), mod: st.ModTime(), value: value, used: r.clock}
	r.trimLocked()
	r.mu.Unlock()
	return value, true
}

func latestTool(data []byte) string {
	latest := ""
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"tool_use"`)) {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "assistant" {
			continue
		}
		for _, block := range row.Message.Content {
			if block.Type == "tool_use" && block.Name != "" {
				latest = block.Name
			}
		}
	}
	return latest
}

func (r *Reader) readNotices(path string, cost *Measurement) (map[string]verdict, bool) {
	r.mu.Lock()
	seen, had := r.notices[path]
	r.mu.Unlock()
	cost.Stats++
	st, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if !had || st.Size() < seen.offset {
		seen = noticeCache{found: map[string]verdict{}}
	}
	if seen.found == nil {
		seen.found = map[string]verdict{}
	}
	if st.Size() > seen.offset {
		f, err := os.Open(path)
		if err != nil {
			return nil, false
		}
		defer f.Close()
		if _, err := f.Seek(seen.offset, io.SeekStart); err != nil {
			return nil, false
		}
		reader := bufio.NewReaderSize(f, 256<<10)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 && line[len(line)-1] == '\n' {
				seen.offset += int64(len(line))
				cost.Bytes += int64(len(line))
				if bytes.Contains(line, []byte("task-notification")) {
					if id, v, ok := notice(line); ok {
						seen.found[id] = v
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					return nil, false
				}
				break
			}
		}
		cost.Opened++
	}
	r.mu.Lock()
	r.clock++
	seen.used = r.clock
	r.notices[path] = seen
	r.trimLocked()
	r.mu.Unlock()
	return cloneVerdicts(seen.found), true
}

func notice(line []byte) (string, verdict, bool) {
	var row struct {
		Content    string `json:"content"`
		Attachment struct {
			Prompt string `json:"prompt"`
		} `json:"attachment"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &row) != nil {
		return "", verdict{}, false
	}
	blob := row.Content
	if blob == "" {
		blob = row.Attachment.Prompt
	}
	id, status := tag("task-id", blob), tag("status", blob)
	if id == "" || status == "" {
		return "", verdict{}, false
	}
	return id, verdict{
		Status: status, Result: firstLine(tag("result", blob)), Tokens: integer(tag("subagent_tokens", blob)),
		Tools: integer(tag("tool_uses", blob)), Seconds: float(tag("duration_ms", blob)) / 1000,
	}, true
}

func tag(name, text string) string {
	start, end := "<"+name+">", "</"+name+">"
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	rest := text[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func firstLine(value string) string {
	if i := strings.IndexByte(value, '\n'); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func integer(value string) int64 {
	var out int64
	_, _ = fmtSscan(value, &out)
	return out
}

func float(value string) float64 {
	var out float64
	_, _ = fmtSscan(value, &out)
	return out
}

// Kept behind a variable so tests can exercise malformed values without
// importing a parser helper from another adapter.
var fmtSscan = func(value string, out any) (int, error) {
	dec := json.NewDecoder(strings.NewReader(value))
	return 1, dec.Decode(out)
}

func cloneVerdicts(in map[string]verdict) map[string]verdict {
	out := make(map[string]verdict, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (r *Reader) trimLocked() {
	for len(r.metas) > r.limit {
		key := oldestMeta(r.metas)
		delete(r.metas, key)
		r.evicted++
	}
	for len(r.doings) > r.limit {
		key := oldestDoing(r.doings)
		delete(r.doings, key)
		r.evicted++
	}
	for len(r.notices) > r.limit {
		key := oldestNotice(r.notices)
		delete(r.notices, key)
		r.evicted++
	}
}

func oldestMeta(rows map[string]metaCache) string {
	key, at := "", ^uint64(0)
	for k, v := range rows {
		if v.used < at {
			key, at = k, v.used
		}
	}
	return key
}
func oldestDoing(rows map[string]doingCache) string {
	key, at := "", ^uint64(0)
	for k, v := range rows {
		if v.used < at {
			key, at = k, v.used
		}
	}
	return key
}
func oldestNotice(rows map[string]noticeCache) string {
	key, at := "", ^uint64(0)
	for k, v := range rows {
		if v.used < at {
			key, at = k, v.used
		}
	}
	return key
}

// AgentPath resolves only a child of the named session. An id from the route
// is never joined before its lexical check, and a Codex rollout is accepted
// only when its head positively says both subagent and this conversation.
func (r *Reader) AgentPath(s session.Session, id string) (string, bool) {
	if !validID(id) || s.ConversationID == "" {
		return "", false
	}
	switch s.Assistant {
	case session.AssistantClaude:
		if s.CWD == "" {
			return "", false
		}
		root := strings.TrimSuffix(transcript.ClaudePath(r.Home, s.CWD, s.ConversationID), ".jsonl")
		folder := filepath.Join(root, "subagents")
		return contained(folder, filepath.Join(folder, "agent-"+id+".jsonl"))
	case session.AssistantCodex:
		path := transcript.CodexPath(r.Home, id)
		path, ok := contained(filepath.Join(r.Home, ".codex", "sessions"), path)
		if !ok {
			return "", false
		}
		meta, ok := codexHead(path)
		if !ok || meta.conversation != s.ConversationID || meta.thread != id || meta.source != "subagent" {
			return "", false
		}
		return path, true
	}
	return "", false
}

func contained(root, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}

type codexMeta struct{ conversation, thread, source string }

func codexHead(path string) (codexMeta, bool) {
	if path == "" {
		return codexMeta{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return codexMeta{}, false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, 1<<20), 64<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return codexMeta{}, false
	}
	_, identity, ok := decodeCodexHead(line)
	return identity, ok
}

func decodeCodexHead(line []byte) (meta, codexMeta, bool) {
	var row struct {
		Type    string `json:"type"`
		Payload struct {
			SessionID     string `json:"session_id"`
			ID            string `json:"id"`
			ThreadSource  string `json:"thread_source"`
			AgentNickname string `json:"agent_nickname"`
			Timestamp     string `json:"timestamp"`
			Source        struct {
				Subagent map[string]json.RawMessage `json:"subagent"`
			} `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &row) != nil || row.Type != "session_meta" {
		return meta{}, codexMeta{}, false
	}
	identity := codexMeta{row.Payload.SessionID, row.Payload.ID, row.Payload.ThreadSource}
	if identity.conversation == "" || identity.thread == "" {
		return meta{}, codexMeta{}, false
	}
	keys := make([]string, 0, len(row.Payload.Source.Subagent))
	for key := range row.Payload.Source.Subagent {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	value := meta{
		What: row.Payload.AgentNickname, Conversation: identity.conversation,
		Thread: identity.thread, Source: identity.source,
	}
	if len(keys) > 0 {
		value.Type = keys[0]
	}
	value.At, _ = time.Parse(time.RFC3339Nano, row.Payload.Timestamp)
	return value, identity, true
}
