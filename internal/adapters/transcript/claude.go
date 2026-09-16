// Package transcript reads what the assistants write about themselves. These
// are their own records, so a reading here is better evidence than anything
// drawn on a screen — and the sessions say so on the wire.
package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClaudeRegistry is one row of ~/.claude/sessions/<pid>.json: Claude Code's own
// live record of a running session. It carries the conversation id, the tmux
// pane and an official status, none of which has to be guessed from a screen.
type ClaudeRegistry struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Tmux      string `json:"tmux"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

// Pane returns the tmux pane id out of a value like "clawdline:@800.%801".
func (r ClaudeRegistry) Pane() string {
	if i := strings.LastIndex(r.Tmux, "."); i >= 0 {
		return r.Tmux[i+1:]
	}
	return ""
}

// ClaudeRegistryByPID reads every live session record Claude Code keeps.
func ClaudeRegistryByPID(home string) map[int]ClaudeRegistry {
	out := map[int]ClaudeRegistry{}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r ClaudeRegistry
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		out[pid] = r
	}
	return out
}

// ClaudeTitle reads the newest title a conversation gave itself.
//
// The transcript is append-only and a session retitles itself as the work
// changes, so the last record wins. The file is read to its end rather than
// tailed by bytes: a title is short and the cost is bounded by the transcript,
// which this only does for sessions that are actually running.
func ClaudeTitle(home, cwd, sessionID string) string {
	if cwd == "" || sessionID == "" {
		return ""
	}
	slug := strings.ReplaceAll(cwd, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	title := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap reject before parsing: most records are not titles.
		if !strings.Contains(string(line), `"ai-title"`) {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "ai-title" {
			continue
		}
		if rec.AITitle != "" {
			title = rec.AITitle
		}
	}
	return title
}
