package antigravitysession

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// On 2026-11-11 a Gemini turn ended with a deploy command still running as a
// native background task. Three minutes later the task finished and Gemini
// woke itself: it ran a check and wrote its final report. None of that turn
// crossed ACP, which only streams updates for a session/prompt; only the
// native log recorded it. The command stayed "running" and the report never
// appeared.
//
// A watch follows that log only while a finished turn left tools running. It
// costs one stat per interval while nothing changes, reads only the bytes
// appended since the last read, and ends once every background task finished
// and the turn Gemini started itself ended, or when the process closes.

// backgroundPollInterval is how often a watched native log is stat'ed.
var backgroundPollInterval = 2 * time.Second

type nativeRow struct {
	Index     *int64 `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
	Tools     []struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"tool_calls"`
}

type nativeWatch struct {
	path   string
	file   os.FileInfo
	offset int64
	// tasks maps a native background task ID to the tool row it updates.
	tasks map[string]string
	// awaiting lists, in order, the tools of Gemini's own turn whose result
	// row has not been read yet.
	awaiting []string
	step     int64
	stop     chan struct{}
}

var (
	taskStartedPattern  = regexp.MustCompile(`background task with task id: (\S+)`)
	taskFinishedPattern = regexp.MustCompile(`Task id "([^"]+)" finished with result:`)
	// A message from one of the conversation's own tasks, such as a schedule
	// timer firing. Gemini answers it with a turn of its own.
	taskSenderPattern = regexp.MustCompile(`sender=(\S+/task-\d+)`)
	// manage_task status: "Task: <id>" then "Status: RUNNING|DONE|...".
	taskStatusPattern = regexp.MustCompile(`Task: (\S+)\s+Status: ([A-Z_]+)`)
	exitCodePattern   = regexp.MustCompile(`Command exited with code (-?\d+)`)
)

func (h *Host) nativeLogPath(conversationID string) string {
	if !isNativeID(conversationID) {
		return ""
	}
	return filepath.Join(h.opts.AccountDir, "antigravity-acp", "brain", conversationID, ".system_generated", "logs", "transcript_full.jsonl")
}

func isNativeID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// splitRows parses the complete lines of data. It answers the rows and the
// bytes consumed; a trailing partial line is left for the next read. A
// malformed complete line ends the watch: the log is not guessed at.
func splitRows(data []byte) ([]nativeRow, int64, bool) {
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, 0, true
	}
	var rows []nativeRow
	for _, line := range bytes.Split(data[:end], []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r nativeRow
		if json.Unmarshal(line, &r) != nil || r.Index == nil || r.Source == "" || r.Type == "" {
			return nil, 0, false
		}
		rows = append(rows, r)
	}
	return rows, int64(end + 1), true
}

func taskDescription(content string) string {
	_, rest, ok := strings.Cut(content, "Task Description: ")
	if !ok {
		return ""
	}
	desc, _, _ := strings.Cut(rest, "\n")
	return strings.TrimSpace(desc)
}

func sameCommand(title, desc string) bool {
	if title == "" || desc == "" {
		return false
	}
	return title == desc || strings.HasPrefix(desc, title) || strings.HasPrefix(title, desc)
}

// scheduleTitle is how ACP titles Gemini's schedule tool; its native task
// description starts with timerDescription.
const (
	scheduleTitle    = "Run schedule"
	timerDescription = "Timer: "
)

// nativeTaskStatus maps a manage_task status to a tool status, or "" while
// the task still runs or the status is not one this reader knows.
func nativeTaskStatus(status string) string {
	switch status {
	case "DONE", "COMPLETED", "FINISHED", "SUCCEEDED":
		return "completed"
	case "ERROR", "FAILED", "KILLED", "CANCELLED", "CANCELED":
		return "failed"
	}
	return ""
}

func resultStatus(content string) string {
	if m := exitCodePattern.FindStringSubmatch(content); m != nil && m[1] != "0" {
		return "failed"
	}
	return "completed"
}

// runningTools answers the titles of the tools the turn that just ended left
// running. It reads c.activeTools, so a turn without one costs no query; each
// title is one indexed lookup.
func (c *conversation) runningTools() (map[string]string, error) {
	out := map[string]string{}
	for id := range c.activeTools {
		ordinal, e, err := c.readEntry(0, id)
		if err != nil {
			return nil, err
		}
		if ordinal != 0 && toolStillRunning(e) {
			out[id] = e.Activity.Title
		}
	}
	return out, nil
}

// stopWatch is called under c.mu.
func (c *conversation) stopWatch() {
	if c.watch != nil {
		close(c.watch.stop)
		c.watch = nil
	}
	c.nativeTurn = false
}

// watchBackground is called under c.mu after a turn ended normally. Only a
// tool that matches exactly one native background task is followed.
func (h *Host) watchBackground(c *conversation) {
	c.stopWatch()
	if len(c.activeTools) == 0 || h.assistant() != session.AssistantAntigravity || c.store == nil || c.client == nil || c.storeErr != nil {
		return
	}
	running, err := c.runningTools()
	if err != nil || len(running) == 0 {
		return
	}
	path := h.nativeLogPath(c.row.ConversationID)
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > transcript.ReadBudget {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, transcript.ReadBudget))
	if err != nil {
		return
	}
	rows, consumed, ok := splitRows(data)
	if !ok {
		return
	}
	started := map[string]string{} // task ID -> description, latest wins
	var last int64 = -1
	latestTimer := ""
	for _, r := range rows {
		last = *r.Index
		if r.Source == "MODEL" && r.Type == "GENERIC" && r.Status == "RUNNING" {
			if m := taskStartedPattern.FindStringSubmatch(r.Content); m != nil {
				desc := taskDescription(r.Content)
				started[m[1]] = desc
				if strings.HasPrefix(desc, timerDescription) {
					latestTimer = m[1]
				}
			}
		}
	}
	w := &nativeWatch{path: path, file: info, offset: consumed, tasks: map[string]string{}, step: last, stop: make(chan struct{})}
	for task, desc := range started {
		var match []string
		for id, title := range running {
			if sameCommand(title, desc) {
				match = append(match, id)
			}
		}
		if len(match) == 1 {
			w.tasks[task] = match[0]
			delete(running, match[0])
		}
	}
	// ACP titles every schedule call alike, so only the turn's one running
	// schedule is paired, with the latest timer the log started.
	var schedules []string
	for id, title := range running {
		if title == scheduleTitle {
			schedules = append(schedules, id)
		}
	}
	if len(schedules) == 1 && latestTimer != "" {
		if _, taken := w.tasks[latestTimer]; !taken {
			w.tasks[latestTimer] = schedules[0]
		}
	}
	if len(w.tasks) == 0 {
		return
	}
	c.watch = w
	client := c.client
	h.jobs.Add(1)
	go h.followBackground(c, w, client)
}

func (h *Host) followBackground(c *conversation, w *nativeWatch, client *antigravityprocess.Client) {
	defer h.jobs.Done()
	ticker := time.NewTicker(backgroundPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
		}
		if !h.readBackground(c, w, client) {
			c.mu.Lock()
			if c.watch == w {
				c.stopWatch()
				c.changed()
			}
			c.mu.Unlock()
			return
		}
	}
}

// readBackground answers false when the watch should end.
func (h *Host) readBackground(c *conversation, w *nativeWatch, client *antigravityprocess.Client) bool {
	info, err := os.Stat(w.path)
	if err != nil || !os.SameFile(info, w.file) || info.Size() < w.offset {
		return false
	}
	c.mu.Lock()
	alive := c.watch == w && c.client == client && c.client.Err() == nil
	c.mu.Unlock()
	if !alive {
		return false
	}
	if info.Size() == w.offset {
		return true
	}
	if info.Size()-w.offset > transcript.ReadBudget {
		return false
	}
	f, err := os.Open(w.path)
	if err != nil {
		return false
	}
	data := make([]byte, info.Size()-w.offset)
	n, err := f.ReadAt(data, w.offset)
	f.Close()
	if err != nil && err != io.EOF {
		return false
	}
	rows, consumed, ok := splitRows(data[:n])
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watch != w {
		return false
	}
	w.offset += consumed
	for _, r := range rows {
		if *r.Index <= w.step {
			return false
		}
		w.step = *r.Index
		if err := h.applyNative(c, w, r); err != nil {
			return false
		}
	}
	return len(w.tasks) > 0 || c.nativeTurn
}

func (c *conversation) nativeTool(id string, status string, title *string) error {
	d := &antigravityacp.ToolCallDelta{ToolCallID: id, Status: &status, Title: title}
	if title != nil {
		kind := "execute"
		d.Kind = &kind
	}
	return c.project(antigravityacp.SessionUpdate{Tool: d})
}

// applyNative is called under c.mu. While a person's prompt runs, ACP streams
// that turn, so only a background task's own result is taken from the log.
func (h *Host) applyNative(c *conversation, w *nativeWatch, r nativeRow) error {
	if r.Source == "SYSTEM" && r.Type == "SYSTEM_MESSAGE" {
		task, status := "", ""
		if m := taskFinishedPattern.FindStringSubmatch(r.Content); m != nil {
			task, status = m[1], resultStatus(r.Content)
		} else if m := taskSenderPattern.FindStringSubmatch(r.Content); m != nil {
			// A timer firing is that schedule call's end, and a wake-up.
			task, status = m[1], "completed"
		} else {
			return nil
		}
		if id, ok := w.tasks[task]; ok {
			delete(w.tasks, task)
			if err := c.nativeTool(id, status, nil); err != nil {
				return err
			}
		} else if !strings.HasPrefix(task, c.row.ConversationID+"/") {
			return nil
		}
		if !c.busy {
			// Gemini answers its own task's message with a turn of its own.
			c.nativeTurn = true
			c.touchActivity()
		}
		return nil
	}
	if c.busy || !c.nativeTurn || r.Source != "MODEL" {
		return nil
	}
	c.touchActivity()
	switch r.Type {
	case "PLANNER_RESPONSE":
		if strings.TrimSpace(r.Content) != "" {
			c.textOrdinal, c.textRole = 0, ""
			if err := c.project(antigravityacp.SessionUpdate{Kind: "agent_message_chunk", Text: r.Content}); err != nil {
				return err
			}
			c.textOrdinal, c.textRole = 0, ""
		}
		for i, tool := range r.Tools {
			var args struct {
				CommandLine string `json:"CommandLine"`
			}
			_ = json.Unmarshal(tool.Args, &args)
			title := args.CommandLine
			if title == "" {
				title = tool.Name
			}
			id := "native:" + c.row.ConversationID + ":" + strconv.FormatInt(*r.Index, 10) + ":" + strconv.Itoa(i)
			if err := c.nativeTool(id, "in_progress", &title); err != nil {
				return err
			}
			w.awaiting = append(w.awaiting, id)
		}
		if len(r.Tools) == 0 && r.Status == "DONE" && len(w.awaiting) == 0 {
			c.nativeTurn = false
			c.changed()
		}
	case "GENERIC":
		if len(w.awaiting) == 0 {
			return nil
		}
		id := w.awaiting[0]
		w.awaiting = w.awaiting[1:]
		if m := taskStatusPattern.FindStringSubmatch(r.Content); m != nil {
			if tool, ok := w.tasks[m[1]]; ok {
				if done := nativeTaskStatus(m[2]); done != "" {
					delete(w.tasks, m[1])
					if err := c.nativeTool(tool, done, nil); err != nil {
						return err
					}
				}
			}
		}
		if r.Status == "RUNNING" {
			if m := taskStartedPattern.FindStringSubmatch(r.Content); m != nil {
				w.tasks[m[1]] = id
				return nil
			}
		}
		status := resultStatus(r.Content)
		if r.Status != "DONE" && r.Status != "RUNNING" {
			status = "failed"
		}
		return c.nativeTool(id, status, nil)
	}
	return nil
}
