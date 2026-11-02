// Package claudeusage asks the installed Claude Code for its plan windows.
//
// Claude Code hands 5h and 7d to a configured statusLine.command and nothing
// else, so a machine without one has no file to read. This package is the
// other way in: Claude Code's own `get_usage` control request, answered with
// its own sign-in. Clawdline never sees or refreshes a credential, and no
// prompt is sent, so no model request is made and no quota is spent.
package claudeusage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// RequestSecondsLimit is how long one ask may take, process start included.
	RequestSecondsLimit = 10
	// RefreshSecondsLimit is the floor between two asks on one machine.
	RefreshSecondsLimit = 15 * 60
	// ReplyBytesLimit is the most of Claude Code's output one ask reads; the
	// answer is one line of about 3 KiB.
	ReplyBytesLimit = 1 << 20
)

// Window is one plan window as Claude Code reported it.
type Window struct {
	Name        string // "5h" or "7d", the status line's names
	UsedPercent float64
	ResetsAt    *int64 // Unix seconds; nil when Claude Code did not say
}

// Outcome says why an ask has no windows; "" when it has some.
type Outcome string

const (
	NoExecutable Outcome = "no_executable" // no Claude Code is installed to ask
	NoPlan       Outcome = "no_plan"       // API key, Bedrock, Vertex or signed out
	NoWindow     Outcome = "no_window"     // a plan, and no live window in the answer
	Failed       Outcome = "failed"        // no answer: an older Claude Code, a crash, the deadline
)

// Args start Claude Code as a print-mode control endpoint that never receives
// a prompt: no user, project or local settings (so none of the person's hooks
// or plugins run), no MCP servers, and no transcript on disk. `--bare` is
// deliberately absent: it also skips the keychain that holds the sign-in.
var Args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json",
	"--verbose", "--setting-sources", "", "--no-session-persistence", "--strict-mcp-config"}

// request is the Agent SDK's experimental `get_usage` control request.
// `skip_behaviors` skips a scan of a week of local transcripts.
const request = `{"type":"control_request","request_id":"clawdline-usage","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// Ask runs the Claude Code at executable once, in an empty scratch directory,
// and stops it as soon as its answer is read.
func Ask(ctx context.Context, executable string) ([]Window, Outcome) {
	if executable == "" {
		return nil, NoExecutable
	}
	dir, err := os.MkdirTemp("", "clawdline-claude-usage-")
	if err != nil {
		return nil, Failed
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, executable, Args...)
	cmd.Dir = dir
	// Stderr stays nil (the null device): an io.Writer here makes Wait copy
	// until every inheritor closes it, so a child Claude Code leaves behind
	// would hold the ask open long after its answer arrived.
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, Failed
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, Failed
	}
	if cmd.Start() != nil {
		return nil, Failed
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := io.WriteString(stdin, request); err != nil {
		return nil, Failed
	}
	lines := bufio.NewScanner(io.LimitReader(stdout, ReplyBytesLimit))
	lines.Buffer(make([]byte, 0, 64<<10), ReplyBytesLimit)
	for lines.Scan() {
		if windows, outcome, ours := reply(lines.Bytes(), time.Now()); ours {
			return windows, outcome
		}
	}
	return nil, Failed
}

// reply reads one stream-json line; ours is false for any line that is not
// the answer to this ask.
func reply(line []byte, now time.Time) (windows []Window, outcome Outcome, ours bool) {
	var msg struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Available  *bool                      `json:"rate_limits_available"`
				RateLimits map[string]json.RawMessage `json:"rate_limits"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &msg) != nil || msg.Type != "control_response" ||
		msg.Response.RequestID != "clawdline-usage" {
		return nil, "", false
	}
	if msg.Response.Subtype != "success" || msg.Response.Response.Available == nil {
		// An older Claude Code without `get_usage` answers with an error.
		return nil, Failed, true
	}
	if !*msg.Response.Response.Available {
		return nil, NoPlan, true
	}
	for _, w := range []struct{ key, name string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		var v struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    *string  `json:"resets_at"`
		}
		raw, ok := msg.Response.Response.RateLimits[w.key]
		if !ok || json.Unmarshal(raw, &v) != nil || v.Utilization == nil || *v.Utilization < 0 || *v.Utilization > 100 {
			continue
		}
		window := Window{Name: w.name, UsedPercent: *v.Utilization}
		if v.ResetsAt != nil {
			if at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*v.ResetsAt)); err == nil {
				if !at.After(now) {
					continue // The window has reset since Claude Code measured it.
				}
				reset := at.Unix()
				window.ResetsAt = &reset
			}
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return nil, NoWindow, true
	}
	return windows, "", true
}
